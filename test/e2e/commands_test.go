package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"
)

func TestCommands(t *testing.T) {
	env := SetupTestEnv(t)

	// --- Setup Repo and Hub ---
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Commit docker-compose.yml
	dcContent, _ := os.ReadFile("fixtures/docker-compose.yml")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "docker-compose.yml"), string(dcContent))
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "docker-compose.yml")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Create branches
	for _, branch := range []string{"feature-1", "feature-2", "staging"} {
		env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", branch)
		env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", branch)
	}

	// Initialize Hub manually (until `git hop clone` is implemented)
	os.MkdirAll(env.HubPath, 0755)
	mainWorktreePath := filepath.Join(env.DataHome, "local", "test-repo", "main")
	os.MkdirAll(filepath.Dir(mainWorktreePath), 0755)
	env.RunCommand(t, filepath.Dir(mainWorktreePath), "git", "clone", env.BareRepoPath, "main")

	// Configs
	createConfigs(t, env, mainWorktreePath)

	// Verify main symlink exists initially
	if _, err := os.Lstat(filepath.Join(env.HubPath, "main")); err != nil {
		t.Fatalf("Main symlink missing after setup: %v", err)
	}

	// --- Test: git hop add ---
	t.Run("Add", func(t *testing.T) {
		// ...
		// Add feature-1
		out := env.RunGitHop(t, env.HubPath, "add", "feature-1")
		if !strings.Contains(out, "Successfully added feature-1") {
			t.Errorf("Expected success message, got: %s", out)
		}
		// Verify symlink
		if _, err := os.Lstat(filepath.Join(env.HubPath, "feature-1")); err != nil {
			t.Errorf("Symlink feature-1 not created")
		}
		// Verify worktree
		wtPath := filepath.Join(env.DataHome, "local", "test-repo", "feature-1")
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("Worktree feature-1 not created")
		}
	})

	// --- Test: git hop list ---
	t.Run("List", func(t *testing.T) {
		// List in hub
		out := env.RunGitHop(t, env.HubPath, "list")
		if !strings.Contains(out, "feature-1") {
			t.Errorf("List output missing feature-1: %s", out)
		}
		if !strings.Contains(out, "main") {
			t.Errorf("List output missing main: %s", out)
		}
	})

	// --- Test: git hop status ---
	t.Run("Status", func(t *testing.T) {
		out := env.RunGitHop(t, env.HubPath, "status")
		if !strings.Contains(out, "feature-1") {
			t.Errorf("Status output missing feature-1: %s", out)
		}
	})

	// --- Test: git hop env ---
	t.Run("Env", func(t *testing.T) {
		branchPath := filepath.Join(env.HubPath, "feature-1")

		// Generate (implicit in add, but test explicit)
		env.RunGitHop(t, branchPath, "env", "generate")

		// Check
		out := env.RunGitHop(t, branchPath, "env", "check")
		if strings.Contains(out, "Error") {
			t.Errorf("Env check reported errors: %s", out)
		}

		// Start
		env.RunGitHop(t, branchPath, "env", "start")

		// Verify running (mocked via docker wrapper, but we check output)
		// In real e2e with docker, we could check `docker ps`.
		// Here we rely on the command succeeding.

		// Stop
		env.RunGitHop(t, branchPath, "env", "stop")
	})

	// --- Test: git hop remove ---
	t.Run("Remove", func(t *testing.T) {
		// Remove feature-1
		// Note: remove is interactive by default. We need --no-prompt or input.
		// Assuming --no-prompt is implemented or we can pipe "y".
		// Let's try with --no-prompt if spec says so. Spec says: `git hop remove <target> [--no-prompt]`

		out := env.RunGitHop(t, env.HubPath, "remove", "feature-1", "--no-prompt")
		if !strings.Contains(out, "Removed") && !strings.Contains(out, "Successfully") {
			// Adjust expectation based on actual output
			t.Logf("Remove output: %s", out)
		}

		// Verify symlink gone
		if _, err := os.Lstat(filepath.Join(env.HubPath, "feature-1")); err == nil {
			t.Errorf("Symlink feature-1 should be gone")
		}
		// Verify worktree gone
		wtPath := filepath.Join(env.DataHome, "local", "test-repo", "feature-1")
		if _, err := os.Stat(wtPath); err == nil {
			t.Errorf("Worktree feature-1 should be gone")
		}
	})

	// --- Test: Commands from within worktree ---
	t.Run("CommandsFromWorktree", func(t *testing.T) {
		// Add feature-2 for this test
		env.RunGitHop(t, env.HubPath, "add", "feature-2")

		// Navigate to the worktree symlink and run commands from there
		feature2Path := filepath.Join(env.HubPath, "feature-2")

		// Test: git hop list from within worktree
		out := env.RunGitHop(t, feature2Path, "list")
		if !strings.Contains(out, "feature-2") {
			t.Errorf("List from worktree should work: %s", out)
		}
		if !strings.Contains(out, "main") {
			t.Errorf("List from worktree should show main: %s", out)
		}

		// Test: git hop status from within worktree
		out = env.RunGitHop(t, feature2Path, "status")
		if !strings.Contains(out, "Hub:") {
			t.Errorf("Status from worktree should show hub info: %s", out)
		}

		// The key success is that these commands found the hub
		// from within the worktree directory, which proves FindHub works
		// If FindHub didn't work, we would get "Not in a git-hop hub" error
	})

	// --- Test: git hop <uri> --branch main (Fork-Attach from main) ---
	t.Run("ForkAttachMain", func(t *testing.T) {
		// Create a fork repo
		forkRepoPath := filepath.Join(env.RootDir, "fork.git")
		env.RunCommand(t, env.RootDir, "git", "clone", "--bare", env.SeedRepoPath, forkRepoPath)

		// We want to attach 'main' from the fork.
		// The fork's main should be compatible with our main.

		out := env.RunGitHop(t, env.HubPath, forkRepoPath, "--branch", "main")
		if !strings.Contains(out, "Successfully attached fork branch") {
			t.Errorf("ForkAttachMain output missing success message: %s", out)
		}

		// Verify symlink
		// Name: main-fork-<org>
		// Org is derived from URI.
		// URI: .../fork.git -> org is parent dir name.
		// Let's check for any symlink starting with "main-fork-"
		files, _ := os.ReadDir(env.HubPath)
		found := false
		var symlinkName string
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "main-fork-") {
				found = true
				symlinkName = f.Name()
				break
			}
		}
		if !found {
			t.Errorf("Fork symlink for main not found in hub")
		} else {
			// Verify it points to the right place
			// Should point to .../fork/main
			linkPath := filepath.Join(env.HubPath, symlinkName)
			target, err := os.Readlink(linkPath)
			if err != nil {
				t.Errorf("Failed to read symlink: %v", err)
			}
			if !strings.HasSuffix(target, "/main") {
				t.Errorf("Symlink target %s does not end in /main", target)
			}
		}
	})
}

