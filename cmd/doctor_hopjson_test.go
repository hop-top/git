package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// TestDoctorFixStateIssues_ClearsHopJSONRows is the regression for
// `doctor --fix` pruning only state.json. `git hop status` renders the
// hub table from hop.json, so a Missing row survived every --fix run.
//
// This asserts at doctor's own wiring level, not the shared helper's:
// reverting the call site alone must turn this red.
func TestDoctorFixStateIssues_ClearsHopJSONRows(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath,
		[]string{"main", "feat/gone"},
		[]string{"main"})

	st := stateWithHub(hubPath)

	fixed := fixStateIssues(fs, mocks.NewMockGit(), st, hubPath)

	assert.Equal(t, 1, fixed, "doctor must count the pruned hop.json row")
	assert.ElementsMatch(t, []string{"main"}, hubBranchKeys(t, fs, hubPath),
		"doctor --fix must drop the row 'git hop status' reports as Missing")
}

// TestDoctorFixStateIssues_BacksUpHopJSON pins that doctor's rewrite is
// undoable via `git hop repair --undo`, same guarantee prune gives.
func TestDoctorFixStateIssues_BacksUpHopJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath,
		[]string{"main", "feat/gone"},
		[]string{"main"})

	st := stateWithHub(hubPath)

	require.Equal(t, 1, fixStateIssues(fs, mocks.NewMockGit(), st, hubPath))

	backup := hop.NewRepairBackup(fs, hubPath)
	backups, err := backup.List()
	require.NoError(t, err)
	require.Len(t, backups, 1, "one snapshot per mutating doctor --fix")

	restored, err := afero.ReadFile(fs, filepath.Join(backup.Path(backups[0].ID), "hop.json"))
	require.NoError(t, err)
	assert.Contains(t, string(restored), "feat/gone",
		"snapshot must capture the pre-fix hop.json")
}

// TestDoctorFixStateIssues_NoOpWhenClean is the negative case: a healthy
// hub must produce no hop.json rewrite and no spurious backup.
func TestDoctorFixStateIssues_NoOpWhenClean(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main"}, []string{"main"})

	st := stateWithHub(hubPath)

	assert.Equal(t, 0, fixStateIssues(fs, mocks.NewMockGit(), st, hubPath))
	assert.ElementsMatch(t, []string{"main"}, hubBranchKeys(t, fs, hubPath))
	exists, _ := afero.DirExists(fs, filepath.Join(hubPath, ".hop", "backups"))
	assert.False(t, exists, "clean hub must not accumulate backups")
}

// TestDoctorFixStateIssues_ScopedToCurrentHub guards the blast radius:
// doctor runs from one hub, so it must not rewrite a sibling repo's
// hop.json just because that hub is registered in global state.
func TestDoctorFixStateIssues_ScopedToCurrentHub(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	otherHub := "/hubs/other"
	writePruneHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})
	writePruneHub(t, fs, otherHub, []string{"main", "feat/also-gone"}, []string{"main"})

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/other"] = &state.RepositoryState{
		URI:           "git@github.com:test/other.git",
		Org:           "test",
		Repo:          "other",
		DefaultBranch: "main",
		Worktrees:     map[string]*state.WorktreeState{},
		Hubs:          []*state.HubState{{Path: otherHub, Mode: "local"}},
	}

	assert.Equal(t, 1, fixStateIssues(fs, mocks.NewMockGit(), st, hubPath))
	assert.ElementsMatch(t, []string{"main", "feat/also-gone"},
		hubBranchKeys(t, fs, otherHub),
		"doctor --fix in one hub must leave other hubs' hop.json alone")
	exists, _ := afero.DirExists(fs, filepath.Join(otherHub, ".hop", "backups"))
	assert.False(t, exists, "no snapshot for an untouched hub")
}

// TestDoctorFixStateIssues_NoHubFallsBackToState covers `doctor --fix`
// run outside any hub: nothing to scope to, so no hop.json is rewritten.
func TestDoctorFixStateIssues_NoHubFallsBackToState(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})

	st := stateWithHub(hubPath)

	assert.Equal(t, 0, fixStateIssues(fs, mocks.NewMockGit(), st, ""))
	assert.ElementsMatch(t, []string{"main", "feat/gone"}, hubBranchKeys(t, fs, hubPath),
		"outside a hub, doctor must not rewrite hop.json")
}
