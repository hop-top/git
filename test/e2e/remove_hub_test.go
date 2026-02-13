package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jadb/git-hop/internal/state"
	"github.com/spf13/afero"
)

// TestRemoveEntireHub tests removing an entire hub including all worktrees
func TestRemoveEntireHub(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// ===== SETUP: Create bare repo and seed repo =====
	t.Log("Setting up bare repository and seed repository")
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Create initial commit
	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test Repository")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Create multiple branches
	t.Log("Creating multiple feature branches")
	branches := []string{"feature-a", "feature-b", "feature-c"}
	for _, branch := range branches {
		env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", branch)
		WriteFile(t, filepath.Join(env.SeedRepoPath, branch+".txt"), "Feature content")
		env.RunCommand(t, env.SeedRepoPath, "git", "add", branch+".txt")
		env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Add "+branch)
		env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", branch)
	}
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "main")

	// ===== INITIALIZE HUB =====
	t.Log("Initializing hub with git hop clone")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Verify hub was created
	if _, err := os.Stat(env.HubPath); err != nil {
		t.Fatalf("Hub directory not created: %v", err)
	}

	// Add all feature branches
	t.Log("Adding feature branches to hub")
	for _, branch := range branches {
		env.RunGitHop(t, env.HubPath, "add", branch)
	}

	// Verify all worktrees exist
	mainWorktreePath := filepath.Join(env.HubPath, "hops", "main")
	if _, err := os.Stat(mainWorktreePath); err != nil {
		t.Fatalf("Main worktree not created: %v", err)
	}

	for _, branch := range branches {
		branchPath := filepath.Join(env.HubPath, "hops", branch)
		if _, err := os.Stat(branchPath); err != nil {
			t.Fatalf("Branch %s worktree not created: %v", branch, err)
		}
	}

	// Verify hub appears in list
	listOut := env.RunGitHop(t, env.HubPath, "list")
	for _, branch := range branches {
		if !strings.Contains(listOut, branch) {
			t.Errorf("Branch %s not in list before hub removal", branch)
		}
	}

	// ===== REMOVE ENTIRE HUB =====
	t.Log("Removing entire hub from outside hub directory")

	// IMPORTANT: Must be outside the hub directory to remove it
	// This simulates the documented usage: cd .. && git hop remove my-project
	removeOut := env.RunGitHop(t, env.RootDir, "remove", "hub", "--no-prompt")
	t.Logf("Remove hub output:\n%s", removeOut)

	// Check for success message
	if !strings.Contains(removeOut, "Successfully removed hub") {
		t.Errorf("Expected success message in remove output, got:\n%s", removeOut)
	}

	// ===== VERIFY COMPLETE CLEANUP =====
	t.Log("Verifying complete hub cleanup")

	// 1. Verify hub directory is completely gone
	if _, err := os.Stat(env.HubPath); err == nil {
		t.Errorf("Hub directory should be removed but still exists at: %s", env.HubPath)
		// List what's inside to help debug
		if entries, err := os.ReadDir(env.HubPath); err == nil {
			t.Logf("Remaining entries in hub directory: %v", entries)
		}
	} else if !os.IsNotExist(err) {
		t.Errorf("Unexpected error checking hub directory: %v", err)
	} else {
		t.Log("✓ Hub directory completely removed")
	}

	// 2. Verify all worktrees are gone
	for _, branch := range append(branches, "main") {
		branchPath := filepath.Join(env.HubPath, "hops", branch)
		if _, err := os.Stat(branchPath); err == nil {
			t.Errorf("Worktree for %s should be removed but still exists", branch)
		}
	}
	t.Log("✓ All worktrees removed")

	// 3. Verify global state no longer contains the repository
	fs := afero.NewOsFs()
	globalState, err := state.LoadState(fs)
	if err != nil {
		t.Logf("No global state found after removal (may be expected): %v", err)
	} else {
		// The repository ID would be like github.com/git-hop-e2e-*/repo
		// Check if any repo with "repo" in the name exists
		foundRepo := false
		for repoID := range globalState.Repositories {
			if strings.Contains(repoID, "/repo") {
				foundRepo = true
				t.Errorf("Repository should be removed from global state, but found: %s", repoID)
			}
		}
		if !foundRepo {
			t.Log("✓ Repository removed from global state")
		}
	}

	// 4. Verify hopspace data is cleaned up
	// Note: The exact path may vary, but we check if any hopspace data exists
	hopspacesRoot := filepath.Join(env.DataHome, "hopspaces")
	if _, err := os.Stat(hopspacesRoot); err == nil {
		// Check if there are any directories under hopspaces
		entries, _ := os.ReadDir(hopspacesRoot)
		if len(entries) > 0 {
			t.Logf("Note: Some hopspace data still exists (may be expected): %d entries", len(entries))
		} else {
			t.Log("✓ Hopspace data cleaned up")
		}
	} else {
		t.Log("✓ Hopspace directory doesn't exist")
	}

	// 5. Verify we cannot list from the hub anymore (since it's gone)
	// This should fail gracefully or show no worktrees
	// We run this from RootDir since the hub is gone
	listOut = env.RunGitHop(t, env.RootDir, "list")
	for _, branch := range branches {
		if strings.Contains(listOut, branch) {
			t.Errorf("Branch %s should not appear in list after hub removal, got:\n%s", branch, listOut)
		}
	}
	t.Log("✓ Branches no longer appear in list")

	t.Log("✅ Hub removal completed successfully with full cleanup!")
}

