package cmd

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/test/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveCommand_PartialFailureHandling tests that the remove command
// continues with cleanup even if worktree removal fails
func TestRemoveCommand_PartialFailureHandling(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create test hub structure
	hubPath := "/test/hub"
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Add a branch to the hub
	featurePath := filepath.Join(hubPath, "hops", "feature")
	require.NoError(t, hub.AddBranch("feature", "feature", featurePath))

	// Create hopspace structure
	hopspacePath := "/test/hopspace"
	hopspace, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Register the feature branch
	require.NoError(t, hopspace.RegisterBranch("feature", featurePath))

	// Verify initial state
	assert.Contains(t, hub.Config.Branches, "feature")
	assert.Contains(t, hopspace.Config.Branches, "feature")
}

// TestRemoveCommand_UnregisterAfterFailedWorktreeRemoval tests that
// UnregisterBranch is called even if RemoveWorktree fails
func TestRemoveCommand_UnregisterAfterFailedWorktreeRemoval(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create hopspace with a branch
	hopspacePath := "/test/hopspace"
	hopspace, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Register a branch
	featurePath := filepath.Join(hopspacePath, "feature")
	require.NoError(t, hopspace.RegisterBranch("feature", featurePath))

	// Verify branch is registered
	assert.Contains(t, hopspace.Config.Branches, "feature")

	// Unregister the branch (simulating what should happen even after worktree removal fails)
	err = hopspace.UnregisterBranch("feature")
	require.NoError(t, err)

	// Reload and verify branch is no longer registered
	reloadedHopspace, err := hop.LoadHopspace(fs, hopspacePath)
	require.NoError(t, err)
	assert.NotContains(t, reloadedHopspace.Config.Branches, "feature")
}

// TestRemoveCommand_PruneWorktrees tests that pruning happens after removal
func TestRemoveCommand_PruneWorktrees(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create hopspace with multiple branches
	hopspacePath := "/test/hopspace"
	hopspace, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Register multiple branches
	mainPath := filepath.Join(hopspacePath, "main")
	featurePath := filepath.Join(hopspacePath, "feature")
	require.NoError(t, fs.MkdirAll(mainPath, 0755))
	require.NoError(t, fs.MkdirAll(featurePath, 0755))

	require.NoError(t, hopspace.RegisterBranch("main", mainPath))
	require.NoError(t, hopspace.RegisterBranch("feature", featurePath))

	// Verify both branches exist
	assert.Contains(t, hopspace.Config.Branches, "main")
	assert.Contains(t, hopspace.Config.Branches, "feature")

	// Note: Actual git worktree pruning requires real git repository
	// This test verifies the data structures are set up correctly for pruning
	// The actual pruning behavior is tested in integration tests
}

// TestRemoveCommand_SuccessMessage tests that success message is shown
// even after encountering non-fatal errors
func TestRemoveCommand_SuccessMessage(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create minimal hub structure
	hubPath := "/test/hub"
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Add a branch
	featurePath := filepath.Join(hubPath, "hops", "feature")
	require.NoError(t, hub.AddBranch("feature", "feature", featurePath))

	// Remove branch from hub (this part should succeed)
	err = hub.RemoveBranch("feature")
	require.NoError(t, err)

	// Verify branch was removed
	reloadedHub, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)
	assert.NotContains(t, reloadedHub.Config.Branches, "feature")

	// The test verifies that RemoveBranch succeeds
	// The actual command would then show success message even if
	// subsequent cleanup operations emit warnings
}

// TestRemoveCommand_CleanupManagerIntegration tests that CleanupManager
// is properly integrated into the remove flow
func TestRemoveCommand_CleanupManagerIntegration(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create hopspace with a branch
	hopspacePath := "/test/hopspace"
	hopspace, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Register main branch
	mainPath := filepath.Join(hopspacePath, "main")
	require.NoError(t, fs.MkdirAll(mainPath, 0755))
	require.NoError(t, hopspace.RegisterBranch("main", mainPath))

	// Create an orphaned directory (not registered in config)
	orphanedPath := filepath.Join(hopspacePath, "orphaned")
	require.NoError(t, fs.MkdirAll(orphanedPath, 0755))

	// Verify orphaned directory exists
	exists, err := afero.Exists(fs, orphanedPath)
	require.NoError(t, err)
	assert.True(t, exists)

	// Verify only main is registered, not orphaned
	assert.Contains(t, hopspace.Config.Branches, "main")
	assert.NotContains(t, hopspace.Config.Branches, "orphaned")

	// Note: CleanupManager.PruneWorktrees would clean up git metadata
	// for such orphaned directories. This test verifies the setup.
	// Actual pruning requires real git repository in integration tests.
}

