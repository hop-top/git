package hop

import (
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
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

// TestPruneWorktrees_NonExistentPaths tests that pruning with stale/non-existent
// paths in the config doesn't cause errors (Bug 2 regression test)
func TestPruneWorktrees_NonExistentPaths(t *testing.T) {
	fs := afero.NewMemMapFs()

	mockRunner := &MockCommandRunner{
		RunInDirFunc: func(dir string, cmd string, args ...string) (string, error) {
			// Should never be called since no valid paths exist
			t.Error("git command was called with non-existent path")
			return "", nil
		},
	}

	g := &git.Git{Runner: mockRunner}
	cleanup := NewCleanupManager(fs, g)

	// Create a hopspace with paths that don't exist on filesystem
	hopspace := &Hopspace{
		Path: "/test/hopspace",
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Exists: true,
					Path:   "/non/existent/path/main",
				},
				"feature": {
					Exists: true,
					Path:   "/another/invalid/path/feature",
				},
			},
		},
	}

	// Should not error when all paths are invalid - should just skip pruning
	err := cleanup.PruneWorktrees(hopspace)
	assert.NoError(t, err)
}

// TestPruneWorktrees_MixedValidInvalidPaths tests that pruning selects
// the first valid path when some paths are invalid
func TestPruneWorktrees_MixedValidInvalidPaths(t *testing.T) {
	fs := afero.NewMemMapFs()

	var capturedDir string
	mockRunner := &MockCommandRunner{
		RunInDirFunc: func(dir string, cmd string, args ...string) (string, error) {
			capturedDir = dir
			return "", nil
		},
	}

	g := &git.Git{Runner: mockRunner}
	cleanup := NewCleanupManager(fs, g)

	// Create one valid path and one invalid path
	validPath := "/test/hopspace/hops/feature"
	require.NoError(t, fs.MkdirAll(validPath, 0755))

	hopspace := &Hopspace{
		Path: "/test/hopspace",
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Exists: true,
					Path:   "/non/existent/path/main", // Invalid
				},
				"feature": {
					Exists: true,
					Path:   validPath, // Valid
				},
			},
		},
	}

	// Should successfully prune using the valid path
	err := cleanup.PruneWorktrees(hopspace)
	assert.NoError(t, err)

	// Should have called git with the valid path (not the invalid one)
	assert.Equal(t, validPath, capturedDir)
}
