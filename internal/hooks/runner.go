package hooks

import (
	"errors"
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
	"pre-clone",
	"post-clone",
	// "switch" rather than "checkout": git checkout changes refs or files
	// within a single working tree, whereas these select an existing worktree.
	"pre-worktree-switch",
	"post-worktree-switch",
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

// ExitNavigationHandled is the exit status a hook uses to report that it
// already moved the user to the target worktree, so git-hop's shell
// wrapper must NOT cd afterwards.
//
// The wrapper resolves the hub's `current` symlink itself, after the
// binary has exited -- the binary's own os.Chdir only moves the git-hop
// process. Under a window-per-worktree integration (tmux, for instance) a
// post-worktree-switch hook selects the target window; the wrapper then
// also cds the ORIGINATING window's shell into the same worktree, putting
// two windows in one worktree. This code is how a hook says "navigation is
// already done".
//
// The wrapper's only channel back from the binary is `$?` -- it runs
// `command git hop "$@"` and reads the status, capturing neither stream.
// So the signal has to be an exit code, and it has to be one nothing else
// claims: porcelain fixes 0/1/128/129 (see CLAUDE.md), and 128+N is the
// shell's signal band. 93 sits outside both and is unused across the tree.
const ExitNavigationHandled = 93

// RunResult reports how a hook run finished, for the cases a bare error
// cannot express.
//
// NavigationHandled is set when a post-worktree-switch hook exited
// ExitNavigationHandled. That is a SUCCESS, not a failure -- the hook did
// its job and is declining the wrapper's cd -- so it is carried as a typed
// result rather than folded into the error return.
type RunResult struct {
	// NavigationHandled reports that the hook navigated the user itself
	// and git-hop should propagate ExitNavigationHandled to its caller.
	NavigationHandled bool
}

// navigationHandledFor reports whether exit code `code` from hook
// `hookName` should be read as the handled-navigation directive.
//
// Deliberately scoped to post-worktree-switch alone. Every other hook name
// runs in a context where nothing is about to navigate, so there is nothing
// for a hook to have "already handled" -- honouring 93 there would only
// convert a genuine failure into a silent success. Keeping the reserved
// code inert everywhere else also preserves the binding constraint that a
// hook which knows nothing about this mechanism behaves exactly as before:
// the blast radius is one hook name.
func navigationHandledFor(hookName string, code int) bool {
	return hookName == "post-worktree-switch" && code == ExitNavigationHandled
}

// hookExitCode extracts the process exit status from a cmd.Run error,
// reporting ok=false when the failure was not an exit status at all
// (binary missing, permission denied, signal).
func hookExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

// ExecuteHook executes a hook if it exists.
// Returns error if hook execution fails or if hook blocks the operation.
func (r *Runner) ExecuteHook(hookName string, worktreePath string, repoID string, branch string, args ...string) (RunResult, error) {
	return r.run(hookName, worktreePath, repoID, branch, nil, args...)
}

// ExecuteHookWithDetector executes a hook with additional detector environment variables.
func (r *Runner) ExecuteHookWithDetector(hookName string, worktreePath string, repoID string, branch string, detectorEnv map[string]string, args ...string) (RunResult, error) {
	return r.run(hookName, worktreePath, repoID, branch, detectorEnv, args...)
}

// run is the single hook-execution path behind both ExecuteHook and
// ExecuteHookWithDetector, so the reserved-exit-code handling cannot drift
// between them. extraEnv is nil for the plain variant.
func (r *Runner) run(hookName string, worktreePath string, repoID string, branch string, extraEnv map[string]string, args ...string) (RunResult, error) {
	if err := ValidateHookName(hookName); err != nil {
		return RunResult{}, err
	}

	hookFile := r.FindHookFile(hookName, worktreePath, repoID)
	if hookFile == "" {
		// No hook found, silently succeed.
		return RunResult{}, nil
	}

	info, err := r.fs.Stat(hookFile)
	if err != nil {
		return RunResult{}, fmt.Errorf("failed to stat hook file: %w", err)
	}

	if runtime.GOOS != "windows" {
		if info.Mode()&0111 == 0 {
			return RunResult{}, fmt.Errorf("hook file is not executable: %s", hookFile)
		}
	}

	env := r.GetHookEnv(hookName, worktreePath, repoID, branch)
	for k, v := range extraEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.Command(hookFile, args...)
	cmd.Env = append(os.Environ(), env...)
	// Both streams stay wired straight to the terminal, including on the
	// handled path below: a hook that took over navigation still gets to
	// tell the user what it did.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if code, ok := hookExitCode(err); ok && navigationHandledFor(hookName, code) {
			return RunResult{NavigationHandled: true}, nil
		}
		return RunResult{}, fmt.Errorf("hook %s failed: %w", hookName, err)
	}

	return RunResult{}, nil
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
