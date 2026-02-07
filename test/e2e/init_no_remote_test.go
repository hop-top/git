package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
)

func TestE2E_InitConvertWithoutRemote(t *testing.T) {
	env := SetupTestEnv(t)
	fs := afero.NewOsFs()

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

	// Test conversion programmatically (option 2: regular repo + worktrees)
	// Since we can't automate interactive input yet, we use the converter directly
	g := git.New()
	converter := hop.NewConverter(fs, g)
	converter.KeepBackup = false

	// Convert to regular repo + worktrees (useBare=false)
	result, err := converter.ConvertToBareWorktree(repoPath, false, false)
	if err != nil {
		t.Fatalf("Conversion failed: %v", err)
		for _, errMsg := range result.Errors {
			t.Logf("Error: %s", errMsg)
		}
	}

	if !result.Success {
		t.Fatalf("Expected conversion success, got failure")
		for _, errMsg := range result.Errors {
			t.Logf("Error: %s", errMsg)
		}
	}

	// Verify hop.json was created
	hopConfigPath := filepath.Join(repoPath, "hop.json")
	if _, err := os.Stat(hopConfigPath); os.IsNotExist(err) {
		t.Errorf("hop.json not created")
	}

	// Verify worktrees directory was created (for future branches)
	worktreesDir := filepath.Join(repoPath, "worktrees")
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		t.Errorf("worktrees directory not created")
	}

	// For regular repo (option 2), main branch stays in repo root
	// Verify repo root still has files
	if _, err := os.Stat(filepath.Join(repoPath, "README.md")); os.IsNotExist(err) {
		t.Errorf("README.md not found in repo root after conversion")
	}

	// Verify .git is still a directory (not converted to bare)
	gitDir := filepath.Join(repoPath, ".git")
	stat, err := os.Stat(gitDir)
	if err != nil {
		t.Errorf(".git directory not found: %v", err)
	} else if !stat.IsDir() {
		t.Errorf(".git should be a directory, not a file")
	}

	// Verify hop.json structure
	hopConfig, err := os.ReadFile(hopConfigPath)
	if err != nil {
		t.Fatalf("Failed to read hop.json: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(hopConfig, &config); err != nil {
		t.Fatalf("Failed to parse hop.json: %v", err)
	}

	// Verify repo section
	repo, ok := config["repo"].(map[string]interface{})
	if !ok {
		t.Fatal("hop.json missing 'repo' section")
	}

	// Verify structure is "regular-worktree" (not "bare-worktree")
	if structure, ok := repo["structure"].(string); !ok || structure != "regular-worktree" {
		t.Errorf("Expected structure to be 'regular-worktree', got: %v", structure)
	}

	// Verify defaultBranch
	if branch, ok := repo["defaultBranch"].(string); !ok || branch != "main" {
		t.Errorf("Expected defaultBranch to be 'main', got: %v", branch)
	}

	// Verify org/repo are derived from path (no remote)
	if org, ok := repo["org"].(string); !ok || org == "" {
		t.Error("Expected org to be set from path")
	}
	if repoName, ok := repo["repo"].(string); !ok || repoName == "" {
		t.Error("Expected repo to be set from path")
	}

	// Verify uri is empty (no remote)
	if uri, ok := repo["uri"].(string); !ok || uri != "" {
		t.Errorf("Expected empty uri for no remote, got: %v", uri)
	}

	// Verify branches section
	branches, ok := config["branches"].(map[string]interface{})
	if !ok {
		t.Fatal("hop.json missing 'branches' section")
	}

	// Verify main branch is configured with path "." (repo root)
	mainBranch, ok := branches["main"].(map[string]interface{})
	if !ok {
		t.Fatal("hop.json missing 'main' branch configuration")
	}

	if path, ok := mainBranch["path"].(string); !ok || path != "." {
		t.Errorf("Expected main branch path to be '.', got: %v", path)
	}

	// Verify warnings about no remote
	foundWarning := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "No remote configured") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("Expected warning about no remote")
	}

	t.Log("Conversion test successful!")
	t.Log("Repository path:", repoPath)
	t.Log("Structure: regular-worktree (option 2)")
	for _, warning := range result.Warnings {
		t.Logf("Warning: %s", warning)
	}
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
