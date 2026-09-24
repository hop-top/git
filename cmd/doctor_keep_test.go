package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// A missing worktree doctor keeps (git has it locked, or the user
// answered "Keep as-is") must survive every pass of the run. The
// prompt-driven and locked end-to-end runs live in
// test/script/testdata/doctor_keep_as_is.txtar and
// doctor_locked_missing.txtar; these cover the passes on their own.

// TestDoctorFix_KeepsLockedWorktreeHopJSONRow: the hop.json row of a
// locked worktree whose directory is gone survives --fix even when state
// never recorded the worktree, so no prompt ever ran for it.
func TestDoctorFix_KeepsLockedWorktreeHopJSONRow(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "usb", "gone"}, []string{"main"})

	g := mocks.NewMockGit()
	g.WorktreeListOut = porcelainEntry(filepath.Join(hubPath, config.MakeWorktreePath("usb")), "usb", "locked on the usb drive") +
		porcelainEntry(filepath.Join(hubPath, config.MakeWorktreePath("gone")), "gone", "prunable")

	var r doctorReport
	fixed := fixStateIssues(fs, g, stateWithHub(hubPath), hubPath, nil, doctorOpts{fix: true}, &r)

	assert.Equal(t, 1, fixed, "only the unlocked row is pruned: %+v", r.records)
	assert.ElementsMatch(t, []string{"main", "usb"}, hubBranchKeys(t, fs, hubPath))
}

// TestDoctorFix_KeepsLockedWorktreeStateEntry: a locked worktree's state
// entry is kept without prompting (a non-interactive stdin cannot answer
// here, which would also keep it, so the proof is that no prompt runs
// and the merge test is never asked), and its hop.json row with it.
func TestDoctorFix_KeepsLockedWorktreeStateEntry(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "usb"}, []string{"main"})
	usb := filepath.Join(hubPath, config.MakeWorktreePath("usb"))

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"usb": {Path: usb, Type: "linked", HubPath: hubPath},
	}

	g := mocks.NewMockGit()
	g.WorktreeListOut = porcelainEntry(usb, "usb", "locked")
	// Were the lock ignored, usb would count as merged and be removed.
	g.Runner.Responses = map[string]string{hubPath + ":git branch --merged main": "  usb\n* main\n"}

	for _, opts := range []doctorOpts{{fix: true, dryRun: true}, {fix: true}} {
		var r doctorReport
		fixed := fixStateIssues(fs, g, st, hubPath, nil, opts, &r)
		assert.Zero(t, fixed, "dry-run=%v: nothing to fix: %+v", opts.dryRun, r.records)
		assert.Contains(t, st.Repositories["github.com/test/repo"].Worktrees, "usb")
		assert.ElementsMatch(t, []string{"main", "usb"}, hubBranchKeys(t, fs, hubPath))
	}
}

// TestFixMissingWorktrees_ReportsKept: every entry left in place is
// reported kept, and nothing else is.
func TestFixMissingWorktrees_ReportsKept(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	require.NoError(t, fs.MkdirAll(hubPath, 0o755))
	path := func(b string) string { return filepath.Join(hubPath, config.MakeWorktreePath(b)) }

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"merged": {Path: path("merged"), HubPath: hubPath},
		"open":   {Path: path("open"), HubPath: hubPath},
		"usb":    {Path: path("usb"), HubPath: hubPath},
	}
	g := mocks.NewMockGit()
	g.WorktreeListOut = porcelainEntry(path("usb"), "usb", "locked")
	g.Runner.Responses = map[string]string{hubPath + ":git branch --merged main": "  merged\n* main\n"}

	var r doctorReport
	resolved, kept := fixMissingWorktrees(fs, g, st, nil, doctorOpts{fix: true, dryRun: true}, &r)

	assert.Equal(t, 1, resolved, "the merged entry would be removed")
	assert.True(t, kept.has(path("usb")), "a locked worktree is kept")
	assert.True(t, kept.has(path("open")), "an entry the preview would ask about is kept")
	assert.False(t, kept.has(path("merged")), "a removed entry is not kept")
}

func TestLockedWorktree(t *testing.T) {
	p := "/hubs/repo/hops/usb"
	locked := func(porcelain string) bool {
		_, ok := worktreeLock(porcelain, p)
		return ok
	}
	assert.True(t, locked(porcelainEntry(p, "usb", "locked")))
	assert.True(t, locked(porcelainEntry(p, "usb", "locked on the usb drive")))
	assert.False(t, locked(porcelainEntry(p, "usb", "prunable")), "prunable, not locked")
	assert.False(t, locked(porcelainEntry("/hubs/repo/hops/other", "other", "locked")), "another path's lock")
}
