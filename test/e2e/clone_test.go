package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClone_OutsideRepo(t *testing.T) {
	env := SetupTestEnv(t)

	// 1. Create Bare Repo
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)

	// 2. Seed Repo
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// 3. Create a directory OUTSIDE of any git repo to run the command from
	outsideDir := filepath.Join(env.RootDir, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}

	// 4. Run `git hop <uri>` from that directory
	// We expect it to clone into a new directory named after the repo in the current dir
	// URI: .../repo.git -> repo
	repoName := filepath.Base(env.BareRepoPath)
	repoName = strings.TrimSuffix(repoName, ".git")
	expectedHubPath := filepath.Join(outsideDir, repoName)

	t.Logf("Running git hop clone from %s", outsideDir)
	out := env.RunGitHop(t, outsideDir, env.BareRepoPath)

	if !strings.Contains(out, "Successfully cloned") {
		t.Errorf("Expected success message, got: %s", out)
	}

	// 5. Verify Hub Created
	if _, err := os.Stat(expectedHubPath); err != nil {
		t.Errorf("Hub directory not created at %s", expectedHubPath)
	}

	// Verify hop.json exists in hub
	if _, err := os.Stat(filepath.Join(expectedHubPath, "hop.json")); err != nil {
		t.Errorf("hop.json not found in hub")
	}

	// Verify symlink to main exists
	if _, err := os.Lstat(filepath.Join(expectedHubPath, "main")); err != nil {
		t.Errorf("main symlink not found in hub")
	}
}
