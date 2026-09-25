package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// realPath resolves symlinks (macOS /var -> /private/var) so paths compare
// with what `pwd -P` records.
func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *gitflowEnv) currentBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(e.RunCommand(t, dir, "git", "branch", "--show-current"))
}

func (e *gitflowEnv) branchExists(t *testing.T, branch string) bool {
	t.Helper()
	_, err := e.RunCommandAllowFail(t, e.HubPath, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// A bare hub is no work tree, so git-flow runs in the worktree add creates
// for the branch: git flow start creates the branch there, once, and no
// other worktree changes branch. Remove runs finish in the branch's own
// worktree.
func TestGitflowBareHub_AddAndRemoveRunInWorktree(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)

	e.gitHop(t, "add", "feature/x")

	wt := filepath.Join(e.HubPath, "hops", "feature", "x")
	if got := e.flowCalls(t); strings.Join(got, "|") != "feature start x --no-worktree" {
		t.Fatalf("git-flow calls = %q, want one start", got)
	}
	// The stub records the branch checked out when git-flow started: the
	// worktree is detached, so the branch did not exist before start.
	if got, want := e.flowWhere(t)[0], realPath(t, wt)+" -"; got != want {
		t.Errorf("git flow start ran at %q, want %q", got, want)
	}
	if got := e.currentBranch(t, wt); got != "feature/x" {
		t.Errorf("new worktree is on %q, want feature/x", got)
	}
	if got := e.currentBranch(t, filepath.Join(e.HubPath, "hops", "main")); got != "main" {
		t.Errorf("default-branch worktree switched to %q", got)
	}

	e.gitHop(t, "remove", "feature/x", "--no-prompt")

	calls := e.flowCalls(t)
	if len(calls) != 2 || calls[1] != "feature finish x" {
		t.Fatalf("git-flow calls = %q, want start then finish", calls)
	}
	// The parent (main) is checked out in hops/main, so finish runs in the
	// branch's own worktree with the branch still checked out.
	if got, want := e.flowWhere(t)[1], realPath(t, e.HubPath)+"/hops/feature/x feature/x"; got != want {
		t.Errorf("git flow finish ran at %q, want %q", got, want)
	}
	if e.branchExists(t, "feature/x") {
		t.Error("feature/x survived remove")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir survived remove: %v", err)
	}
}

// With the finish target checked out in no worktree, git-flow-next would
// fall back to the bare hub (and fail) from the branch's worktree, or
// switch whatever worktree it runs in to the target. Remove detaches the
// branch's own worktree, about to go anyway, and finishes from there.
func TestGitflowBareHub_RemoveDetachesWhenParentCheckedOutNowhere(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.RunCommand(t, e.HubPath, "git", "branch", "develop", "main")
	e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature.parent", "develop")

	e.gitHop(t, "add", "feature/x")
	e.gitHop(t, "remove", "feature/x", "--no-prompt")

	where := e.flowWhere(t)
	if len(where) != 2 {
		t.Fatalf("git-flow ran %d times, want 2: %q", len(where), where)
	}
	if got, want := where[1], realPath(t, e.HubPath)+"/hops/feature/x -"; got != want {
		t.Errorf("git flow finish ran at %q, want %q (detached)", got, want)
	}
	if got := e.currentBranch(t, filepath.Join(e.HubPath, "hops", "main")); got != "main" {
		t.Errorf("default-branch worktree switched to %q", got)
	}
}

// An existing branch is checked out, not started: git flow start would
// refuse a branch that already exists.
func TestGitflowBareHub_ExistingBranchNotStarted(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.RunCommand(t, e.HubPath, "git", "branch", "feature/y", "main")

	e.gitHop(t, "add", "feature/y")

	if calls := e.flowCalls(t); len(calls) != 0 {
		t.Errorf("git-flow ran for an existing branch: %q", calls)
	}
	if got := e.currentBranch(t, filepath.Join(e.HubPath, "hops", "feature", "y")); got != "feature/y" {
		t.Errorf("worktree is on %q, want feature/y", got)
	}
}

// A branch that exists only on origin is someone's work to join: it is
// checked out tracking origin, not started afresh by git-flow.
func TestGitflowBareHub_RemoteOnlyBranchNotStarted(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.RunCommand(t, e.SeedRepoPath, "git", "push", "-q", "origin", "main:feature/r")

	e.gitHop(t, "add", "feature/r", "--fetch")

	if calls := e.flowCalls(t); len(calls) != 0 {
		t.Errorf("git-flow ran for a branch on origin: %q", calls)
	}
	wt := filepath.Join(e.HubPath, "hops", "feature", "r")
	if got := strings.TrimSpace(e.RunCommand(t, wt, "git", "rev-parse", "--abbrev-ref", "@{upstream}")); got != "origin/feature/r" {
		t.Errorf("feature/r tracks %q, want origin/feature/r", got)
	}
}

// --from is git flow start's [base].
func TestGitflowBareHub_FromIsStartBase(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.RunCommand(t, e.HubPath, "git", "branch", "other", "main")

	e.gitHop(t, "add", "feature/z", "--from", "other")

	if got := e.flowCalls(t); strings.Join(got, "|") != "feature start z other --no-worktree" {
		t.Errorf("git-flow calls = %q", got)
	}
}

// A git flow start that fails, or that does not leave the new worktree on
// the branch, leaves nothing behind: no worktree, no branch, no hop.json
// entry.
func TestGitflowBareHub_FailedStartRollsBack(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, knob, msg string }{
		{"fails", ".fail", "git flow feature start x failed"},
		{"no checkout", ".nocheckout", "git flow start left"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := setupGitflowEnv(t)
			e.enable(t)
			WriteFile(t, e.flowLog+c.knob, "")
			assertStartRolledBack(t, e, c.msg)
		})
	}
}

