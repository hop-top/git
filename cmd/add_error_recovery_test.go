package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddCommand_RecoveryFromOrphanedDirectory tests worktree creation recovery
// This is an integration test that verifies CreateWorktreeTransactional cleans up
// an orphaned directory before creating a worktree.
func TestAddCommand_RecoveryFromOrphanedDirectory(t *testing.T) {
	// Create a temporary directory for our test
	tempDir := t.TempDir()

	// Initialize a real git repo using exec commands
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	err := cmd.Run()
	require.NoError(t, err, "Failed to initialize git repo")

	// Configure git for testing
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tempDir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tempDir
	_ = cmd.Run()

	// Create initial commit (required for worktree operations)
	testFile := filepath.Join(tempDir, "README.md")
	err = os.WriteFile(testFile, []byte("# Test Repo"), 0644)
	require.NoError(t, err)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tempDir
	err = cmd.Run()
	require.NoError(t, err)

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tempDir
	err = cmd.Run()
	require.NoError(t, err)

	// Setup filesystem and hopspace
	fs := afero.NewOsFs()
	hopspace := &hop.Hopspace{
		Path: tempDir,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Path:   tempDir,
					Exists: true,
				},
			},
		},
	}

	// Create an orphaned directory (simulates a previous failed worktree creation)
	orphanedPath := filepath.Join(tempDir, "feature-branch")
	err = fs.MkdirAll(orphanedPath, 0755)
	require.NoError(t, err, "Failed to create orphaned directory")

	// Write some content to make it non-empty (orphaned directory)
	orphanedFile := filepath.Join(orphanedPath, "junk.txt")
	err = afero.WriteFile(fs, orphanedFile, []byte("orphaned content"), 0644)
	require.NoError(t, err, "Failed to write orphaned file")

	// Verify orphaned directory exists
	exists, err := afero.DirExists(fs, orphanedPath)
	require.NoError(t, err)
	require.True(t, exists, "Orphaned directory should exist before test")

	// Create worktree manager
	g := git.New()
	wm := hop.NewWorktreeManager(fs, g)

	// Use CreateWorktreeTransactional which should clean up the orphaned directory
	// and successfully create the worktree
	worktreePath, err := wm.CreateWorktreeTransactional(
		hopspace,
		tempDir,
		"feature-branch",
		"{branch}", // Will expand to tempDir/feature-branch (relative to hubPath)
		"testorg",
		"testrepo",
	)

	// Assert: Should succeed and return the expected path
	require.NoError(t, err, "CreateWorktreeTransactional should succeed after cleanup")
	assert.Equal(t, orphanedPath, worktreePath, "Should return expected worktree path")

	// Verify the directory now contains a valid git worktree
	gitDir := filepath.Join(worktreePath, ".git")
	exists, err = afero.Exists(fs, gitDir)
	require.NoError(t, err)
	assert.True(t, exists, "Worktree should have .git file after creation")

	// Verify the orphaned junk file is gone (directory was cleaned)
	exists, err = afero.Exists(fs, orphanedFile)
	require.NoError(t, err)
	assert.False(t, exists, "Orphaned file should be removed after cleanup")

	// Verify worktree is registered in git using exec
	cmd = exec.Command("git", "worktree", "list")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), "feature-branch", "Worktree should be listed in git worktree list")
}
