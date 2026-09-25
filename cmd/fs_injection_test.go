package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// Removal runs git from the default branch's worktree, found on the
// filesystem the hub lives on. The live-worktree probe used to stat the
// real disk, so with the hub on any other fs it fell back to the hub.
func TestRemoveBranchWorktree_BasePathProbedOnInjectedFs(t *testing.T) {
	fs, hub, hubPath, _ := newRemoveTestHub(t)
	mockGit := mocks.NewMockGit()

	require.NoError(t, removeBranchWorktree(fs, mockGit, hub, hubPath, "feature"))

	require.NotEmpty(t, mockGit.WorktreeListCalls)
	assert.Equal(t, filepath.Join(hubPath, "hops", "main"), mockGit.WorktreeListCalls[0])
}

// The current worktree is resolved on the filesystem the hub was found on.
func TestResolveCurrentWorktree_StatsInjectedFs(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewBasePathFs(afero.NewOsFs(), root)
	_, err := hop.CreateHub(fs, "/hub", "https://github.com/org/repo.git", "org", "repo", "main")
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll("/hub/hops/main", 0o755))
	require.NoError(t, os.Symlink(filepath.Join("hops", "main"), filepath.Join(root, "hub", "current")))

	got, ok := resolveCurrentWorktree(fs, "/hub")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join("/hub", "hops", "main"), got)
}
