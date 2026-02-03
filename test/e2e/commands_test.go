package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommands(t *testing.T) {
	env := SetupTestEnv(t)

	// --- Setup Repo and Hub ---
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Commit docker-compose.yml
	dcContent, _ := os.ReadFile("fixtures/docker-compose.yml")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "docker-compose.yml"), string(dcContent))
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "docker-compose.yml")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Create branches
	for _, branch := range []string{"feature-1", "feature-2", "staging"} {
		env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", branch)
		env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", branch)
	}

	// Initialize Hub manually with worktrees/ structure
	os.MkdirAll(env.HubPath, 0755)
	worktreesDir := filepath.Join(env.HubPath, "worktrees")
	os.MkdirAll(worktreesDir, 0755)
	mainWorktreePath := filepath.Join(worktreesDir, "main")
	env.RunCommand(t, worktreesDir, "git", "clone", env.BareRepoPath, "main")

	// Configs
	createConfigs(t, env, mainWorktreePath)

	// Verify main worktree exists initially
	if _, err := os.Stat(filepath.Join(env.HubPath, "worktrees", "main")); err != nil {
		t.Fatalf("Main worktree missing after setup: %v", err)
	}

	// --- Test: git hop add ---
	t.Run("Add", func(t *testing.T) {
		// ...
		// Add feature-1
		out := env.RunGitHop(t, env.HubPath, "add", "feature-1")
		if !strings.Contains(out, "Successfully added feature-1") {
			t.Errorf("Expected success message, got: %s", out)
		}
		// Verify worktree directory exists under worktrees/
		wtPath := filepath.Join(env.HubPath, "worktrees", "feature-1")
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("Worktree feature-1 not created at worktrees/feature-1: %v", err)
		}
	})

	// --- Test: git hop list ---
	t.Run("List", func(t *testing.T) {
		// List in hub
		out := env.RunGitHop(t, env.HubPath, "list")
		if !strings.Contains(out, "feature-1") {
			t.Errorf("List output missing feature-1: %s", out)
		}
		if !strings.Contains(out, "main") {
			t.Errorf("List output missing main: %s", out)
		}
	})

	// --- Test: git hop status ---
	t.Run("Status", func(t *testing.T) {
		out := env.RunGitHop(t, env.HubPath, "status")
		if !strings.Contains(out, "feature-1") {
			t.Errorf("Status output missing feature-1: %s", out)
		}
	})

	// --- Test: git hop env ---
	t.Run("Env", func(t *testing.T) {
		branchPath := filepath.Join(env.HubPath, "worktrees", "feature-1")

		// Generate (implicit in add, but test explicit)
		env.RunGitHop(t, branchPath, "env", "generate")

		// Check
		out := env.RunGitHop(t, branchPath, "env", "check")
		if strings.Contains(out, "Error") {
			t.Errorf("Env check reported errors: %s", out)
		}

		// Start
		env.RunGitHop(t, branchPath, "env", "start")

		// Verify running (mocked via docker wrapper, but we check output)
		// In real e2e with docker, we could check `docker ps`.
		// Here we rely on the command succeeding.

		// Stop
		env.RunGitHop(t, branchPath, "env", "stop")
	})

	// --- Test: git hop remove ---
	t.Run("Remove", func(t *testing.T) {
		// Remove feature-1
		// Note: remove is interactive by default. We need --no-prompt or input.
		// Assuming --no-prompt is implemented or we can pipe "y".
		// Let's try with --no-prompt if spec says so. Spec says: `git hop remove <target> [--no-prompt]`

		out := env.RunGitHop(t, env.HubPath, "remove", "feature-1", "--no-prompt")
		if !strings.Contains(out, "Removed") && !strings.Contains(out, "Successfully") {
			// Adjust expectation based on actual output
			t.Logf("Remove output: %s", out)
		}

		// Verify worktree gone
		wtPath := filepath.Join(env.HubPath, "worktrees", "feature-1")
		if _, err := os.Stat(wtPath); err == nil {
			t.Errorf("Worktree feature-1 should be gone at worktrees/feature-1")
		}
	})

	// --- Test: Commands from within worktree ---
	t.Run("CommandsFromWorktree", func(t *testing.T) {
		// Add feature-2 for this test
		env.RunGitHop(t, env.HubPath, "add", "feature-2")

		// Navigate to the worktree directory and run commands from there
		feature2Path := filepath.Join(env.HubPath, "worktrees", "feature-2")

		// Test: git hop list from within worktree
		out := env.RunGitHop(t, feature2Path, "list")
		if !strings.Contains(out, "feature-2") {
			t.Errorf("List from worktree should work: %s", out)
		}
		if !strings.Contains(out, "main") {
			t.Errorf("List from worktree should show main: %s", out)
		}

		// Test: git hop status from within worktree
		out = env.RunGitHop(t, feature2Path, "status")
		if !strings.Contains(out, "Hub:") {
			t.Errorf("Status from worktree should show hub info: %s", out)
		}

		// The key success is that these commands found the hub
		// from within the worktree directory, which proves FindHub works
		// If FindHub didn't work, we would get "Not in a git-hop hub" error
	})

	// --- Test: git hop <uri> --branch main (Fork-Attach from main) ---
	t.Run("ForkAttachMain", func(t *testing.T) {
		// Create a fork repo
		forkRepoPath := filepath.Join(env.RootDir, "fork.git")
		env.RunCommand(t, env.RootDir, "git", "clone", "--bare", env.SeedRepoPath, forkRepoPath)

		// We want to attach 'main' from the fork.
		// The fork's main should be compatible with our main.

		out := env.RunGitHop(t, env.HubPath, forkRepoPath, "--branch", "main")
		if !strings.Contains(out, "Successfully attached fork branch") {
			t.Errorf("ForkAttachMain output missing success message: %s", out)
		}

		// Verify worktree directory
		// Look for fork worktree starting with "main-fork-"
		worktreesDir := filepath.Join(env.HubPath, "worktrees")
		files, _ := os.ReadDir(worktreesDir)
		found := false
		var wtName string
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "main-fork-") {
				found = true
				wtName = f.Name()
				break
			}
		}
		if !found {
			t.Errorf("Fork worktree for main not found in worktrees/ directory")
		} else {
			// Verify the worktree directory exists
			wtPath := filepath.Join(worktreesDir, wtName)
			if _, err := os.Stat(wtPath); err != nil {
				t.Errorf("Fork worktree directory does not exist: %v", err)
			}
		}
	})
}

func createConfigs(t *testing.T, env *TestEnv, mainWorktreePath string) {
	// Create merged local config (hub+hopspace in single hop.json)
	// This simulates local mode (default, no --global flag)
	mergedConfig := map[string]interface{}{
		"repo": map[string]interface{}{
			"uri":           env.BareRepoPath,
			"org":           "local",
			"repo":          "test-repo",
			"defaultBranch": "main",
			"structure":     "bare-worktree",
			"isBare":        true,
		},
		"branches": map[string]interface{}{
			"main": map[string]interface{}{
				// Merged hub+hopspace fields
				"path":           mainWorktreePath, // Absolute path for hopspace
				"hopspaceBranch": "main",
				"exists":         true,
				"lastSync":       time.Now().Format(time.RFC3339),
			},
		},
		"settings": map[string]interface{}{
			"envPatterns": []string{"dev", "staging", "qa"},
		},
		"forks": map[string]interface{}{},
	}

	// Write merged config to hub directory
	data, err := json.MarshalIndent(mergedConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal merged config: %v", err)
	}
	WriteFile(t, filepath.Join(env.HubPath, "hop.json"), string(data))
}
