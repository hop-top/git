package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
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
		noVerify, _ := cmd.Flags().GetBool("no-verify")
		force, _ := cmd.Root().PersistentFlags().GetBool("force")

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
				// Guard: cannot remove the default branch
				if target == hub.Config.Repo.DefaultBranch {
					output.Fatal("Cannot remove the default branch '%s'.", target)
				}

				// Guard: cannot remove worktree while inside it
				guardBranchCfg := hub.Config.Branches[target]
				absWorktree, _ := filepath.Abs(config.ResolveWorktreePath(guardBranchCfg.Path, hubPath))
				absCwd, _ := filepath.Abs(cwd)
				if absCwd == absWorktree || strings.HasPrefix(absCwd, absWorktree+string(filepath.Separator)) {
					output.Fatal("Cannot remove branch '%s': you are currently inside its worktree. Change to a different worktree first.", target)
				}

				// Safety gate: probe the worktree and require --force /
				// --no-verify based on merged/pushed/dirty state. Skip
				// when the worktree is missing on disk (nothing to lose).
				gateRequired := false
				if _, err := fs.Stat(absWorktree); err == nil {
					safety := inspectBranchSafety(g, absWorktree, target, hub.Config.Repo.DefaultBranch)
					if err := removeGate(safety, force, noVerify); err != nil {
						output.Fatal("%s", err.Error())
					}
					// Gate fired (and was satisfied by flags) when any of
					// these are true. We use this to decide whether the
					// confirmation prompt is appropriate.
					gateRequired = !safety.Merged || !safety.Clean
				}

				// Only prompt when the operation is risky. Clean+merged
				// removals proceed silently per spec.
				if !noPrompt && gateRequired {
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

				repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

				// Create detector manager and register detectors
				detectorMgr := detector.NewManager(fs, g)
				detectorMgr.Register(detector.NewGitFlowNextDetector(g))
				detectorMgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))

				// Execute pre-remove (detector OnRemove)
				detectorCtx := context.Background()
				branchInfo, err := detectorMgr.ExecutePreRemove(detectorCtx, target, hubPath, worktreePath)
				if err != nil {
					output.Fatal("Branch type detector failed: %v", err)
				}

				// Execute pre-worktree-remove hook with detector env vars
				hookRunner := hooks.NewRunner(fs)
				detectorEnv := detectorMgr.GetDetectorEnvVars(branchInfo)
				if err := hookRunner.ExecuteHookWithDetector("pre-worktree-remove", worktreePath, repoID, target, detectorEnv); err != nil {
					output.Fatal("Hook pre-worktree-remove failed: %v", err)
				}

				// Resolve a live base path for git commands (worktree remove, branch -D).
				// Prefer the default branch worktree; fall back to any other live worktree;
				// finally use hubPath itself (bare repo) so git commands always have a
				// valid working directory even when all tracked worktrees are missing.
				resolveBasePath := func() string {
					candidates := []string{}
					if mainBranch, exists := hub.Config.Branches[hub.Config.Repo.DefaultBranch]; exists {
						candidates = append(candidates, config.ResolveWorktreePath(mainBranch.Path, hubPath))
					}
					for bn, bc := range hub.Config.Branches {
						if bn != target && bc.Path != "" {
							candidates = append(candidates, config.ResolveWorktreePath(bc.Path, hubPath))
						}
					}
					for _, p := range candidates {
						if info, err := os.Stat(p); err == nil && info.IsDir() {
							return p
						}
					}
					return hubPath
				}
				absBasePath := resolveBasePath()

				// Try git worktree remove
				if err := g.WorktreeRemove(absBasePath, worktreePath, true); err != nil {
					output.Warn("Failed to remove worktree via git: %v", err)
				}

				// Always try to remove the directory physically as well
				output.Info("Removing worktree directory: %s", worktreePath)
				if err := fs.RemoveAll(worktreePath); err != nil {
					output.Error("Failed to remove worktree directory: %v", err)
				} else {
					output.Info("Successfully removed worktree directory")

					// Remove parent dir (e.g. feat/, fix/) if now empty.
					cleanupMgr := hop.NewCleanupManager(fs, g)
					if err := cleanupMgr.RemoveEmptyParent(worktreePath, hubPath); err != nil {
						output.Warn("Failed to remove empty parent directory: %v", err)
					}
				}

				// Delete local and remote branches
				if err := g.DeleteLocalBranch(absBasePath, target); err != nil {
					output.Warn("Failed to delete local branch: %v", err)
				}

				if g.HasRemoteBranch(absBasePath, target) {
					if err := g.DeleteRemoteBranch(absBasePath, target); err != nil {
						output.Warn("Failed to delete remote branch: %v", err)
					}
				}

				// Remove branch from hub config so it no longer appears in status
				delete(hub.Config.Branches, target)
				writer := config.NewWriter(fs)
				if err := writer.WriteHubConfig(hubPath, hub.Config); err != nil {
					output.Warn("Failed to update hub config: %v", err)
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
					if err := st.RemoveWorktree(repoID, target); err != nil {
						output.Warn("Failed to update state: %v", err)
					} else {
						if err := state.SaveState(fs, st); err != nil {
							output.Warn("Failed to save state: %v", err)
						}
					}
				}

				// Execute post-worktree-remove hook
				if err := hookRunner.ExecuteHookWithDetector("post-worktree-remove", worktreePath, repoID, target, detectorEnv); err != nil {
					output.Warn("Hook post-worktree-remove failed: %v", err)
				}

				// Update current symlink to point back to defaultBranch worktree
				if err := updateCurrentToDefault(fs, hub, hubPath); err != nil {
					output.Warn("Failed to update current symlink: %v", err)
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

// updateCurrentToDefault updates the "current" symlink to point to the defaultBranch worktree.
// Called after removing a branch so the symlink stays valid.
func updateCurrentToDefault(fs afero.Fs, hub *hop.Hub, hubPath string) error {
	defaultBranch := hub.Config.Repo.DefaultBranch
	branchCfg, ok := hub.Config.Branches[defaultBranch]
	if !ok {
		return fmt.Errorf("default branch %q not found in hub", defaultBranch)
	}
	defaultPath := config.ResolveWorktreePath(branchCfg.Path, hubPath)
	return hop.UpdateCurrentSymlink(fs, hubPath, defaultPath)
}

func init() {
	cli.RootCmd.AddCommand(removeCmd)
	removeCmd.Flags().Bool("no-prompt", false, "Skip the confirmation prompt only; does NOT bypass the safety gate")
	removeCmd.Flags().Bool("no-verify", false, "Allow removal of dirty worktrees or unpushed commits (gate bypass)")
	removeCmd.ValidArgsFunction = completeBranchNames
}
