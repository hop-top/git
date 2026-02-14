package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemovePathResolution tests that the remove command correctly resolves
// relative worktree paths when running from within a worktree directory.
// This catches the bug where filepath.Abs(relativePath) would create duplicated paths.
func TestRemovePathResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Setup bare repo
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Create initial commit
	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test Repo")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Create feature branch
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature")

	// Clone with git-hop to create hub structure
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Add feature branch
	env.RunGitHop(t, env.HubPath, "add", "feature")

	// Verify both worktrees exist
	mainWorktreePath := filepath.Join(env.HubPath, "hops", "main")
	featureWorktreePath := filepath.Join(env.HubPath, "hops", "feature")

	if _, err := os.Stat(mainWorktreePath); err != nil {
		t.Fatalf("Main worktree not found: %v", err)
	}
	if _, err := os.Stat(featureWorktreePath); err != nil {
		t.Fatalf("Feature worktree not found: %v", err)
	}

	// Run remove from a DIFFERENT worktree (the main one)
	// Previously this test ran from within the target worktree, but the CWD guard
	// now correctly prevents that. The path resolution bug is still exercised
	// because basePath is resolved relative to hubPath regardless of CWD.
	out := env.RunGitHop(t, mainWorktreePath, "remove", "feature", "--no-prompt")
	t.Logf("Remove command output:\n%s", out)

	// THE BUG: Check for warning about failed git worktree remove with duplicated path
	// The error message would contain something like "hops/feature/hops/main"
	if strings.Contains(out, "hops/feature/hops") {
		t.Errorf("Bug detected: Output contains duplicated path segments indicating filepath.Abs was used incorrectly:\n%s", out)
	}

	// Also check for the specific error pattern from git worktree remove
	if strings.Contains(out, "chdir") && strings.Contains(out, "hops/main/hops") {
		t.Errorf("Bug detected: Path duplication error in git worktree remove:\n%s", out)
	}

	// The bug shows up as a warning, not a failure
	if strings.Contains(out, "WARN") && strings.Contains(out, "Failed to remove worktree via git") {
		// This is expected with the buggy code - log it
		t.Logf("Warning detected (expected with bug): %s", out)
		// But the test should still fail if we see path duplication
		if strings.Contains(out, "/hops/") && strings.Count(out, "/hops/") > 2 {
			t.Errorf("Bug detected: Multiple /hops/ in path suggests duplication:\n%s", out)
		}
	}

	// Verify feature worktree was actually removed
	if _, err := os.Stat(featureWorktreePath); err == nil {
		t.Errorf("Feature worktree should have been removed but still exists at: %s", featureWorktreePath)
	}

	// Verify main worktree still exists (should not be affected)
	if _, err := os.Stat(mainWorktreePath); err != nil {
		t.Errorf("Main worktree should still exist but is gone: %v", err)
	}
}
