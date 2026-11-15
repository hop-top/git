package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// removeHub is a hub with an initial commit on main and no other branch.
func removeHub(t *testing.T) *TestEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	return env
}

// localBranches lists the hub's local branches.
func localBranches(t *testing.T, env *TestEnv) []string {
	t.Helper()
	out := env.RunCommand(t, env.HubPath, "git", "branch", "--format=%(refname:short)")
	return strings.Fields(out)
}

func hasBranch(t *testing.T, env *TestEnv, name string) bool {
	t.Helper()
	for _, b := range localBranches(t, env) {
		if b == name {
			return true
		}
	}
	return false
}

// worktreeListed reports whether git's registry lists a worktree at path.
func worktreeListed(t *testing.T, env *TestEnv, path string) bool {
	t.Helper()
	out := env.RunCommand(t, env.HubPath, "git", "worktree", "list", "--porcelain")
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err == nil {
		path = filepath.Join(resolved, filepath.Base(path))
	}
	return strings.Contains(out, "worktree "+path+"\n")
}

type removeRec struct {
	Kind             string `json:"kind"`
	Branch           string `json:"branch"`
	LocalBranch      string `json:"local_branch"`
	Removed          bool   `json:"removed"`
	BranchDeleted    bool   `json:"branch_deleted"`
	BranchKeptReason string `json:"branch_kept_reason"`
	Reason           string `json:"reason"`
	DryRun           bool   `json:"dry_run"`
}

// removeJSON runs git hop remove with --format json and decodes the records.
func removeJSON(t *testing.T, env *TestEnv, args ...string) ([]removeRec, string, int) {
	t.Helper()
	full := append([]string{"remove", "--format", "json"}, args...)
	stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, full...)
	var recs []removeRec
	if strings.TrimSpace(stdout) != "" {
		if err := json.Unmarshal([]byte(stdout), &recs); err != nil {
			t.Fatalf("decode %q: %v\nstderr: %s", stdout, err, stderr)
		}
	}
	return recs, stdout + stderr, code
}

func worktreeRec(t *testing.T, recs []removeRec, branch string) removeRec {
	t.Helper()
	for _, r := range recs {
		if r.Kind == "worktree" && r.Branch == branch {
			return r
		}
	}
	t.Fatalf("no worktree record for %q in %+v", branch, recs)
	return removeRec{}
}

// A registered hop removed clean and merged takes its branch with it.
func TestRemoveBranchCleanup_RegisteredHopBranchGone(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")

	recs, out, code := removeJSON(t, env, "--no-prompt", "feat/a")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if hasBranch(t, env, "feat/a") {
		t.Errorf("local branch feat/a left behind")
	}
	if r := worktreeRec(t, recs, "feat/a"); !r.BranchDeleted || r.BranchKeptReason != "" {
		t.Errorf("record = %+v, want branch_deleted and no kept reason", r)
	}
}

// The branch deleted is the one checked out in the worktree, not the
// hop.json key.
func TestRemoveBranchCleanup_WorktreeOnOtherBranch(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	wt := filepath.Join(env.HubPath, "hops", "feat", "a")
	env.RunCommand(t, wt, "git", "checkout", "-b", "other")

	recs, out, code := removeJSON(t, env, "--no-prompt", "feat/a")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if hasBranch(t, env, "other") {
		t.Errorf("branch checked out in the worktree (other) left behind")
	}
	if !hasBranch(t, env, "feat/a") {
		t.Errorf("feat/a, not checked out in the worktree, was deleted")
	}
	r := worktreeRec(t, recs, "feat/a")
	if !r.BranchDeleted || r.LocalBranch != "other" {
		t.Errorf("record = %+v, want branch_deleted with local_branch other", r)
	}
}

