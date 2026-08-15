package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// recordSwitchHook is a hook script that appends one line per invocation to
// a shared log, capturing the switch-specific env the hook was handed.
// Interpolate the hook name; GIT_HOP_FROM_* are deliberately unquoted-absent
// when git-hop omits them, so the log records an empty value in that case.
const recordSwitchHook = `#!/bin/bash
set -e
MARKER_DIR="$GIT_HOP_TEST_MARKER_DIR"
mkdir -p "$MARKER_DIR"
{
  echo "hook=$GIT_HOP_HOOK_NAME"
  echo "branch=$GIT_HOP_BRANCH"
  echo "worktree=$GIT_HOP_WORKTREE_PATH"
  echo "from_branch=$GIT_HOP_FROM_BRANCH"
  echo "from_worktree=$GIT_HOP_FROM_WORKTREE_PATH"
  echo "trigger=$GIT_HOP_TRIGGER"
  echo "branch_type=$GIT_HOP_BRANCH_TYPE"
  echo "---"
} >> "$MARKER_DIR/switch.log"
exit %s
`

// setupSwitchHookEnv builds a hub with two extra worktrees ready to hop
// between, and returns the hub's .git-hop/hooks dir.
func setupSwitchHookEnv(t *testing.T, env *TestEnv) string {
	t.Helper()

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}
	return hooksDir
}

// writeSwitchHook installs the recording hook under hooksDir with the given
// exit code.
func writeSwitchHook(t *testing.T, hooksDir, hookName, exitCode string) {
	t.Helper()
	path := filepath.Join(hooksDir, hookName)
	WriteFile(t, path, strings.Replace(recordSwitchHook, "%s", exitCode, 1))
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("Failed to make %s executable: %v", hookName, err)
	}
}

// readSwitchRecords splits the hook log into one map per invocation, in the
// order the hooks fired.
func readSwitchRecords(t *testing.T, env *TestEnv) []map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.RootDir, "markers", "switch.log"))
	if err != nil {
		t.Fatalf("switch hook log not found: %v", err)
	}
	var records []map[string]string
	cur := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "---" {
			records = append(records, cur)
			cur = map[string]string{}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			cur[k] = v
		}
	}
	return records
}

// evalPath resolves symlinks so path comparisons survive macOS's
// /var -> /private/var temp-dir aliasing. An unresolvable path is returned
// verbatim so a genuinely wrong value still shows up in the failure diff
// rather than being normalized away.
func evalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func TestSwitchHooks_FireInOrderWithFromState(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	hooksDir := setupSwitchHookEnv(t, env)

	// Worktrees to hop between. `add` also moves `current`, so after these
	// two adds `current` points at feature-b.
	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	env.RunGitHop(t, env.HubPath, "add", "feature-b")

	writeSwitchHook(t, hooksDir, "pre-worktree-switch", "0")
	writeSwitchHook(t, hooksDir, "post-worktree-switch", "0")

	env.RunGitHop(t, env.HubPath, "feature-a")

	records := readSwitchRecords(t, env)
	if len(records) != 2 {
		t.Fatalf("expected 2 hook invocations, got %d: %+v", len(records), records)
	}

	pre, post := records[0], records[1]

	if pre["hook"] != "pre-worktree-switch" {
		t.Errorf("first hook = %q, want pre-worktree-switch", pre["hook"])
	}
	if post["hook"] != "post-worktree-switch" {
		t.Errorf("second hook = %q, want post-worktree-switch", post["hook"])
	}

	// git-hop records the paths git resolved, and on macOS the temp root is
	// itself a symlink (/var -> /private/var). EvalSymlinks both sides so the
	// comparison is on real paths, not on which alias the test happened to
	// build.
	wantTo := evalPath(t, filepath.Join(env.HubPath, "hops", "feature-a"))
	wantFrom := evalPath(t, filepath.Join(env.HubPath, "hops", "feature-b"))

	for _, rec := range records {
		if rec["branch"] != "feature-a" {
			t.Errorf("%s: branch = %q, want feature-a", rec["hook"], rec["branch"])
		}
		if got := evalPath(t, rec["worktree"]); got != wantTo {
			t.Errorf("%s: worktree = %q, want %q", rec["hook"], got, wantTo)
		}
		if rec["from_branch"] != "feature-b" {
			t.Errorf("%s: from_branch = %q, want feature-b", rec["hook"], rec["from_branch"])
		}
		if got := evalPath(t, rec["from_worktree"]); got != wantFrom {
			t.Errorf("%s: from_worktree = %q, want %q", rec["hook"], got, wantFrom)
		}
		if rec["trigger"] != "hop" {
			t.Errorf("%s: trigger = %q, want hop", rec["hook"], rec["trigger"])
		}
	}
}

