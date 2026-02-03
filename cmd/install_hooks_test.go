package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/hooks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallHooksCommand(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create a temporary git worktree
	tmpDir := "/tmp/test-worktree"
	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, fs.MkdirAll(gitDir, 0755))

	// Change to test directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)

	// Create hook runner
	runner := hooks.NewRunner(fs)
	err := runner.InstallHooks(tmpDir)

	require.NoError(t, err)

	// Verify hooks directory created
	hooksDir := filepath.Join(tmpDir, ".git-hop", "hooks")
	exists, err := afero.DirExists(fs, hooksDir)
	require.NoError(t, err)
	assert.True(t, exists)
}
