package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/afero"
)

// Runner handles hook execution following the priority system
type Runner struct {
	fs afero.Fs
}

// NewRunner creates a new hook runner
func NewRunner(fs afero.Fs) *Runner {
	return &Runner{fs: fs}
}

// ValidHookNames is the list of valid hook names
var ValidHookNames = []string{
	"pre-worktree-add",
	"post-worktree-add",
	"pre-env-start",
	"post-env-start",
	"pre-env-stop",
	"post-env-stop",
}

// ValidateHookName validates that a hook name is valid
func ValidateHookName(hookName string) error {
	if hookName == "" {
		return fmt.Errorf("hook name cannot be empty")
	}

	for _, valid := range ValidHookNames {
		if hookName == valid {
			return nil
		}
	}

	return fmt.Errorf("invalid hook name: %s (valid hooks: %s)", hookName, strings.Join(ValidHookNames, ", "))
}

// FindHookFile finds the hook file following the priority system:
// 1. Repo override (.git-hop/hooks/<hook-name>)
// 2. Hopspace hook ($XDG_DATA_HOME/git-hop/<org>/<repo>/hooks/<hook-name>)
// 3. Global hook ($XDG_CONFIG_HOME/git-hop/hooks/<hook-name>)
func (r *Runner) FindHookFile(hookName string, worktreePath string, repoID string) string {
	// Priority 1: Repo-level override
	repoHook := filepath.Join(worktreePath, ".git-hop", "hooks", hookName)
	if exists, _ := afero.Exists(r.fs, repoHook); exists {
		return repoHook
	}

	dataHome := getDataHome()
	parts := strings.Split(repoID, "/")
	if len(parts) >= 3 {
		hopspaceHook := filepath.Join(dataHome, "git-hop", parts[0], parts[1], parts[2], "hooks", hookName)
		if exists, _ := afero.Exists(r.fs, hopspaceHook); exists {
			return hopspaceHook
		}
	}

	// Priority 3: Global hook
	configHome := getConfigHome()
	globalHook := filepath.Join(configHome, "git-hop", "hooks", hookName)
	if exists, _ := afero.Exists(r.fs, globalHook); exists {
		return globalHook
	}

	// No hook found
	return ""
}

// ExecuteHook executes a hook if it exists
// Returns error if hook execution fails or if hook blocks the operation
func (r *Runner) ExecuteHook(hookName string, worktreePath string, repoID string, branch string, args ...string) error {
	// Validate hook name
	if err := ValidateHookName(hookName); err != nil {
		return err
	}

	// Find hook file
	hookFile := r.FindHookFile(hookName, worktreePath, repoID)
	if hookFile == "" {
		// No hook found, silently succeed
		return nil
	}

	// Check if hook is executable
	info, err := r.fs.Stat(hookFile)
	if err != nil {
		return fmt.Errorf("failed to stat hook file: %w", err)
	}

	if runtime.GOOS != "windows" {
		if info.Mode()&0111 == 0 {
			return fmt.Errorf("hook file is not executable: %s", hookFile)
		}
	}

	env := r.GetHookEnv(hookName, worktreePath, repoID, branch)

	cmd := exec.Command(hookFile, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %s failed: %w", hookName, err)
	}

	return nil
}

// GetHookEnv returns the environment variables for hook execution
func (r *Runner) GetHookEnv(hookName string, worktreePath string, repoID string, branch string) []string {
	return []string{
		fmt.Sprintf("GIT_HOP_HOOK_NAME=%s", hookName),
		fmt.Sprintf("GIT_HOP_WORKTREE_PATH=%s", worktreePath),
		fmt.Sprintf("GIT_HOP_REPO_ID=%s", repoID),
		fmt.Sprintf("GIT_HOP_BRANCH=%s", branch),
	}
}

// InstallHooks installs git-hop hooks in a worktree
// Creates .git-hop/hooks directory for repo-level hook overrides
func (r *Runner) InstallHooks(worktreePath string) error {
	// Verify this is a git repository
	gitDir := filepath.Join(worktreePath, ".git")
	if exists, _ := afero.DirExists(r.fs, gitDir); !exists {
		return fmt.Errorf("not a git repository: %s", worktreePath)
	}

	// Create .git-hop/hooks directory
	hooksDir := filepath.Join(worktreePath, ".git-hop", "hooks")
	if err := r.fs.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	return nil
}

// getDataHome returns the XDG data home directory
func getDataHome() string {
	if env := os.Getenv("XDG_DATA_HOME"); env != "" {
		return env
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	if home == "" {
		return filepath.Join(".local", "share")
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	default:
		return filepath.Join(home, ".local", "share")
	}
}

// getConfigHome returns the XDG config home directory
func getConfigHome() string {
	if env := os.Getenv("XDG_CONFIG_HOME"); env != "" {
		return env
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	if home == "" {
		return filepath.Join(".config")
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Preferences")
	default:
		return filepath.Join(home, ".config")
	}
}
