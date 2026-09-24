package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `git hop init` leaves the current branch in an initial worktree
// (hops/<branch> for a bare conversion, the repo root for --regular), so it
// dispatches the same worktree hook clone does: post-worktree-add, for that
// worktree, after committed hooks are mirrored. Clone fires no
// pre-worktree-add, and neither does init.

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

// --no-hooks turns off every hook init would touch: no hooks dir, no
// committed-hook mirror, and no lifecycle dispatch.
func TestInitLifecycleHooks_NoHooksSkipsDispatch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := initWithLifecycleHooks(t, env)

	env.RunGitHop(t, repoPath, "init", "--no-prompt", "--no-hooks")

	if records := readLifecycleRecords(t, env); len(records) != 0 {
		t.Fatalf("--no-hooks ran hooks: %v", hookSeq(records))
	}
}

// The dry-run preview matches the real run: with --no-hooks it names no
// hook, because none would run.
func TestInitLifecycleHooks_NoHooksDryRunPreviewsNone(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := initWithLifecycleHooks(t, env)

	out := env.RunGitHopCombined(t, repoPath, "init", "--no-prompt", "--no-hooks", "--dry-run")

	if records := readLifecycleRecords(t, env); len(records) != 0 {
		t.Fatalf("--dry-run ran hooks: %v", hookSeq(records))
	}
	if strings.Contains(out, "Would run hook") {
		t.Errorf("--no-hooks dry-run previews a hook that will not run:\n%s", out)
	}
}

// A regular conversion keeps the repo root as the working tree of the
// current branch. That root is the conversion's initial worktree, so it
// gets the same post-worktree-add a bare conversion gives hops/<branch>,
// with the same variables.
func TestInitLifecycleHooks_RegularFiresPostWorktreeAddForRepoRoot(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := initWithLifecycleHooks(t, env)

	env.RunGitHop(t, repoPath, "init", "--no-prompt", "--regular")

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"post-worktree-add"})
	rec := records[0]
	if got := rec["branch"]; got != "main" {
		t.Errorf("post-worktree-add branch = %q; want main", got)
	}
	if got := rec["worktree"]; got != repoPath && !samePath(t, got, repoPath) {
		t.Errorf("post-worktree-add worktree = %q; want repo root %q", got, repoPath)
	}
	if got, want := rec["repo_id"], "github.com/"+filepath.Base(env.RootDir)+"/proj"; got != want {
		t.Errorf("post-worktree-add repo ID = %q; want %q", got, want)
	}
}

// A post-worktree-add committed to a repo converted with --regular sits
// in the repo root's .git-hop/hooks/ and applies to that root.
func TestInitLifecycleHooks_RegularCommittedRepoHookFires(t *testing.T) {
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

	env.RunGitHop(t, repoPath, "init", "--no-prompt", "--regular", "--hooks", "none")

	assertHookSeq(t, readLifecycleRecords(t, env), []string{"post-worktree-add"})
}

func TestInitLifecycleHooks_RegularNoHooksSkipsDispatch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := initWithLifecycleHooks(t, env)

	env.RunGitHop(t, repoPath, "init", "--no-prompt", "--regular", "--no-hooks")

	if records := readLifecycleRecords(t, env); len(records) != 0 {
		t.Fatalf("--regular --no-hooks ran hooks: %v", hookSeq(records))
	}
}

func TestInitLifecycleHooks_RegularDryRunPreviewsWithoutRunning(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := initWithLifecycleHooks(t, env)

	out := env.RunGitHopCombined(t, repoPath, "init", "--no-prompt", "--regular", "--dry-run")

	if records := readLifecycleRecords(t, env); len(records) != 0 {
		t.Fatalf("--dry-run ran hooks: %v", hookSeq(records))
	}
	want := "Would run hook post-worktree-add (" + filepath.Join(globalHooksDir(env), "post-worktree-add") + ")"
	if !strings.Contains(out, want) {
		t.Errorf("dry-run output missing %q:\n%s", want, out)
	}
}

func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
