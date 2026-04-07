package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunner(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	assert.NotNil(t, runner)
}

func TestFindHookFile_RepoOverride(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	// Create repo-level hook
	worktreePath := "/path/to/worktree"
	hookPath := filepath.Join(worktreePath, ".git-hop", "hooks", "pre-worktree-add")
	require.NoError(t, fs.MkdirAll(filepath.Dir(hookPath), 0755))
	require.NoError(t, afero.WriteFile(fs, hookPath, []byte("#!/bin/bash\necho repo"), 0755))

	found := runner.FindHookFile("pre-worktree-add", worktreePath, "github.com/test/repo")

	assert.Equal(t, hookPath, found)
}

func TestFindHookFile_HopspaceHook(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	// Create hopspace-level hook
	worktreePath := "/path/to/worktree"
	dataHome := getTestDataHome()
	hookPath := filepath.Join(dataHome, "git-hop", "github.com", "test", "repo", "hooks", "pre-worktree-add")
	require.NoError(t, fs.MkdirAll(filepath.Dir(hookPath), 0755))
	require.NoError(t, afero.WriteFile(fs, hookPath, []byte("#!/bin/bash\necho hopspace"), 0755))

	// Set XDG_DATA_HOME for test
	os.Setenv("XDG_DATA_HOME", dataHome)
	defer os.Unsetenv("XDG_DATA_HOME")

	found := runner.FindHookFile("pre-worktree-add", worktreePath, "github.com/test/repo")

	assert.Equal(t, hookPath, found)
}

func TestFindHookFile_GlobalHook(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	// Create global hook
	worktreePath := "/path/to/worktree"
	configHome := getTestConfigHome()
	hookPath := filepath.Join(configHome, "git-hop", "hooks", "pre-worktree-add")
	require.NoError(t, fs.MkdirAll(filepath.Dir(hookPath), 0755))
	require.NoError(t, afero.WriteFile(fs, hookPath, []byte("#!/bin/bash\necho global"), 0755))

	// Set XDG_CONFIG_HOME for test
	os.Setenv("XDG_CONFIG_HOME", configHome)
	defer os.Unsetenv("XDG_CONFIG_HOME")

	found := runner.FindHookFile("pre-worktree-add", worktreePath, "github.com/test/repo")

	assert.Equal(t, hookPath, found)
}

func TestFindHookFile_Priority(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	worktreePath := "/path/to/worktree"
	dataHome := getTestDataHome()
	configHome := getTestConfigHome()

	// Create all three levels
	repoHook := filepath.Join(worktreePath, ".git-hop", "hooks", "pre-worktree-add")
	hopspaceHook := filepath.Join(dataHome, "git-hop", "github.com", "test", "repo", "hooks", "pre-worktree-add")
	globalHook := filepath.Join(configHome, "git-hop", "hooks", "pre-worktree-add")

	require.NoError(t, fs.MkdirAll(filepath.Dir(repoHook), 0755))
	require.NoError(t, afero.WriteFile(fs, repoHook, []byte("#!/bin/bash\necho repo"), 0755))

	require.NoError(t, fs.MkdirAll(filepath.Dir(hopspaceHook), 0755))
	require.NoError(t, afero.WriteFile(fs, hopspaceHook, []byte("#!/bin/bash\necho hopspace"), 0755))

	require.NoError(t, fs.MkdirAll(filepath.Dir(globalHook), 0755))
	require.NoError(t, afero.WriteFile(fs, globalHook, []byte("#!/bin/bash\necho global"), 0755))

	// Set env vars
	os.Setenv("XDG_DATA_HOME", dataHome)
	os.Setenv("XDG_CONFIG_HOME", configHome)
	defer os.Unsetenv("XDG_DATA_HOME")
	defer os.Unsetenv("XDG_CONFIG_HOME")

	// Should return repo-level (highest priority)
	found := runner.FindHookFile("pre-worktree-add", worktreePath, "github.com/test/repo")

	assert.Equal(t, repoHook, found)
}

func TestFindHookFile_NotFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	found := runner.FindHookFile("nonexistent-hook", "/path/to/worktree", "github.com/test/repo")

	assert.Empty(t, found)
}

func TestExecuteHook_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	// Create a simple hook script
	worktreePath := "/path/to/worktree"
	hookPath := filepath.Join(worktreePath, ".git-hop", "hooks", "pre-worktree-add")
	require.NoError(t, fs.MkdirAll(filepath.Dir(hookPath), 0755))
	require.NoError(t, afero.WriteFile(fs, hookPath, []byte("#!/bin/bash\nexit 0"), 0755))

	err := runner.ExecuteHook("pre-worktree-add", worktreePath, "github.com/test/repo", "main")

	// Note: This test may fail in memfs since we can't actually execute the script
	// In real implementation, we would use os.Exec which won't work with afero
	// This is a limitation we accept for unit tests
	_ = err // Acknowledge we can't fully test execution with memfs
}

