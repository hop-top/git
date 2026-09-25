package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// newRecoveryRepo makes a git repository with one commit and the
// hopspace that records it as main.
func newRecoveryRepo(t *testing.T) (string, *hop.Hopspace) {
	t.Helper()
	tempDir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test User"},
		{"commit", "--allow-empty", "-m", "Initial commit"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	return tempDir, &hop.Hopspace{
		Path: tempDir,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {Path: tempDir, Exists: true},
			},
		},
	}
}

// TestAddCommand_RefusesNonEmptyDirectory: a directory neither git nor
// hop.json knows, holding files, is left as it was and the add refused.
func TestAddCommand_RefusesNonEmptyDirectory(t *testing.T) {
	tempDir, hopspace := newRecoveryRepo(t)
	fs := afero.NewOsFs()

	occupiedPath := filepath.Join(tempDir, "feature-branch")
	keep := filepath.Join(occupiedPath, "junk.txt")
	require.NoError(t, fs.MkdirAll(occupiedPath, 0755))
	require.NoError(t, afero.WriteFile(fs, keep, []byte("uncommitted work"), 0644))

	wm := hop.NewWorktreeManager(fs, git.New())
	worktreePath, err := wm.CreateWorktreeTransactional(
		hopspace, tempDir, "feature-branch", "{branch}", "testorg", "testrepo", "", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists and is not an empty directory")
	assert.Equal(t, occupiedPath, worktreePath)
	got, readErr := os.ReadFile(keep)
	require.NoError(t, readErr, "the file in the directory must survive")
	assert.Equal(t, "uncommitted work", string(got))
	_, statErr := os.Stat(filepath.Join(occupiedPath, ".git"))
	assert.True(t, os.IsNotExist(statErr), "no worktree may be created over it")
}

// TestAddCommand_ReusesEmptyDirectory: an empty directory at the target
// path is reused, as git worktree add reuses it.
func TestAddCommand_ReusesEmptyDirectory(t *testing.T) {
	tempDir, hopspace := newRecoveryRepo(t)
	fs := afero.NewOsFs()

	emptyPath := filepath.Join(tempDir, "feature-branch")
	require.NoError(t, fs.MkdirAll(emptyPath, 0755))

	wm := hop.NewWorktreeManager(fs, git.New())
	worktreePath, err := wm.CreateWorktreeTransactional(
		hopspace, tempDir, "feature-branch", "{branch}", "testorg", "testrepo", "", "")

	require.NoError(t, err)
	assert.Equal(t, emptyPath, worktreePath)
	_, statErr := os.Stat(filepath.Join(emptyPath, ".git"))
	assert.NoError(t, statErr, "worktree should have a .git file")

	list := exec.Command("git", "worktree", "list")
	list.Dir = tempDir
	out, err := list.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(out), "feature-branch")
}
