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
	"hop.top/git/internal/events"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/kit/bus"
)

var removeCmd = &cobra.Command{
	Use:     "remove [target]",
	Aliases: []string{"rm", "delete", "del"},
	Short:   "Remove a hub, hopspace, or branch",
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		noPrompt, _ := cmd.Flags().GetBool("no-prompt")
		noVerify, _ := cmd.Flags().GetBool("no-verify")
		merged, _ := cmd.Flags().GetBool("merged")
		force, _ := cmd.Root().PersistentFlags().GetBool("force")

		// Validate flag/arg combinations.
		if merged && len(args) > 0 {
			output.Fatal("cannot pass both target and --merged")
		}
		if !merged && len(args) == 0 {
			output.Fatal("usage: git hop remove <target> | --merged")
		}

		fs := afero.NewOsFs()
		g := git.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		// --merged path: collect merged worktrees and remove each.
		if merged {
			runRemoveMerged(fs, g, cwd, force, noVerify, noPrompt)
			return
		}

		target := args[0]

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

				if err := removeBranchWorktree(fs, g, hub, hubPath, target); err != nil {
					output.Fatal("%s", err.Error())
				}
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

// removeBranchWorktree executes the actual removal pipeline for a single
// branch: detector pre-remove, hooks, git worktree remove, dir cleanup,
// branch deletion, hub-config update, hopspace unregister, state update,
// post-hook, current-symlink update, and event emission.
//
// It assumes all guards (default-branch check, cwd-inside check) and the
// safety gate have already been satisfied by the caller. Sub-step
// failures that are recoverable are logged with output.Warn; unrecoverable
// failures (detector, pre-hook) are returned as errors so callers can
// decide whether to abort (single-target) or report-and-continue (--merged).
func removeBranchWorktree(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, branch string) error {
	output.Info("Removing branch %s from hub...", branch)

	// Get the worktree path from the hub config BEFORE removing from config
	branchConfig := hub.Config.Branches[branch]
	// Resolve the worktree path (may be relative like "hops/branch")
	worktreePath := config.ResolveWorktreePath(branchConfig.Path, hubPath)

	repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

	// Create detector manager and register detectors
	detectorMgr := detector.NewManager(fs, g)
	detectorMgr.Register(detector.NewGitFlowNextDetector(g))
	detectorMgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))

	// Execute pre-remove (detector OnRemove)
	detectorCtx := context.Background()
	branchInfo, err := detectorMgr.ExecutePreRemove(detectorCtx, branch, hubPath, worktreePath)
	if err != nil {
		return fmt.Errorf("Branch type detector failed: %v", err)
	}

	// Execute pre-worktree-remove hook with detector env vars
	hookRunner := hooks.NewRunner(fs)
	detectorEnv := detectorMgr.GetDetectorEnvVars(branchInfo)
	if err := hookRunner.ExecuteHookWithDetector("pre-worktree-remove", worktreePath, repoID, branch, detectorEnv); err != nil {
		return fmt.Errorf("Hook pre-worktree-remove failed: %v", err)
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
			if bn != branch && bc.Path != "" {
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
	if err := g.DeleteLocalBranch(absBasePath, branch); err != nil {
		output.Warn("Failed to delete local branch: %v", err)
	}

	if g.HasRemoteBranch(absBasePath, branch) {
		if err := g.DeleteRemoteBranch(absBasePath, branch); err != nil {
			output.Warn("Failed to delete remote branch: %v", err)
		}
	}

	// Remove branch from hub config so it no longer appears in status
	delete(hub.Config.Branches, branch)
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
		hopspace.UnregisterBranch(branch)

		// Prune stale git metadata
		cleanup := hop.NewCleanupManager(fs, g)
		if err := cleanup.PruneWorktrees(hopspace); err != nil {
			output.Warn("Failed to prune worktrees: %v", err)
		}
	}

	// Update global state
	st, err := state.LoadState(fs)
	if err == nil {
		if err := st.RemoveWorktree(repoID, branch); err != nil {
			output.Warn("Failed to update state: %v", err)
		} else {
			if err := state.SaveState(fs, st); err != nil {
				output.Warn("Failed to save state: %v", err)
			}
		}
	}

	// Execute post-worktree-remove hook
	if err := hookRunner.ExecuteHookWithDetector("post-worktree-remove", worktreePath, repoID, branch, detectorEnv); err != nil {
		output.Warn("Hook post-worktree-remove failed: %v", err)
	}

	// Update current symlink to point back to defaultBranch worktree
	if err := updateCurrentToDefault(fs, hub, hubPath); err != nil {
		output.Warn("Failed to update current symlink: %v", err)
	}

	// Emit worktree.removed event.
	_ = cli.EventBus.Publish(context.Background(), bus.NewEvent(
		events.WorktreeRemoved, events.Source,
		events.WorktreeEvent{
			Path:         worktreePath,
			Branch:       branch,
			HopspacePath: hopspacePath,
			RepoPath:     hubPath,
		},
	))

	output.Info("Successfully removed %s", branch)
	return nil
}

// mergedCandidate captures one row in the candidate list built by --merged.
// reason is non-empty when the branch was excluded; included candidates
// have reason == "".
type mergedCandidate struct {
	Branch       string
	WorktreePath string
	Skip         bool
	Reason       string
}

