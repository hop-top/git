package cmd

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorCommand_DetectsOrphanedDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Setup hopspace with orphaned directories
	hopspacePath := "/tmp/test-hopspace"
	hopsDir := filepath.Join(hopspacePath, "hops")
	uri := "git@github.com:test/repo.git"
	org := "test"
	repo := "repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	require.NoError(t, err)

	// Register main branch
	require.NoError(t, hopspace.RegisterBranch("main", filepath.Join(hopsDir, "main")))

	// Create main directory and orphaned directories
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "main"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "orphaned-1"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "orphaned-2"), 0755))

	// Create validator
	validator := hop.NewStateValidator(fs, nil)

	// Detect orphaned directories
	orphaned, err := validator.DetectOrphanedDirectories(hopspace)
	require.NoError(t, err)

	// Should find two orphaned directories
	assert.Len(t, orphaned, 2)
	assert.ElementsMatch(t, []string{"orphaned-1", "orphaned-2"}, orphaned)
}

func TestDoctorCommand_CleansUpOrphanedDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Setup hopspace with orphaned directory
	hopspacePath := "/tmp/test-hopspace"
	hopsDir := filepath.Join(hopspacePath, "hops")
	orphanedPath := filepath.Join(hopsDir, "orphaned")
	uri := "git@github.com:test/repo.git"
	org := "test"
	repo := "repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	require.NoError(t, err)

	// Register main branch
	require.NoError(t, hopspace.RegisterBranch("main", filepath.Join(hopsDir, "main")))

	// Create directories
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "main"), 0755))
	require.NoError(t, fs.MkdirAll(orphanedPath, 0755))

	// Create a file in the orphaned directory to verify recursive deletion
	require.NoError(t, afero.WriteFile(fs, filepath.Join(orphanedPath, "test.txt"), []byte("test"), 0644))

	// Verify orphaned directory exists
	exists, err := afero.DirExists(fs, orphanedPath)
	require.NoError(t, err)
	require.True(t, exists)

	// Create cleanup manager and remove orphaned directory
	cleanup := hop.NewCleanupManager(fs, nil)
	err = cleanup.CleanupOrphanedDirectory(orphanedPath)
	require.NoError(t, err)

	// Verify orphaned directory is gone
	exists, err = afero.Exists(fs, orphanedPath)
	require.NoError(t, err)
	assert.False(t, exists, "Orphaned directory should be removed")
}

func TestDoctorCommand_NoOrphanedDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Setup hopspace with all directories registered
	hopspacePath := "/tmp/test-hopspace"
	hopsDir := filepath.Join(hopspacePath, "hops")
	uri := "git@github.com:test/repo.git"
	org := "test"
	repo := "repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	require.NoError(t, err)

	// Register all branches
	require.NoError(t, hopspace.RegisterBranch("main", filepath.Join(hopsDir, "main")))
	require.NoError(t, hopspace.RegisterBranch("develop", filepath.Join(hopsDir, "develop")))

	// Create directories
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "main"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "develop"), 0755))

	// Create validator
	validator := hop.NewStateValidator(fs, nil)

	// Detect orphaned directories
	orphaned, err := validator.DetectOrphanedDirectories(hopspace)
	require.NoError(t, err)

	// Should find no orphaned directories
	assert.Empty(t, orphaned)
}
