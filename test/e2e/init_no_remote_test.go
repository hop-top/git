package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_InitConvertWithoutRemote(t *testing.T) {
	env := SetupTestEnv(t)

	// Create a local repository without a remote
	repoPath := filepath.Join(env.RootDir, "local-repo")
	env.RunCommand(t, env.RootDir, "git", "init", repoPath)

	// Configure git for this repo
	env.RunCommand(t, repoPath, "git", "config", "user.name", "Test User")
	env.RunCommand(t, repoPath, "git", "config", "user.email", "test@example.com")

	// Create initial commit
	WriteFile(t, filepath.Join(repoPath, "README.md"), "# Test Repo\n")
	env.RunCommand(t, repoPath, "git", "add", "README.md")
	env.RunCommand(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, repoPath, "git", "branch", "-M", "main")

	// Verify no remote exists
	remotes := env.RunCommand(t, repoPath, "git", "remote")
	if strings.TrimSpace(remotes) != "" {
		t.Fatalf("Expected no remotes, got: %s", remotes)
	}

	// TODO: Run git hop init with option 2 (regular repo + worktrees)
	// This would require automated input or a flag to select the option
	// For now, we'll test the conversion programmatically

	t.Log("Test repository created successfully without remote")
	t.Log("Repository path:", repoPath)
}

func TestE2E_InitConvertBareWithoutRemote(t *testing.T) {
	env := SetupTestEnv(t)

	// Create a local repository without a remote
	repoPath := filepath.Join(env.RootDir, "local-repo")
	env.RunCommand(t, env.RootDir, "git", "init", repoPath)

	// Configure git for this repo
	env.RunCommand(t, repoPath, "git", "config", "user.name", "Test User")
	env.RunCommand(t, repoPath, "git", "config", "user.email", "test@example.com")

	// Create initial commit on main branch
	WriteFile(t, filepath.Join(repoPath, "README.md"), "# Test Repo\n")
	env.RunCommand(t, repoPath, "git", "add", "README.md")
	env.RunCommand(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, repoPath, "git", "branch", "-M", "main")

	// Verify no remote exists
	remotes := env.RunCommand(t, repoPath, "git", "remote")
	if strings.TrimSpace(remotes) != "" {
		t.Fatalf("Expected no remotes, got: %s", remotes)
	}

	// Since we can't automate the interactive prompt yet, we'll skip the actual conversion
	// and just verify the setup works
	t.Skip("Interactive conversion requires user input - skipping for now")

	// After conversion, we should verify:
	// 1. hop.json exists with proper structure
	// 2. Worktree structure is created
	// 3. Main worktree is accessible
}

func TestE2E_RegisterAsIsWithoutRemote(t *testing.T) {
	env := SetupTestEnv(t)

	// Create a local repository without a remote
	repoPath := filepath.Join(env.RootDir, "local-repo")
	env.RunCommand(t, env.RootDir, "git", "init", repoPath)

	// Configure git for this repo
	env.RunCommand(t, repoPath, "git", "config", "user.name", "Test User")
	env.RunCommand(t, repoPath, "git", "config", "user.email", "test@example.com")

	// Create initial commit
	WriteFile(t, filepath.Join(repoPath, "README.md"), "# Test Repo\n")
	env.RunCommand(t, repoPath, "git", "add", "README.md")
	env.RunCommand(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, repoPath, "git", "branch", "-M", "main")

	// Verify no remote exists
	remotes := env.RunCommand(t, repoPath, "git", "remote")
	if strings.TrimSpace(remotes) != "" {
		t.Fatalf("Expected no remotes, got: %s", remotes)
	}

	t.Skip("Interactive registration requires user input - will implement with option flag")

	// After registration, verify:
	// 1. Repository is registered in global registry
	// 2. Uses local path for org/repo naming
}

func TestConversionCreatesValidHopConfig(t *testing.T) {
	env := SetupTestEnv(t)

	// Create a local repository without a remote
	repoPath := filepath.Join(env.RootDir, "local-repo")
	env.RunCommand(t, env.RootDir, "git", "init", repoPath)

	// Configure git
	env.RunCommand(t, repoPath, "git", "config", "user.name", "Test User")
	env.RunCommand(t, repoPath, "git", "config", "user.email", "test@example.com")

	// Create initial commit
	WriteFile(t, filepath.Join(repoPath, "README.md"), "# Test Repo\n")
	env.RunCommand(t, repoPath, "git", "add", "README.md")
	env.RunCommand(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, repoPath, "git", "branch", "-M", "main")

	// Skip the actual conversion test for now
	t.Skip("Need to implement non-interactive conversion flag")

	// Read hop.json
	hopConfigPath := filepath.Join(repoPath, "hop.json")
	content, err := os.ReadFile(hopConfigPath)
	if err != nil {
		t.Fatalf("Failed to read hop.json: %v", err)
	}

	// Parse and verify structure
	var config map[string]interface{}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("Failed to parse hop.json: %v", err)
	}

	// Verify repo section
	repo, ok := config["repo"].(map[string]interface{})
	if !ok {
		t.Fatal("hop.json missing 'repo' section")
	}

	// Verify uri is empty string (no remote)
	if uri, ok := repo["uri"].(string); !ok || uri != "" {
		t.Errorf("Expected empty uri for no remote, got: %v", uri)
	}

	// Verify org/repo are derived from path
	if org, ok := repo["org"].(string); !ok || org == "" {
		t.Error("Expected org to be set from path")
	}
	if repoName, ok := repo["repo"].(string); !ok || repoName == "" {
		t.Error("Expected repo to be set from path")
	}

	// Verify defaultBranch
	if branch, ok := repo["defaultBranch"].(string); !ok || branch != "main" {
		t.Errorf("Expected defaultBranch to be 'main', got: %v", branch)
	}

	// Verify structure
	if structure, ok := repo["structure"].(string); !ok || structure != "bare-worktree" {
		t.Errorf("Expected structure to be 'bare-worktree', got: %v", structure)
	}

	t.Log("hop.json structure is valid")
}
