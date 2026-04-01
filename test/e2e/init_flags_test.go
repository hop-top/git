package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInit_NoHooks_SkipsHooksDir verifies that --no-hooks prevents creation
// of .git-hop/hooks/ in the main worktree.
func TestInit_NoHooks_SkipsHooksDir(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := setupInitRepo(t)

	// Clone with --no-hooks; git-hop clone doesn't expose init flags directly,
	// so we clone normally then re-init with --no-hooks on the resulting hub.
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Remove hooks dir that clone created, then re-init with --no-hooks
	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	os.RemoveAll(hooksDir)

	env.RunGitHopCombined(t, env.HubPath, "init", "--no-hooks")

	if _, err := os.Stat(hooksDir); !os.IsNotExist(err) {
		t.Errorf(".git-hop/hooks/ should NOT be created with --no-hooks, but it exists: %s", hooksDir)
	}
}

// TestInit_DefaultsNoShellIntegration verifies that running init without
// --enable-chdir does NOT modify any shell RC file.
func TestInit_DefaultsNoShellIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := setupInitRepo(t)
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// No shell RC files should have been created in our isolated HOME
	bashrc := filepath.Join(env.RootDir, ".bashrc")
	zshrc := filepath.Join(env.RootDir, ".zshrc")

	for _, rc := range []string{bashrc, zshrc} {
		if _, err := os.Stat(rc); !os.IsNotExist(err) {
			content, _ := os.ReadFile(rc)
			if strings.Contains(string(content), "git-hop") {
				t.Errorf("shell integration written to %s without --enable-chdir", rc)
			}
		}
	}
}

// TestInit_EnableChdir_InstallsShellWrapper verifies that --enable-chdir
// writes the shell wrapper to the RC file.
func TestInit_EnableChdir_InstallsShellWrapper(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := setupInitRepo(t)
	// Add SHELL so shell detection works in the isolated environment
	env.EnvVars = append(env.EnvVars, "SHELL=/bin/bash")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Re-init with --enable-chdir from inside the hub
	env.RunGitHop(t, env.HubPath, "init", "--enable-chdir")

	bashrc := filepath.Join(env.RootDir, ".bashrc")
	content, readErr := os.ReadFile(bashrc)
	if readErr != nil {
		t.Fatalf("bashrc not created after --enable-chdir: %v", readErr)
	}
	if !strings.Contains(string(content), "git-hop") {
		t.Errorf("shell wrapper not written to bashrc:\n%s", content)
	}
}