// collectMergedCandidates iterates hub branches, skips the default branch
// and the currently-active worktree, and probes each remaining branch
// with inspectBranchSafety. Branches whose tip has zero commits ahead of
// the default branch (Merged=true) are returned for removal. Branches
// missing on disk are reported as skipped with a prune suggestion.
//
// The hub map is read but never mutated here — callers act on a stable
// snapshot.
func collectMergedCandidates(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, cwd string) (toRemove []mergedCandidate, skipped []mergedCandidate) {
	defaultBranch := hub.Config.Repo.DefaultBranch
	absCwd, _ := filepath.Abs(cwd)

	for branch, branchCfg := range hub.Config.Branches {
		// Skip the default branch unconditionally.
		if branch == defaultBranch {
			continue
		}

		worktreePath := config.ResolveWorktreePath(branchCfg.Path, hubPath)
		absWorktree, _ := filepath.Abs(worktreePath)

		// Skip the worktree the user is currently inside.
		if absCwd == absWorktree || strings.HasPrefix(absCwd, absWorktree+string(filepath.Separator)) {
			skipped = append(skipped, mergedCandidate{
				Branch:       branch,
				WorktreePath: worktreePath,
				Skip:         true,
				Reason:       "currently inside this worktree",
			})
			continue
		}

		// Worktree path missing on disk: tell the user to prune.
		if _, err := fs.Stat(absWorktree); err != nil {
			skipped = append(skipped, mergedCandidate{
				Branch:       branch,
				WorktreePath: worktreePath,
				Skip:         true,
				Reason:       fmt.Sprintf("skipping %s: worktree path missing; run 'git hop prune' first", branch),
			})
			continue
		}

		// Probe merge state. inspectBranchSafety is read-only.
		safety := inspectBranchSafety(g, absWorktree, branch, defaultBranch)
		if !safety.Merged {
			continue
		}

		toRemove = append(toRemove, mergedCandidate{
			Branch:       branch,
			WorktreePath: worktreePath,
		})
	}
	return toRemove, skipped
}

// runRemoveMerged drives the --merged removal flow. Order of operations:
// snapshot the candidate list, prompt (unless --no-prompt), then loop
// each candidate through removeGate + removeBranchWorktree. Track removed
// vs. skipped counts and exit non-zero if any candidate was skipped due
// to a safety-gate failure.
func runRemoveMerged(fs afero.Fs, g git.GitInterface, cwd string, force, noVerify, noPrompt bool) {
	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		output.Fatal("Not in a hub: %v", err)
	}
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	// Snapshot the candidate list FIRST. We do not mutate the hub config
	// during iteration; each removal is performed against the snapshot.
	toRemove, preSkipped := collectMergedCandidates(fs, g, hub, hubPath, cwd)

	// Surface pre-iteration skips (cwd-inside, missing path) up front so
	// the user knows why something they expected to disappear didn't.
	for _, sk := range preSkipped {
		output.Info("Skipping %s: %s", sk.Branch, sk.Reason)
	}

	if len(toRemove) == 0 {
		output.Info("No merged worktrees to remove.")
		return
	}

	// Show the candidate list.
	output.Info("Merged worktrees to remove:")
	for _, c := range toRemove {
		output.Info("  %s  %s", c.Branch, c.WorktreePath)
	}

	if !noPrompt {
		if !output.Confirm(fmt.Sprintf("Remove %d merged worktree(s)?", len(toRemove))) {
			output.Info("Cancelled.")
			return
		}
	}

	removed := 0
	skippedReasons := []string{}
	for _, c := range toRemove {
		// Re-check the gate per-candidate. The earlier collection only
		// confirmed Merged=true; the gate also enforces the dirty-state
		// rule when --no-verify isn't set.
		safety := inspectBranchSafety(g, c.WorktreePath, c.Branch, hub.Config.Repo.DefaultBranch)
		if err := removeGate(safety, force, noVerify); err != nil {
			reason := fmt.Sprintf("%s: %s", c.Branch, err.Error())
			output.Warn("Skipping %s", reason)
			skippedReasons = append(skippedReasons, reason)
			continue
		}

		if err := removeBranchWorktree(fs, g, hub, hubPath, c.Branch); err != nil {
			reason := fmt.Sprintf("%s: %s", c.Branch, err.Error())
			output.Warn("Failed to remove %s", reason)
			skippedReasons = append(skippedReasons, reason)
			continue
		}
		removed++
	}

	output.Info("Removed %d, skipped %d (%d reasons)", removed, len(skippedReasons), len(skippedReasons))

	if len(skippedReasons) > 0 {
		// Mirror single-target remove's exit convention: bubble up a
		// non-zero status when any candidate was blocked or failed.
		output.Fatal("some merged worktrees could not be removed")
	}
}

func init() {
	cli.RootCmd.AddCommand(removeCmd)
	removeCmd.Flags().Bool("no-prompt", false, "Skip the confirmation prompt only; does NOT bypass the safety gate")
	removeCmd.Flags().Bool("no-verify", false, "Allow removal of dirty worktrees or unpushed commits (gate bypass)")
	removeCmd.Flags().Bool("merged", false, "Remove all worktrees whose branch is merged into the default branch (skips the default branch itself and the active worktree)")
	removeCmd.ValidArgsFunction = completeBranchNames
}