func TestSwitchHooks_FailingPreLeavesSymlinkUntouched(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	hooksDir := setupSwitchHookEnv(t, env)

	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	env.RunGitHop(t, env.HubPath, "add", "feature-b")

	currentPath := filepath.Join(env.HubPath, "current")
	before, err := os.Readlink(currentPath)
	if err != nil {
		t.Fatalf("failed to read current symlink before switch: %v", err)
	}

	// pre- rejects; post- would record if it ever ran.
	writeSwitchHook(t, hooksDir, "pre-worktree-switch", "1")
	writeSwitchHook(t, hooksDir, "post-worktree-switch", "0")

	cmd := exec.Command(env.BinPath, "feature-a")
	cmd.Dir = env.HubPath
	cmd.Env = env.EnvVars
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("switch should have failed when pre-worktree-switch exits non-zero, got: %s", out)
	}
	t.Logf("blocked switch output:\n%s", out)

	after, err := os.Readlink(currentPath)
	if err != nil {
		t.Fatalf("failed to read current symlink after blocked switch: %v", err)
	}
	if after != before {
		t.Errorf("current symlink changed despite failing pre-worktree-switch: %q -> %q", before, after)
	}

	records := readSwitchRecords(t, env)
	for _, rec := range records {
		if rec["hook"] == "post-worktree-switch" {
			t.Errorf("post-worktree-switch ran despite failing pre- hook: %+v", rec)
		}
	}
	if len(records) != 1 || records[0]["hook"] != "pre-worktree-switch" {
		t.Errorf("expected exactly one pre-worktree-switch record, got %+v", records)
	}
}

func TestSwitchHooks_NoCurrentSymlinkOmitsFromState(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	hooksDir := setupSwitchHookEnv(t, env)

	env.RunGitHop(t, env.HubPath, "add", "feature-a")

	// Drop `current` entirely: the first-hop-after-clone shape. From-state
	// must come back empty rather than erroring the switch.
	if err := os.Remove(filepath.Join(env.HubPath, "current")); err != nil {
		t.Fatalf("failed to remove current symlink: %v", err)
	}

	writeSwitchHook(t, hooksDir, "pre-worktree-switch", "0")
	writeSwitchHook(t, hooksDir, "post-worktree-switch", "0")

	env.RunGitHop(t, env.HubPath, "feature-a")

	records := readSwitchRecords(t, env)
	if len(records) != 2 {
		t.Fatalf("expected 2 hook invocations, got %d: %+v", len(records), records)
	}
	for _, rec := range records {
		if rec["from_branch"] != "" {
			t.Errorf("%s: from_branch = %q, want empty", rec["hook"], rec["from_branch"])
		}
		if rec["from_worktree"] != "" {
			t.Errorf("%s: from_worktree = %q, want empty", rec["hook"], rec["from_worktree"])
		}
		if rec["trigger"] != "hop" {
			t.Errorf("%s: trigger = %q, want hop", rec["hook"], rec["trigger"])
		}
	}

	// The switch still lands: `current` is recreated pointing at feature-a.
	target, err := os.Readlink(filepath.Join(env.HubPath, "current"))
	if err != nil {
		t.Fatalf("current symlink not recreated: %v", err)
	}
	if want := filepath.Join("hops", "feature-a"); target != want {
		t.Errorf("current = %q, want %q", target, want)
	}
}
