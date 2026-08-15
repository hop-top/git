package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hop.top/git/internal/hooks"
)

// navHandledHook stands in for a window-per-worktree integration: it does
// its own navigation (here, just recording that it ran), announces it on
// stdout, and exits the reserved code to tell git-hop's shell wrapper not
// to cd on top of it.
const navHandledHook = `#!/bin/bash
MARKER_DIR="$GIT_HOP_TEST_MARKER_DIR"
mkdir -p "$MARKER_DIR"
echo "$GIT_HOP_BRANCH" >> "$MARKER_DIR/nav-handled.log"
echo "NAV_HOOK_STDOUT_MARKER"
exit %s
`

// writeNavHook installs navHandledHook under hooksDir with the given exit
// code.
func writeNavHook(t *testing.T, hooksDir, hookName, exitCode string) {
	t.Helper()
	path := filepath.Join(hooksDir, hookName)
	WriteFile(t, path, strings.Replace(navHandledHook, "%s", exitCode, 1))
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("Failed to make %s executable: %v", hookName, err)
	}
}

// TestSwitchNavDirective_ReservedCodeReachesProcessExit is the end-to-end
// proof of the whole chain: a post-worktree-switch hook exits the reserved
// code, the runner reads it as a typed result rather than an error, the
// switch path re-raises it, and a real caller of the real binary observes
// it as git-hop's own process exit status.
//
// That process exit status is the ONLY channel the generated shell wrapper
// can observe -- it runs `command git hop "$@"` and reads `$?`. If this
// assertion fails, the wrapper can never learn that navigation was handled.
func TestSwitchNavDirective_ReservedCodeReachesProcessExit(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	hooksDir := setupSwitchHookEnv(t, env)

	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	env.RunGitHop(t, env.HubPath, "add", "feature-b")

	writeNavHook(t, hooksDir, "post-worktree-switch", strconv.Itoa(hooks.ExitNavigationHandled))

	stdout, stderr, exitCode := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "feature-a")
	t.Logf("stdout:\n%s\nstderr:\n%s", stdout, stderr)

	if exitCode != hooks.ExitNavigationHandled {
		t.Fatalf("exit code = %d, want %d (the handled-navigation directive never reached the process exit status)",
			exitCode, hooks.ExitNavigationHandled)
	}

	// The hook's own stdout must survive the handled path -- a hook that
	// took over navigation still gets to tell the user what it did.
	if !strings.Contains(stdout, "NAV_HOOK_STDOUT_MARKER") {
		t.Errorf("hook stdout missing from git-hop stdout; got:\n%s", stdout)
	}

	// The switch itself must still have COMPLETED. The reserved code says
	// "navigation handled", not "switch aborted", so `current` must point
	// at the target.
	target, err := os.Readlink(filepath.Join(env.HubPath, "current"))
	if err != nil {
		t.Fatalf("current symlink unreadable after handled switch: %v", err)
	}
	if want := filepath.Join("hops", "feature-a"); target != want {
		t.Errorf("current = %q, want %q -- handled-navigation must not abort the switch", target, want)
	}

	// And the hook really did run, for the branch we asked for.
	log, err := os.ReadFile(filepath.Join(env.RootDir, "markers", "nav-handled.log"))
	if err != nil {
		t.Fatalf("nav hook log not found: %v", err)
	}
	if got := strings.TrimSpace(string(log)); got != "feature-a" {
		t.Errorf("nav hook log = %q, want feature-a", got)
	}
}

// TestSwitchNavDirective_UnawareHookStillExitsZero pins the binding
// constraint: a post-worktree-switch hook that knows nothing about the
// directive must behave exactly as it did before the mechanism existed.
func TestSwitchNavDirective_UnawareHookStillExitsZero(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	hooksDir := setupSwitchHookEnv(t, env)

	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	env.RunGitHop(t, env.HubPath, "add", "feature-b")

	writeNavHook(t, hooksDir, "post-worktree-switch", "0")

	_, stderr, exitCode := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "feature-a")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0 for a hook that exits 0; stderr:\n%s", exitCode, stderr)
	}
}

// TestSwitchNavDirective_GenericFailureStillWarns pins the third class: a
// post-worktree-switch hook exiting a generic non-zero code is still a
// failure, and is still only a warning (the switch has already landed by
// the time the post- hook runs), so git-hop itself still exits 0.
func TestSwitchNavDirective_GenericFailureStillWarns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	hooksDir := setupSwitchHookEnv(t, env)

	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	env.RunGitHop(t, env.HubPath, "add", "feature-b")

	writeNavHook(t, hooksDir, "post-worktree-switch", "1")

	_, stderr, exitCode := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "feature-a")
	t.Logf("stderr:\n%s", stderr)

	if exitCode == hooks.ExitNavigationHandled {
		t.Fatalf("generic hook failure was mistaken for the handled-navigation directive (exit %d)", exitCode)
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 -- a failing post- hook only warns", exitCode)
	}
	if !strings.Contains(stderr, "post-worktree-switch") {
		t.Errorf("expected a warning naming post-worktree-switch, got stderr:\n%s", stderr)
	}
}
