package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commitInWorktree adds a commit to the branch checked out at dir.
func (e *gitflowEnv) commitInWorktree(t *testing.T, dir, file string) {
	t.Helper()
	WriteFile(t, filepath.Join(dir, file), file+"\n")
	e.RunCommand(t, dir, "git", "add", file)
	e.RunCommand(t, dir, "git", "commit", "-q", "-m", file)
}

// gitHopFails runs git-hop in the hub and fails the test unless it exits
// non-zero.
func (e *gitflowEnv) gitHopFails(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := e.RunCommandWithExit(t, e.HubPath, e.BinPath, args...)
	if code == 0 {
		t.Fatalf("git-hop %v exited 0\nstdout: %s\nstderr: %s", args, stdout, stderr)
	}
	return stdout, stderr
}

// With hop.gitflow.enabled, remove's not-merged gate does not apply to a
// branch git flow finish handles: finish merges it into the type's parent
// before the worktree goes. Its unpushed commits need no --no-verify
// either; the merge keeps them, on the parent.
func TestGitflowRemoveGate_FinishSkipsNotMergedGate(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.gitHop(t, "add", "feature/x")
	wt := filepath.Join(e.HubPath, "hops", "feature", "x")
	e.commitInWorktree(t, wt, "work.txt")

	e.gitHop(t, "remove", "feature/x")

	calls := e.flowCalls(t)
	if len(calls) != 2 || calls[1] != "feature finish x" {
		t.Fatalf("git-flow calls = %q, want start then finish", calls)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree survived remove: %v", err)
	}
}

// The gate is skipped only for a branch finish runs for: with git-flow
// off, or for a branch no git-flow type matches, unmerged work still
// needs --force (and --no-verify, unpushed).
func TestGitflowRemoveGate_OtherBranchesStillGated(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		enabled bool
		branch  string
		wtPath  []string
	}{
		{"git-flow off", false, "feature/x", []string{"feature", "x"}},
		{"not a git-flow type", true, "plain", []string{"plain"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := setupGitflowEnv(t)
			if c.enabled {
				e.enable(t)
			}
			e.gitHop(t, "add", c.branch)
			wt := filepath.Join(append([]string{e.HubPath, "hops"}, c.wtPath...)...)
			e.commitInWorktree(t, wt, "work.txt")

			_, stderr := e.gitHopFails(t, "remove", c.branch, "--no-prompt")
			if !strings.Contains(stderr, "not merged into default") || !strings.Contains(stderr, "--force --no-verify") {
				t.Errorf("refusal does not ask for --force --no-verify:\n%s", stderr)
			}
			if _, err := os.Stat(wt); err != nil {
				t.Errorf("worktree removed despite the gate: %v", err)
			}
			for _, call := range e.flowCalls(t) {
				if strings.Contains(call, "finish") {
					t.Errorf("git flow finish ran for a refused remove: %q", call)
				}
			}
		})
	}
}

// Finish runs in the branch's worktree, which the removal then deletes,
// so a dirty worktree is refused before finish runs, whatever the flags:
// --no-verify does not let uncommitted or untracked work go.
func TestGitflowRemoveGate_DirtyWorktreeRefusedWithAnyFlags(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		dirty func(t *testing.T, e *gitflowEnv, wt string)
	}{
		{"untracked", func(t *testing.T, e *gitflowEnv, wt string) {
			WriteFile(t, filepath.Join(wt, "scratch.txt"), "keep me\n")
		}},
		{"modified", func(t *testing.T, e *gitflowEnv, wt string) {
			WriteFile(t, filepath.Join(wt, "work.txt"), "changed\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := setupGitflowEnv(t)
			e.enable(t)
			e.gitHop(t, "add", "feature/x")
			wt := filepath.Join(e.HubPath, "hops", "feature", "x")
			e.commitInWorktree(t, wt, "work.txt")
			c.dirty(t, e, wt)

			for _, args := range [][]string{
				{"remove", "feature/x", "--no-prompt"},
				{"remove", "feature/x", "--no-prompt", "--no-verify"},
				{"remove", "feature/x", "--no-prompt", "--force", "--no-verify"},
			} {
				_, stderr := e.gitHopFails(t, args...)
				if !strings.Contains(stderr, "git flow finish runs in it") {
					t.Errorf("git-hop %v: refusal does not name finish:\n%s", args, stderr)
				}
			}
			if calls := e.flowCalls(t); len(calls) != 1 {
				t.Errorf("git flow ran past the gate: %q", calls)
			}
			if status := e.RunCommand(t, wt, "git", "status", "--porcelain"); strings.TrimSpace(status) == "" {
				t.Error("the uncommitted work is gone")
			}
			if got := e.currentBranch(t, wt); got != "feature/x" {
				t.Errorf("worktree is on %q, want feature/x", got)
			}
		})
	}
}

// A failing finish aborts the removal before anything is removed: the
// worktree, its branch and the hub's record of it all stay, and the
// pre-worktree-remove hook never runs.
func TestGitflowRemoveGate_FailedFinishRemovesNothing(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.gitHop(t, "add", "feature/x")
	wt := filepath.Join(e.HubPath, "hops", "feature", "x")
	e.commitInWorktree(t, wt, "work.txt")
	WriteFile(t, e.flowLog+".finishfail", "")

	_, stderr := e.gitHopFails(t, "remove", "feature/x", "--no-prompt")

	if !strings.Contains(stderr, "git flow feature finish x failed") {
		t.Errorf("stderr does not report the failed finish:\n%s", stderr)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed after a failed finish: %v", err)
	}
	if got := e.currentBranch(t, wt); got != "feature/x" {
		t.Errorf("worktree is on %q, want feature/x", got)
	}
	if !e.branchExists(t, "feature/x") {
		t.Error("feature/x deleted after a failed finish")
	}
	hopJSON, err := os.ReadFile(filepath.Join(e.HubPath, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hopJSON), `"feature/x"`) {
		t.Errorf("hop.json lost feature/x:\n%s", hopJSON)
	}
	if _, err := os.Stat(filepath.Join(e.markerDir, "pre-worktree-remove")); !os.IsNotExist(err) {
		t.Errorf("pre-worktree-remove ran for a remove that failed its finish: %v", err)
	}
}

// The preview evaluates the gate as the real run does: an unmerged
// branch finish handles passes it, and a dirty one is refused.
func TestGitflowRemoveGate_DryRun(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.gitHop(t, "add", "feature/x")
	wt := filepath.Join(e.HubPath, "hops", "feature", "x")
	e.commitInWorktree(t, wt, "work.txt")

	out, _ := e.gitHop(t, "remove", "feature/x", "-n", "--no-prompt")
	for _, want := range []string{"git flow finish merges it", "Would run 'git flow feature finish x'"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}

	WriteFile(t, filepath.Join(wt, "scratch.txt"), "keep me\n")
	e.gitHopFails(t, "remove", "feature/x", "-n", "--no-prompt", "--force", "--no-verify")
	if calls := e.flowCalls(t); len(calls) != 1 {
		t.Errorf("dry-run invoked git-flow: %q", calls)
	}
}
