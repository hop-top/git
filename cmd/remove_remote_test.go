package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// newRemoveTestHub builds a hub with a default branch plus one removable
// feature branch, both materialized on the in-memory fs so
// removeBranchWorktree resolves a live base path.
func newRemoveTestHub(t *testing.T) (afero.Fs, *hop.Hub, string, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	hubPath := "/test/hub"

	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	mainPath := filepath.Join(hubPath, "hops", "main")
	require.NoError(t, hub.AddBranch("main", "main", mainPath))
	require.NoError(t, fs.MkdirAll(mainPath, 0o755))

	featurePath := filepath.Join(hubPath, "hops", "feature")
	require.NoError(t, hub.AddBranch("feature", "feature", featurePath))
	require.NoError(t, fs.MkdirAll(featurePath, 0o755))

	return fs, hub, hubPath, featurePath
}

// TestRemoveBranchWorktree_DoesNotProbeRemoteByDefault pins the hang
// reported against `git hop remove`: the removal pipeline fired
// `git ls-remote` (via HasRemoteBranch) on every removal, an unbounded
// network round-trip. With an unreachable origin the call never
// returns and remove hangs with no output.
//
// Remote deletion is opt-in, so a default removal must stay entirely
// offline: no HasRemoteBranch probe, no DeleteRemoteBranch.
func TestRemoveBranchWorktree_DoesNotProbeRemoteByDefault(t *testing.T) {
	fs, hub, hubPath, _ := newRemoveTestHub(t)

	mockGit := mocks.NewMockGit()
	// An unreachable origin: any remote probe blocks far longer than a
	// user would tolerate. If the removal path calls this, the test
	// fails on the recorded call rather than hanging the suite.
	mockGit.HasRemoteBranchFunc = func(dir, branch string) bool {
		time.Sleep(30 * time.Second)
		return true
	}

	done := make(chan error, 1)
	go func() {
		done <- removeBranchWorktree(fs, mockGit, hub, hubPath, "feature")
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("removeBranchWorktree hung: removal path made an unbounded remote call")
	}

	assert.Empty(t, mockGit.HasRemoteBranchCalls,
		"default remove must not probe origin; remote deletion is opt-in")
	assert.Empty(t, mockGit.DeletedRemoteBranches,
		"default remove must not delete the remote branch")

	// Local cleanup still happens.
	assert.Contains(t, mockGit.DeletedLocalBranches, "feature")
	assert.NotContains(t, hub.Config.Branches, "feature")
}

// TestRemoveBranchWorktree_DeletesRemoteWhenRequested verifies the
// opt-in path still deletes the remote branch.
func TestRemoveBranchWorktree_DeletesRemoteWhenRequested(t *testing.T) {
	fs, hub, hubPath, _ := newRemoveTestHub(t)

	mockGit := mocks.NewMockGit()
	mockGit.RemoteBranchExists = true

	err := removeBranchWorktreeWithRemote(fs, mockGit, hub, hubPath, "feature", true)
	require.NoError(t, err)

	assert.NotEmpty(t, mockGit.HasRemoteBranchCalls,
		"opt-in remote deletion must probe origin first")
	assert.Contains(t, mockGit.DeletedRemoteBranches, "feature")
}

// TestRemoveBranchWorktree_SkipsRemoteDeleteWhenAbsent verifies that
// with remote deletion requested but no matching remote branch, the
// push is not attempted.
func TestRemoveBranchWorktree_SkipsRemoteDeleteWhenAbsent(t *testing.T) {
	fs, hub, hubPath, _ := newRemoveTestHub(t)

	mockGit := mocks.NewMockGit()
	mockGit.RemoteBranchExists = false

	err := removeBranchWorktreeWithRemote(fs, mockGit, hub, hubPath, "feature", true)
	require.NoError(t, err)

	assert.NotEmpty(t, mockGit.HasRemoteBranchCalls)
	assert.Empty(t, mockGit.DeletedRemoteBranches)
}
