package hop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/git"
	"github.com/spf13/afero"
)

func TestConvertToBareWorktree_NoRemote(t *testing.T) {
	// Create a real temp directory for git operations
	tmpDir, err := os.MkdirTemp("", "git-hop-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test-repo")

	// Initialize a real git repository
	cmd := exec.Command("git", "init", repoPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git
	exec.Command("git", "-C", repoPath, "config", "user.name", "Test User").Run()
	exec.Command("git", "-C", repoPath, "config", "user.email", "test@example.com").Run()

	// Create initial commit
	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	exec.Command("git", "-C", repoPath, "add", "README.md").Run()
	exec.Command("git", "-C", repoPath, "commit", "-m", "Initial commit").Run()
	exec.Command("git", "-C", repoPath, "branch", "-M", "main").Run()

	// Verify no remote exists
	cmd = exec.Command("git", "-C", repoPath, "remote")
	output, _ := cmd.Output()
	if len(output) > 0 {
		t.Fatalf("Expected no remotes, got: %s", output)
	}

	fs := afero.NewOsFs()
	g := git.New()
	converter := NewConverter(fs, g)
	converter.KeepBackup = false

	// Test regular repo conversion (useBare=false)
	result, err := converter.ConvertToBareWorktree(repoPath, false, false)

	// Should succeed without requiring remote URL
	if err != nil {
		t.Errorf("Conversion failed: %v", err)
		for _, errMsg := range result.Errors {
			t.Logf("Error: %s", errMsg)
		}
		t.FailNow()
	}

	if !result.Success {
		t.Errorf("Expected conversion success, got failure")
		for _, errMsg := range result.Errors {
			t.Logf("Error: %s", errMsg)
		}
		t.FailNow()
	}

	// Verify hop.json was created
	hopConfigPath := filepath.Join(repoPath, "hop.json")
	if _, err := os.Stat(hopConfigPath); os.IsNotExist(err) {
		t.Errorf("hop.json not created")
	}

	// Verify worktrees directory was created
	worktreesDir := filepath.Join(repoPath, "worktrees")
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		t.Errorf("worktrees directory not created")
	}

	// For regular repo, main branch is repo root, not in worktrees/
	// Verify repo root still has files
	if _, err := os.Stat(filepath.Join(repoPath, "README.md")); os.IsNotExist(err) {
		t.Errorf("README.md not in repo root")
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
		t.Errorf("Expected warning about no remote, warnings: %v", result.Warnings)
	}

	t.Logf("Conversion successful!")
	t.Logf("Backup path: %s", result.BackupPath)
	for _, warning := range result.Warnings {
		t.Logf("Warning: %s", warning)
	}
}

func TestConvertToBareWorktree_BareNoRemote(t *testing.T) {
	// Create a real temp directory for git operations
	tmpDir, err := os.MkdirTemp("", "git-hop-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test-repo")

	// Initialize a real git repository
	cmd := exec.Command("git", "init", repoPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git
	exec.Command("git", "-C", repoPath, "config", "user.name", "Test User").Run()
	exec.Command("git", "-C", repoPath, "config", "user.email", "test@example.com").Run()

	// Create initial commit
	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	exec.Command("git", "-C", repoPath, "add", "README.md").Run()
	exec.Command("git", "-C", repoPath, "commit", "-m", "Initial commit").Run()
	exec.Command("git", "-C", repoPath, "branch", "-M", "main").Run()

	// Verify no remote exists
	cmd = exec.Command("git", "-C", repoPath, "remote")
	output, _ := cmd.Output()
	if len(output) > 0 {
		t.Fatalf("Expected no remotes, got: %s", output)
	}

	// Test conversion
	fs := afero.NewOsFs()
	g := git.New()
	converter := NewConverter(fs, g)
	converter.KeepBackup = true // Keep backup for inspection

	// Test bare repo conversion (useBare=true)
	result, err := converter.ConvertToBareWorktree(repoPath, true, false)

	if err != nil {
		t.Errorf("Conversion failed: %v", err)
		for _, errMsg := range result.Errors {
			t.Logf("Error: %s", errMsg)
		}
		t.FailNow()
	}

	if !result.Success {
		t.Errorf("Expected conversion success, got failure")
		for _, errMsg := range result.Errors {
			t.Logf("Error: %s", errMsg)
		}
		t.FailNow()
	}

	// Verify hop.json was created
	hopConfigPath := filepath.Join(repoPath, "hop.json")
	if _, err := os.Stat(hopConfigPath); os.IsNotExist(err) {
		t.Errorf("hop.json not created")
	}

	// Verify worktrees directory was created
	worktreesDir := filepath.Join(repoPath, "worktrees")
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		t.Errorf("worktrees directory not created")
	}

	// Verify warnings about no remote
	foundWarning := false
	for _, warning := range result.Warnings {
		if warning == "No remote configured - using local path for backup organization" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("Expected warning about no remote, warnings: %v", result.Warnings)
	}

	t.Logf("Conversion successful!")
	t.Logf("Backup path: %s", result.BackupPath)
	for _, warning := range result.Warnings {
		t.Logf("Warning: %s", warning)
	}
}
