package cmd

import (
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
		return runNotifyChdir(afero.NewOsFs(), args[0], previousDir())
	},
}

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
	if _, err := runner.ExecuteHookWithDetector(
		"post-worktree-switch", worktreePath, repoID, branch, hookEnv,
	); err != nil {
		// A failing hook must not make the user's cd look broken: the cd
		// already succeeded and is not undoable. Report and move on.
		fmt.Fprintf(os.Stderr, "warning: hook post-worktree-switch failed: %v\n", err)
	}

	// The handled-navigation directive is meaningless here. It exists so a
	// hook can tell the shell WRAPPER not to cd after `git hop <branch>`;
	// on this path nothing is waiting to cd, because the user already
	// moved themselves. Swallow it rather than exiting 93 into a prompt.
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