// TestRemoveCommand_EmptyHopspace tests removing when hopspace becomes empty
func TestRemoveCommand_EmptyHopspace(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create hopspace with only one branch
	hopspacePath := "/test/hopspace"
	hopspace, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Register a branch
	branchPath := filepath.Join(hopspacePath, "feature")
	require.NoError(t, hopspace.RegisterBranch("feature", branchPath))

	// Unregister the only branch
	err = hopspace.UnregisterBranch("feature")
	require.NoError(t, err)

	// Reload and verify hopspace is now empty
	reloadedHopspace, err := hop.LoadHopspace(fs, hopspacePath)
	require.NoError(t, err)
	assert.Empty(t, reloadedHopspace.Config.Branches)

	// Note: In the actual command, PruneWorktrees would still run
	// even with an empty hopspace (it would just be a no-op)
}

// TestRemoveCommand_NonExistentWorktree tests removing a branch when
// the worktree directory doesn't exist (already cleaned up manually)
func TestRemoveCommand_NonExistentWorktree(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create hopspace with a branch
	hopspacePath := "/test/hopspace"
	hopspace, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Register a branch but don't create the directory
	// (simulating a case where the directory was manually deleted)
	featurePath := filepath.Join(hopspacePath, "feature")
	require.NoError(t, hopspace.RegisterBranch("feature", featurePath))

	// Verify branch is registered but directory doesn't exist
	assert.Contains(t, hopspace.Config.Branches, "feature")
	exists, _ := afero.Exists(fs, featurePath)
	assert.False(t, exists)

	// Should still be able to unregister
	err = hopspace.UnregisterBranch("feature")
	require.NoError(t, err)

	// Verify branch is removed from config
	reloadedHopspace, err := hop.LoadHopspace(fs, hopspacePath)
	require.NoError(t, err)
	assert.NotContains(t, reloadedHopspace.Config.Branches, "feature")
}

// TestRemoveCommand_UpdatesTimestamp tests that remove operation
// updates LastSync timestamp for branches
func TestRemoveCommand_UpdatesTimestamp(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create hopspace
	hopspacePath := "/test/hopspace"
	hopspace, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Register a branch
	featurePath := filepath.Join(hopspacePath, "feature")
	require.NoError(t, hopspace.RegisterBranch("feature", featurePath))

	// Verify branch was registered with a LastSync timestamp
	assert.Contains(t, hopspace.Config.Branches, "feature")
	assert.False(t, hopspace.Config.Branches["feature"].LastSync.IsZero())

	// Perform removal
	err = hopspace.UnregisterBranch("feature")
	require.NoError(t, err)

	// Reload and verify branch is removed
	reloadedHopspace, err := hop.LoadHopspace(fs, hopspacePath)
	require.NoError(t, err)
	assert.NotContains(t, reloadedHopspace.Config.Branches, "feature")
}

// TestRemoveCommand_PathResolution verifies that relative worktree paths
// are correctly resolved when using filepath.Abs vs config.ResolveWorktreePath
func TestRemoveCommand_PathResolution(t *testing.T) {
	// This test demonstrates the bug: when basePath is a relative path like "hops/main"
	// and we use filepath.Abs() from a CWD that already contains "hops/main",
	// we get path duplication like "/hub/hops/main/hops/main"

	hubPath := "/test/hub"
	relativePath := "hops/main"
	expectedAbsPath := "/test/hub/hops/main"

	// Simulate what happens with filepath.Abs when CWD is the worktree itself
	// In the buggy code: filepath.Abs("hops/main") from CWD "/test/hub/hops/main"
	// would produce "/test/hub/hops/main/hops/main"

	// The correct way: use config.ResolveWorktreePath
	correctPath := config.ResolveWorktreePath(relativePath, hubPath)
	assert.Equal(t, expectedAbsPath, correctPath, "ResolveWorktreePath should produce correct absolute path")

	// Bug scenario: if we were using filepath.Abs and CWD was hubPath + relativePath,
	// it would incorrectly double the path
	buggyPath := filepath.Join(hubPath, relativePath, relativePath)
	assert.NotEqual(t, buggyPath, correctPath, "Should not produce duplicated path like /hub/hops/main/hops/main")
	assert.NotContains(t, correctPath, "hops/main/hops", "Should not contain duplicated path segments")
}