func assertStartRolledBack(t *testing.T, e *gitflowEnv, msg string) {
	t.Helper()
	_, stderr, code := e.RunCommandWithExit(t, e.HubPath, e.BinPath, "add", "feature/x")
	if code == 0 {
		t.Fatal("add succeeded although git flow start did not start the branch")
	}
	if !strings.Contains(stderr, msg) {
		t.Errorf("stderr lacks %q:\n%s", msg, stderr)
	}
	if _, err := os.Stat(filepath.Join(e.HubPath, "hops", "feature", "x")); !os.IsNotExist(err) {
		t.Errorf("worktree dir left behind: %v", err)
	}
	if out := e.RunCommand(t, e.HubPath, "git", "worktree", "list", "--porcelain"); strings.Contains(out, "feature/x") {
		t.Errorf("worktree still registered:\n%s", out)
	}
	if e.branchExists(t, "feature/x") {
		t.Error("branch left behind")
	}
	hopJSON, _ := os.ReadFile(filepath.Join(e.HubPath, "hop.json"))
	if strings.Contains(string(hopJSON), "feature/x") {
		t.Errorf("hop.json records feature/x:\n%s", hopJSON)
	}
}

// The real git-flow-next, when installed: a full add/commit/remove cycle
// in a bare hub merges into develop (checked out nowhere) and leaves the
// default-branch worktree alone; the remove needs no --force for the
// unmerged, unpushed branch, and refuses while the worktree is dirty. Started from main with --from, the
// branch still finishes into its type's parent, develop.
func TestGitflowBareHub_RealGitflowNext(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		from []string
		base string
	}{
		{"type start point", nil, "develop"},
		{"from main", []string{"--from", "main"}, "main"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			realGitflowCycle(t, c.from, c.base)
		})
	}
}

