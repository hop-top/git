package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClone_CustomPath verifies that cloning with a custom target path
// creates the correct directory structure without nesting
func TestClone_CustomPath(t *testing.T) {
	env := SetupTestEnv(t)

	// 1. Create Bare Repo
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)

	// 2. Seed Repo
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// 3. Create a directory to run the command from
	testDir := filepath.Join(env.RootDir, "testdir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}

	// 4. Run `git hop <uri> customtarget` with a custom target path
	customTarget := "mycustomproject"
	expectedPath := filepath.Join(testDir, customTarget)

	t.Logf("Running git hop clone with custom target from %s", testDir)
	out := env.RunGitHop(t, testDir, env.BareRepoPath, customTarget)

	if !strings.Contains(out, "Successfully cloned") {
		t.Errorf("Expected success message, got: %s", out)
	}

	// 5. Verify the project root was created at the correct location
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("Project directory not created at %s: %v", expectedPath, err)
	}

	// 6. Verify hop.json exists in the correct location
	hopJSONPath := filepath.Join(expectedPath, "hop.json")
	if _, err := os.Stat(hopJSONPath); err != nil {
		t.Errorf("hop.json not found at %s: %v", hopJSONPath, err)
	}

	// 7. Verify worktree exists in the correct location (NOT nested)
	correctWorktreePath := filepath.Join(expectedPath, "hops", "main")
	if _, err := os.Stat(correctWorktreePath); err != nil {
		t.Errorf("main worktree not found at correct path %s: %v", correctWorktreePath, err)
	}

	// 8. Verify that the WRONG nested path does NOT exist
	wrongNestedPath := filepath.Join(expectedPath, customTarget, "hops", "main")
	if _, err := os.Stat(wrongNestedPath); err == nil {
		t.Errorf("Found worktree at WRONG nested path %s - this is the bug we're fixing!", wrongNestedPath)
	}

	// 9. Verify the hops/ directory is not empty
	hopsDir := filepath.Join(expectedPath, "hops")
	entries, err := os.ReadDir(hopsDir)
	if err != nil {
		t.Errorf("Failed to read hops directory: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("hops directory is empty, but should contain the main worktree")
	}

	// 10. Verify git worktree list shows the correct path
	out = env.RunCommand(t, expectedPath, "git", "worktree", "list")
	if !strings.Contains(out, filepath.Join(expectedPath, "hops", "main")) {
		t.Errorf("Git worktree list doesn't show correct path. Output: %s", out)
	}
	if strings.Contains(out, filepath.Join(customTarget, customTarget)) {
		t.Errorf("Git worktree list shows nested path (bug!). Output: %s", out)
	}
}
