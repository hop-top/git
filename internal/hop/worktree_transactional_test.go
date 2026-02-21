package hop

import (
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: These tests focus on validation and cleanup logic.
// Actual git operations would fail without a real git repo, so we test up to the CreateWorktree call.

func TestCreateWorktreeTransactional_Clean(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	g := git.New()
	manager := NewWorktreeManager(fs, g)

	// Create hopspace directory structure
	hopspacePath := "/test/hopspace"
	hubPath := filepath.Join(hopspacePath, "hub")
	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	// Create a base worktree that exists
	baseWorktreePath := filepath.Join(hopspacePath, "hops", "main")
	require.NoError(t, fs.MkdirAll(baseWorktreePath, 0755))

	// Setup hopspace config with existing worktree
	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Path:   baseWorktreePath,
					Exists: true,
				},
			},
		},
		fs: fs,
	}

	branch := "feature-1"
	locationPattern := "{hubPath}/../hops/{branch}"
	org := "test-org"
	repo := "test-repo"

	// Execute - will fail at git operation but should pass validation
	worktreePath, err := manager.CreateWorktreeTransactional(
		hopspace,
		hubPath,
		branch,
		locationPattern,
		org,
		repo,
	)

	// Assert - expect git error but path should be computed correctly
	expectedPath := filepath.Join(hopspacePath, "hops", branch)
	assert.Equal(t, expectedPath, worktreePath)
	// Will fail at git operation since not a real repo
	assert.Error(t, err)
}

func TestCreateWorktreeTransactional_CleansUpOrphanedDirectory(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	g := git.New()
	manager := NewWorktreeManager(fs, g)

	// Create hopspace directory structure
	hopspacePath := "/test/hopspace"
	hubPath := filepath.Join(hopspacePath, "hub")
	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	// Create a base worktree that exists
	baseWorktreePath := filepath.Join(hopspacePath, "hops", "main")
	require.NoError(t, fs.MkdirAll(baseWorktreePath, 0755))

	// Setup hopspace config with existing worktree
	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Path:   baseWorktreePath,
					Exists: true,
				},
			},
		},
		fs: fs,
	}

	branch := "feature-1"
	locationPattern := "{hubPath}/../hops/{branch}"
	org := "test-org"
	repo := "test-repo"

	// Create an orphaned directory at the target location (not in config)
	expectedPath := filepath.Join(hopspacePath, "hops", branch)
	require.NoError(t, fs.MkdirAll(expectedPath, 0755))

	// Write a file to make sure it's a real orphaned directory
	orphanedFile := filepath.Join(expectedPath, "some-file.txt")
	require.NoError(t, afero.WriteFile(fs, orphanedFile, []byte("orphaned"), 0644))

	// Verify the orphaned directory exists before the operation
	exists, err := afero.DirExists(fs, expectedPath)
	require.NoError(t, err)
	require.True(t, exists)

	// Execute - should detect orphaned directory and clean it up before creating worktree
	worktreePath, err := manager.CreateWorktreeTransactional(
		hopspace,
		hubPath,
		branch,
		locationPattern,
		org,
		repo,
	)

	// Assert
	assert.Equal(t, expectedPath, worktreePath)

	// The orphaned directory should have been cleaned up
	// We can verify by checking if the orphaned file is gone
	fileExists, _ := afero.Exists(fs, orphanedFile)
	assert.False(t, fileExists, "Orphaned file should have been cleaned up")

	// Will fail at git operation since not a real repo, but cleanup should have happened
	assert.Error(t, err)
}

func TestCreateWorktreeTransactional_AlreadyExists(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	g := git.New()
	manager := NewWorktreeManager(fs, g)

	// Create hopspace directory structure
	hopspacePath := "/test/hopspace"
	hubPath := filepath.Join(hopspacePath, "hub")
	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	// Create a base worktree that exists
	baseWorktreePath := filepath.Join(hopspacePath, "hops", "main")
	require.NoError(t, fs.MkdirAll(baseWorktreePath, 0755))

	// Setup hopspace config with existing worktree
	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {
					Path:   baseWorktreePath,
					Exists: true,
				},
			},
		},
		fs: fs,
	}

	branch := "feature-1"
	locationPattern := "{hubPath}/../hops/{branch}"
	org := "test-org"
	repo := "test-repo"

	// Create a directory at target location that IS registered in config
	// This simulates a registered worktree that already exists
	expectedPath := filepath.Join(hopspacePath, "hops", branch)
	require.NoError(t, fs.MkdirAll(expectedPath, 0755))

	// Register it in config
	hopspace.Config.Branches[branch] = config.HopspaceBranch{
		Path:   expectedPath,
		Exists: true,
	}

	// Execute - should fail because worktree already exists (registered directory)
	worktreePath, err := manager.CreateWorktreeTransactional(
		hopspace,
		hubPath,
		branch,
		locationPattern,
		org,
		repo,
	)

	// Assert - should return error from CreateWorktree about already existing
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	// Path should be cleaned version of what was computed
	assert.Equal(t, filepath.Clean(expectedPath), worktreePath)
}

func TestCreateWorktreeTransactional_EmptyHubPath(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	g := git.New()
	manager := NewWorktreeManager(fs, g)

	hopspace := &Hopspace{
		Path: "/test/hopspace",
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{},
		},
		fs: fs,
	}

	// Execute with empty hubPath
	worktreePath, err := manager.CreateWorktreeTransactional(
		hopspace,
		"", // empty hubPath
		"feature-1",
		"{hubPath}/../hops/{branch}",
		"test-org",
		"test-repo",
	)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hubPath cannot be empty")
	assert.Empty(t, worktreePath)
}

func TestCreateWorktreeTransactional_EmptyBranch(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	g := git.New()
	manager := NewWorktreeManager(fs, g)

	hopspacePath := "/test/hopspace"
	hubPath := filepath.Join(hopspacePath, "hub")
	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{},
		},
		fs: fs,
	}

	// Execute with empty branch
	worktreePath, err := manager.CreateWorktreeTransactional(
		hopspace,
		hubPath,
		"", // empty branch
		"{hubPath}/../hops/{branch}",
		"test-org",
		"test-repo",
	)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "branch cannot be empty")
	assert.Empty(t, worktreePath)
}
