package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/state"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:     "remove [target]",
	Aliases: []string{"rm", "delete", "del"},
	Short:   "Remove a hub, hopspace, or branch",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]
		noPrompt, _ := cmd.Flags().GetBool("no-prompt")

		fs := afero.NewOsFs()
		g := git.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		// Check if we are in a hub
		hubPath, err := hop.FindHub(fs, cwd)
		if err == nil {
			hub, err := hop.LoadHub(fs, hubPath)
			if err != nil {
				output.Fatal("Failed to load hub: %v", err)
			}

			// Check if target is a branch in the hub
			if _, ok := hub.Config.Branches[target]; ok {
				if !noPrompt {
					// Prompt for confirmation before removing the worktree
					branchConfig := hub.Config.Branches[target]
					worktreePath := config.ResolveWorktreePath(branchConfig.Path, hubPath)

					confirmed := output.ConfirmDeletion(target, []output.CardField{
						{Key: "Type", Value: "Branch worktree"},
						{Key: "Path", Value: worktreePath},
						{Key: "Hub", Value: hubPath},
					})

					if !confirmed {
						output.Info("Cancelled.")
						return
					}
				}

				output.Info("Removing branch %s from hub...", target)

				// Get the worktree path from the hub config BEFORE removing from config
				branchConfig := hub.Config.Branches[target]
				// Resolve the worktree path (may be relative like "hops/branch")
				worktreePath := config.ResolveWorktreePath(branchConfig.Path, hubPath)

				// We need to remove the symlink and update config
				if err := hub.RemoveBranch(target); err != nil {
					output.Fatal("Failed to remove branch from hub: %v", err)
				}

				// We also need to remove the worktree from the hopspace?
				// The specs say: "Remove the symlink from the Hub. Remove the Worktree from the Hopspace."
				// So yes.

				// Use main worktree as base for git worktree remove command
				var basePath string
				if mainBranch, exists := hub.Config.Branches[hub.Config.Repo.DefaultBranch]; exists {
					basePath = mainBranch.Path
				} else {
					// Fallback: use any other worktree
					for bn, bc := range hub.Config.Branches {
						if bn != target && bc.Path != "" {
							basePath = bc.Path
							break
						}
					}
				}

				if basePath != "" {
					// Resolve the base path relative to hub (may be relative path like "hops/main")
					absBasePath := config.ResolveWorktreePath(basePath, hubPath)

					// Try git worktree remove
					if err := g.WorktreeRemove(absBasePath, worktreePath, true); err != nil {
						output.Warn("Failed to remove worktree via git: %v", err)
					}
				}

				// Always try to remove the directory physically as well
				output.Info("Removing worktree directory: %s", worktreePath)
				if err := fs.RemoveAll(worktreePath); err != nil {
					output.Error("Failed to remove worktree directory: %v", err)
				} else {
					output.Info("Successfully removed worktree directory")
				}

				// Load Hopspace to unregister
				dataHome := hop.GetGitHopDataHome()
				hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)
				hopspace, err := hop.LoadHopspace(fs, hopspacePath)
				if err == nil {
					// Unregister from hopspace (silent if branch doesn't exist)
					hopspace.UnregisterBranch(target)

					// Prune stale git metadata
					cleanup := hop.NewCleanupManager(fs, g)
					if err := cleanup.PruneWorktrees(hopspace); err != nil {
						output.Warn("Failed to prune worktrees: %v", err)
					}
				}

				// Update global state
				st, err := state.LoadState(fs)
				if err == nil {
					repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
					if err := st.RemoveWorktree(repoID, target); err != nil {
						output.Warn("Failed to update state: %v", err)
					} else {
						if err := state.SaveState(fs, st); err != nil {
							output.Warn("Failed to save state: %v", err)
						}
					}
				}

				output.Info("Successfully removed %s", target)
				return
			}
		}

		// Check if target is a path to a hub directory
		targetPath := target
		if !filepath.IsAbs(target) {
			targetPath = filepath.Join(cwd, target)
		}

		// Check if target is a hub
		if hop.IsHub(fs, targetPath) {
			if !noPrompt {
				// Load hub to get branch count
				hub, err := hop.LoadHub(fs, targetPath)
				if err == nil {
					branchCount := len(hub.Config.Branches)

					confirmed := output.ConfirmDeletion(targetPath, []output.CardField{
						{Key: "Type", Value: "Hub"},
						{Key: "Branches", Value: fmt.Sprintf("%d", branchCount)},
						{Key: "Repository", Value: fmt.Sprintf("%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)},
					})

					if !confirmed {
						output.Info("Cancelled.")
						return
					}
				} else {
					// Fallback if we can't load hub config
					confirmed := output.ConfirmDeletion(targetPath, []output.CardField{
						{Key: "Type", Value: "Hub"},
					})

					if !confirmed {
						output.Info("Cancelled.")
						return
					}
				}
			}

			output.Info("Removing hub at %s...", targetPath)

			// Load hub to get repo info
			hub, err := hop.LoadHub(fs, targetPath)
			if err != nil {
				output.Fatal("Failed to load hub: %v", err)
			}

			repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

			// Remove all worktrees
			for branchName, branchConfig := range hub.Config.Branches {
				worktreePath := config.ResolveWorktreePath(branchConfig.Path, targetPath)
				output.Info("Removing worktree for branch %s...", branchName)

				if err := fs.RemoveAll(worktreePath); err != nil {
					output.Warn("Failed to remove worktree %s: %v", branchName, err)
				}
			}

			// Remove hub directory
			output.Info("Removing hub directory...")
			if err := fs.RemoveAll(targetPath); err != nil {
				output.Fatal("Failed to remove hub directory: %v", err)
			}

			// Remove from global state
			st, err := state.LoadState(fs)
			if err == nil {
				if err := st.RemoveRepository(repoID); err != nil {
					output.Warn("Failed to update state: %v", err)
				} else {
					if err := state.SaveState(fs, st); err != nil {
						output.Warn("Failed to save state: %v", err)
					}
				}
			}

			// Clean up hopspace data
			dataHome := hop.GetGitHopDataHome()
			hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)
			if exists, _ := afero.DirExists(fs, hopspacePath); exists {
				output.Info("Cleaning up hopspace data...")
				if err := fs.RemoveAll(hopspacePath); err != nil {
					output.Warn("Failed to remove hopspace data: %v", err)
				}
			}

			output.Success("Successfully removed hub: %s", targetPath)
			return
		}

		// Target not found
		output.Fatal("Target %s not found or not supported yet", target)
	},
}

func init() {
	cli.RootCmd.AddCommand(removeCmd)
	removeCmd.Flags().Bool("no-prompt", false, "Do not prompt for confirmation")
	removeCmd.ValidArgsFunction = completeBranchNames
}
