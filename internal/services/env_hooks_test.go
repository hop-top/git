package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteHook(t *testing.T) {
	// Create temp directory for test scripts
	tmpDir, err := os.MkdirTemp("", "hook-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := HookContext{
		WorktreePath: tmpDir,
		Branch:       "test-branch",
		RepoPath:     "/tmp/repo",
		Command:      "start",
	}

	t.Run("executes successful hook script", func(t *testing.T) {
		// Create a simple script that exits 0
		scriptPath := filepath.Join(tmpDir, "success.sh")
		script := "#!/bin/bash\necho 'Hook executed'\nexit 0\n"
		err := os.WriteFile(scriptPath, []byte(script), 0755)
		require.NoError(t, err)

		err = ExecuteHook("success.sh", ctx)
		assert.NoError(t, err)
	})

	t.Run("fails when hook script not found", func(t *testing.T) {
		err := ExecuteHook("nonexistent.sh", ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("fails when hook exits non-zero", func(t *testing.T) {
		scriptPath := filepath.Join(tmpDir, "fail.sh")
		script := "#!/bin/bash\necho 'Hook failed'\nexit 1\n"
		err := os.WriteFile(scriptPath, []byte(script), 0755)
		require.NoError(t, err)

		err = ExecuteHook("fail.sh", ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exited with error")
	})

	t.Run("passes environment variables to hook", func(t *testing.T) {
		scriptPath := filepath.Join(tmpDir, "env-check.sh")
		script := `#!/bin/bash
if [ -z "$HOP_WORKTREE_PATH" ]; then
	echo "Missing HOP_WORKTREE_PATH"
	exit 1
fi
if [ -z "$HOP_BRANCH" ]; then
	echo "Missing HOP_BRANCH"
	exit 1
fi
if [ -z "$HOP_REPO_PATH" ]; then
	echo "Missing HOP_REPO_PATH"
	exit 1
fi
if [ -z "$HOP_COMMAND" ]; then
	echo "Missing HOP_COMMAND"
	exit 1
fi
echo "All env vars present"
exit 0
`
		err := os.WriteFile(scriptPath, []byte(script), 0755)
		require.NoError(t, err)

		err = ExecuteHook("env-check.sh", ctx)
		assert.NoError(t, err)
	})

	t.Run("executes hook with command prefix", func(t *testing.T) {
		scriptPath := filepath.Join(tmpDir, "with-bash.sh")
		script := "#!/bin/bash\necho 'Executed via bash'\nexit 0\n"
		err := os.WriteFile(scriptPath, []byte(script), 0755)
		require.NoError(t, err)

		err = ExecuteHook("bash with-bash.sh", ctx)
		assert.NoError(t, err)
	})
}

func TestExecuteHooks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := HookContext{
		WorktreePath: tmpDir,
		Branch:       "test-branch",
		RepoPath:     "/tmp/repo",
		Command:      "start",
	}

	t.Run("executes multiple hooks in order", func(t *testing.T) {
		// Create output file to track execution order
		outputFile := filepath.Join(tmpDir, "output.txt")

		script1 := filepath.Join(tmpDir, "hook1.sh")
		err := os.WriteFile(script1, []byte("#!/bin/bash\necho 'hook1' >> output.txt\n"), 0755)
		require.NoError(t, err)

		script2 := filepath.Join(tmpDir, "hook2.sh")
		err = os.WriteFile(script2, []byte("#!/bin/bash\necho 'hook2' >> output.txt\n"), 0755)
		require.NoError(t, err)

		hooks := []string{"hook1.sh", "hook2.sh"}
		err = ExecuteHooks(hooks, ctx)
		assert.NoError(t, err)

		// Check execution order
		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		assert.Equal(t, []string{"hook1", "hook2"}, lines)
	})

	t.Run("stops on first failure", func(t *testing.T) {
		outputFile := filepath.Join(tmpDir, "output2.txt")

		script1 := filepath.Join(tmpDir, "hook-ok.sh")
		err := os.WriteFile(script1, []byte("#!/bin/bash\necho 'ok' >> output2.txt\n"), 0755)
		require.NoError(t, err)

		script2 := filepath.Join(tmpDir, "hook-fail.sh")
		err = os.WriteFile(script2, []byte("#!/bin/bash\necho 'fail' >> output2.txt\nexit 1\n"), 0755)
		require.NoError(t, err)

		script3 := filepath.Join(tmpDir, "hook-never.sh")
		err = os.WriteFile(script3, []byte("#!/bin/bash\necho 'never' >> output2.txt\n"), 0755)
		require.NoError(t, err)

		hooks := []string{"hook-ok.sh", "hook-fail.sh", "hook-never.sh"}
		err = ExecuteHooks(hooks, ctx)
		require.Error(t, err)

		// Check that third hook was not executed
		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.NotContains(t, string(content), "never")
		assert.Contains(t, string(content), "ok")
		assert.Contains(t, string(content), "fail")
	})

	t.Run("succeeds with empty hook list", func(t *testing.T) {
		err := ExecuteHooks([]string{}, ctx)
		assert.NoError(t, err)
	})
}

func TestExecuteHooksWithTimeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "timeout-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := HookContext{
		WorktreePath: tmpDir,
		Branch:       "test-branch",
		RepoPath:     "/tmp/repo",
		Command:      "start",
	}

	t.Run("completes before timeout", func(t *testing.T) {
		scriptPath := filepath.Join(tmpDir, "fast.sh")
		script := "#!/bin/bash\necho 'Fast hook'\nexit 0\n"
		err := os.WriteFile(scriptPath, []byte(script), 0755)
		require.NoError(t, err)

		err = ExecuteHooksWithTimeout([]string{"fast.sh"}, ctx, 5*time.Second)
		assert.NoError(t, err)
	})

	t.Run("times out for slow hook", func(t *testing.T) {
		scriptPath := filepath.Join(tmpDir, "slow.sh")
		script := "#!/bin/bash\nsleep 10\nexit 0\n"
		err := os.WriteFile(scriptPath, []byte(script), 0755)
		require.NoError(t, err)

		err = ExecuteHooksWithTimeout([]string{"slow.sh"}, ctx, 500*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out")
	})
}

func TestBuildHookEnv(t *testing.T) {
	ctx := HookContext{
		WorktreePath: "/path/to/worktree",
		Branch:       "feature-branch",
		RepoPath:     "/path/to/repo",
		Command:      "start",
	}

	env := buildHookEnv(ctx)

	// Check that hop-specific vars are present
	hasWorktreePath := false
	hasBranch := false
	hasRepoPath := false
	hasCommand := false

	for _, e := range env {
		if strings.HasPrefix(e, "HOP_WORKTREE_PATH=") {
			hasWorktreePath = true
			assert.Contains(t, e, "/path/to/worktree")
		}
		if strings.HasPrefix(e, "HOP_BRANCH=") {
			hasBranch = true
			assert.Contains(t, e, "feature-branch")
		}
		if strings.HasPrefix(e, "HOP_REPO_PATH=") {
			hasRepoPath = true
			assert.Contains(t, e, "/path/to/repo")
		}
		if strings.HasPrefix(e, "HOP_COMMAND=") {
			hasCommand = true
			assert.Contains(t, e, "start")
		}
	}

	assert.True(t, hasWorktreePath, "Missing HOP_WORKTREE_PATH")
	assert.True(t, hasBranch, "Missing HOP_BRANCH")
	assert.True(t, hasRepoPath, "Missing HOP_REPO_PATH")
	assert.True(t, hasCommand, "Missing HOP_COMMAND")

	// Should also include system environment
	assert.Greater(t, len(env), 4, "Should include system env vars")
}
