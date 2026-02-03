package hop

import (
	"testing"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupOrphanedDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	cleanup := NewCleanupManager(fs, g)

	// Create a directory to be cleaned up
	orphanedPath := "/path/to/orphaned"
	require.NoError(t, fs.MkdirAll(orphanedPath, 0755))

	// Verify it exists
	exists, err := afero.DirExists(fs, orphanedPath)
	require.NoError(t, err)
	require.True(t, exists)

	// Clean it up
	err = cleanup.CleanupOrphanedDirectory(orphanedPath)
	assert.NoError(t, err)

	// Verify it's gone
	exists, err = afero.DirExists(fs, orphanedPath)
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestCleanupOrphanedDirectory_NotExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	cleanup := NewCleanupManager(fs, g)

	// Try to clean up a directory that doesn't exist
	nonExistentPath := "/path/to/nonexistent"
	err := cleanup.CleanupOrphanedDirectory(nonExistentPath)

	// Should not return an error if already gone
	assert.NoError(t, err)
}

// MockCommandRunner is a mock implementation of CommandRunner for testing
type MockCommandRunner struct {
	RunInDirFunc func(dir string, cmd string, args ...string) (string, error)
}

func (m *MockCommandRunner) Run(cmd string, args ...string) (string, error) {
	return m.RunInDir("", cmd, args...)
}

func (m *MockCommandRunner) RunInDir(dir string, cmd string, args ...string) (string, error) {
	if m.RunInDirFunc != nil {
		return m.RunInDirFunc(dir, cmd, args...)
	}
	return "", nil
}

func TestPruneWorktrees(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create a mock command runner
	var capturedDir string
	var capturedCmd string
	var capturedArgs []string
	mockRunner := &MockCommandRunner{
		RunInDirFunc: func(dir string, cmd string, args ...string) (string, error) {
			capturedDir = dir
			capturedCmd = cmd
			capturedArgs = args
			return "", nil
		},
	}

	// Create git wrapper with mock runner
	g := &git.Git{Runner: mockRunner}
	cleanup := NewCleanupManager(fs, g)

	// Create the directory structure in the memory filesystem
	require.NoError(t, fs.MkdirAll("/test/hopspace/hops/main", 0755))
	require.NoError(t, fs.MkdirAll("/test/hopspace/hops/feature", 0755))

	// Create a hopspace with branches
	hopspace := &Hopspace{
		Path: "/test/hopspace",
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Exists: true,
					Path:   "/test/hopspace/hops/main",
				},
				"feature": {
					Exists: true,
					Path:   "/test/hopspace/hops/feature",
				},
			},
		},
	}

	// Prune worktrees
	err := cleanup.PruneWorktrees(hopspace)
	require.NoError(t, err)

	// Verify git worktree prune was called with correct arguments
	// Note: map iteration is non-deterministic, so either path is valid
	assert.Contains(t, []string{"/test/hopspace/hops/main", "/test/hopspace/hops/feature"}, capturedDir)
	assert.Equal(t, "git", capturedCmd)
	assert.Equal(t, []string{"worktree", "prune"}, capturedArgs)
}

func TestPruneWorktrees_NoWorktrees(t *testing.T) {
	fs := afero.NewMemMapFs()

	mockRunner := &MockCommandRunner{}
	g := &git.Git{Runner: mockRunner}
	cleanup := NewCleanupManager(fs, g)

	// Create a hopspace with no existing worktrees
	hopspace := &Hopspace{
		Path: "/test/hopspace",
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Exists: false,
					Path:   "",
				},
			},
		},
	}

	// Should not error when no worktrees exist
	err := cleanup.PruneWorktrees(hopspace)
	assert.NoError(t, err)
}
