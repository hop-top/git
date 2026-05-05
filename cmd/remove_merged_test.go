package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubMergedCommands wires the MockGit responses inspectBranchSafety
// expects for a given (dir, branch, defaultBranch). Setting merged=true
// makes the branch-vs-default rev-list count return "0".
func stubMergedCommands(m *mocks.MockGit, dir, branch, defaultBranch string, merged bool) {
	mergedKey := dir + ":git rev-list --count " + branch + " --not " + defaultBranch
	if merged {
		m.Runner.Responses[mergedKey] = "0"
	} else {
		m.Runner.Responses[mergedKey] = "3"
	}
	// Make the origin verify fail so Pushed=false. The gate doesn't need
	// it for a merged branch, but inspectBranchSafety still calls it.
	verifyOriginKey := dir + ":git rev-parse --verify refs/remotes/origin/" + branch
	m.Runner.Errors[verifyOriginKey] = errors.New("unknown ref")
}

// newHubWithBranches builds an in-memory hub at hubPath with the given
// branches registered. Each branch directory is created on the in-memory
// fs unless skipMkdir contains the branch name (used to simulate the
// missing-on-disk case).
func newHubWithBranches(t *testing.T, fs afero.Fs, hubPath string, branches []string, skipMkdir map[string]bool) *hop.Hub {
	t.Helper()
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	for _, b := range branches {
		rel := filepath.Join("hops", b)
		abs := filepath.Join(hubPath, rel)
		if !skipMkdir[b] {
			require.NoError(t, fs.MkdirAll(abs, 0755))
		}
		require.NoError(t, hub.AddBranch(b, b, rel))
	}
	return hub
}

// TestCollectMergedCandidates_SingleMerged: a single feature branch that
// is merged into main makes the candidate list.
func TestCollectMergedCandidates_SingleMerged(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/h"
	hub := newHubWithBranches(t, fs, hubPath, []string{"main", "feature"}, nil)

	mock := mocks.NewMockGit()
	mock.StatusOverride = &git.Status{Branch: "feature", Clean: true}
	stubMergedCommands(mock, filepath.Join(hubPath, "hops", "feature"), "feature", "main", true)

	// cwd is somewhere outside any worktree.
	toRemove, skipped := collectMergedCandidates(fs, mock, hub, hubPath, "/elsewhere")

	require.Len(t, toRemove, 1)
	assert.Equal(t, "feature", toRemove[0].Branch)
	assert.Empty(t, skipped)
}

// TestCollectMergedCandidates_MergedDirty: collection includes
// merged-but-dirty branches; the gate (per-removal) decides what happens.
func TestCollectMergedCandidates_MergedDirty(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/h"
	hub := newHubWithBranches(t, fs, hubPath, []string{"main", "feature"}, nil)

	mock := mocks.NewMockGit()
	mock.StatusOverride = &git.Status{Branch: "feature", Clean: false, Files: []string{"? x"}}
	stubMergedCommands(mock, filepath.Join(hubPath, "hops", "feature"), "feature", "main", true)

	toRemove, _ := collectMergedCandidates(fs, mock, hub, hubPath, "/elsewhere")
	require.Len(t, toRemove, 1, "merged+dirty must still be collected; gate decides per-removal")
}

// TestCollectMergedCandidates_NotMerged: branches ahead of default are
// excluded from the candidate list outright.
func TestCollectMergedCandidates_NotMerged(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/h"
	hub := newHubWithBranches(t, fs, hubPath, []string{"main", "feature"}, nil)

	mock := mocks.NewMockGit()
	mock.StatusOverride = &git.Status{Branch: "feature", Clean: true}
	stubMergedCommands(mock, filepath.Join(hubPath, "hops", "feature"), "feature", "main", false)

	toRemove, skipped := collectMergedCandidates(fs, mock, hub, hubPath, "/elsewhere")
	assert.Empty(t, toRemove, "non-merged branches must not be collected")
	assert.Empty(t, skipped, "non-merged branches are silently excluded, not skipped-with-reason")
}

// TestCollectMergedCandidates_EmptyHub: a hub holding only the default
// branch produces an empty toRemove list and no skip rows. The cobra
// Run func uses this state to print "No merged worktrees to remove."
// and exit 0.
func TestCollectMergedCandidates_EmptyHub(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/h"
	hub := newHubWithBranches(t, fs, hubPath, []string{"main"}, nil)

	mock := mocks.NewMockGit()
	toRemove, skipped := collectMergedCandidates(fs, mock, hub, hubPath, "/elsewhere")
	assert.Empty(t, toRemove)
	assert.Empty(t, skipped)
}

// TestCollectMergedCandidates_DefaultBranchExcluded: the default branch
// is never collected even when its row appears merged (it always is).
func TestCollectMergedCandidates_DefaultBranchExcluded(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/h"
	hub := newHubWithBranches(t, fs, hubPath, []string{"main"}, nil)

	mock := mocks.NewMockGit()
	mock.StatusOverride = &git.Status{Branch: "main", Clean: true}
	stubMergedCommands(mock, filepath.Join(hubPath, "hops", "main"), "main", "main", true)

	toRemove, skipped := collectMergedCandidates(fs, mock, hub, hubPath, "/elsewhere")
	assert.Empty(t, toRemove, "default branch must never be a candidate")
	assert.Empty(t, skipped, "default branch is silently filtered, not skipped-with-reason")
}

