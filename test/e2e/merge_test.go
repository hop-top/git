package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMergeEnv creates a hub with "main" default branch + a "feature/work" branch.
// The feature branch has one commit so merge can actually run.
// Returns (*TestEnv, featureBranchName).
func setupMergeEnv(t *testing.T) (*TestEnv, string) {
	t.Helper()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature/work")

	// Add a commit on feature/work so the merge has something to do
	featurePath := filepath.Join(env.HubPath, "hops", "feature/work")
	env.RunCommand(t, featurePath, "git", "commit", "--allow-empty", "-m", "feature work")

	return env, "feature/work"
}

func TestMerge_TwoArg_MergesAndCleansUp(t *testing.T) {
	env, featureBranch := setupMergeEnv(t)

	env.RunGitHop(t, env.HubPath, "merge", featureBranch, "main")

	// Source dir should be gone
	srcPath := filepath.Join(env.HubPath, "hops", featureBranch)
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("expected source worktree dir to be gone: %s", srcPath)
	}

	// Hub config should not have source branch
	data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatalf("failed to read hop.json: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse hop.json: %v", err)
	}
	branches, _ := cfg["branches"].(map[string]interface{})
	if _, exists := branches[featureBranch]; exists {
		t.Errorf("expected %s to be removed from hop.json branches", featureBranch)
	}

	// current symlink should resolve to into-branch (main) worktree
	currentLink := filepath.Join(env.HubPath, "current")
	target, err := os.Readlink(currentLink)
	if err != nil {
		t.Fatalf("failed to read current symlink: %v", err)
	}
	absTarget, _ := filepath.Abs(filepath.Join(env.HubPath, target))
	mainPath := filepath.Join(env.HubPath, "hops", "main")
	if absTarget != mainPath {
		t.Errorf("expected current symlink → %s, got %s", mainPath, absTarget)
	}
}

func TestMerge_OneArg_FromInsideWorktree(t *testing.T) {
	env, featureBranch := setupMergeEnv(t)

	// Run 1-arg from inside the feature worktree
	featurePath := filepath.Join(env.HubPath, "hops", featureBranch)
	env.RunGitHop(t, featurePath, "merge", "main")

	// Source dir should be gone
	if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
		t.Errorf("expected source worktree dir to be gone: %s", featurePath)
	}

	// Into-branch (main) worktree should still exist
	mainPath := filepath.Join(env.HubPath, "hops", "main")
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("expected main worktree to still exist: %v", err)
	}
}

func TestMerge_SourceDirRemoved(t *testing.T) {
	env, featureBranch := setupMergeEnv(t)

	env.RunGitHop(t, env.HubPath, "merge", featureBranch, "main")

	srcPath := filepath.Join(env.HubPath, "hops", featureBranch)
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("source worktree directory should not exist after merge: %s", srcPath)
	}
}

func TestMerge_IntoBranchWorktreeIntact(t *testing.T) {
	env, featureBranch := setupMergeEnv(t)

	env.RunGitHop(t, env.HubPath, "merge", featureBranch, "main")

	mainPath := filepath.Join(env.HubPath, "hops", "main")
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("into-branch (main) worktree should still exist: %v", err)
	}
}

func TestMerge_HubConfigUpdated(t *testing.T) {
	env, featureBranch := setupMergeEnv(t)

	env.RunGitHop(t, env.HubPath, "merge", featureBranch, "main")

	data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatalf("failed to read hop.json: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse hop.json: %v", err)
	}
	branches, _ := cfg["branches"].(map[string]interface{})

	if _, exists := branches[featureBranch]; exists {
		t.Errorf("expected %s to be removed from hop.json branches", featureBranch)
	}
	if _, exists := branches["main"]; !exists {
		t.Error("expected main to still be present in hop.json branches")
	}
}

func TestMerge_CurrentSymlinkUpdated(t *testing.T) {
	env, featureBranch := setupMergeEnv(t)

	env.RunGitHop(t, env.HubPath, "merge", featureBranch, "main")

	currentLink := filepath.Join(env.HubPath, "current")
	target, err := os.Readlink(currentLink)
	if err != nil {
		t.Fatalf("failed to read current symlink: %v", err)
	}
	absTarget, _ := filepath.Abs(filepath.Join(env.HubPath, target))
	mainPath := filepath.Join(env.HubPath, "hops", "main")
	if absTarget != mainPath {
		t.Errorf("expected current symlink → %s, got %s", mainPath, absTarget)
	}
}

func TestMerge_SameBranch_Blocked(t *testing.T) {
	env, _ := setupMergeEnv(t)

	result := runCommandExpectError(t, env, env.HubPath, env.BinPath, "merge", "main", "main")
	if !strings.Contains(result, "differ") && !strings.Contains(result, "same") && !strings.Contains(result, "differ") {
		t.Errorf("expected same-branch error, got: %s", result)
	}
}

func TestMerge_SourceNotInHub_Blocked(t *testing.T) {
	env, _ := setupMergeEnv(t)

	result := runCommandExpectError(t, env, env.HubPath, env.BinPath, "merge", "nonexistent/branch", "main")
	if !strings.Contains(result, "not found") && !strings.Contains(result, "nonexistent") {
		t.Errorf("expected 'not found' error for non-existent source, got: %s", result)
	}
}

func TestMerge_NoFF_Flag(t *testing.T) {
	env, featureBranch := setupMergeEnv(t)

	env.RunGitHop(t, env.HubPath, "merge", "--no-ff", featureBranch, "main")

	// Verify merge commit exists: main should have a merge commit in its log
	mainPath := filepath.Join(env.HubPath, "hops", "main")
	out := env.RunCommand(t, mainPath, "git", "log", "--oneline", "--merges", "-1")
	if out == "" {
		t.Error("expected a merge commit in main after --no-ff merge, got none")
	}
}
