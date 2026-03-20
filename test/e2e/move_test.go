package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMoveEnv creates a hub with a "main" worktree and one feature branch ready to move.
func setupMoveEnv(t *testing.T) (*TestEnv, string) {
	t.Helper()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature/old")

	return env, "feature/old"
}

func TestMove_TwoArg_RenamesWorktree(t *testing.T) {
	env, _ := setupMoveEnv(t)

	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	// Old worktree dir should be gone
	oldPath := filepath.Join(env.HubPath, "hops", "feature/old")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expected old worktree dir to be gone: %s", oldPath)
	}

	// New worktree dir should exist
	newPath := filepath.Join(env.HubPath, "hops", "feature/new")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("expected new worktree dir to exist at %s: %v", newPath, err)
	}
}

func TestMove_GitBranchRenamed(t *testing.T) {
	env, _ := setupMoveEnv(t)
	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	// git should know feature/new, not feature/old
	mainPath := filepath.Join(env.HubPath, "hops", "main")
	out := env.RunCommand(t, mainPath, "git", "branch", "--list", "feature/new")
	if !strings.Contains(out, "feature/new") {
		t.Errorf("expected feature/new branch to exist; got: %s", out)
	}
	out2 := env.RunCommand(t, mainPath, "git", "branch", "--list", "feature/old")
	if strings.Contains(out2, "feature/old") {
		t.Errorf("expected feature/old branch to be gone; got: %s", out2)
	}
}

func TestMove_HubConfigUpdated(t *testing.T) {
	env, _ := setupMoveEnv(t)
	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	// Read hop.json directly
	data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatalf("failed to read hop.json: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse hop.json: %v", err)
	}
	branches, _ := cfg["branches"].(map[string]interface{})
	if _, exists := branches["feature/new"]; !exists {
		t.Error("expected feature/new in hop.json branches")
	}
	if _, exists := branches["feature/old"]; exists {
		t.Error("expected feature/old to be removed from hop.json branches")
	}
}

func TestMove_OneArg_FromInsideWorktree(t *testing.T) {
	env, _ := setupMoveEnv(t)

	// Determine what the worktree path actually is
	worktreePath := filepath.Join(env.HubPath, "hops", "feature/old")

	env.RunGitHop(t, worktreePath, "move", "feature/renamed")

	newPath := filepath.Join(env.HubPath, "hops", "feature/renamed")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("expected new worktree dir at %s: %v", newPath, err)
	}
}

func TestMove_DefaultBranch_Blocked(t *testing.T) {
	env, _ := setupMoveEnv(t)

	result := runCommandExpectError(t, env, env.HubPath, env.BinPath, "move", "main", "main-renamed")
	if !strings.Contains(result, "default branch") {
		t.Errorf("expected 'default branch' error, got: %s", result)
	}
}

func TestMove_NewNameAlreadyExists_Blocked(t *testing.T) {
	env, _ := setupMoveEnv(t)
	env.RunGitHop(t, env.HubPath, "add", "feature/other")

	result := runCommandExpectError(t, env, env.HubPath, env.BinPath, "move", "feature/old", "feature/other")
	if !strings.Contains(result, "already exists") {
		t.Errorf("expected 'already exists' error, got: %s", result)
	}
}

func TestMove_UpdatesCurrentSymlink(t *testing.T) {
	env, _ := setupMoveEnv(t)

	// Point current symlink at the old worktree — use relative path like UpdateCurrentSymlink does
	oldPath := filepath.Join(env.HubPath, "hops", "feature/old")
	relOldPath, _ := filepath.Rel(env.HubPath, oldPath)
	currentLink := filepath.Join(env.HubPath, "current")
	os.Remove(currentLink)
	if err := os.Symlink(relOldPath, currentLink); err != nil {
		t.Fatalf("failed to create current symlink: %v", err)
	}

	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	target, err := os.Readlink(currentLink)
	if err != nil {
		t.Fatalf("failed to read current symlink: %v", err)
	}
	newPath := filepath.Join(env.HubPath, "hops", "feature/new")
	absTarget, _ := filepath.Abs(filepath.Join(env.HubPath, target))
	if absTarget != newPath {
		t.Errorf("expected current symlink to point to %s, got %s", newPath, absTarget)
	}
}

