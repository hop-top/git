package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupHubWithMissingWorktrees creates a hub with two branches, then
// physically deletes both worktree directories so they are "Missing".
// Returns hubPath and the names of the two branches.
func setupHubWithMissingWorktrees(t *testing.T) (*TestEnv, string, string) {
	t.Helper()

	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	branchA := "feat/alpha"
	branchB := "feat/beta"

	for _, b := range []string{branchA, branchB} {
		env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", b)
		env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", b)
	}
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", branchA)
	env.RunGitHop(t, env.HubPath, "add", branchB)

	// Simulate "Missing" state: delete ALL worktree directories from disk so
	// no live worktree is available as a basePath for git commands.
	dirsToRemove := []string{
		filepath.Join(env.HubPath, "hops", "main"),
		filepath.Join(env.HubPath, "hops", branchA),
		filepath.Join(env.HubPath, "hops", branchB),
	}
	for _, d := range dirsToRemove {
		if err := os.RemoveAll(d); err != nil {
			t.Fatalf("failed to remove worktree dir %s: %v", d, err)
		}
	}

	return env, branchA, branchB
}

// TestRemove_AllWorktreesMissing_NoChdir verifies that removing a branch
// whose hub has no live worktrees does not emit a chdir error.
//
// Failing symptom: WARN with "chdir … no such file or directory"
func TestRemove_AllWorktreesMissing_NoChdir(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env, branchA, _ := setupHubWithMissingWorktrees(t)

	out := env.RunGitHopCombined(t, env.HubPath, "remove", "--no-prompt", branchA)
	t.Logf("remove output:\n%s", out)

	if strings.Contains(out, "chdir") {
		t.Errorf("got chdir error; basePath resolved to a missing directory:\n%s", out)
	}
	if strings.Contains(out, "no such file or directory") {
		t.Errorf("got 'no such file or directory' in output:\n%s", out)
	}
}

// TestRemove_BranchDisappearsFromStatus verifies that after removing a branch
// it no longer appears in `git hop status`, even when all sibling worktrees
// are missing from disk.
//
// Failing symptom: removed branch still listed as "Missing" in status output.
func TestRemove_BranchDisappearsFromStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env, branchA, branchB := setupHubWithMissingWorktrees(t)

	env.RunGitHopCombined(t, env.HubPath, "remove", "--no-prompt", branchA)

	out := env.RunGitHop(t, env.HubPath, "status")
	t.Logf("status output after remove:\n%s", out)

	if strings.Contains(out, branchA) {
		t.Errorf("removed branch %q still appears in status:\n%s", branchA, out)
	}

	// Sanity: branchB not removed, should still be listed (as Missing)
	if !strings.Contains(out, branchB) {
		t.Errorf("unremoved branch %q missing from status:\n%s", branchB, out)
	}
}