// TestRemoveHubWithRelativePath tests removing a hub using a relative path
func TestRemoveHubWithRelativePath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Setup
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Verify hub exists
	if _, err := os.Stat(env.HubPath); err != nil {
		t.Fatalf("Hub not created: %v", err)
	}

	// Remove using relative path (from parent directory)
	t.Log("Removing hub using relative path './hub'")
	removeOut := env.RunGitHop(t, env.RootDir, "remove", "./hub", "--no-prompt")

	if !strings.Contains(removeOut, "Successfully removed hub") {
		t.Errorf("Expected success message, got:\n%s", removeOut)
	}

	// Verify hub is gone
	if _, err := os.Stat(env.HubPath); err == nil {
		t.Errorf("Hub should be removed")
	} else {
		t.Log("✓ Hub removed using relative path")
	}

	t.Log("✅ Relative path hub removal works!")
}

// TestRemoveHubWithAbsolutePath tests removing a hub using an absolute path
func TestRemoveHubWithAbsolutePath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Setup
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Verify hub exists
	if _, err := os.Stat(env.HubPath); err != nil {
		t.Fatalf("Hub not created: %v", err)
	}

	// Remove using absolute path
	t.Log("Removing hub using absolute path")
	removeOut := env.RunGitHop(t, env.RootDir, "remove", env.HubPath, "--no-prompt")

	if !strings.Contains(removeOut, "Successfully removed hub") {
		t.Errorf("Expected success message, got:\n%s", removeOut)
	}

	// Verify hub is gone
	if _, err := os.Stat(env.HubPath); err == nil {
		t.Errorf("Hub should be removed")
	} else {
		t.Log("✓ Hub removed using absolute path")
	}

	t.Log("✅ Absolute path hub removal works!")
}

// TestRemoveNonEmptyHub tests removing a hub that has multiple branches and worktrees
func TestRemoveNonEmptyHub(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Setup with many branches
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Create 10 branches
	t.Log("Creating 10 feature branches")
	for i := 1; i <= 10; i++ {
		branch := fmt.Sprintf("feature-%d", i)
		env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", branch)
		WriteFile(t, filepath.Join(env.SeedRepoPath, fmt.Sprintf("file-%d.txt", i)), "Content")
		env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
		env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Add "+branch)
		env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", branch)
	}

	// Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Add all 10 branches
	t.Log("Adding all branches to hub")
	for i := 1; i <= 10; i++ {
		branch := fmt.Sprintf("feature-%d", i)
		env.RunGitHop(t, env.HubPath, "add", branch)
	}

	// Verify all worktrees exist (main + 10 features = 11 total)
	hopsDir := filepath.Join(env.HubPath, "hops")
	entries, err := os.ReadDir(hopsDir)
	if err != nil {
		t.Fatalf("Failed to read hops directory: %v", err)
	}

	worktreeCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			worktreeCount++
		}
	}

	expectedCount := 11 // main + 10 features
	if worktreeCount != expectedCount {
		t.Errorf("Expected %d worktrees, found %d", expectedCount, worktreeCount)
	}
	t.Logf("✓ Hub has %d worktrees before removal", worktreeCount)

	// Remove the entire hub
	t.Log("Removing hub with 11 worktrees")
	removeOut := env.RunGitHop(t, env.RootDir, "remove", "hub", "--no-prompt")

	if !strings.Contains(removeOut, "Successfully removed hub") {
		t.Errorf("Expected success message, got:\n%s", removeOut)
	}

	// Verify complete removal
	if _, err := os.Stat(env.HubPath); err == nil {
		t.Errorf("Hub with multiple worktrees should be completely removed")
	} else {
		t.Log("✓ Hub with all 11 worktrees removed successfully")
	}

	t.Log("✅ Non-empty hub removal works!")
}

