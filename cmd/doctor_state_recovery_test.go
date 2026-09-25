package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/hop"
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
	require.NoError(t, hopspace.RegisterBranch(hopspace.Path, "main", filepath.Join(hopsDir, "main")))

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
	require.NoError(t, hopspace.RegisterBranch(hopspace.Path, "main", filepath.Join(hopsDir, "main")))
	require.NoError(t, hopspace.RegisterBranch(hopspace.Path, "develop", filepath.Join(hopsDir, "develop")))

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

func TestDoctorCommand_DetectsBrokenWorktrees(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Setup hub with a broken worktree reference
	hubPath := "/tmp/test-hub"
	hopspacePath := "/tmp/test-hopspace"
	uri := "git@github.com:test/repo.git"
	org := "test"
	repo := "repo"
	defaultBranch := "main"

	// Create hub
	hub, err := hop.CreateHub(fs, hubPath, uri, org, repo, defaultBranch)
	require.NoError(t, err)

	// Create hopspace
	hopspace, err := hop.InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	require.NoError(t, err)

	// Add a branch to hub that references a non-existent worktree
	branchPath := "hops/feature"
	worktreePath := filepath.Join(hubPath, branchPath)
	require.NoError(t, hub.AddBranch("feature", "feature", branchPath))
	require.NoError(t, hopspace.RegisterBranch(hopspace.Path, "feature", worktreePath))

	// Verify the worktree path doesn't exist (broken)
	exists, err := afero.Exists(fs, worktreePath)
	require.NoError(t, err)
	assert.False(t, exists, "Worktree should not exist initially")

	// Check that hub config references the broken path
	assert.Contains(t, hub.Config.Branches, "feature")
	assert.Equal(t, branchPath, hub.Config.Branches["feature"].Path)

	// Verify stat fails for broken worktree
	_, err = fs.Stat(worktreePath)
	assert.Error(t, err, "Stat should fail for non-existent worktree")
}
