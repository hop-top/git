package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHooks_PreWorktreeAdd_GitFlow(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}

	preWorktreeAddHook := filepath.Join(hooksDir, "pre-worktree-add")
	hookContent := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"
MARKER_DIR="/tmp/git-hop-pre-add-markers"
mkdir -p "$MARKER_DIR"

# Use sanitized branch name for filename (replace / with -)
SAFE_BRANCH=$(echo "$BRANCH" | tr '/' '-')

case "$BRANCH" in
    feature/*|release/*|hotfix/*|support/*)
        echo "GIT_FLOW_DETECTED=true" >> "$MARKER_DIR/$SAFE_BRANCH.marker"
        ;;
    *)
        echo "GIT_FLOW_DETECTED=false" >> "$MARKER_DIR/$SAFE_BRANCH.marker"
        ;;
esac

exit 0
`
	WriteFile(t, preWorktreeAddHook, hookContent)
	if err := os.Chmod(preWorktreeAddHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	tests := []struct {
		branch     string
		expectFlow bool
	}{
		{"feature/test-feature", true},
		{"release/v1.0.0", true},
		{"hotfix/critical-fix", true},
		{"support/legacy-support", true},
		{"regular-branch", false},
	}

	markerDir := "/tmp/git-hop-pre-add-markers"
	os.RemoveAll(markerDir)

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			env.RunGitHop(t, env.HubPath, "add", tt.branch)

			// Use sanitized branch name for filename
			safeBranch := strings.ReplaceAll(tt.branch, "/", "-")
			markerFile := filepath.Join(markerDir, safeBranch+".marker")
			content, err := os.ReadFile(markerFile)
			if err != nil {
				t.Fatalf("Hook marker file not found at %s: %v", markerFile, err)
			}

			contentStr := string(content)
			if tt.expectFlow {
				if !strings.Contains(contentStr, "GIT_FLOW_DETECTED=true") {
					t.Errorf("Expected git-flow detection for branch %s, got: %s", tt.branch, contentStr)
				}
			} else {
				if !strings.Contains(contentStr, "GIT_FLOW_DETECTED=false") {
					t.Errorf("Expected non-git-flow detection for branch %s, got: %s", tt.branch, contentStr)
				}
			}

			env.RunGitHop(t, env.HubPath, "remove", tt.branch, "--no-prompt")
		})
	}

	os.RemoveAll(markerDir)
}

func TestHooks_PostWorktreeAdd(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}

	postWorktreeAddHook := filepath.Join(hooksDir, "post-worktree-add")
	hookContent := `#!/bin/bash
set -e

cd "$GIT_HOP_WORKTREE_PATH"

echo "Hook executed for branch: $GIT_HOP_BRANCH" > .post-add-marker
echo "Repo ID: $GIT_HOP_REPO_ID" >> .post-add-marker
echo "Worktree Path: $GIT_HOP_WORKTREE_PATH" >> .post-add-marker

exit 0
`
	WriteFile(t, postWorktreeAddHook, hookContent)
	if err := os.Chmod(postWorktreeAddHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	branch := "test-post-hook"
	env.RunGitHop(t, env.HubPath, "add", branch)

	worktreePath := filepath.Join(env.HubPath, "hops", branch)
	markerFile := filepath.Join(worktreePath, ".post-add-marker")

	content, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("Post-hook marker file not found at %s: %v", markerFile, err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "Hook executed for branch: test-post-hook") {
		t.Errorf("Post-hook marker missing expected content, got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "Repo ID:") {
		t.Errorf("Post-hook marker missing repo ID, got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "Worktree Path:") {
		t.Errorf("Post-hook marker missing worktree path, got: %s", contentStr)
	}

	env.RunGitHop(t, env.HubPath, "remove", branch, "--no-prompt")
}

func TestHooks_PreWorktreeRemove_GitFlow(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	branch := "feature/remove-test"
	env.RunGitHop(t, env.HubPath, "add", branch)

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}

	preWorktreeRemoveHook := filepath.Join(hooksDir, "pre-worktree-remove")
	hookContent := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"

case "$BRANCH" in
    feature/*|release/*|hotfix/*)
        echo "GIT_FLOW_FINISH=true" > "$GIT_HOP_WORKTREE_PATH/.pre-remove-marker"
        echo "Branch: $BRANCH" >> "$GIT_HOP_WORKTREE_PATH/.pre-remove-marker"
        ;;
    *)
        echo "GIT_FLOW_FINISH=false" > "$GIT_HOP_WORKTREE_PATH/.pre-remove-marker"
        ;;
esac

exit 0
`
	WriteFile(t, preWorktreeRemoveHook, hookContent)
	if err := os.Chmod(preWorktreeRemoveHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	env.RunGitHop(t, env.HubPath, "remove", branch, "--no-prompt")

	markerFile := filepath.Join(env.HubPath, "hooks-execution-marker")
	if _, err := os.Stat(markerFile); err == nil {
		content, err := os.ReadFile(markerFile)
		if err == nil {
			contentStr := string(content)
			if !strings.Contains(contentStr, "pre-worktree-remove-executed") {
				t.Errorf("Pre-worktree-remove hook marker not found")
			}
		}
	}
}

func TestHooks_PostWorktreeRemove(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	branch := "test-post-remove"
	env.RunGitHop(t, env.HubPath, "add", branch)

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}

	postWorktreeRemoveHook := filepath.Join(hooksDir, "post-worktree-remove")
	hookContent := `#!/bin/bash
set -e

MARKER_DIR="/tmp/git-hop-test-markers"
mkdir -p "$MARKER_DIR"

echo "post-remove-hook-executed" >> "$MARKER_DIR/post-remove-$GIT_HOP_BRANCH.marker"
echo "Branch: $GIT_HOP_BRANCH" >> "$MARKER_DIR/post-remove-$GIT_HOP_BRANCH.marker"
echo "Repo: $GIT_HOP_REPO_ID" >> "$MARKER_DIR/post-remove-$GIT_HOP_BRANCH.marker"

exit 0
`
	WriteFile(t, postWorktreeRemoveHook, hookContent)
	if err := os.Chmod(postWorktreeRemoveHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	env.RunGitHop(t, env.HubPath, "remove", branch, "--no-prompt")

	markerFile := filepath.Join("/tmp", "git-hop-test-markers", "post-remove-"+branch+".marker")
	content, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("Post-worktree-remove hook marker not found at %s: %v", markerFile, err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "post-remove-hook-executed") {
		t.Errorf("Post-worktree-remove hook did not execute, got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "Branch: test-post-remove") {
		t.Errorf("Post-worktree-remove marker missing branch info, got: %s", contentStr)
	}

	os.RemoveAll("/tmp/git-hop-test-markers")
}

func TestHooks_PrioritySystem(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	globalHooksDir := filepath.Join(env.RootDir, ".config", "git-hop", "hooks")
	if err := os.MkdirAll(globalHooksDir, 0755); err != nil {
		t.Fatalf("Failed to create global hooks directory: %v", err)
	}

	globalHook := filepath.Join(globalHooksDir, "pre-worktree-add")
	globalHookContent := `#!/bin/bash
MARKER_DIR="/tmp/git-hop-hook-priority"
mkdir -p "$MARKER_DIR"
echo "GLOBAL_HOOK" > "$MARKER_DIR/$GIT_HOP_BRANCH.marker"
exit 0
`
	WriteFile(t, globalHook, globalHookContent)
	if err := os.Chmod(globalHook, 0755); err != nil {
		t.Fatalf("Failed to make global hook executable: %v", err)
	}

	markerDir := "/tmp/git-hop-hook-priority"
	os.RemoveAll(markerDir)

	branch1 := "test-global-hook"
	env.RunGitHop(t, env.HubPath, "add", branch1)

	markerFile1 := filepath.Join(markerDir, branch1+".marker")
	content1, err := os.ReadFile(markerFile1)
	if err != nil {
		t.Fatalf("Global hook marker not found: %v", err)
	}
	if !strings.Contains(string(content1), "GLOBAL_HOOK") {
		t.Errorf("Expected global hook to execute, got: %s", string(content1))
	}

	env.RunGitHop(t, env.HubPath, "remove", branch1, "--no-prompt")

	repoHooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(repoHooksDir, 0755); err != nil {
		t.Fatalf("Failed to create repo hooks directory: %v", err)
	}

	repoHook := filepath.Join(repoHooksDir, "pre-worktree-add")
	repoHookContent := `#!/bin/bash
MARKER_DIR="/tmp/git-hop-hook-priority"
mkdir -p "$MARKER_DIR"
echo "REPO_HOOK" > "$MARKER_DIR/$GIT_HOP_BRANCH.marker"
exit 0
`
	WriteFile(t, repoHook, repoHookContent)
	if err := os.Chmod(repoHook, 0755); err != nil {
		t.Fatalf("Failed to make repo hook executable: %v", err)
	}

	branch2 := "test-repo-hook"
	env.RunGitHop(t, env.HubPath, "add", branch2)

	markerFile2 := filepath.Join(markerDir, branch2+".marker")
	content2, err := os.ReadFile(markerFile2)
	if err != nil {
		t.Fatalf("Repo hook marker not found: %v", err)
	}
	if !strings.Contains(string(content2), "REPO_HOOK") {
		t.Errorf("Expected repo hook to execute (override global), got: %s", string(content2))
	}

	env.RunGitHop(t, env.HubPath, "remove", branch2, "--no-prompt")
	os.RemoveAll(markerDir)
}

func TestHooks_EnvironmentVariables(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}

	preWorktreeAddHook := filepath.Join(hooksDir, "pre-worktree-add")
	hookContent := `#!/bin/bash
set -e

MARKER_DIR="/tmp/git-hop-env-vars"
mkdir -p "$MARKER_DIR"

ENV_FILE="$MARKER_DIR/$GIT_HOP_BRANCH.env"

echo "GIT_HOP_HOOK_NAME=$GIT_HOP_HOOK_NAME" > "$ENV_FILE"
echo "GIT_HOP_WORKTREE_PATH=$GIT_HOP_WORKTREE_PATH" >> "$ENV_FILE"
echo "GIT_HOP_REPO_ID=$GIT_HOP_REPO_ID" >> "$ENV_FILE"
echo "GIT_HOP_BRANCH=$GIT_HOP_BRANCH" >> "$ENV_FILE"

exit 0
`
	WriteFile(t, preWorktreeAddHook, hookContent)
	if err := os.Chmod(preWorktreeAddHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	branch := "test-env-vars"
	env.RunGitHop(t, env.HubPath, "add", branch)

	envFile := filepath.Join("/tmp/git-hop-env-vars", branch+".env")

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("Hook environment variables file not found: %v", err)
	}

	contentStr := string(content)

	if !strings.Contains(contentStr, "GIT_HOP_HOOK_NAME=pre-worktree-add") {
		t.Errorf("Missing or incorrect GIT_HOP_HOOK_NAME, got: %s", contentStr)
	}

	if !strings.Contains(contentStr, "GIT_HOP_BRANCH=test-env-vars") {
		t.Errorf("Missing or incorrect GIT_HOP_BRANCH, got: %s", contentStr)
	}

	if !strings.Contains(contentStr, "GIT_HOP_REPO_ID=") {
		t.Errorf("Missing GIT_HOP_REPO_ID, got: %s", contentStr)
	}

	if !strings.Contains(contentStr, "GIT_HOP_WORKTREE_PATH=") {
		t.Errorf("Missing GIT_HOP_WORKTREE_PATH, got: %s", contentStr)
	}

	env.RunGitHop(t, env.HubPath, "remove", branch, "--no-prompt")
	os.RemoveAll("/tmp/git-hop-env-vars")
}

func TestHooks_HookFailure_BlocksOperation(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}

	failingHook := filepath.Join(hooksDir, "pre-worktree-add")
	hookContent := `#!/bin/bash
echo "This hook always fails"
exit 1
`
	WriteFile(t, failingHook, hookContent)
	if err := os.Chmod(failingHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	branch := "should-not-be-created"

	// Run command and expect it to fail
	cmd := exec.Command(env.BinPath, "add", branch)
	cmd.Dir = env.HubPath
	cmd.Env = env.EnvVars
	output, err := cmd.CombinedOutput()
	if err == nil {
		// If command succeeded, the hook didn't block - this is a bug
		t.Fatalf("Command should have failed due to failing hook, but it succeeded: %s", string(output))
	}

	// Verify the worktree was NOT created
	worktreePath := filepath.Join(env.HubPath, "hops", branch)
	if _, err := os.Stat(worktreePath); err == nil {
		// Clean up if worktree was created
		os.RemoveAll(worktreePath)
		t.Fatal("Worktree should not have been created when hook fails")
	}

	t.Logf("Hook correctly blocked operation: %s", string(output))
}

func TestHooks_CompleteGitFlowWorkflow(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create hooks directory: %v", err)
	}

	preAddHook := filepath.Join(hooksDir, "pre-worktree-add")
	preAddContent := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"

case "$BRANCH" in
    feature/*)
        NAME="${BRANCH#feature/}"
        echo "[pre-worktree-add] Git-flow: Starting feature '$NAME'"
        ;;
    release/*)
        NAME="${BRANCH#release/}"
        echo "[pre-worktree-add] Git-flow: Starting release '$NAME'"
        ;;
    hotfix/*)
        NAME="${BRANCH#hotfix/}"
        echo "[pre-worktree-add] Git-flow: Starting hotfix '$NAME'"
        ;;
esac

exit 0
`
	WriteFile(t, preAddHook, preAddContent)
	if err := os.Chmod(preAddHook, 0755); err != nil {
		t.Fatalf("Failed to make pre-add hook executable: %v", err)
	}

	postAddHook := filepath.Join(hooksDir, "post-worktree-add")
	postAddContent := `#!/bin/bash
set -e

cd "$GIT_HOP_WORKTREE_PATH"

echo "[post-worktree-add] Setting up dependencies for $GIT_HOP_BRANCH"

if [ -f package.json ]; then
    echo "[post-worktree-add] Installing npm dependencies..."
    npm ci --quiet
fi

exit 0
`
	WriteFile(t, postAddHook, postAddContent)
	if err := os.Chmod(postAddHook, 0755); err != nil {
		t.Fatalf("Failed to make post-add hook executable: %v", err)
	}

	preRemoveHook := filepath.Join(hooksDir, "pre-worktree-remove")
	preRemoveContent := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"

case "$BRANCH" in
    feature/*)
        NAME="${BRANCH#feature/}"
        echo "[pre-worktree-remove] Git-flow: Finishing feature '$NAME'"
        ;;
    release/*)
        NAME="${BRANCH#release/}"
        echo "[pre-worktree-remove] Git-flow: Finishing release '$NAME'"
        ;;
    hotfix/*)
        NAME="${BRANCH#hotfix/}"
        echo "[pre-worktree-remove] Git-flow: Finishing hotfix '$NAME'"
        ;;
esac

exit 0
`
	WriteFile(t, preRemoveHook, preRemoveContent)
	if err := os.Chmod(preRemoveHook, 0755); err != nil {
		t.Fatalf("Failed to make pre-remove hook executable: %v", err)
	}

	postRemoveHook := filepath.Join(hooksDir, "post-worktree-remove")
	postRemoveContent := `#!/bin/bash
set -e

echo "[post-worktree-remove] Cleanup complete for branch: $GIT_HOP_BRANCH"

exit 0
`
	WriteFile(t, postRemoveHook, postRemoveContent)
	if err := os.Chmod(postRemoveHook, 0755); err != nil {
		t.Fatalf("Failed to make post-remove hook executable: %v", err)
	}

	branch := "feature/complete-workflow-test"

	addOutput := env.RunGitHop(t, env.HubPath, "add", branch)
	t.Logf("Add output:\n%s", addOutput)

	worktreePath := filepath.Join(env.HubPath, "hops", "feature", "complete-workflow-test")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("Worktree not created at %s: %v", worktreePath, err)
	}

	removeOutput := env.RunGitHop(t, env.HubPath, "remove", branch, "--no-prompt")
	t.Logf("Remove output:\n%s", removeOutput)

	if _, err := os.Stat(worktreePath); err == nil {
		t.Errorf("Worktree still exists after removal at %s", worktreePath)
	}
}