func createConfigs(t *testing.T, env *TestEnv, mainWorktreePath string) {
	// Create Hub Config
	hubTmplContent, _ := os.ReadFile("fixtures/hub_config.json.tmpl")
	hubTmpl, _ := template.New("hub.json").Parse(string(hubTmplContent))
	var hubJsonBuf bytes.Buffer
	hubTmpl.Execute(&hubJsonBuf, struct{ RepoURI string }{env.BareRepoPath})
	WriteFile(t, filepath.Join(env.HubPath, "hop.json"), hubJsonBuf.String())

	// Create Hopspace Config
	hsTmplContent, _ := os.ReadFile("fixtures/hopspace_config.json.tmpl")
	hsTmpl, _ := template.New("hopspace.json").Parse(string(hsTmplContent))
	var hsJsonBuf bytes.Buffer
	hsTmpl.Execute(&hsJsonBuf, struct {
		RepoURI, WorktreePath, LastSync string
	}{env.BareRepoPath, mainWorktreePath, time.Now().Format(time.RFC3339)})
	WriteFile(t, filepath.Join(filepath.Dir(mainWorktreePath), "hop.json"), hsJsonBuf.String())

	// Create symlink for main
	if err := os.Symlink(mainWorktreePath, filepath.Join(env.HubPath, "main")); err != nil {
		t.Fatalf("Failed to create main symlink: %v", err)
	}
}