// A failed branch deletion is reported with its reason and is not buried
// under a bare success line.
func TestRemoveBranchCleanup_DeleteFailureReported(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	lock := filepath.Join(env.HubPath, "refs", "heads", "feat", "a.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	recs, out, _ := removeJSON(t, env, "--no-prompt", "feat/a")
	r := worktreeRec(t, recs, "feat/a")
	if !r.Removed || r.BranchDeleted || r.BranchKeptReason == "" {
		t.Errorf("record = %+v, want removed, branch kept, with a reason", r)
	}
	if !hasBranch(t, env, "feat/a") {
		t.Errorf("branch unexpectedly gone")
	}

	os.Remove(lock)
	env.RunGitHop(t, env.HubPath, "add", "feat/b")
	lockB := filepath.Join(env.HubPath, "refs", "heads", "feat", "b.lock")
	if err := os.WriteFile(lockB, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	human := env.RunGitHopCombined(t, env.HubPath, "remove", "--no-prompt", "feat/b")
	if strings.Contains(human, "Successfully removed feat/b\n") {
		t.Errorf("bare success line with the branch left behind:\n%s", human)
	}
	if !strings.Contains(human, "feat/b") || !strings.Contains(human, "kept") {
		t.Errorf("human output does not name the kept branch:\n%s", human)
	}
	_ = out
}

// --keep-branch and hop.remove.keepBranch keep the branch and say so.
func TestRemoveBranchCleanup_KeepBranch(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	env.RunGitHop(t, env.HubPath, "add", "feat/b")

	recs, out, code := removeJSON(t, env, "--no-prompt", "--keep-branch", "feat/a")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !hasBranch(t, env, "feat/a") {
		t.Errorf("--keep-branch deleted the branch")
	}
	r := worktreeRec(t, recs, "feat/a")
	if r.BranchDeleted || !strings.Contains(r.BranchKeptReason, "keep") {
		t.Errorf("record = %+v, want kept with a keep reason", r)
	}

	env.RunCommand(t, env.HubPath, "git", "config", "hop.remove.keepBranch", "true")
	recs, _, _ = removeJSON(t, env, "--no-prompt", "feat/b")
	if !hasBranch(t, env, "feat/b") {
		t.Errorf("hop.remove.keepBranch did not keep the branch")
	}
	if r := worktreeRec(t, recs, "feat/b"); r.BranchDeleted || r.BranchKeptReason == "" {
		t.Errorf("record = %+v, want kept with a reason", r)
	}
}

// --merged removes every merged worktree's branch too.
func TestRemoveBranchCleanup_MergedBulk(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	for _, b := range []string{"feat/a", "feat/b"} {
		env.RunGitHop(t, env.HubPath, "add", b)
	}
	recs, out, code := removeJSON(t, env, "--merged", "--no-prompt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, b := range []string{"feat/a", "feat/b"} {
		if hasBranch(t, env, b) {
			t.Errorf("branch %s left behind", b)
		}
		if r := worktreeRec(t, recs, b); !r.BranchDeleted {
			t.Errorf("record for %s = %+v", b, r)
		}
	}
}

// A squash-merged branch passes the gate and its branch goes (git branch
// -d would refuse it).
func TestRemoveBranchCleanup_SquashMerged(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	wt := filepath.Join(env.HubPath, "hops", "feat", "a")
	WriteFile(t, filepath.Join(wt, "a.txt"), "a")
	env.RunCommand(t, wt, "git", "add", "a.txt")
	env.RunCommand(t, wt, "git", "commit", "-m", "add a")
	mainWt := filepath.Join(env.HubPath, "hops", "main")
	env.RunCommand(t, mainWt, "git", "merge", "--squash", "feat/a")
	env.RunCommand(t, mainWt, "git", "commit", "-m", "squash a")

	_, out, code := removeJSON(t, env, "--no-prompt", "--no-verify", "feat/a")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if hasBranch(t, env, "feat/a") {
		t.Errorf("squash-merged branch left behind")
	}
}

// --force removes an unmerged branch.
func TestRemoveBranchCleanup_ForceUnmerged(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	wt := filepath.Join(env.HubPath, "hops", "feat", "a")
	WriteFile(t, filepath.Join(wt, "a.txt"), "a")
	env.RunCommand(t, wt, "git", "add", "a.txt")
	env.RunCommand(t, wt, "git", "commit", "-m", "add a")

	if _, _, code := removeJSON(t, env, "--no-prompt", "feat/a"); code == 0 {
		t.Fatalf("unmerged removal passed the gate without --force")
	}
	if !hasBranch(t, env, "feat/a") {
		t.Fatalf("refused removal still deleted the branch")
	}
	_, out, code := removeJSON(t, env, "--no-prompt", "--force", "--no-verify", "feat/a")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if hasBranch(t, env, "feat/a") {
		t.Errorf("--force left the branch behind")
	}
}

// The preview reports what the run then does.
func TestRemoveBranchCleanup_DryRunMatchesRun(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	env.RunCommand(t, filepath.Join(env.HubPath, "hops", "feat", "a"), "git", "checkout", "-b", "other")

	dry, _, code := removeJSON(t, env, "--dry-run", "--no-prompt", "feat/a")
	if code != 0 {
		t.Fatalf("dry-run exit %d", code)
	}
	if !hasBranch(t, env, "other") {
		t.Fatalf("dry-run deleted a branch")
	}
	run, _, _ := removeJSON(t, env, "--no-prompt", "feat/a")
	d, r := worktreeRec(t, dry, "feat/a"), worktreeRec(t, run, "feat/a")
	d.DryRun = false
	if d != r {
		t.Errorf("dry-run record %+v != run record %+v", d, r)
	}
}

// addPlain adds a worktree with plain git, leaving hop.json unaware of it.
func addPlain(t *testing.T, env *TestEnv, branch string) string {
	t.Helper()
	path := filepath.Join(env.HubPath, "hops", "plain-"+strings.ReplaceAll(branch, "/", "-"))
	env.RunCommand(t, env.HubPath, "git", "worktree", "add", "-b", branch, path, "main")
	return path
}

func TestRemoveBranchCleanup_UnregisteredWorktree(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	path := addPlain(t, env, "plain/x")

	recs, out, code := removeJSON(t, env, "--no-prompt", "plain/x")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("worktree dir left behind")
	}
	if hasBranch(t, env, "plain/x") {
		t.Errorf("branch left behind")
	}
	if r := worktreeRec(t, recs, "plain/x"); !r.Removed || !r.BranchDeleted {
		t.Errorf("record = %+v", r)
	}
}

func TestRemoveBranchCleanup_UnregisteredUnmergedIsGated(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	path := addPlain(t, env, "plain/x")
	WriteFile(t, filepath.Join(path, "x.txt"), "x")
	env.RunCommand(t, path, "git", "add", "x.txt")
	env.RunCommand(t, path, "git", "commit", "-m", "x")

	if _, _, code := removeJSON(t, env, "--no-prompt", "plain/x"); code == 0 {
		t.Fatalf("unmerged unregistered worktree removed without --force")
	}
	if !hasBranch(t, env, "plain/x") {
		t.Errorf("refused removal deleted the branch")
	}
}

func TestRemoveBranchCleanup_MergedCoversUnregistered(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	mergedPath := addPlain(t, env, "plain/merged")
	openPath := addPlain(t, env, "plain/open")
	WriteFile(t, filepath.Join(openPath, "x.txt"), "x")
	env.RunCommand(t, openPath, "git", "add", "x.txt")
	env.RunCommand(t, openPath, "git", "commit", "-m", "x")

	_, out, _ := removeJSON(t, env, "--merged", "--no-prompt")
	if _, err := os.Stat(mergedPath); err == nil {
		t.Errorf("merged unregistered worktree left behind:\n%s", out)
	}
	if hasBranch(t, env, "plain/merged") || hasBranch(t, env, "feat/a") {
		t.Errorf("merged branches left behind: %v", localBranches(t, env))
	}
	if _, err := os.Stat(openPath); err != nil || !hasBranch(t, env, "plain/open") {
		t.Errorf("unmerged unregistered worktree touched")
	}
}

// prune clears git's record of an unregistered worktree whose directory
// is gone, and hop.json's entry for a deleted worktree.
func TestPruneUnregisteredAndStaleEntries(t *testing.T) {
	t.Parallel()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "feat/a")
	path := addPlain(t, env, "plain/x")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	// feat/a deleted with plain git: hop.json keeps its entry.
	env.RunCommand(t, env.HubPath, "git", "worktree", "remove", "--force", filepath.Join(env.HubPath, "hops", "feat", "a"))
	env.RunCommand(t, env.HubPath, "git", "branch", "-D", "feat/a")

	env.RunGitHop(t, env.HubPath, "prune")

	if worktreeListed(t, env, path) {
		t.Errorf("git still lists the deleted unregistered worktree")
	}
	hopJSON, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(hopJSON), "feat/a") {
		t.Errorf("hop.json still has an entry for the deleted worktree:\n%s", hopJSON)
	}
}