func realGitflowCycle(t *testing.T, from []string, wantBase string) {
	if _, err := exec.LookPath("git-flow"); err != nil {
		t.Skip("git-flow not installed")
	}
	if out, err := exec.Command("git", "flow", "version").CombinedOutput(); err != nil || !strings.Contains(string(out), "git-flow-next") {
		t.Skip("git-flow on PATH is not git-flow-next")
	}

	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main", "main:develop")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	for _, kv := range [][2]string{
		{"gitflow.version", "1.0"},
		{"gitflow.initialized", "true"},
		{"gitflow.branch.main.type", "base"},
		{"gitflow.branch.develop.type", "base"},
		{"gitflow.branch.develop.parent", "main"},
		{"gitflow.branch.feature.type", "topic"},
		{"gitflow.branch.feature.parent", "develop"},
		{"gitflow.branch.feature.startpoint", "develop"},
		{"gitflow.branch.feature.prefix", "feat/"},
		{"hop.gitflow.enabled", "true"},
	} {
		env.RunCommand(t, env.HubPath, "git", "config", kv[0], kv[1])
	}
	if out := env.RunCommand(t, env.HubPath, "git", "branch", "--list", "develop"); strings.TrimSpace(out) == "" {
		env.RunCommand(t, env.HubPath, "git", "branch", "develop", "origin/develop")
	}

	env.RunGitHop(t, env.HubPath, append([]string{"add", "feat/z"}, from...)...)

	wt := filepath.Join(env.HubPath, "hops", "feat", "z")
	main := filepath.Join(env.HubPath, "hops", "main")
	if got := strings.TrimSpace(env.RunCommand(t, wt, "git", "branch", "--show-current")); got != "feat/z" {
		t.Fatalf("worktree is on %q, want feat/z", got)
	}
	if got := strings.TrimSpace(env.RunCommand(t, env.HubPath, "git", "config", "gitflow.branch.feat/z.base")); got != wantBase {
		t.Errorf("gitflow.branch.feat/z.base = %q, want %q", got, wantBase)
	}
	if got := strings.TrimSpace(env.RunCommand(t, main, "git", "branch", "--show-current")); got != "main" {
		t.Errorf("default-branch worktree switched to %q", got)
	}

	WriteFile(t, filepath.Join(wt, "feature.txt"), "x\n")
	env.RunCommand(t, wt, "git", "add", "feature.txt")
	env.RunCommand(t, wt, "git", "commit", "-m", "feature")

	// Untracked work in the worktree: finish would run there and the
	// removal then discard it, so the remove is refused, --no-verify or
	// not, before git-flow runs.
	WriteFile(t, filepath.Join(wt, "scratch.txt"), "keep me\n")
	if _, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "remove", "feat/z", "--force", "--no-verify", "--no-prompt"); code == 0 {
		t.Fatalf("remove of a dirty feat/z succeeded\nstderr: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(wt, "scratch.txt")); err != nil {
		t.Fatalf("untracked work lost: %v", err)
	}
	if out := env.RunCommand(t, env.HubPath, "git", "log", "--format=%s", "develop"); strings.Contains(out, "feature") {
		t.Fatalf("develop got the feature from a refused remove:\n%s", out)
	}
	if err := os.Remove(filepath.Join(wt, "scratch.txt")); err != nil {
		t.Fatal(err)
	}

	// Neither merged into main nor pushed, and no flags: finish merges
	// the branch into develop, so remove's not-merged gate does not
	// apply, and the merge keeps the unpushed commit.
	env.RunGitHop(t, env.HubPath, "remove", "feat/z")

	if _, err := env.RunCommandAllowFail(t, env.HubPath, "git", "rev-parse", "--verify", "--quiet", "refs/heads/feat/z"); err == nil {
		t.Error("feat/z survived remove")
	}
	if out := env.RunCommand(t, env.HubPath, "git", "log", "--format=%s", "develop"); !strings.Contains(out, "feature") {
		t.Errorf("develop lacks the finished feature:\n%s", out)
	}
	if got := strings.TrimSpace(env.RunCommand(t, main, "git", "branch", "--show-current")); got != "main" {
		t.Errorf("default-branch worktree switched to %q", got)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir survived remove: %v", err)
	}
}

// The preview names git flow start only for a branch a real run starts.
func TestGitflowBareHub_DryRunExistingBranch(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.RunCommand(t, e.HubPath, "git", "branch", "feature/y", "main")

	out, _ := e.gitHop(t, "add", "feature/y", "-n")
	if strings.Contains(out, "git flow") {
		t.Errorf("preview runs git flow for an existing branch:\n%s", out)
	}
	if !strings.Contains(out, "Would check out existing branch 'feature/y'") {
		t.Errorf("preview lacks the checkout of the existing branch:\n%s", out)
	}
}
