package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hop.top/git/internal/git"
)

// HookContext provides context information for hook execution
type HookContext struct {
	WorktreePath string
	Branch       string
	RepoPath     string
	Command      string // "start", "stop"
	// Out receives the hook's output; nil means os.Stdout.
	Out io.Writer
}

func (c HookContext) out() io.Writer {
	if c.Out == nil {
		return os.Stdout
	}
	return c.Out
}

// hookWaitDelay bounds how long a killed hook's output is waited for,
// should a descendant escape the kill and hold the pipe open.
const hookWaitDelay = 2 * time.Second

// ExecuteHooks executes a list of hooks in order
func ExecuteHooks(hooks []string, ctx HookContext) error {
	return executeHooks(context.Background(), hooks, ctx)
}

func executeHooks(runCtx context.Context, hooks []string, ctx HookContext) error {
	for i, hook := range hooks {
		if err := executeHook(runCtx, hook, ctx); err != nil {
			return fmt.Errorf("hook %d failed: %w", i+1, err)
		}
	}
	return nil
}

// ExecuteHook executes a single hook script
func ExecuteHook(hook string, ctx HookContext) error {
	return executeHook(context.Background(), hook, ctx)
}

// executeHook runs hook until it exits or runCtx ends. When runCtx ends
// the hook is killed along with every process it started, and its output
// is collected before returning: once this returns, nothing the hook
// started writes to ctx.Out.
func executeHook(runCtx context.Context, hook string, ctx HookContext) error {
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
		cmd = exec.CommandContext(runCtx, hookPath)
	} else {
		// Multiple parts - first is command (e.g., "bash"), second is script path
		hookPath = filepath.Join(ctx.WorktreePath, parts[1])
		if _, err := os.Stat(hookPath); os.IsNotExist(err) {
			return fmt.Errorf("hook script not found: %s", parts[1])
		}
		// Build command with absolute path to script
		args := []string{hookPath}
		args = append(args, parts[2:]...)
		cmd = exec.CommandContext(runCtx, parts[0], args...)
	}

	// Set working directory to worktree
	cmd.Dir = ctx.WorktreePath

	// Set environment variables
	cmd.Env = buildHookEnv(ctx)

	// Capture output. Killing only the hook would leave anything it
	// started holding the output pipe (and running); kill the group.
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	afterStart := git.ConfigureProcessGroup(cmd)
	cmd.WaitDelay = hookWaitDelay

	err := cmd.Start()
	if err == nil {
		afterStart()
		// Wait returns only after the output copy has finished.
		err = cmd.Wait()
	}

	// Show output to user
	if output.Len() > 0 {
		fmt.Fprintf(ctx.out(), "  %s\n", output.String())
	}

	if runErr := runCtx.Err(); runErr != nil {
		return runErr
	}
	if err != nil {
		return fmt.Errorf("hook exited with error: %w\nOutput: %s", err, output.String())
	}

	return nil
}

// ExecuteHooksWithTimeout executes hooks with a timeout. A hook still
// running at the deadline is killed with everything it started, and has
// stopped writing to ctx.Out by the time this returns.
func ExecuteHooksWithTimeout(hooks []string, ctx HookContext, timeout time.Duration) error {
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := executeHooks(runCtx, hooks, ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("hook execution timed out after %v", timeout)
	}
	return err
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