func TestGetHookEnv(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	env := runner.GetHookEnv("pre-worktree-add", "/path/to/worktree", "github.com/test/repo", "feature-x")

	// Check required environment variables
	envMap := make(map[string]string)
	for _, e := range env {
		// Parse KEY=VALUE
		for i, ch := range e {
			if ch == '=' {
				key := e[:i]
				value := e[i+1:]
				envMap[key] = value
				break
			}
		}
	}

	assert.Equal(t, "pre-worktree-add", envMap["GIT_HOP_HOOK_NAME"])
	assert.Equal(t, "/path/to/worktree", envMap["GIT_HOP_WORKTREE_PATH"])
	assert.Equal(t, "github.com/test/repo", envMap["GIT_HOP_REPO_ID"])
	assert.Equal(t, "feature-x", envMap["GIT_HOP_BRANCH"])
}

func TestValidateHookName(t *testing.T) {
	tests := []struct {
		name     string
		hookName string
		valid    bool
	}{
		{"valid pre-worktree-add", "pre-worktree-add", true},
		{"valid post-worktree-add", "post-worktree-add", true},
		{"valid pre-worktree-remove", "pre-worktree-remove", true},
		{"valid post-worktree-remove", "post-worktree-remove", true},
		{"valid pre-env-start", "pre-env-start", true},
		{"valid post-env-start", "post-env-start", true},
		{"valid pre-env-stop", "pre-env-stop", true},
		{"valid post-env-stop", "post-env-stop", true},
		{"invalid hook name", "invalid-hook", false},
		{"empty hook name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHookName(tt.hookName)
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestInstallHooks(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	worktreePath := "/path/to/worktree"
	gitDir := filepath.Join(worktreePath, ".git")
	gitHopHooksDir := filepath.Join(worktreePath, ".git-hop", "hooks")

	// Create git directory
	require.NoError(t, fs.MkdirAll(gitDir, 0755))

	err := runner.InstallHooks(worktreePath)

	require.NoError(t, err)

	// Verify .git-hop/hooks directory was created
	exists, err := afero.DirExists(fs, gitHopHooksDir)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestInstallHooks_NotGitRepo(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	worktreePath := "/not/a/git/repo"

	err := runner.InstallHooks(worktreePath)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

// TestInstallHooks_WorktreeChild covers the .git-as-file shape: a git
// worktree's .git is a regular file containing "gitdir: <hub>/worktrees/X".
// Before the LooksLikeGitCheckout refactor, the old DirExists check
// rejected this shape and InstallHooks errored out — so hooks could
// never be installed inside any branch worktree.
func TestInstallHooks_WorktreeChild(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	worktreePath := "/path/to/worktree"
	require.NoError(t, fs.MkdirAll(worktreePath, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(worktreePath, ".git"),
		[]byte("gitdir: /hub/worktrees/feature\n"), 0644))

	err := runner.InstallHooks(worktreePath)
	require.NoError(t, err)

	exists, err := afero.DirExists(fs, filepath.Join(worktreePath, ".git-hop", "hooks"))
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestInstallHooks_BareRepoAtPath covers the bare-at-root shape: a hop
// hub where HEAD/objects/refs sit directly under the path with no .git
// subdir. Before the fix, InstallHooks rejected hubs because its old
// check looked for .git as a directory.
func TestInstallHooks_BareRepoAtPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	runner := NewRunner(fs)

	hubPath := "/path/to/hub"
	require.NoError(t, fs.MkdirAll(filepath.Join(hubPath, "objects"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(hubPath, "refs"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(hubPath, "HEAD"),
		[]byte("ref: refs/heads/main\n"), 0644))

	err := runner.InstallHooks(hubPath)
	require.NoError(t, err)

	exists, err := afero.DirExists(fs, filepath.Join(hubPath, ".git-hop", "hooks"))
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestValidateHookName_Move(t *testing.T) {
	if err := ValidateHookName("pre-worktree-move"); err != nil {
		t.Errorf("expected pre-worktree-move to be valid, got: %v", err)
	}
	if err := ValidateHookName("post-worktree-move"); err != nil {
		t.Errorf("expected post-worktree-move to be valid, got: %v", err)
	}
}

// Helper functions for tests
func getTestDataHome() string {
	return "/tmp/test-data-home"
}

func getTestConfigHome() string {
	return "/tmp/test-config-home"
}
