package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRootCommand_NonExistentBranch tests that `git hop <branch>` without explicit
// subcommand should NOT create a new worktree if the branch doesn't exist.
// It should fail with an error instead.
func TestRootCommand_NonExistentBranch(t *testing.T) {
	env := SetupTestEnv(t)

	// Setup: Create a bare repo with main branch
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Commit a file so we have content
	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test Repo")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Initialize Hub manually with hops/ structure
	os.MkdirAll(env.HubPath, 0755)
	hopsDir := filepath.Join(env.HubPath, "hops")
	os.MkdirAll(hopsDir, 0755)
	mainWorktreePath := filepath.Join(hopsDir, "main")
	env.RunCommand(t, hopsDir, "git", "clone", env.BareRepoPath, "main")

	// Create minimal config
	createConfigs(t, env, mainWorktreePath)

	// Test: Try to run `git hop lsit` where `lsit` is a non-existent branch
	// This should FAIL, not create a new worktree
	// We expect this to fail
	result := runCommandExpectError(t, env, env.HubPath, env.BinPath, "lsit")

	// Verify it did NOT create a worktree
	lsitWorktreePath := filepath.Join(hopsDir, "lsit")
	if _, err := os.Stat(lsitWorktreePath); err == nil {
		t.Errorf("BUG CONFIRMED: Worktree 'lsit' was created when it should not have been")
	}

	// The command should have failed with an appropriate error
	if strings.Contains(result, "Successfully added") || strings.Contains(result, "Adding branch") {
		t.Errorf("BUG: Command output suggests a worktree was added: %s", result)
	}

	// We expect an error message indicating the worktree doesn't exist
	if !strings.Contains(result, "does not exist") {
		t.Errorf("Expected 'does not exist' error message, got: %s", result)
	}
}

// TestRootCommand_ExistingWorktree tests that `git hop <branch>` should succeed
// when the worktree already exists and switches to it
func TestRootCommand_ExistingWorktree(t *testing.T) {
	env := SetupTestEnv(t)

	// Setup: Create a bare repo with main and feature-1 branches
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Commit a file
	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test Repo")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Create feature-1 branch
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-1")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-1")

	// Initialize Hub
	os.MkdirAll(env.HubPath, 0755)
	hopsDir := filepath.Join(env.HubPath, "hops")
	os.MkdirAll(hopsDir, 0755)
	mainWorktreePath := filepath.Join(hopsDir, "main")
	env.RunCommand(t, hopsDir, "git", "clone", env.BareRepoPath, "main")

	createConfigs(t, env, mainWorktreePath)

	// Explicitly create feature-1 worktree using `git hop add`
	env.RunGitHop(t, env.HubPath, "add", "feature-1")

	// Verify worktree exists
	feature1Path := filepath.Join(hopsDir, "feature-1")
	if _, err := os.Stat(feature1Path); err != nil {
		t.Fatalf("Feature-1 worktree should exist: %v", err)
	}

	// Test: `git hop feature-1` should switch to it successfully
	result := runCommandExpectError(t, env, env.HubPath, env.BinPath, "feature-1")

	// Should succeed
	if !strings.Contains(result, "Switched to worktree") {
		t.Errorf("Expected 'Switched to worktree' message, got: %s", result)
	}

	// Should show the path
	if !strings.Contains(result, feature1Path) {
		t.Errorf("Expected output to contain worktree path '%s', got: %s", feature1Path, result)
	}
}

// runCommandExpectError runs a command and returns stdout+stderr even on failure
func runCommandExpectError(t *testing.T, env *TestEnv, dir, name string, args ...string) string {
	t.Helper()

	fullArgs := append([]string{name}, args...)
	t.Logf("Running (expect error): %s in %s", strings.Join(fullArgs, " "), dir)

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env.EnvVars

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	if err == nil {
		t.Logf("Command succeeded (no error): %s", output)
	} else {
		t.Logf("Command failed (error): %v\nOutput: %s", err, output)
	}

	return output
}