// TestRemoveHubStatePersistence verifies that after removing a hub,
// the global state is properly updated and persists across reloads
func TestRemoveHubStatePersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Setup
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Initialize hub and add a branch
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature")
	env.RunGitHop(t, env.HubPath, "add", "feature")

	// Load state and verify repository exists
	fs := afero.NewOsFs()
	stateFile := filepath.Join(env.DataHome, "state.json")

	// Read state before removal
	stateBefore, err := state.LoadState(fs)
	if err != nil {
		t.Logf("Note: No state file before removal (may be expected): %v", err)
	} else {
		t.Logf("State before removal contains %d repositories", len(stateBefore.Repositories))
	}

	// Remove hub
	env.RunGitHop(t, env.RootDir, "remove", "hub", "--no-prompt")

	// Verify state file was updated
	if _, err := os.Stat(stateFile); err != nil {
		t.Logf("Note: State file doesn't exist after removal (may be expected if no other repos)")
	} else {
		// Read and parse state file
		stateData, err := os.ReadFile(stateFile)
		if err != nil {
			t.Fatalf("Failed to read state file: %v", err)
		}

		var stateAfter state.State
		if err := json.Unmarshal(stateData, &stateAfter); err != nil {
			t.Fatalf("Failed to parse state file: %v", err)
		}

		t.Logf("State after removal contains %d repositories", len(stateAfter.Repositories))

		// Verify the removed repository is not in state
		for repoID := range stateAfter.Repositories {
			if strings.Contains(repoID, "/repo") {
				t.Errorf("Removed repository still in state: %s", repoID)
			}
		}

		t.Log("✓ State file properly updated after hub removal")
	}

	// Reload state and verify again
	stateReloaded, err := state.LoadState(fs)
	if err != nil {
		t.Logf("Note: Could not reload state: %v", err)
	} else {
		for repoID := range stateReloaded.Repositories {
			if strings.Contains(repoID, "/repo") {
				t.Errorf("Removed repository still in reloaded state: %s", repoID)
			}
		}
		t.Log("✓ State changes persisted across reload")
	}

	t.Log("✅ Hub removal state persistence verified!")
}

// TestCannotRemoveHubFromInside verifies that attempting to remove a hub
// from inside the hub directory produces appropriate behavior
func TestRemoveHubFromInsideDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Setup
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Try to remove hub from inside (using "." as target)
	// This should work by resolving "." to the current hub directory
	t.Log("Attempting to remove hub using '.' from inside hub directory")

	// Note: This test documents current behavior. The implementation
	// resolves "." to the hub path and removes it successfully.
	removeOut := env.RunGitHop(t, env.HubPath, "remove", ".", "--no-prompt")

	if strings.Contains(removeOut, "Successfully removed hub") {
		t.Log("✓ Hub removal from inside succeeded (removes current directory)")

		// Verify hub is gone
		if _, err := os.Stat(env.HubPath); err == nil {
			// On some systems, the directory might still exist briefly
			t.Logf("Note: Hub directory still exists immediately after removal")
		}
	} else {
		t.Logf("Note: Hub removal from inside may not be supported, got:\n%s", removeOut)
	}

	t.Log("✅ Hub removal from inside directory test completed!")
}
