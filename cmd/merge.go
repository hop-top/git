package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/events"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/kit/bus"
)

var mergeCmd = &cobra.Command{
	Use:   "merge [source-branch] <into-branch>",
	Short: "Merge a worktree branch into a receiving branch, then delete the source worktree",
	Long: `Merges the source branch into the receiving (into) branch, removes the source
worktree, and symlinks "current" to the receiving branch's worktree.

If only one argument is given, the current branch is used as the source.`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		noFF, _ := cmd.Flags().GetBool("no-ff")

		fs := afero.NewOsFs()
		g := git.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		hubPath, err := hop.FindHub(fs, cwd)
		if err != nil {
			output.Fatal("Not in a git-hop hub.")
		}

		hub, err := hop.LoadHub(fs, hubPath)
		if err != nil {
			output.Fatal("Failed to load hub: %v", err)
		}

		var sourceBranch, intoBranch string

		if len(args) == 1 {
			// Infer source from cwd
			intoBranch = args[0]
			sourceBranch, err = g.GetCurrentBranch(cwd)
			if err != nil || sourceBranch == "" {
				output.Fatal("Could not detect current branch. Use: git hop merge <source> <into>")
			}
			// Verify cwd is inside the source branch worktree
			if bc, ok := hub.Config.Branches[sourceBranch]; ok {
				absWorktree, _ := filepath.Abs(config.ResolveWorktreePath(bc.Path, hubPath))
				absCwd, _ := filepath.Abs(cwd)
				if absCwd != absWorktree && !strings.HasPrefix(absCwd, absWorktree+string(filepath.Separator)) {
					output.Fatal("Current directory is not inside the worktree for branch '%s'.", sourceBranch)
				}
			} else {
				output.Fatal("Current branch '%s' is not tracked in this hub.", sourceBranch)
			}
		} else {
			sourceBranch = args[0]
			intoBranch = args[1]
		}

		// Guard: cannot merge a branch into itself
		if sourceBranch == intoBranch {
			output.Fatal("Source and receiving branch must differ.")
		}

		// Resolve source path
		srcCfg, ok := hub.Config.Branches[sourceBranch]
		if !ok {
			output.Fatal("Branch '%s' not found in hub.", sourceBranch)
		}
		srcPath := config.ResolveWorktreePath(srcCfg.Path, hubPath)

		// Resolve receiving branch path
		intoCfg, ok := hub.Config.Branches[intoBranch]
		if !ok {
			output.Fatal("Branch '%s' not found in hub.", intoBranch)
		}
		intoPath := config.ResolveWorktreePath(intoCfg.Path, hubPath)

		// Guard: for 2-arg form, cannot be inside source worktree when merging/removing it.
		// (1-arg form requires being inside the source worktree to infer it.)
		if len(args) == 2 {
			absSrcPath, _ := filepath.Abs(srcPath)
			absCwd, _ := filepath.Abs(cwd)
			if absCwd == absSrcPath || strings.HasPrefix(absCwd, absSrcPath+string(filepath.Separator)) {
				output.Fatal("Cannot merge branch '%s': you are currently inside its worktree. Change to a different worktree first.", sourceBranch)
			}
		}

		// Guard: source worktree must be clean before merge
		srcStatus, err := g.GetStatus(srcPath)
		if err != nil {
			output.Warn("Could not check source worktree status: %v", err)
		} else if !srcStatus.Clean {
			output.Fatal("Source worktree '%s' has uncommitted changes. Commit or stash them first.", sourceBranch)
		}

		repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

		// Perform merge: run `git merge` inside the receiving branch's worktree
		mergeArgs := []string{"merge"}
		if noFF {
			mergeArgs = append(mergeArgs, "--no-ff")
		}
		mergeArgs = append(mergeArgs, sourceBranch)

		output.Info("Merging '%s' → '%s'...", sourceBranch, intoBranch)
		if _, err := g.RunInDir(intoPath, "git", mergeArgs...); err != nil {
			output.Fatal("Merge failed: %v", err)
		}

		output.Info("Merge successful.")

		// Determine base path for git worktree remove (use receiving branch path)
		basePath := intoPath

		// Remove source worktree via git
		if err := g.WorktreeRemove(basePath, srcPath, true); err != nil {
			output.Warn("Failed to remove worktree via git: %v", err)
		}

		// Remove source worktree directory
		output.Info("Removing worktree directory: %s", srcPath)
		if err := fs.RemoveAll(srcPath); err != nil {
			output.Warn("Failed to remove worktree directory: %v", err)
		}

		// Remove source branch from hub config
		if err := hub.RemoveBranch(sourceBranch); err != nil {
			output.Fatal("Failed to remove branch from hub config: %v", err)
		}

		// Delete local and remote source branch
		if err := g.DeleteLocalBranch(basePath, sourceBranch); err != nil {
			output.Warn("Failed to delete local branch '%s': %v", sourceBranch, err)
		}
		if g.HasRemoteBranch(basePath, sourceBranch) {
			if err := g.DeleteRemoteBranch(basePath, sourceBranch); err != nil {
				output.Warn("Failed to delete remote branch '%s': %v", sourceBranch, err)
			}
		}

		// Prune stale hopspace data
		dataHome := hop.GetGitHopDataHome()
		hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)
		if hopspace, err := hop.LoadHopspace(fs, hopspacePath); err == nil {
			hopspace.UnregisterBranch(sourceBranch)
			cleanup := hop.NewCleanupManager(fs, g)
			if err := cleanup.PruneWorktrees(hopspace); err != nil {
				output.Warn("Failed to prune worktrees: %v", err)
			}
		}

		// Update global state
		st, err := state.LoadState(fs)
		if err == nil {
			if err := st.RemoveWorktree(repoID, sourceBranch); err != nil {
				output.Warn("Failed to update state: %v", err)
			} else if err := state.SaveState(fs, st); err != nil {
				output.Warn("Failed to save state: %v", err)
			}
		}

		// Symlink "current" → receiving branch worktree
		if err := hop.UpdateCurrentSymlink(fs, hubPath, intoPath); err != nil {
			output.Warn("Failed to update current symlink: %v", err)
		} else {
			output.Info("Symlinked 'current' → %s", intoPath)
		}

		// Emit worktree.merged event.
		_ = cli.EventBus.Publish(context.Background(), bus.NewEvent(
			events.WorktreeMerged, events.Source,
			events.WorktreeEvent{
				Path:         srcPath,
				Branch:       sourceBranch,
				HopspacePath: hopspacePath,
				RepoPath:     hubPath,
			},
		))

		output.Success("Merged '%s' into '%s' and cleaned up source worktree.", sourceBranch, intoBranch)
	},
}

func init() {
	cli.RootCmd.AddCommand(mergeCmd)
	mergeCmd.Flags().Bool("no-ff", false, "Create a merge commit even when fast-forward is possible")
	mergeCmd.ValidArgsFunction = completeBranchNames
}
