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

// TestDoctorFix_DefaultBranchNeverMerged: a missing default-branch
// worktree the hub check could not recreate is not auto-removed by the
// state check as "merged into itself", and its hop.json row stays. `git
// branch --merged main` does list main.
func TestDoctorFix_DefaultBranchNeverMerged(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main"}, nil)
	mainPath := filepath.Join(hubPath, config.MakeWorktreePath("main"))

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"main": {Path: mainPath, Type: "linked", HubPath: hubPath},
	}
	require.NoError(t, state.SaveState(fs, st))

	g := mocks.NewMockGit()
	g.Runner.Responses = map[string]string{hubPath + ":git branch --merged main": "* main\n"}

	for _, opts := range []doctorOpts{{fix: true, dryRun: true}, {fix: true}} {
		r := runDoctor(fs, g, hubPath, opts)

		for _, rec := range r.records {
			assert.NotContains(t, rec.Message, "merged", "dry-run=%v: %+v", opts.dryRun, rec)
		}
		loaded, err := state.LoadState(fs)
		require.NoError(t, err)
		assert.Contains(t, loaded.Repositories["github.com/test/repo"].Worktrees, "main", "dry-run=%v", opts.dryRun)
		assert.Contains(t, hubBranchKeys(t, fs, hubPath), "main", "dry-run=%v", opts.dryRun)
	}
}

func TestMergedIntoDefault(t *testing.T) {
	g := mocks.NewMockGit()
	g.Runner.Responses = map[string]string{
		"/hub:git branch --merged main": "  feat\n* main\n",
		"/hub:git branch --merged HEAD": "  feat\n* main\n",
	}

	assert.True(t, mergedIntoDefault(g, "/hub", "feat", "main"))
	assert.False(t, mergedIntoDefault(g, "/hub", "main", "main"), "the default branch is not merged into itself")
	assert.False(t, mergedIntoDefault(g, "/hub", "feat", ""), "an unknown default branch answers false")
	assert.False(t, mergedIntoDefault(g, "/hub", "other", "main"), "a branch git does not list")
}
