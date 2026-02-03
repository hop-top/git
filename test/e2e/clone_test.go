package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClone_OutsideRepo(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)

	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	outsideDir := filepath.Join(env.RootDir, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}

	repoName := filepath.Base(env.BareRepoPath)
	repoName = strings.TrimSuffix(repoName, ".git")
	expectedHubPath := filepath.Join(outsideDir, repoName)

	t.Logf("Running git hop clone from %s", outsideDir)
	out := env.RunGitHop(t, outsideDir, env.BareRepoPath)

	if !strings.Contains(out, "Successfully cloned") {
		t.Errorf("Expected success message, got: %s", out)
	}

	if _, err := os.Stat(expectedHubPath); err != nil {
		t.Errorf("Hub directory not created at %s", expectedHubPath)
	}

	if _, err := os.Stat(filepath.Join(expectedHubPath, "hop.json")); err != nil {
		t.Errorf("hop.json not found in hub")
	}

	if _, err := os.Stat(filepath.Join(expectedHubPath, "hops", "main")); err != nil {
		t.Errorf("main worktree not found at hops/main: %v", err)
	}
}
