package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hooks"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// Two hubs of one repository, one with its own hop.dataLayout. hubA's
// ({host}/{org}/{repo}) puts the hopspace hooks dir where releases before
// hop.dataLayout mirrored hooks; hubB's (the default, {org}/{repo})
// elsewhere. State lists hubB first, the hub the move used to follow.
type layoutSplitEnv struct {
	legacyHooksEnv
	hubA, hubB string
}

func newLayoutSplitEnv(t *testing.T) layoutSplitEnv {
	t.Helper()
	e := layoutSplitEnv{
		legacyHooksEnv: newLegacyHooksEnv(t),
		hubA:           gitRepoDir(t, map[string]string{"hop.dataLayout": "{host}/{org}/{repo}"}),
		hubB:           gitRepoDir(t, nil),
	}
	require.NoError(t, e.fs.MkdirAll(e.hubA, 0o755))
	require.NoError(t, e.fs.MkdirAll(e.hubB, 0o755))
	st := state.NewState()
	st.AddRepository("github.com/acme/widgets", &state.RepositoryState{
		URI: "git@github.com:acme/widgets.git", Org: "acme", Repo: "widgets",
		Hubs: []*state.HubState{{Path: e.hubB, Mode: state.HubModeLocal}, {Path: e.hubA, Mode: state.HubModeLocal}},
	})
	require.NoError(t, state.SaveState(e.fs, st))
	return e
}

// movedFrom returns the dirs doctor reports it moved away.
func movedFrom(r doctorReport) []string {
	var dirs []string
	for _, rec := range r.records {
		if rec.Kind == doctorKindFixed && rec.Check == doctorCheckHopspace {
			dirs = append(dirs, rec.Subject)
		}
	}
	return dirs
}

// doctor --fix does not move the old hooks dir while the hubs resolve
// different locations for it: it warns, names each hub's value and how
// to align them. A mirror from the hub with its own value then warns
// too, and writes where that hub's lookup reads; no hook lands where
// doctor moved hooks away from.
func TestDoctorFix_LegacyHooksNotMovedWhileHubsDisagree(t *testing.T) {
	e := newLayoutSplitEnv(t)

	var r doctorReport
	_, stderr := captureRemoveOutput(t, func() {
		r = runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true})
	})

	ok, _ := afero.Exists(e.fs, filepath.Join(e.legacy, "post-worktree-add"))
	assert.True(t, ok, "the old hooks must stay while the hubs disagree")
	created, _ := afero.Exists(e.fs, e.newDir)
	assert.False(t, created, "nothing may move to one hub's location while another resolves the old one")
	recs := legacyHooksRecords(r, e.legacy)
	if assert.Len(t, recs, 1, "want one warning and no repair; got %+v", r.records) {
		assert.Equal(t, doctorKindWarning, recs[0].Kind)
		assert.Contains(t, recs[0].Message, "resolve hop.dataLayout to different hopspaces")
		assert.Contains(t, recs[0].Message, e.hubA)
		assert.Contains(t, recs[0].Message, e.hubB)
		assert.Contains(t, recs[0].Message, "nothing moved")
		assert.NotContains(t, recs[0].Message, "doctor --fix", "--fix cannot align the hubs; it must not be suggested")
	}
	assert.NoError(t, doctorResult(r))
	for _, want := range []string{
		"hint:   " + e.hubA + ": {host}/{org}/{repo} (local config)",
		"hint:   " + e.hubB + ": {org}/{repo} (default)",
		"hint:   git config --global hop.dataLayout <layout>",
		"hint:   git -C " + e.hubA + " config --unset hop.dataLayout",
	} {
		assert.Contains(t, stderr, want)
	}

	writeTestFile(t, e.fs, filepath.Join(e.hubA, ".git-hop", "hooks", "post-worktree-remove"), "#!/bin/sh\necho mirrored\n")
	var res hooks.Result
	_, stderr = captureRemoveOutput(t, func() {
		var err error
		res, err = hooks.MirrorCommittedHooks(e.fs, hooks.MirrorOpts{
			WorktreePath: e.hubA,
			RepoID:       "github.com/acme/widgets",
			RepoURI:      "git@github.com:acme/widgets.git",
			Mode:         hooks.ModeCopy,
		})
		require.NoError(t, err)
	})

	assert.Equal(t, 1, res.Installed, "%+v", res.Hooks)
	assert.Contains(t, stderr, "warning: hubs of acme/widgets resolve hop.dataLayout to different hopspaces")
	assert.Contains(t, stderr, "hint:   git -C "+e.hubA+" config --unset hop.dataLayout")
	ok, _ = afero.Exists(e.fs, filepath.Join(e.legacy, "post-worktree-remove"))
	assert.True(t, ok, "the hook goes where hubA's hop.dataLayout puts it")
	for _, dir := range movedFrom(r) {
		entries, _ := afero.ReadDir(e.fs, dir)
		assert.Empty(t, entries, "hooks landed at %s, which doctor moved away", dir)
	}
}

// Hubs that agree get the move, as before.
func TestDoctorFix_LegacyHooksMoveWhenHubsAgree(t *testing.T) {
	e := newLayoutSplitEnv(t)
	st, err := state.LoadState(e.fs)
	require.NoError(t, err)
	st.Repositories["github.com/acme/widgets"].Hubs = []*state.HubState{{Path: e.hubB, Mode: state.HubModeLocal}}
	require.NoError(t, state.SaveState(e.fs, st))

	r := runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true})

	ok, _ := afero.Exists(e.fs, filepath.Join(e.newDir, "post-worktree-add"))
	assert.True(t, ok, "the old hooks move where every hub resolves them")
	assert.Equal(t, []string{e.legacy}, movedFrom(r))
}