func TestMove_CurrentSymlink_UnrelatedNotChanged(t *testing.T) {
	env, _ := setupMoveEnv(t)
	env.RunGitHop(t, env.HubPath, "add", "feature/other")

	// Point current symlink at feature/other — use relative path like UpdateCurrentSymlink does
	otherPath := filepath.Join(env.HubPath, "hops", "feature/other")
	relOtherPath, _ := filepath.Rel(env.HubPath, otherPath)
	currentLink := filepath.Join(env.HubPath, "current")
	os.Remove(currentLink)
	os.Symlink(relOtherPath, currentLink)

	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	target, _ := os.Readlink(currentLink)
	absTarget, _ := filepath.Abs(filepath.Join(env.HubPath, target))
	if absTarget != otherPath {
		t.Errorf("expected current symlink to still point to %s, got %s", otherPath, absTarget)
	}
}

func TestMove_PreHook_EnvVars(t *testing.T) {
	env, _ := setupMoveEnv(t)

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	os.MkdirAll(hooksDir, 0755)

	markerFile := filepath.Join(env.RootDir, "move-hook-marker")
	hookContent := "#!/bin/bash\necho \"OLD=$GIT_HOP_OLD_BRANCH NEW=$GIT_HOP_NEW_BRANCH\" > " + markerFile + "\n"
	hookPath := filepath.Join(hooksDir, "pre-worktree-move")
	WriteFile(t, hookPath, hookContent)
	os.Chmod(hookPath, 0755)

	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	content, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("hook marker not written: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "OLD=feature/old") {
		t.Errorf("expected OLD=feature/old in hook output, got: %s", s)
	}
	if !strings.Contains(s, "NEW=feature/new") {
		t.Errorf("expected NEW=feature/new in hook output, got: %s", s)
	}
}

func TestMove_PreHook_Failure_Blocks(t *testing.T) {
	env, _ := setupMoveEnv(t)

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	os.MkdirAll(hooksDir, 0755)
	hookPath := filepath.Join(hooksDir, "pre-worktree-move")
	WriteFile(t, hookPath, "#!/bin/bash\nexit 1\n")
	os.Chmod(hookPath, 0755)

	runCommandExpectError(t, env, env.HubPath, env.BinPath, "move", "feature/old", "feature/new")

	// Old worktree should still be there
	oldPath := filepath.Join(env.HubPath, "hops", "feature/old")
	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("expected old worktree to still exist after blocked move: %v", err)
	}
}

func TestMove_PostHook_RunsAfterSuccess(t *testing.T) {
	env, _ := setupMoveEnv(t)

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	os.MkdirAll(hooksDir, 0755)
	markerFile := filepath.Join(env.RootDir, "post-move-marker")
	hookPath := filepath.Join(hooksDir, "post-worktree-move")
	WriteFile(t, hookPath, "#!/bin/bash\ntouch "+markerFile+"\n")
	os.Chmod(hookPath, 0755)

	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	if _, err := os.Stat(markerFile); err != nil {
		t.Errorf("expected post-worktree-move hook to have run: %v", err)
	}
}

func TestMove_PostHook_Failure_NonFatal(t *testing.T) {
	env, _ := setupMoveEnv(t)

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	os.MkdirAll(hooksDir, 0755)
	hookPath := filepath.Join(hooksDir, "post-worktree-move")
	WriteFile(t, hookPath, "#!/bin/bash\nexit 1\n")
	os.Chmod(hookPath, 0755)

	// Should NOT fail even though post hook exits 1
	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	// New path should exist — move completed
	newPath := filepath.Join(env.HubPath, "hops", "feature/new")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("expected move to have completed despite failing post-hook: %v", err)
	}
}

func TestMove_OldPathGone_NewPathExists(t *testing.T) {
	env, _ := setupMoveEnv(t)
	env.RunGitHop(t, env.HubPath, "move", "feature/old", "feature/new")

	oldPath := filepath.Join(env.HubPath, "hops", "feature/old")
	newPath := filepath.Join(env.HubPath, "hops", "feature/new")

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old path should not exist: %s", oldPath)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new path should exist: %s, err: %v", newPath, err)
	}
}

func TestMove_OneArg_FailsOutsideWorktree(t *testing.T) {
	env, _ := setupMoveEnv(t)

	// Call from hub root — hub is a bare repo, GetCurrentBranch will fail/return empty
	result := runCommandExpectError(t, env, env.HubPath, env.BinPath, "move", "feature/new")
	if !strings.Contains(result, "branch") {
		t.Errorf("expected an error about branch detection, got: %s", result)
	}
}
