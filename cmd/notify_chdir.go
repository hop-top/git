package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/shell"
)

// notifyChdirCmd fires post-worktree-switch for a worktree the user
// reached by plain cd rather than by `git hop <branch>`.
//
// Hidden, following completionCmd's precedent, because it is plumbing: the
// installed shell integration calls it, and nothing else should. There is
// deliberately no user-facing command for this -- a person typing it by
// hand would be announcing a switch that did not happen.
//
// Only the POST hook runs here, never the pre- hook. pre-worktree-switch
// exists to veto a switch that has not happened yet; by the time a chdir
// handler sees $PWD the cd is already done and cannot be taken back, so
// firing a vetoable hook would be offering a veto that nothing honours.
// Hooks tell the two paths apart by GIT_HOP_TRIGGER=chdir.
var notifyChdirCmd = &cobra.Command{
	Use:    shell.NotifyChdirCommand + " <path>",
	Short:  "Report a directory change into a registered worktree",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		err := runNotifyChdir(afero.NewOsFs(), args[0], previousDir())

		// os.Exit rather than returning the sentinel, for the same reason
		// the hop path does it (internal/cli/root.go): every error out of
		// RunE collapses to exit 1 at cmd.Execute, and 1 is the one status
		// the handler must not act on. The status IS the payload here, so
		// it is raised where nothing can flatten it.
		if errors.Is(err, errNavigationHandled) {
			os.Exit(hooks.ExitNavigationHandled)
		}
		return err
	},
}

// errNavigationHandled is how runNotifyChdir reports that the hook
// navigated the user itself.
//
// A sentinel rather than a bare bool return because every other outcome on
// this path is already "nil, quietly" -- a second return value would put an
// unused word on every caller to carry a case that fires almost never.
// Tests read it through exitCodeFor, so the assertion is on the status the
// shell will see rather than on an internal shape.
var errNavigationHandled = errors.New("navigation handled by hook")

// previousDir reports the directory the shell was in before this chdir.
//
// The handler passes it explicitly in GIT_HOP_CHDIR_FROM because $OLDPWD
// cannot be relied on to reach a child process: zsh reports it as exported
// yet it arrives empty in the subprocess, which silently emptied the
// from-state on every chdir switch. The handler already tracks the previous
// worktree to decide whether anything switched at all, so it passes what it
// knows rather than hoping the shell propagates it.
//
// $OLDPWD remains the fallback for anyone invoking the subcommand outside
// the generated handler.
func previousDir() string {
	if v := os.Getenv("GIT_HOP_CHDIR_FROM"); v != "" {
		return v
	}
	return os.Getenv("OLDPWD")
}

// runNotifyChdir re-verifies the shell's claim and dispatches the hook.
//
// The shell handler's prefix test is fast, not authoritative: its cache can
// name a worktree that has since been removed, or one whose hub entry is
// gone. So every field is re-derived from hop.json here. A path that no
// longer resolves to a registered worktree is not an error -- it is a stale
// cache, which is expected -- so it exits quietly.
func runNotifyChdir(fs afero.Fs, path string, oldPwd string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}

	hubPath, err := hop.FindHub(fs, abs)
	if err != nil {
		return nil
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return nil
	}

	branch, worktreePath := lookupRegisteredBranch(hub, hubPath, abs)
	if branch == "" {
		// Inside a hub but not inside any registered worktree.
		return nil
	}

	// From-state for the chdir path comes from where the shell just was.
	// The `current` symlink cannot serve here: a plain cd never updates
	// it, so on a manual navigation it still names whatever worktree the
	// last `git hop` selected -- which is the destination, not the origin,
	// once the user has hopped and then cd'd away. $OLDPWD is the only
	// record of the actual previous directory. When it was not itself a
	// registered worktree both fields stay empty and SwitchEnvVars omits
	// them, matching how the hop path reports a first switch.
	var fromBranch, fromWorktreePath string
	if oldPwd != "" {
		if oldAbs, err := filepath.Abs(oldPwd); err == nil {
			fromBranch, fromWorktreePath = lookupRegisteredBranch(hub, hubPath, oldAbs)
		}
	}

	// Same worktree in and out: the user moved between subdirectories, not
	// between worktrees. Nothing switched, so nothing is announced.
	if fromWorktreePath == worktreePath {
		return nil
	}

	repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

	hookEnv := hooks.SwitchEnvVars(fromBranch, fromWorktreePath, hooks.TriggerChdir)

	runner := hooks.NewRunner(fs)
	result, err := runner.ExecuteHookWithDetector(
		"post-worktree-switch", worktreePath, repoID, branch, hookEnv,
	)
	if err != nil {
		// A failing hook must not make the user's cd look broken: the cd
		// already succeeded and is not undoable. Report and move on -- and
		// specifically do NOT report the directive below, because a hook
		// that failed navigated nothing.
		fmt.Fprintf(os.Stderr, "warning: hook post-worktree-switch failed: %v\n", err)
		return nil
	}

	// The handled-navigation directive means the opposite thing here than it
	// does on the hop path, and both readings need it propagated.
	//
	// After `git hop <branch>` the wrapper is ABOUT to cd, and 93 tells it
	// not to. On this path nothing is waiting to cd -- the user cd'd
	// themselves, which is exactly the problem. A hook that navigates
	// (window-per-worktree tmux, say) selects the destination's window while
	// the shell that ran the cd stays in the originating pane, whose $PWD is
	// now the destination as well: two places pointing into one worktree.
	//
	// The shell handler is the only thing that can put that pane back, and
	// its only channel back from this process is the exit status. So the
	// directive is raised rather than swallowed, and the handler reads it as
	// "undo the cd you just saw".
	if result.NavigationHandled {
		return errNavigationHandled
	}

	return nil
}

// lookupRegisteredBranch maps an absolute path to the hub-registered branch
// whose worktree contains it, returning the branch name and the worktree
// root. The longest matching root wins so a worktree nested inside another
// resolves to the inner one.
func lookupRegisteredBranch(hub *hop.Hub, hubPath string, abs string) (string, string) {
	var bestBranch, bestPath string

	for name, b := range hub.Config.Branches {
		root, err := filepath.Abs(config.ResolveWorktreePath(b.Path, hubPath))
		if err != nil {
			continue
		}
		root = filepath.Clean(root)

		if abs != root && !isUnder(abs, root) {
			continue
		}
		if len(root) > len(bestPath) {
			bestBranch, bestPath = name, root
		}
	}

	return bestBranch, bestPath
}

// isUnder reports whether abs sits strictly inside root. The separator is
// part of the comparison on purpose: without it "/w/feature-2" reads as
// being inside "/w/feature".
func isUnder(abs, root string) bool {
	if root == "" {
		return false
	}
	prefix := root
	if prefix[len(prefix)-1] != filepath.Separator {
		prefix += string(filepath.Separator)
	}
	return len(abs) > len(prefix)-1 && abs[:len(prefix)] == prefix
}

func init() {
	cli.RootCmd.AddCommand(notifyChdirCmd)
}
