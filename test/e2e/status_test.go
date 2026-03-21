package e2e

import (
	"strings"
	"testing"
)

// TestStatus_LinkedWithAbsolutePath verifies that git hop status shows "Linked"
// when branch paths in hop.json are stored as absolute paths.
// Regression: filepath.Join(hubPath, absolutePath) produced a wrong path,
// causing all branches to show "Missing" even when worktrees existed.
func TestStatus_LinkedWithAbsolutePath(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	// add creates the worktree with an absolute path in hop.json
	env.RunGitHop(t, env.HubPath, "add", "feature/abs-path-test")

	out := env.RunGitHop(t, env.HubPath, "status")

	if strings.Contains(out, "Missing") {
		t.Errorf("expected no Missing entries when worktrees exist on disk, got:\n%s", out)
	}
	if !strings.Contains(out, "Linked") {
		t.Errorf("expected 'Linked' for existing worktree, got:\n%s", out)
	}
}