// TestCollectMergedCandidates_CwdInsideExcluded: when the user's cwd is
// inside a worktree, that worktree is reported as skipped with a clear
// reason and never included in toRemove.
func TestCollectMergedCandidates_CwdInsideExcluded(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/h"
	hub := newHubWithBranches(t, fs, hubPath, []string{"main", "feature"}, nil)

	mock := mocks.NewMockGit()
	mock.StatusOverride = &git.Status{Branch: "feature", Clean: true}
	stubMergedCommands(mock, filepath.Join(hubPath, "hops", "feature"), "feature", "main", true)

	cwd := filepath.Join(hubPath, "hops", "feature", "src")
	toRemove, skipped := collectMergedCandidates(fs, mock, hub, hubPath, cwd)

	assert.Empty(t, toRemove)
	require.Len(t, skipped, 1)
	assert.Equal(t, "feature", skipped[0].Branch)
	assert.Contains(t, skipped[0].Reason, "currently inside")
}

// TestCollectMergedCandidates_MissingPathSkipped: a branch whose
// worktree dir was deleted is reported as skipped with the prune
// suggestion, not crashed on.
func TestCollectMergedCandidates_MissingPathSkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/h"
	hub := newHubWithBranches(t, fs, hubPath, []string{"main", "feature"}, map[string]bool{"feature": true})

	mock := mocks.NewMockGit()
	mock.StatusOverride = &git.Status{Branch: "feature", Clean: true}
	// stub responses are irrelevant; collection should bail before probing.

	toRemove, skipped := collectMergedCandidates(fs, mock, hub, hubPath, "/elsewhere")

	assert.Empty(t, toRemove)
	require.Len(t, skipped, 1)
	assert.Equal(t, "feature", skipped[0].Branch)
	assert.Contains(t, skipped[0].Reason, "git hop prune")
}

// TestRemoveMergedFlag_Wired ensures the --merged flag exists, is a
// bool, and defaults to false.
func TestRemoveMergedFlag_Wired(t *testing.T) {
	flag := removeCmd.Flags().Lookup("merged")
	require.NotNil(t, flag, "remove command must expose a --merged flag")
	assert.Equal(t, "bool", flag.Value.Type())
	assert.Equal(t, "false", flag.DefValue)
}

// gitRun runs git in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestCollectMergedCandidates_RealGit_MergedFeature exercises the
// candidate-collection logic against a real git repo. After merging
// `feature` into `main`, the feature worktree must show up in the
// to-remove list and main must NOT.
func TestCollectMergedCandidates_RealGit_MergedFeature(t *testing.T) {
	hubPath := t.TempDir()

	// Initialize a regular (non-bare) repo on main and seed it.
	gitRun(t, hubPath, "init", "-b", "main")
	gitRun(t, hubPath, "config", "user.email", "test@example.com")
	gitRun(t, hubPath, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(hubPath, "README.md"), []byte("v1"), 0644))
	gitRun(t, hubPath, "add", ".")
	gitRun(t, hubPath, "commit", "-m", "initial")

	// Create a feature branch with one commit, then merge it back into main.
	gitRun(t, hubPath, "branch", "feature")
	gitRun(t, hubPath, "checkout", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(hubPath, "feat.txt"), []byte("f"), 0644))
	gitRun(t, hubPath, "add", ".")
	gitRun(t, hubPath, "commit", "-m", "feature work")
	gitRun(t, hubPath, "checkout", "main")
	gitRun(t, hubPath, "merge", "--no-ff", "feature", "-m", "merge feature")

	// Materialize a worktree for the feature branch under hops/feature so
	// inspectBranchSafety has a real working dir to probe.
	featurePath := filepath.Join(hubPath, "hops", "feature")
	gitRun(t, hubPath, "worktree", "add", featurePath, "feature")

	// Build the hub config: main's path is hubPath itself, feature's path
	// is hops/feature (relative).
	fs := afero.NewOsFs()
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		// LoadHub will fail because there's no hop.json yet; create it.
		hub, err = hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
		require.NoError(t, err)
	}
	require.NoError(t, hub.AddBranch("main", "main", "."))
	require.NoError(t, hub.AddBranch("feature", "feature", filepath.Join("hops", "feature")))

	g := git.New()
	toRemove, skipped := collectMergedCandidates(fs, g, hub, hubPath, "/tmp/elsewhere-not-a-worktree")

	require.Len(t, toRemove, 1, "feature must be the only candidate after merging into main")
	assert.Equal(t, "feature", toRemove[0].Branch)
	assert.Empty(t, skipped, "no rows should be skipped-with-reason in the happy path")

	// Sanity: main must never appear in either list.
	for _, c := range toRemove {
		assert.NotEqual(t, "main", c.Branch, "default branch must never be a candidate")
	}
	for _, c := range skipped {
		assert.NotEqual(t, "main", c.Branch, "default branch must never be reported as skipped")
	}
}
