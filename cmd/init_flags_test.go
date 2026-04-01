package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoHooksFlag_SkipsHookInstall(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/tmp/test-repo"
	require.NoError(t, fs.MkdirAll(filepath.Join(repoPath, ".git"), 0755))

	// --no-hooks: pass noHooks=true, hooks dir should NOT be created
	installInitHooksConditional(fs, repoPath, "", false, true)

	exists, _ := afero.DirExists(fs, filepath.Join(repoPath, ".git-hop", "hooks"))
	assert.False(t, exists, "hooks dir should not be created when --no-hooks is set")
}

func TestNoHooksFlagAbsent_InstallsHooks(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/tmp/test-repo"
	require.NoError(t, fs.MkdirAll(filepath.Join(repoPath, ".git"), 0755))

	// no --no-hooks: pass noHooks=false, hooks dir should be created
	installInitHooksConditional(fs, repoPath, "", false, false)

	exists, _ := afero.DirExists(fs, filepath.Join(repoPath, ".git-hop", "hooks"))
	assert.True(t, exists, "hooks dir should be created when --no-hooks is absent")
}

func TestEnableChdirFlag_InstallsShellIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("SHELL", "/bin/bash")
	defer os.Unsetenv("SHELL")
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	fs := afero.NewOsFs()

	// --enable-chdir: should install shell integration
	err := maybeInstallShellIntegration(fs, true)
	assert.NoError(t, err)

	rcPath := filepath.Join(tmpDir, ".bashrc")
	content, readErr := os.ReadFile(rcPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "git-hop", "shell integration should be installed when --enable-chdir is set")
}

func TestEnableChdirFlagAbsent_SkipsShellIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("SHELL", "/bin/bash")
	defer os.Unsetenv("SHELL")

	fs := afero.NewOsFs()

	// no --enable-chdir: should NOT install shell integration
	err := maybeInstallShellIntegration(fs, false)
	assert.NoError(t, err)

	rcPath := filepath.Join(tmpDir, ".bashrc")
	_, statErr := os.Stat(rcPath)
	assert.True(t, os.IsNotExist(statErr), "shell integration should not be installed when --enable-chdir is absent")
}

func TestIdempotentRun_NoHooksFlag_DoesNotInstallHooks(t *testing.T) {
	repoPath := t.TempDir()
	fs := afero.NewOsFs()
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, ".git", "worktrees"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, ".git", "HEAD"),
		[]byte("ref: refs/heads/main\n"), 0644))

	out := captureStdout(t, func() {
		handleAlreadyInitializedWithFlags(fs, nil, repoPath, "bare-worktree", true, false)
	})

	_ = out
	exists, _ := afero.DirExists(fs, filepath.Join(repoPath, ".git-hop", "hooks"))
	assert.False(t, exists, "hooks should not be installed on idempotent run with --no-hooks")
}

func TestIdempotentRun_NoFlagsInstallsHooks(t *testing.T) {
	repoPath := t.TempDir()
	fs := afero.NewOsFs()
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, ".git", "worktrees"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, ".git", "HEAD"),
		[]byte("ref: refs/heads/main\n"), 0644))

	captureStdout(t, func() {
		handleAlreadyInitializedWithFlags(fs, nil, repoPath, "bare-worktree", false, false)
	})

	exists, _ := afero.DirExists(fs, filepath.Join(repoPath, ".git-hop", "hooks"))
	assert.True(t, exists, "hooks should be installed on idempotent run without --no-hooks")
}
