package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `git hop init` creates the hub's initial worktree (hops/<branch>) the
// same way clone does, so it dispatches the same worktree hook clone does:
// post-worktree-add, for that worktree, after committed hooks are mirrored.
// Clone fires no pre-worktree-add, and neither does init.

// initWithLifecycleHooks seeds a plain repo, installs the recorder under
// every lifecycle name at global level, and returns the repo path.
func initWithLifecycleHooks(t *testing.T, env *TestEnv) string {
	t.Helper()
	repoPath := seedPlainRepo(t, env, "proj", "main")
	installLifecycleHooks(t, env)
	return repoPath
}

func TestInitLifecycleHooks_FiresPostWorktreeAddForInitialWorktree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := initWithLifecycleHooks(t, env)

	env.RunGitHop(t, repoPath, "init", "--no-prompt")

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"post-worktree-add"})
	rec := records[0]
	if got := rec["branch"]; got != "main" {
		t.Errorf("post-worktree-add branch = %q; want main", got)
	}
	// Compare against the resolved path: the temp root may sit behind a
	// symlink (macOS /var -> /private/var).
	want := filepath.Join(repoPath, "hops", "main")
	if got := rec["worktree"]; got != want && !samePath(t, got, want) {
		t.Errorf("post-worktree-add worktree = %q; want %q", got, want)
	}
	if got := rec["worktree_dir"]; got != "present" {
		t.Errorf("post-worktree-add ran before the worktree existed (worktree_dir=%s)", got)
	}
}

// A post-worktree-add committed to the repo being converted lives in the
// new worktree's .git-hop/hooks/ and must apply to that same worktree, as
// it does for clone.
func TestInitLifecycleHooks_CommittedRepoHookFires(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := seedPlainRepo(t, env, "proj", "main")

	hook := filepath.Join(repoPath, ".git-hop", "hooks", "post-worktree-add")
	if err := os.MkdirAll(filepath.Dir(hook), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	WriteFile(t, hook, strings.Replace(recordLifecycleHook, "%s", "0", 1))
	if err := os.Chmod(hook, 0755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	env.RunCommand(t, repoPath, "git", "add", ".git-hop")
	env.RunCommand(t, repoPath, "git", "commit", "-m", "add hook")

	env.RunGitHop(t, repoPath, "init", "--no-prompt", "--hooks", "none")

	assertHookSeq(t, readLifecycleRecords(t, env), []string{"post-worktree-add"})
}

func TestInitLifecycleHooks_DryRunPreviewsWithoutRunning(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := initWithLifecycleHooks(t, env)

	out := env.RunGitHopCombined(t, repoPath, "init", "--no-prompt", "--dry-run")

	if records := readLifecycleRecords(t, env); len(records) != 0 {
		t.Fatalf("--dry-run ran hooks: %v", hookSeq(records))
	}
	want := "Would run hook post-worktree-add (" + filepath.Join(globalHooksDir(env), "post-worktree-add") + ")"
	if !strings.Contains(out, want) {
		t.Errorf("dry-run output missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "pre-worktree-add") {
		t.Errorf("dry-run previews pre-worktree-add, which init does not dispatch:\n%s", out)
	}
}

func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
