package cmd

import (
	"errors"
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
		mainPath: {Path: mainPath, Branch: "main", Type: "linked", HubPath: hubPath},
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
		assert.Contains(t, stateBranches(loaded.Repositories["github.com/test/repo"]), "main", "dry-run=%v", opts.dryRun)
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

// TestMergedIntoDefault_RewrittenMerges: a branch whose work landed on
// the default branch under rewritten commits counts as merged, though
// `git branch --merged` does not list it. The verdict is the one remove
// and status reach (branchWorkLandedIn): a squash-merge (the in-memory
// merge is the default branch's tree) or a rebase-merge (git cherry marks
// every commit '-'). Work that never shipped, in whole or in part, and a
// branch git cannot resolve stay unmerged.
func TestMergedIntoDefault_RewrittenMerges(t *testing.T) {
	g := mocks.NewMockGit()
	g.Runner.Responses = map[string]string{
		"/hub:git branch --merged main":  "* main\n",
		"/hub:git rev-parse main^{tree}": "tree-main\n",

		"/hub:git merge-tree --write-tree main squashed": "tree-main\n",
		"/hub:git cherry main squashed":                  "+ s1\n",

		"/hub:git merge-tree --write-tree main rebased": "tree-other\n",
		"/hub:git cherry main rebased":                  "- r1\n- r2\n",

		"/hub:git merge-tree --write-tree main unmerged": "tree-other\n",
		"/hub:git cherry main unmerged":                  "+ u1\n",

		"/hub:git merge-tree --write-tree main partial": "tree-other\n",
		"/hub:git cherry main partial":                  "- p1\n+ p2\n",
	}
	g.Runner.Errors = map[string]error{
		"/hub:git merge-tree --write-tree main gone": errors.New("not a valid object name gone"),
		"/hub:git cherry main gone":                  errors.New("unknown commit gone"),
	}

	assert.True(t, mergedIntoDefault(g, "/hub", "squashed", "main"), "squash-merged")
	assert.True(t, mergedIntoDefault(g, "/hub", "rebased", "main"), "rebase-merged")
	assert.False(t, mergedIntoDefault(g, "/hub", "unmerged", "main"), "never shipped")
	assert.False(t, mergedIntoDefault(g, "/hub", "partial", "main"), "partly shipped")
	assert.False(t, mergedIntoDefault(g, "/hub", "gone", "main"), "a branch ref that no longer exists")
}
