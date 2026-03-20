package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

var moveCmd = &cobra.Command{
	Use:     "move [old-branch] <new-branch>",
	Aliases: []string{"rename", "mv"},
	Short:   "Rename a worktree and its branch",
	Args:    cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
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

		var oldBranch, newBranch string

		if len(args) == 1 {
			// Infer old branch from cwd
			newBranch = args[0]
			oldBranch, err = g.GetCurrentBranch(cwd)
			if err != nil || oldBranch == "" {
				output.Fatal("Could not detect current branch. Use: git hop move <old> <new>")
			}
			// Verify cwd is inside this branch's worktree
			if bc, ok := hub.Config.Branches[oldBranch]; ok {
				absWorktree, _ := filepath.Abs(config.ResolveWorktreePath(bc.Path, hubPath))
				absCwd, _ := filepath.Abs(cwd)
				if absCwd != absWorktree && !strings.HasPrefix(absCwd, absWorktree+string(filepath.Separator)) {
					output.Fatal("Current directory is not inside the worktree for branch '%s'.", oldBranch)
				}
			} else {
				output.Fatal("Current branch '%s' is not tracked in this hub.", oldBranch)
			}
		} else {
			oldBranch = args[0]
			newBranch = args[1]
		}

		// Guard: default branch
		if oldBranch == hub.Config.Repo.DefaultBranch {
			output.Fatal("Cannot move the default branch '%s'.", oldBranch)
		}

		// Resolve old path
		branchCfg, ok := hub.Config.Branches[oldBranch]
		if !ok {
			output.Fatal("Branch '%s' not found in hub.", oldBranch)
		}
		oldPath := config.ResolveWorktreePath(branchCfg.Path, hubPath)

		// Compute new path for hook env vars
		globalLoader := config.NewGlobalLoader()
		globalConfig, err := globalLoader.Load()
		if err != nil {
			globalConfig = globalLoader.GetDefaults()
		}

		dataHome := hop.GetGitHopDataHome()
		ctx := hop.WorktreeLocationContext{
			HubPath:  hubPath,
			Branch:   newBranch,
			Org:      hub.Config.Repo.Org,
			Repo:     hub.Config.Repo.Repo,
			DataHome: dataHome,
		}
		newPath := filepath.Clean(hop.ExpandWorktreeLocation(globalConfig.Defaults.WorktreeLocation, ctx))

		repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

		// Detector (for hook env vars)
		detectorMgr := detector.NewManager(fs, g)
		detectorMgr.Register(detector.NewGitFlowNextDetector(g))
		detectorMgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))
		detectorCtx := context.Background()
		branchInfo, err := detectorMgr.ExecutePreAdd(detectorCtx, oldBranch, hubPath, oldPath)
		if err != nil {
			output.Fatal("Detector failed: %v", err)
		}
		detectorEnv := detectorMgr.GetDetectorEnvVars(branchInfo)

		// Add move-specific env vars
		detectorEnv["GIT_HOP_OLD_BRANCH"] = oldBranch
		detectorEnv["GIT_HOP_NEW_BRANCH"] = newBranch
		detectorEnv["GIT_HOP_OLD_PATH"] = oldPath
		detectorEnv["GIT_HOP_NEW_PATH"] = newPath

		// Pre-worktree-move hook
		hookRunner := hooks.NewRunner(fs)
		if err := hookRunner.ExecuteHookWithDetector("pre-worktree-move", oldPath, repoID, oldBranch, detectorEnv); err != nil {
			output.Fatal("Hook pre-worktree-move failed: %v", err)
		}

		output.Info("Moving '%s' → '%s'...", oldBranch, newBranch)

		// Load hopspace — try local first, then global
		hopspace, err := hop.LoadHopspace(fs, hubPath)
		if err != nil {
			hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)
			hopspace, err = hop.LoadHopspace(fs, hopspacePath)
			if err != nil {
				output.Fatal("Failed to load hopspace: %v", err)
			}
		}

		// Execute move
		wm := hop.NewWorktreeManager(fs, g)
		actualOldPath, actualNewPath, err := wm.MoveWorktree(hopspace, hub, oldBranch, newBranch, globalConfig.Defaults.WorktreeLocation, hub.Config.Repo.Org, hub.Config.Repo.Repo)
		if err != nil {
			output.Fatal("Failed to move worktree: %v", err)
		}

		// Update current symlink if it pointed to old path
		if target, err := hop.GetCurrentSymlink(fs, hubPath); err == nil {
			absTarget, _ := filepath.Abs(filepath.Join(hubPath, target))
			if absTarget == actualOldPath {
				if err := hop.UpdateCurrentSymlink(fs, hubPath, actualNewPath); err != nil {
					output.Warn("Failed to update current symlink: %v", err)
				}
			}
		}

		// Update global state
		st, err := state.LoadState(fs)
		if err != nil {
			st = state.NewState()
		}
		_ = st.RemoveWorktree(repoID, oldBranch)
		if err := st.AddWorktree(repoID, newBranch, &state.WorktreeState{
			Path:         actualNewPath,
			Type:         "linked",
			HubPath:      hubPath,
			CreatedAt:    time.Now(),
			LastAccessed: time.Now(),
		}); err != nil {
			output.Warn("Failed to update state: %v", err)
		} else if err := state.SaveState(fs, st); err != nil {
			output.Warn("Failed to save state: %v", err)
		}

		// Rekey ports/volumes configs
		hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)
		loader := config.NewLoader(fs)
		writer := config.NewWriter(fs)
		if portsCfg, err := loader.LoadPortsConfig(hopspacePath); err == nil {
			if entry, ok := portsCfg.Branches[oldBranch]; ok {
				delete(portsCfg.Branches, oldBranch)
				portsCfg.Branches[newBranch] = entry
				_ = writer.WritePortsConfig(hopspacePath, portsCfg)
			}
		}
		if volsCfg, err := loader.LoadVolumesConfig(hopspacePath); err == nil {
			if entry, ok := volsCfg.Branches[oldBranch]; ok {
				delete(volsCfg.Branches, oldBranch)
				volsCfg.Branches[newBranch] = entry
				_ = writer.WriteVolumesConfig(hopspacePath, volsCfg)
			}
		}

		// Post-worktree-move hook
		if err := hookRunner.ExecuteHookWithDetector("post-worktree-move", actualNewPath, repoID, newBranch, detectorEnv); err != nil {
			output.Warn("Hook post-worktree-move failed: %v", err)
		}

		output.Info("Moved '%s' → '%s'", oldBranch, newBranch)
		output.Info("Worktree: %s", actualNewPath)
	},
}

func init() {
	cli.RootCmd.AddCommand(moveCmd)
	moveCmd.ValidArgsFunction = completeBranchNames
}
