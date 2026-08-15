package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
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
	"pre-worktree-remove",
	"post-worktree-remove",
	"pre-worktree-move",
	"post-worktree-move",
	"pre-env-start",
	"post-env-start",
	"pre-env-stop",
	"post-env-stop",
	"pre-repair",
	"post-repair",
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
//
// For repo-level hooks, we also search parent directories to find hooks
// at the hub level (useful for sharing hooks across worktrees)
func (r *Runner) FindHookFile(hookName string, worktreePath string, repoID string) string {
	// Priority 1: Repo-level override (also check parent dirs for hub-level hooks)
	repoHook := filepath.Join(worktreePath, ".git-hop", "hooks", hookName)
	if exists, _ := afero.Exists(r.fs, repoHook); exists {
		return repoHook
	}

	// Also check parent directories for repo-level hooks at hub level
	if hook := r.findHookInParentDirs(hookName, worktreePath); hook != "" {
		return hook
	}

	parts := strings.Split(repoID, "/")
	if len(parts) >= 3 {
		hopspaceHook := filepath.Join(hop.GetGitHopDataHome(), parts[0], parts[1], parts[2], "hooks", hookName)
		if exists, _ := afero.Exists(r.fs, hopspaceHook); exists {
			return hopspaceHook
		}
	}

	// Priority 3: Global hook
	globalHook := filepath.Join(hop.GetHooksDir(), hookName)
	if exists, _ := afero.Exists(r.fs, globalHook); exists {
		return globalHook
	}

	// No hook found
	return ""
}

// findHookInParentDirs searches for a hook in parent directories
// This is used for pre-worktree-add since the worktree doesn't exist yet
func (r *Runner) findHookInParentDirs(hookName string, startPath string) string {
	dir := startPath
	for {
		hookPath := filepath.Join(dir, ".git-hop", "hooks", hookName)
		if exists, _ := afero.Exists(r.fs, hookPath); exists {
			return hookPath
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
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

// ExecuteHookWithDetector executes a hook with additional detector environment variables
func (r *Runner) ExecuteHookWithDetector(hookName string, worktreePath string, repoID string, branch string, detectorEnv map[string]string, args ...string) error {
	if err := ValidateHookName(hookName); err != nil {
		return err
	}

	hookFile := r.FindHookFile(hookName, worktreePath, repoID)
	if hookFile == "" {
		return nil
	}

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

	for k, v := range detectorEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

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

// Trigger values for GIT_HOP_TRIGGER, identifying what initiated a
// worktree switch.
const (
	// TriggerHop marks a switch driven by an explicit hop command.
	TriggerHop = "hop"
	// TriggerChdir marks a switch driven by shell directory change.
	TriggerChdir = "chdir"
)

// SwitchEnvVars builds the extra hook environment describing where a
// worktree switch came from and what triggered it. The result is meant to
// be merged into the map passed to ExecuteHookWithDetector, matching how
// per-command extras already reach hooks.
//
// Empty fields are omitted rather than exported as empty strings, so a
// hook can distinguish "no previous worktree" (fresh shell, post-clone)
// from "previous worktree was the empty string". Callers merge with the
// detector map; unset keys never reach the child process.
func SwitchEnvVars(fromBranch string, fromWorktreePath string, trigger string) map[string]string {
	envVars := make(map[string]string)

	if fromBranch != "" {
		envVars["GIT_HOP_FROM_BRANCH"] = fromBranch
	}
	if fromWorktreePath != "" {
		envVars["GIT_HOP_FROM_WORKTREE_PATH"] = fromWorktreePath
	}
	if trigger != "" {
		envVars["GIT_HOP_TRIGGER"] = trigger
	}

	return envVars
}

// InstallHooks installs git-hop hooks in a worktree, hub, or worktree
// child. Creates .git-hop/hooks directory for repo-level hook overrides.
//
// The git-repo presence check accepts three shapes via
// hop.LooksLikeGitCheckout: standard repos (.git dir), worktree children
// (.git file with gitdir: pointer), and bare repos at root
// (HEAD/objects/refs directly under path, e.g. hop hubs).
func (r *Runner) InstallHooks(worktreePath string) error {
	if !hop.LooksLikeGitCheckout(r.fs, worktreePath) {
		return fmt.Errorf("not a git repository: %s", worktreePath)
	}

	hooksDir := filepath.Join(worktreePath, ".git-hop", "hooks")
	if err := r.fs.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	return nil
}
