package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHooksInstalledAfterRegisterAsIs(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/tmp/test-repo"

	// Simulate what registerAsIs does: repo root has .git dir
	require.NoError(t, fs.MkdirAll(filepath.Join(repoPath, ".git"), 0755))

	err := installInitHooks(fs, repoPath, "", false)

	require.NoError(t, err)
	exists, _ := afero.DirExists(fs, filepath.Join(repoPath, ".git-hop", "hooks"))
	assert.True(t, exists, "hooks dir should be created at repo root for register-as-is")
}

func TestHooksInstalledAfterBareConversion(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/tmp/test-repo"
	mainWorktreePath := "/tmp/test-repo/hops/main"

	// Bare repo: worktree has its own .git file (linked worktree)
	require.NoError(t, fs.MkdirAll(filepath.Join(mainWorktreePath, ".git"), 0755))

	err := installInitHooks(fs, repoPath, mainWorktreePath, false)

	require.NoError(t, err)
	exists, _ := afero.DirExists(fs, filepath.Join(mainWorktreePath, ".git-hop", "hooks"))
	assert.True(t, exists, "hooks dir should be in main worktree for bare repo")
	// Should NOT be at bare repo root (no .git there)
	rootExists, _ := afero.DirExists(fs, filepath.Join(repoPath, ".git-hop", "hooks"))
	assert.False(t, rootExists, "hooks dir should not be at bare repo root")
}

func TestHooksInstalledAfterRegularConversion(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/tmp/test-repo"

	// Regular repo: .git is at repo root, mainWorktreePath == repoPath
	require.NoError(t, fs.MkdirAll(filepath.Join(repoPath, ".git"), 0755))

	err := installInitHooks(fs, repoPath, repoPath, true)

	require.NoError(t, err)
	exists, _ := afero.DirExists(fs, filepath.Join(repoPath, ".git-hop", "hooks"))
	assert.True(t, exists, "hooks dir should be at repo root for regular repo conversion")
}

func TestHooksInstallFailsGracefully(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/tmp/test-repo"

	// No .git dir — InstallHooks should fail, but installInitHooks returns the error
	err := installInitHooks(fs, repoPath, "", false)

	assert.Error(t, err)
}
