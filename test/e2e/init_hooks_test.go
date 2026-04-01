package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupInitRepo creates a bare repo + seed with an initial commit,
// ready for `git hop <bare> hub` or `git hop init`.
func setupInitRepo(t *testing.T) *TestEnv {
	t.Helper()
	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	return env
}

// TestInit_CreatesHooksDir verifies that cloning via git-hop creates
// .git-hop/hooks/ inside the main worktree.
func TestInit_CreatesHooksDir(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := setupInitRepo(t)
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	mainWorktree := filepath.Join(env.HubPath, "hops", "main")
	hooksDir := filepath.Join(mainWorktree, ".git-hop", "hooks")

	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		t.Errorf(".git-hop/hooks/ not created in main worktree after clone+init: %s", hooksDir)
	}
}

// TestInit_Idempotent_AlreadyInitialized verifies that running git-hop on an
// already-initialized hub prints "already initialized" and exits 0.
func TestInit_Idempotent_AlreadyInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := setupInitRepo(t)
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Re-run init from inside the hub
	out := env.RunGitHopCombined(t, env.HubPath, "init")
	if !strings.Contains(out, "already initialized") {
		t.Errorf("Expected 'already initialized' on second init, got:\n%s", out)
	}
}

// TestInit_Idempotent_EnsuresHooksDirExists verifies that an idempotent init
// creates .git-hop/hooks/ if it was manually deleted.
func TestInit_Idempotent_EnsuresHooksDirExists(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := setupInitRepo(t)
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Delete the hooks dir
	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	os.RemoveAll(hooksDir)

	// Re-run init — should recreate it
	env.RunGitHopCombined(t, env.HubPath, "init")

	// Hub-level hooks dir should now exist (idempotent run targets hub root)
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		t.Errorf(".git-hop/hooks/ not recreated by idempotent init: %s", hooksDir)
	}
}
