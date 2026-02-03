package hop

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStateValidator(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	validator := NewStateValidator(fs, g)

	assert.NotNil(t, validator)
	assert.Equal(t, fs, validator.fs)
	assert.Equal(t, g, validator.git)
}

func TestDetectOrphanedDirectories(t *testing.T) {
	tests := []struct {
		name          string
		setupFS       func(afero.Fs, string)
		setupConfig   func() *config.HopspaceConfig
		expectedDirs  []string
		expectedError bool
	}{
		{
			name: "no hops directory - returns empty list",
			setupFS: func(fs afero.Fs, hopspacePath string) {
				// Don't create hops directory
			},
			setupConfig: func() *config.HopspaceConfig {
				return &config.HopspaceConfig{
					Branches: make(map[string]config.HopspaceBranch),
				}
			},
			expectedDirs:  []string{},
			expectedError: false,
		},
		{
			name: "empty hops directory - returns empty list",
			setupFS: func(fs afero.Fs, hopspacePath string) {
				require.NoError(t, fs.MkdirAll(filepath.Join(hopspacePath, "hops"), 0755))
			},
			setupConfig: func() *config.HopspaceConfig {
				return &config.HopspaceConfig{
					Branches: make(map[string]config.HopspaceBranch),
				}
			},
			expectedDirs:  []string{},
			expectedError: false,
		},
		{
			name: "all directories registered - returns empty list",
			setupFS: func(fs afero.Fs, hopspacePath string) {
				hopsDir := filepath.Join(hopspacePath, "hops")
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "feature-1"), 0755))
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "feature-2"), 0755))
			},
			setupConfig: func() *config.HopspaceConfig {
				return &config.HopspaceConfig{
					Branches: map[string]config.HopspaceBranch{
						"feature-1": {Path: "feature-1", Exists: true},
						"feature-2": {Path: "feature-2", Exists: true},
					},
				}
			},
			expectedDirs:  []string{},
			expectedError: false,
		},
		{
			name: "one orphaned directory",
			setupFS: func(fs afero.Fs, hopspacePath string) {
				hopsDir := filepath.Join(hopspacePath, "hops")
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "feature-1"), 0755))
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "orphaned"), 0755))
			},
			setupConfig: func() *config.HopspaceConfig {
				return &config.HopspaceConfig{
					Branches: map[string]config.HopspaceBranch{
						"feature-1": {Path: "feature-1", Exists: true},
					},
				}
			},
			expectedDirs:  []string{"orphaned"},
			expectedError: false,
		},
		{
			name: "multiple orphaned directories",
			setupFS: func(fs afero.Fs, hopspacePath string) {
				hopsDir := filepath.Join(hopspacePath, "hops")
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "feature-1"), 0755))
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "orphaned-1"), 0755))
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "orphaned-2"), 0755))
			},
			setupConfig: func() *config.HopspaceConfig {
				return &config.HopspaceConfig{
					Branches: map[string]config.HopspaceBranch{
						"feature-1": {Path: "feature-1", Exists: true},
					},
				}
			},
			expectedDirs:  []string{"orphaned-1", "orphaned-2"},
			expectedError: false,
		},
		{
			name: "ignores files in hops directory",
			setupFS: func(fs afero.Fs, hopspacePath string) {
				hopsDir := filepath.Join(hopspacePath, "hops")
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "feature-1"), 0755))
				require.NoError(t, afero.WriteFile(fs, filepath.Join(hopsDir, "README.md"), []byte("test"), 0644))
			},
			setupConfig: func() *config.HopspaceConfig {
				return &config.HopspaceConfig{
					Branches: map[string]config.HopspaceBranch{
						"feature-1": {Path: "feature-1", Exists: true},
					},
				}
			},
			expectedDirs:  []string{},
			expectedError: false,
		},
		{
			name: "handles absolute paths in config",
			setupFS: func(fs afero.Fs, hopspacePath string) {
				hopsDir := filepath.Join(hopspacePath, "hops")
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "feature-1"), 0755))
				require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "orphaned"), 0755))
			},
			setupConfig: func() *config.HopspaceConfig {
				return &config.HopspaceConfig{
					Branches: map[string]config.HopspaceBranch{
						"feature-1": {Path: "/tmp/hopspace/hops/feature-1", Exists: true},
					},
				}
			},
			expectedDirs:  []string{"orphaned"},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			g := git.New()
			validator := NewStateValidator(fs, g)

			hopspacePath := "/tmp/hopspace"
			tt.setupFS(fs, hopspacePath)

			hopspace := &Hopspace{
				Path:   hopspacePath,
				Config: tt.setupConfig(),
				fs:     fs,
			}

			orphaned, err := validator.DetectOrphanedDirectories(hopspace)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expectedDirs, orphaned)
			}
		})
	}
}

