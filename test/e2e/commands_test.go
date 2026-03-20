package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	for _, branch := range []string{"feature-1", "feature-2", "staging", "feat/slash-branch"} {
		env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", branch)
		env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", branch)
	}

	// Initialize Hub using git hop clone
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Verify main worktree exists initially
	if _, err := os.Stat(filepath.Join(env.HubPath, "hops", "main")); err != nil {
		t.Fatalf("Main worktree missing after setup: %v", err)
	}

	// --- Test: git hop add ---
	t.Run("Add", func(t *testing.T) {
		// ...
		// Add feature-1
		out := env.RunGitHop(t, env.HubPath, "add", "feature-1")
		if !strings.Contains(out, "Created hopspace for 'feature-1'") {
			t.Errorf("Expected success message, got: %s", out)
		}
		// Verify worktree directory exists under hops/
		wtPath := filepath.Join(env.HubPath, "hops", "feature-1")
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("Worktree feature-1 not created at hops/feature-1: %v", err)
		}
	})

	// --- Test: git hop add with slash in branch name ---
	t.Run("AddBranchWithSlash", func(t *testing.T) {
		// Add feat/slash-branch
		out := env.RunGitHop(t, env.HubPath, "add", "feat/slash-branch")
		if !strings.Contains(out, "feat/slash-branch") {
			t.Errorf("Expected success message for feat/slash-branch, got: %s", out)
		}

		// Verify worktree directory exists at correct path: hops/feat/slash-branch
		expectedPath := filepath.Join(env.HubPath, "hops", "feat", "slash-branch")
		if _, err := os.Stat(expectedPath); err != nil {
			t.Errorf("Worktree not created at expected path %s: %v", expectedPath, err)
		}

		// Verify NO orphaned directory at hops/slash-branch
		orphanedPath := filepath.Join(env.HubPath, "hops", "slash-branch")
		if _, err := os.Stat(orphanedPath); err == nil {
			t.Errorf("Orphaned worktree directory found at %s (should not exist)", orphanedPath)
		}

		// Verify hops/ only has expected subdirectories
		hopsDir := filepath.Join(env.HubPath, "hops")
		entries, err := os.ReadDir(hopsDir)
		if err != nil {
			t.Fatalf("Failed to read hops directory: %v", err)
		}

		expectedDirs := map[string]bool{"main": true, "feature-1": true, "feat": true}
		for _, entry := range entries {
			if entry.IsDir() && !expectedDirs[entry.Name()] {
				t.Errorf("Unexpected directory in hops/: %s", entry.Name())
			}
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
		// Hub view: branch table shows Linked for existing worktree
		out := env.RunGitHop(t, env.HubPath, "status")
		if !strings.Contains(out, "feature-1") {
			t.Errorf("Status output missing feature-1: %s", out)
		}
		if !strings.Contains(out, "Linked") {
			t.Errorf("Status hub view should show 'Linked' for existing worktree: %s", out)
		}

		// Target arg: status of a specific branch from hub
		out = env.RunGitHop(t, env.HubPath, "status", "feature-1")
		if !strings.Contains(out, "feature-1") {
			t.Errorf("Status target output missing branch name: %s", out)
		}
		if !strings.Contains(out, "Worktree") {
			t.Errorf("Status target output should show worktree path: %s", out)
		}
	})

	// --- Test: git hop status --all ---
	t.Run("StatusAll", func(t *testing.T) {
		out := env.RunGitHop(t, env.HubPath, "status", "--all")
		if !strings.Contains(out, "Repositories") {
			t.Errorf("Status --all output missing Repositories section: %s", out)
		}
		if !strings.Contains(out, env.DataHome) {
			t.Errorf("Status --all output missing data home path: %s", out)
		}
		if !strings.Contains(out, "Active") {
			t.Errorf("Status --all output missing Active count: %s", out)
		}
	})

	// --- Test: git hop status outside hub/worktree ---
	t.Run("StatusOutsideHub", func(t *testing.T) {
		// RootDir is not a hub or worktree — plain directory
		out := runCommandExpectError(t, env, env.RootDir, env.BinPath, "status")
		if !strings.Contains(out, "Not in") {
			t.Errorf("Status outside hub should say 'Not in ...', got: %s", out)
		}
	})

	// --- Test: git hop env ---
	t.Run("Env", func(t *testing.T) {
		SkipIfDockerNotAvailable(t)
		branchPath := filepath.Join(env.HubPath, "hops", "feature-1")

		// Generate (implicit in add, but test explicit)
		env.RunGitHop(t, branchPath, "env", "generate")

		// Check
		out := env.RunGitHop(t, branchPath, "env", "check")
		if strings.Contains(out, "Error") {
			t.Errorf("Env check reported errors: %s", out)
		}

		// Start
		env.RunGitHop(t, branchPath, "env", "start")

		// status --all should show running services after env start
		allOut := env.RunGitHop(t, env.HubPath, "status", "--all")
		if !strings.Contains(allOut, "running") {
			t.Errorf("Status --all should show 'running' after env start: %s", allOut)
		}

		// Stop
		env.RunGitHop(t, branchPath, "env", "stop")

		// status --all should show stopped after env stop
		allOut = env.RunGitHop(t, env.HubPath, "status", "--all")
		if !strings.Contains(allOut, "stopped") {
			t.Errorf("Status --all should show 'stopped' after env stop: %s", allOut)
		}
	})

	// --- Test: git hop remove ---
	t.Run("Remove", func(t *testing.T) {
		// Remove feature-1
		// Remove is interactive by default, use --no-prompt to avoid confirmation

		out := env.RunGitHop(t, env.HubPath, "remove", "feature-1", "--no-prompt")
		if !strings.Contains(out, "Removed") && !strings.Contains(out, "Successfully") {
			// Adjust expectation based on actual output
			t.Logf("Remove output: %s", out)
		}

		// Verify worktree gone
		wtPath := filepath.Join(env.HubPath, "hops", "feature-1")
		if _, err := os.Stat(wtPath); err == nil {
			t.Errorf("Worktree feature-1 should be gone at hops/feature-1")
		}

		// Hub status should now show Missing for the removed worktree
		out = env.RunGitHop(t, env.HubPath, "status")
		if !strings.Contains(out, "Missing") {
			t.Errorf("Status hub view should show 'Missing' for removed worktree: %s", out)
		}

		// status --all should reflect updated Active/Missing counts
		allOut := env.RunGitHop(t, env.HubPath, "status", "--all")
		if !strings.Contains(allOut, "Missing") {
			t.Errorf("Status --all should show Missing count after removal: %s", allOut)
		}
	})

	// --- Test: Commands from within worktree ---
	t.Run("CommandsFromWorktree", func(t *testing.T) {
		// Add feature-2 for this test
		env.RunGitHop(t, env.HubPath, "add", "feature-2")

		// Navigate to the worktree directory and run commands from there
		feature2Path := filepath.Join(env.HubPath, "hops", "feature-2")

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
		hopsDir := filepath.Join(env.HubPath, "hops")
		files, _ := os.ReadDir(hopsDir)
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
			t.Errorf("Fork worktree for main not found in hops/ directory")
		} else {
			// Verify the worktree directory exists
			wtPath := filepath.Join(hopsDir, wtName)
			if _, err := os.Stat(wtPath); err != nil {
				t.Errorf("Fork worktree directory does not exist: %v", err)
			}
		}
	})
}
