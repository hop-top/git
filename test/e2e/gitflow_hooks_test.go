package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitFlowIntegration_PreWorktreeAdd_HookExecution(t *testing.T) {
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
	hookScript := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"

MARKER_DIR="/tmp/git-hop-gitflow-start"
mkdir -p "$MARKER_DIR"

# Use sanitized branch name for filename
SAFE_BRANCH=$(echo "$BRANCH" | tr '/' '-')

echo "=== Pre-Worktree-Add Hook ==="
echo "Branch: $BRANCH"
echo "Worktree Path: $GIT_HOP_WORKTREE_PATH"

case "$BRANCH" in
    feature/*)
        TYPE="feature"
        NAME="${BRANCH#feature/}"
        echo "Detected git-flow branch: feature '$NAME'"
        echo "HOOK_DETECTED_TYPE=feature" > "$MARKER_DIR/$SAFE_BRANCH.marker"
        echo "HOOK_DETECTED_NAME=$NAME" >> "$MARKER_DIR/$SAFE_BRANCH.marker"
        ;;
    release/*)
        TYPE="release"
        NAME="${BRANCH#release/}"
        echo "Detected git-flow branch: release '$NAME'"
        echo "HOOK_DETECTED_TYPE=release" > "$MARKER_DIR/$SAFE_BRANCH.marker"
        echo "HOOK_DETECTED_NAME=$NAME" >> "$MARKER_DIR/$SAFE_BRANCH.marker"
        ;;
    hotfix/*)
        TYPE="hotfix"
        NAME="${BRANCH#hotfix/}"
        echo "Detected git-flow branch: hotfix '$NAME'"
        echo "HOOK_DETECTED_TYPE=hotfix" > "$MARKER_DIR/$SAFE_BRANCH.marker"
        echo "HOOK_DETECTED_NAME=$NAME" >> "$MARKER_DIR/$SAFE_BRANCH.marker"
        ;;
    support/*)
        TYPE="support"
        NAME="${BRANCH#support/}"
        echo "Detected git-flow branch: support '$NAME'"
        echo "HOOK_DETECTED_TYPE=support" > "$MARKER_DIR/$SAFE_BRANCH.marker"
        echo "HOOK_DETECTED_NAME=$NAME" >> "$MARKER_DIR/$SAFE_BRANCH.marker"
        ;;
    *)
        echo "Non git-flow branch: $BRANCH"
        echo "HOOK_DETECTED_TYPE=none" > "$MARKER_DIR/$SAFE_BRANCH.marker"
        ;;
esac

exit 0
`
	WriteFile(t, preWorktreeAddHook, hookScript)
	if err := os.Chmod(preWorktreeAddHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	tests := []struct {
		name         string
		branch       string
		expectedType string
		expectedName string
	}{
		{
			name:         "feature branch",
			branch:       "feature/user-authentication",
			expectedType: "feature",
			expectedName: "user-authentication",
		},
		{
			name:         "release branch",
			branch:       "release/v2.0.0",
			expectedType: "release",
			expectedName: "v2.0.0",
		},
		{
			name:         "hotfix branch",
			branch:       "hotfix/security-patch",
			expectedType: "hotfix",
			expectedName: "security-patch",
		},
		{
			name:         "support branch",
			branch:       "support/legacy-v1",
			expectedType: "support",
			expectedName: "legacy-v1",
		},
		{
			name:         "regular branch",
			branch:       "random-development-branch",
			expectedType: "none",
			expectedName: "",
		},
	}

	markerDir := "/tmp/git-hop-gitflow-start"
	os.RemoveAll(markerDir)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := env.RunGitHop(t, env.HubPath, "add", tt.branch)
			t.Logf("Add output:\n%s", output)

			safeBranch := strings.ReplaceAll(tt.branch, "/", "-")
			markerFile := filepath.Join(markerDir, safeBranch+".marker")
			content, err := os.ReadFile(markerFile)
			if err != nil {
				t.Fatalf("Git-flow marker file not found at %s: %v", markerFile, err)
			}

			contentStr := string(content)

			if !strings.Contains(contentStr, "HOOK_DETECTED_TYPE="+tt.expectedType) {
				t.Errorf("Expected type %s, got: %s", tt.expectedType, contentStr)
			}

			if tt.expectedName != "" && !strings.Contains(contentStr, "HOOK_DETECTED_NAME="+tt.expectedName) {
				t.Errorf("Expected name %s, got: %s", tt.expectedName, contentStr)
			}

			env.RunGitHop(t, env.HubPath, "remove", tt.branch, "--no-prompt")
		})
	}

	os.RemoveAll(markerDir)
}

func TestGitFlowIntegration_PreWorktreeRemove_HookExecution(t *testing.T) {
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

	preWorktreeRemoveHook := filepath.Join(hooksDir, "pre-worktree-remove")
	hookScript := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"

MARKER_DIR="/tmp/git-hop-gitflow-markers"
mkdir -p "$MARKER_DIR"

# Use sanitized branch name for filename
SAFE_BRANCH=$(echo "$BRANCH" | tr '/' '-')

echo "=== Pre-Worktree-Remove Hook ==="
echo "Branch: $BRANCH"

case "$BRANCH" in
    feature/*)
        TYPE="feature"
        NAME="${BRANCH#feature/}"
        echo "Detected git-flow branch: finishing feature '$NAME'"
        echo "FINISH_TYPE=feature" > "$MARKER_DIR/$SAFE_BRANCH.finish"
        echo "FINISH_NAME=$NAME" >> "$MARKER_DIR/$SAFE_BRANCH.finish"
        ;;
    release/*)
        TYPE="release"
        NAME="${BRANCH#release/}"
        echo "Detected git-flow branch: finishing release '$NAME'"
        echo "FINISH_TYPE=release" > "$MARKER_DIR/$SAFE_BRANCH.finish"
        echo "FINISH_NAME=$NAME" >> "$MARKER_DIR/$SAFE_BRANCH.finish"
        ;;
    hotfix/*)
        TYPE="hotfix"
        NAME="${BRANCH#hotfix/}"
        echo "Detected git-flow branch: finishing hotfix '$NAME'"
        echo "FINISH_TYPE=hotfix" > "$MARKER_DIR/$SAFE_BRANCH.finish"
        echo "FINISH_NAME=$NAME" >> "$MARKER_DIR/$SAFE_BRANCH.finish"
        ;;
    *)
        echo "Non git-flow branch: $BRANCH"
        echo "FINISH_TYPE=none" > "$MARKER_DIR/$SAFE_BRANCH.finish"
        ;;
esac

exit 0
`
	WriteFile(t, preWorktreeRemoveHook, hookScript)
	if err := os.Chmod(preWorktreeRemoveHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	markerDir := "/tmp/git-hop-gitflow-markers"
	os.RemoveAll(markerDir)

	tests := []struct {
		name         string
		branch       string
		expectedType string
		expectedName string
	}{
		{
			name:         "finish feature branch",
			branch:       "feature/awesome-feature",
			expectedType: "feature",
			expectedName: "awesome-feature",
		},
		{
			name:         "finish release branch",
			branch:       "release/v1.5.0",
			expectedType: "release",
			expectedName: "v1.5.0",
		},
		{
			name:         "finish hotfix branch",
			branch:       "hotfix/critical-bug",
			expectedType: "hotfix",
			expectedName: "critical-bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.RunGitHop(t, env.HubPath, "add", tt.branch)

			removeOutput := env.RunGitHop(t, env.HubPath, "remove", tt.branch, "--no-prompt")
			t.Logf("Remove output:\n%s", removeOutput)

			safeBranch := strings.ReplaceAll(tt.branch, "/", "-")
			markerFile := filepath.Join(markerDir, safeBranch+".finish")
			content, err := os.ReadFile(markerFile)
			if err != nil {
				t.Fatalf("Git-flow finish marker not found at %s: %v", markerFile, err)
			}

			contentStr := string(content)

			if !strings.Contains(contentStr, "FINISH_TYPE="+tt.expectedType) {
				t.Errorf("Expected finish type %s, got: %s", tt.expectedType, contentStr)
			}

			if tt.expectedName != "" && !strings.Contains(contentStr, "FINISH_NAME="+tt.expectedName) {
				t.Errorf("Expected finish name %s, got: %s", tt.expectedName, contentStr)
			}
		})
	}

	os.RemoveAll(markerDir)
}

func TestGitFlowIntegration_WorkflowTableFromDocs(t *testing.T) {
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
MARKER_DIR="/tmp/git-hop-workflow-test"
mkdir -p "$MARKER_DIR"

case "$BRANCH" in
    feature/*)
        NAME="${BRANCH#feature/}"
        echo "git flow feature start $NAME" >> "$MARKER_DIR/commands.log"
        ;;
    release/*)
        NAME="${BRANCH#release/}"
        echo "git flow release start $NAME" >> "$MARKER_DIR/commands.log"
        ;;
    hotfix/*)
        NAME="${BRANCH#hotfix/}"
        echo "git flow hotfix start $NAME" >> "$MARKER_DIR/commands.log"
        ;;
esac

exit 0
`
	WriteFile(t, preAddHook, preAddContent)
	if err := os.Chmod(preAddHook, 0755); err != nil {
		t.Fatalf("Failed to make pre-add hook executable: %v", err)
	}

	preRemoveHook := filepath.Join(hooksDir, "pre-worktree-remove")
	preRemoveContent := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"
MARKER_DIR="/tmp/git-hop-workflow-test"
mkdir -p "$MARKER_DIR"

case "$BRANCH" in
    feature/*)
        NAME="${BRANCH#feature/}"
        echo "git flow feature finish $NAME" >> "$MARKER_DIR/commands.log"
        ;;
    release/*)
        NAME="${BRANCH#release/}"
        echo "git flow release finish $NAME" >> "$MARKER_DIR/commands.log"
        ;;
    hotfix/*)
        NAME="${BRANCH#hotfix/}"
        echo "git flow hotfix finish $NAME" >> "$MARKER_DIR/commands.log"
        ;;
esac

exit 0
`
	WriteFile(t, preRemoveHook, preRemoveContent)
	if err := os.Chmod(preRemoveHook, 0755); err != nil {
		t.Fatalf("Failed to make pre-remove hook executable: %v", err)
	}

	markerDir := "/tmp/git-hop-workflow-test"
	os.RemoveAll(markerDir)

	branch := "feature/my-feature"

	addOutput := env.RunGitHop(t, env.HubPath, "add", branch)
	t.Logf("Add output:\n%s", addOutput)

	worktreePath := filepath.Join(env.HubPath, "hops", "feature", "my-feature")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("Worktree not created at %s: %v", worktreePath, err)
	}

	removeOutput := env.RunGitHop(t, env.HubPath, "remove", branch, "--no-prompt")
	t.Logf("Remove output:\n%s", removeOutput)

	commandsLog := filepath.Join(markerDir, "commands.log")
	content, err := os.ReadFile(commandsLog)
	if err != nil {
		t.Fatalf("Commands log not found: %v", err)
	}

	logContent := string(content)
	t.Logf("Commands log:\n%s", logContent)

	expectedCommands := []string{
		"git flow feature start my-feature",
		"git flow feature finish my-feature",
	}

	for _, expected := range expectedCommands {
		if !strings.Contains(logContent, expected) {
			t.Errorf("Expected command '%s' not found in log", expected)
		}
	}

	os.RemoveAll(markerDir)
}

func TestGitFlowIntegration_BranchNameValidation(t *testing.T) {
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
	validationHook := `#!/bin/bash
set -e

BRANCH="$GIT_HOP_BRANCH"

VALID_PREFIXES="^(feature|bugfix|hotfix|release)/"

if [[ ! "$BRANCH" =~ $VALID_PREFIXES ]]; then
    echo "❌ Invalid branch name: $BRANCH"
    echo "Branch must start with: feature/, bugfix/, hotfix/, or release/"
    exit 1
fi

echo "✓ Branch name is valid"
exit 0
`
	WriteFile(t, preAddHook, validationHook)
	if err := os.Chmod(preAddHook, 0755); err != nil {
		t.Fatalf("Failed to make hook executable: %v", err)
	}

	t.Run("valid branch names", func(t *testing.T) {
		validBranches := []string{
			"feature/valid-feature",
			"bugfix/valid-bugfix",
			"hotfix/valid-hotfix",
			"release/valid-release",
		}

		for _, branch := range validBranches {
			t.Run(branch, func(t *testing.T) {
				env.RunGitHop(t, env.HubPath, "add", branch)
				env.RunGitHop(t, env.HubPath, "remove", branch, "--no-prompt")
			})
		}
	})

	t.Run("invalid branch name should be blocked", func(t *testing.T) {
		invalidBranch := "invalid-branch-name"

		// Run command and expect it to fail
		cmd := exec.Command(env.BinPath, "add", invalidBranch)
		cmd.Dir = env.HubPath
		cmd.Env = env.EnvVars
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("Command should have failed due to invalid branch name, but it succeeded: %s", string(output))
		}

		// Verify the worktree was NOT created
		worktreePath := filepath.Join(env.HubPath, "hops", invalidBranch)
		if _, err := os.Stat(worktreePath); err == nil {
			os.RemoveAll(worktreePath)
			t.Fatal("Worktree should not have been created when branch name is invalid")
		}

		t.Logf("Hook correctly blocked invalid branch: %s", string(output))
	})
}