func TestDetectOrphanedDirectories_NoOrphans(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	validator := NewStateValidator(fs, g)

	hopspacePath := "/tmp/hopspace"
	hopsDir := filepath.Join(hopspacePath, "hops")

	// Create directories that are all registered
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "main"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "develop"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(hopsDir, "feature-x"), 0755))

	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main":      {Path: "main", Exists: true},
				"develop":   {Path: "develop", Exists: true},
				"feature-x": {Path: "feature-x", Exists: true},
			},
		},
		fs: fs,
	}

	orphaned, err := validator.DetectOrphanedDirectories(hopspace)

	assert.NoError(t, err)
	assert.Empty(t, orphaned, "Should return empty list when all directories are registered")
}

func TestValidateWorktreeAdd_Clean(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	validator := NewStateValidator(fs, g)

	hopspacePath := "/tmp/hopspace"
	hubPath := filepath.Join(hopspacePath, "hub")
	worktreePath := filepath.Join(hopspacePath, "hops", "feature-1")

	// Create hopspace without the worktree path
	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: make(map[string]config.HopspaceBranch),
		},
		fs: fs,
	}

	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, "feature-1", worktreePath)

	assert.NoError(t, err)
	assert.NotNil(t, validation)
	assert.True(t, validation.IsClean, "Validation should be clean when path doesn't exist")
	assert.True(t, validation.CanProceed, "Should be able to proceed when path doesn't exist")
	assert.False(t, validation.RequiresCleanup, "Should not require cleanup when path doesn't exist")
	assert.Empty(t, validation.Issues, "Should have no issues when path doesn't exist")
}

func TestValidateWorktreeAdd_DirectoryExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	validator := NewStateValidator(fs, g)

	hopspacePath := "/tmp/hopspace"
	hubPath := filepath.Join(hopspacePath, "hub")
	worktreePath := filepath.Join(hopspacePath, "hops", "orphaned")

	// Create hopspace with orphaned directory
	require.NoError(t, fs.MkdirAll(hubPath, 0755))
	require.NoError(t, fs.MkdirAll(worktreePath, 0755))

	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: make(map[string]config.HopspaceBranch),
		},
		fs: fs,
	}

	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, "orphaned", worktreePath)

	assert.NoError(t, err)
	assert.NotNil(t, validation)
	assert.False(t, validation.IsClean, "Validation should not be clean when directory exists")
	assert.False(t, validation.CanProceed, "Should not be able to proceed when orphaned directory exists")
	assert.True(t, validation.RequiresCleanup, "Should require cleanup when directory exists")
	assert.Len(t, validation.Issues, 1, "Should have one issue")

	issue := validation.Issues[0]
	assert.Equal(t, OrphanedDirectory, issue.Type, "Issue type should be OrphanedDirectory")
	assert.Equal(t, worktreePath, issue.Path, "Issue path should be the worktree path")
	assert.Contains(t, issue.Description, "Orphaned", "Description should mention orphaned directory")
}

func TestValidateWorktreeAdd_DirectoryExists_Registered(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	validator := NewStateValidator(fs, g)

	hopspacePath := "/tmp/hopspace"
	hubPath := filepath.Join(hopspacePath, "hub")
	worktreePath := filepath.Join(hopspacePath, "hops", "feature-1")

	// Create hopspace with registered directory
	require.NoError(t, fs.MkdirAll(hubPath, 0755))
	require.NoError(t, fs.MkdirAll(worktreePath, 0755))

	hopspace := &Hopspace{
		Path: hopspacePath,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"feature-1": {Path: worktreePath, Exists: true},
			},
		},
		fs: fs,
	}

	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, "feature-1", worktreePath)

	assert.NoError(t, err)
	assert.NotNil(t, validation)
	assert.False(t, validation.IsClean, "Validation should not be clean when directory exists")
	assert.True(t, validation.CanProceed, "Should be able to proceed when directory is registered")
	assert.True(t, validation.RequiresCleanup, "Should require cleanup even when registered")
	assert.Empty(t, validation.Issues, "Should have no issues when directory is registered")
}