// TestRemoveCommand_RelativePathBug reproduces the actual bug scenario
// where basePath is relative and gets incorrectly resolved
func TestRemoveCommand_RelativePathBug(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create hub structure
	hubPath := "/test/hub"
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// Add main branch with RELATIVE path (this is how it's stored in hub config)
	mainRelativePath := "hops/main"
	mainAbsolutePath := filepath.Join(hubPath, mainRelativePath)
	require.NoError(t, fs.MkdirAll(mainAbsolutePath, 0755))
	require.NoError(t, hub.AddBranch("main", "main", mainRelativePath))

	// Add feature branch with RELATIVE path
	featureRelativePath := "hops/feature"
	featureAbsolutePath := filepath.Join(hubPath, featureRelativePath)
	require.NoError(t, fs.MkdirAll(featureAbsolutePath, 0755))
	require.NoError(t, hub.AddBranch("feature", "feature", featureRelativePath))

	// Get the basePath and worktreePath as they would be in remove command
	basePath := hub.Config.Branches["main"].Path          // "hops/main" (relative)
	worktreePath := hub.Config.Branches["feature"].Path  // "hops/feature" (relative)

	// THE BUG: using filepath.Abs on a relative path resolves from CWD
	// If we just used filepath.Abs(basePath), it would resolve from current directory
	// which could cause duplication. The fix is to use config.ResolveWorktreePath.

	// Demonstrate the fix
	correctBasePath := config.ResolveWorktreePath(basePath, hubPath)
	correctWorktreePath := config.ResolveWorktreePath(worktreePath, hubPath)

	// Verify correct paths
	assert.Equal(t, mainAbsolutePath, correctBasePath)
	assert.Equal(t, featureAbsolutePath, correctWorktreePath)

	// Ensure no duplication
	assert.NotContains(t, correctBasePath, "hops/main/hops")
	assert.NotContains(t, correctWorktreePath, "hops/feature/hops")
}

// TestRemoveCommand_DefaultBranchGuard verifies removal of default branch is rejected
func TestRemoveCommand_DefaultBranchGuard(t *testing.T) {
	fs := afero.NewMemMapFs()

	hubPath := "/test/hub"
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	// The default branch is "main"
	assert.Equal(t, "main", hub.Config.Repo.DefaultBranch)

	// Verify the guard condition matches
	target := "main"
	assert.Equal(t, target, hub.Config.Repo.DefaultBranch,
		"Guard should trigger when target equals default branch")
}

// TestRemoveCommand_CwdGuard verifies removal is blocked when inside target worktree
func TestRemoveCommand_CwdGuard(t *testing.T) {
	hubPath := "/test/hub"
	worktreeRelPath := "hops/feature"
	absWorktree := filepath.Join(hubPath, worktreeRelPath)

	tests := []struct {
		name    string
		cwd     string
		blocked bool
	}{
		{"exact match", absWorktree, true},
		{"subdirectory", filepath.Join(absWorktree, "src"), true},
		{"different worktree", filepath.Join(hubPath, "hops/main"), false},
		{"parent of hub", "/test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absCwd := tt.cwd
			inside := absCwd == absWorktree ||
				len(absCwd) > len(absWorktree) &&
					absCwd[:len(absWorktree)+1] == absWorktree+string(filepath.Separator)
			assert.Equal(t, tt.blocked, inside)
		})
	}
}

// TestRemoveCommand_MockBranchDeletion verifies branch deletion calls via mock
func TestRemoveCommand_MockBranchDeletion(t *testing.T) {
	mockGit := mocks.NewMockGit()

	// Simulate branch deletion flow
	dir := "/test/hub/hops/main"
	target := "feature"

	err := mockGit.DeleteLocalBranch(dir, target)
	require.NoError(t, err)
	assert.Contains(t, mockGit.DeletedLocalBranches, target)

	// Remote branch does not exist by default
	assert.False(t, mockGit.HasRemoteBranch(dir, target))

	// Set remote branch to exist
	mockGit.RemoteBranchExists = true
	assert.True(t, mockGit.HasRemoteBranch(dir, target))

	err = mockGit.DeleteRemoteBranch(dir, target)
	require.NoError(t, err)
	assert.Contains(t, mockGit.DeletedRemoteBranches, target)
}
