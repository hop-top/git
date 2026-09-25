package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/events"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
	"hop.top/kit/go/runtime/bus"
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
		plan := movePlan{
			hub:       hub,
			hubPath:   hubPath,
			repoID:    repoID,
			oldBranch: oldBranch,
			newBranch: newBranch,
			oldPath:   oldPath,
			newPath:   newPath,
		}

		// Everything below writes or runs hooks; the preview stops here.
		if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
			previewMove(fs, g, plan)
			return
		}

		hopspace, err := plan.prepare(fs, g)
		if err != nil {
			output.Fatal("Cannot move '%s': %v", oldBranch, err)
		}
		detectorEnv, err := plan.hookEnv(fs, g)
		if err != nil {
			output.Fatal("Detector failed: %v", err)
		}

		// Pre-worktree-move hook
		hookRunner := hooks.NewRunner(fs)
		if _, err := hookRunner.ExecuteHookWithDetector("pre-worktree-move", oldPath, repoID, oldBranch, detectorEnv); err != nil {
			output.Fatal("Hook pre-worktree-move failed: %v", err)
		}

		output.Info("Moving '%s' -> '%s'...", oldBranch, newBranch)

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
		// A state file that cannot be read is not replaced.
		if st, err := state.LoadState(fs); err != nil {
			output.Warn("Failed to update state: %v", err)
		} else if st.Repositories[repoID] == nil {
			output.Warn("Failed to update state: repository not found: %s", repoID)
		} else {
			_ = st.RemoveWorktreeAt(repoID, actualOldPath)
			if err := st.PutWorktree(repoID, &state.WorktreeState{
				Path:         actualNewPath,
				Branch:       newBranch,
				Type:         "linked",
				HubPath:      hubPath,
				CreatedAt:    time.Now(),
				LastAccessed: time.Now(),
			}); err != nil {
				output.Warn("Failed to update state: %v", err)
			} else if err := state.SaveState(fs, st); err != nil {
				output.Warn("Failed to save state: %v", err)
			}
		}

		// Rekey ports/volumes configs
		hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
		if err := services.RekeyEnvEntry(fs, hopspacePath, hubPath, actualOldPath, actualNewPath, oldBranch, newBranch); err != nil {
			output.Warn("Failed to update ports and volumes: %v", err)
		}

		// Post-worktree-move hook
		if _, err := hookRunner.ExecuteHookWithDetector("post-worktree-move", actualNewPath, repoID, newBranch, detectorEnv); err != nil {
			output.Warn("Hook post-worktree-move failed: %v", err)
		}

		// A rename retires one path and introduces another, so both halves
		// have to reach the shell integration. MoveWorktree already rekeyed
		// the in-memory hub, so restating it here is enough.
		refreshRootsCache(fs, hub, hubPath)

		// Emit worktree.moved event.
		_ = cli.EventBus.Publish(context.Background(), bus.NewEvent(
			events.WorktreeMoved, events.Source,
			events.WorktreeEvent{
				Path:         actualNewPath,
				Branch:       newBranch,
				HopspacePath: hopspacePath,
				RepoPath:     hubPath,
			},
		))

		output.Info("Moved '%s' -> '%s'", oldBranch, newBranch)
		output.Info("Worktree: %s", actualNewPath)
	},
}

// movePlan is everything `git hop move` has decided before its first write.
type movePlan struct {
	hub                  *hop.Hub
	hubPath, repoID      string
	oldBranch, newBranch string
	oldPath, newPath     string
}

// prepare settles every refusal the move can check before its first side
// effect, and returns the hopspace the move will update.
func (p movePlan) prepare(fs afero.Fs, g git.GitInterface) (*hop.Hopspace, error) {
	if err := hop.CheckMove(p.hub, g, p.oldBranch, p.newBranch); err != nil {
		return nil, err
	}
	hopspacePath := hop.ResolveHopspacePath(p.hubPath, p.hub.Config.Repo)
	hopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load hopspace: %v", err)
	}
	return hopspace, nil
}

// hookEnv is the environment the move hooks receive: the old branch's
// detected type plus the move's own variables. Detection only reads git
// config, and no detector action runs: renaming a branch is not starting
// one, so git-flow's start (the detector's add action) does not apply.
func (p movePlan) hookEnv(fs afero.Fs, g git.GitInterface) (map[string]string, error) {
	mgr := detector.NewManager(fs, g)
	mgr.Register(detector.NewGitFlowNextDetector(g))
	mgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))
	info, err := mgr.DetectBranch(p.oldBranch, p.hubPath)
	if err != nil {
		return nil, err
	}
	env := mgr.GetDetectorEnvVars(info)
	env["GIT_HOP_OLD_BRANCH"] = p.oldBranch
	env["GIT_HOP_NEW_BRANCH"] = p.newBranch
	env["GIT_HOP_OLD_PATH"] = p.oldPath
	env["GIT_HOP_NEW_PATH"] = p.newPath
	return env, nil
}

func init() {
	cli.RootCmd.AddCommand(moveCmd)
	moveCmd.ValidArgsFunction = completeBranchNames
}
