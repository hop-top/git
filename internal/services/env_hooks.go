package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HookContext provides context information for hook execution
type HookContext struct {
	WorktreePath string
	Branch       string
	RepoPath     string
	Command      string // "start", "stop"
}

// ExecuteHooks executes a list of hooks in order
func ExecuteHooks(hooks []string, ctx HookContext) error {
	for i, hook := range hooks {
		if err := ExecuteHook(hook, ctx); err != nil {
			return fmt.Errorf("hook %d failed: %w", i+1, err)
		}
	}
	return nil
}

// ExecuteHook executes a single hook script
func ExecuteHook(hook string, ctx HookContext) error {
	// Parse hook command (could be "bash script.sh" or just "script.sh")
	parts := strings.Fields(hook)
	if len(parts) == 0 {
		return fmt.Errorf("empty hook command")
	}

	var cmd *exec.Cmd
	var hookPath string

	if len(parts) == 1 {
		// Single command - assume it's a script path relative to worktree
		hookPath = filepath.Join(ctx.WorktreePath, parts[0])
		if _, err := os.Stat(hookPath); os.IsNotExist(err) {
			return fmt.Errorf("hook script not found: %s", parts[0])
		}
		cmd = exec.Command(hookPath)
	} else {
		// Multiple parts - first is command (e.g., "bash"), second is script path
		hookPath = filepath.Join(ctx.WorktreePath, parts[1])
		if _, err := os.Stat(hookPath); os.IsNotExist(err) {
			return fmt.Errorf("hook script not found: %s", parts[1])
		}
		// Build command with absolute path to script
		args := []string{hookPath}
		args = append(args, parts[2:]...)
		cmd = exec.Command(parts[0], args...)
	}

	// Set working directory to worktree
	cmd.Dir = ctx.WorktreePath

	// Set environment variables
	cmd.Env = buildHookEnv(ctx)

	// Capture output
	output, err := cmd.CombinedOutput()

	// Show output to user
	if len(output) > 0 {
		fmt.Printf("  %s\n", string(output))
	}

	if err != nil {
		return fmt.Errorf("hook exited with error: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// ExecuteHooksWithTimeout executes hooks with a timeout
func ExecuteHooksWithTimeout(hooks []string, ctx HookContext, timeout time.Duration) error {
	done := make(chan error, 1)

	go func() {
		done <- ExecuteHooks(hooks, ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("hook execution timed out after %v", timeout)
	}
}

// buildHookEnv builds environment variables for hook execution
func buildHookEnv(ctx HookContext) []string {
	// Start with current environment
	env := os.Environ()

	// Add hop-specific variables
	env = append(env,
		fmt.Sprintf("HOP_WORKTREE_PATH=%s", ctx.WorktreePath),
		fmt.Sprintf("HOP_BRANCH=%s", ctx.Branch),
		fmt.Sprintf("HOP_REPO_PATH=%s", ctx.RepoPath),
		fmt.Sprintf("HOP_COMMAND=%s", ctx.Command),
	)

	return env
}
