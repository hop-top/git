package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/events"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
	"hop.top/kit/go/runtime/bus"
)

// removeArgs accepts one target, or none when --merged picks them. A
// missing target, or a target beside --merged, is a usage error.
func removeArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
		return err
	}
	merged, _ := cmd.Flags().GetBool("merged")
	deleteVolumes, _ := cmd.Flags().GetBool("delete-volumes")
	switch {
	case merged && len(args) > 0:
		return errors.New("cannot pass both a target and --merged")
	case merged && deleteVolumes:
		return errors.New("--delete-volumes applies only to removing a hub")
	case !merged && len(args) == 0:
		return errors.New("requires a target, or --merged")
	}
	return nil
}

var removeCmd = &cobra.Command{
	Use:     "remove [target]",
	Aliases: []string{"rm", "delete", "del"},
	Short:   "Remove a hub, hopspace, or branch",
	Long: `Remove a hub, hopspace, or branch.

Removing a branch worktree runs a safety gate first. A branch merged into
the branch its worktree was made from (merge commit, squash, or rebase)
with a clean worktree is removed silently. That branch is the worktree's
recorded base, else settings.compareBranch, else the default branch, the
same one status shows; a base that no longer exists falls to the next
choice, and the output says so. --base <branch> checks every candidate
against <branch> instead. The current directory never picks it.
On the default branch either ref counts: with no local branch it is checked
as origin/<default>; a local one behind origin/<default> is checked against
both, and merged into either is merged. A base, compareBranch and --base are
checked as given. remove never fetches.

Anything else needs explicit flags:

  merged, uncommitted or untracked files         --no-verify
  not merged, pushed to origin, clean            --force
  not merged, pushed, uncommitted or untracked   --force --no-verify
  not merged, not pushed                         --force --no-verify

--force covers the not-merged check; --no-verify covers uncommitted or
untracked files (merged or not) and unpushed commits. Neither implies
the other: pushing protects commits, not the files in the worktree.
With hop.gitflow.enabled, a branch git flow finish handles skips the
not-merged and unpushed checks, since finish merges it into its parent
first, but its worktree must be clean whatever the flags.
--no-prompt only skips the confirmation prompt; it never satisfies the
gate. --no-verify does not skip pre-/post-worktree-remove hooks.

The worktree's local branch goes with it: the branch the worktree has
checked out, which is not always the name hop.json lists it under. The
gate above has vouched for it, so it is deleted outright (git branch -D),
once git's record of the worktree is pruned. --keep-branch, or
hop.remove.keepBranch, keeps it. Whenever the branch stays, remove says
which and why: kept by the flag or config, already absent, the default
branch, or git's own error when the check or the deletion failed; the
result's branch_kept_reason says the same. A branch that stays because
git refused to delete it still exits 0: the worktree is gone.

Worktrees made with plain git (git worktree add) are in git's registry but
not in hop.json. Remove takes them by branch name or path, and --merged
takes the merged ones, under the same gate and the same branch deletion;
hop.json is left as it is.

Removing a hub keeps its volume data: the volume directories in the hub,
and in a --global hopspace removed with it, are moved aside to
$GIT_HOP_DATA_HOME/orphaned-volumes/<org>/<repo>/, and a hint says where.
--delete-volumes deletes them with the hub instead.`,
	Args: removeArgs,
	Run: func(cmd *cobra.Command, args []string) {
		noPrompt, _ := cmd.Flags().GetBool("no-prompt")
		noVerify, _ := cmd.Flags().GetBool("no-verify")
		merged, _ := cmd.Flags().GetBool("merged")
		deleteRemote, _ := cmd.Flags().GetBool("delete-remote")
		force, _ := cmd.Root().PersistentFlags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		deleteVolumes, _ := cmd.Flags().GetBool("delete-volumes")

		fs := afero.NewOsFs()
		g := git.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		// --merged path: collect merged worktrees and remove each. The
		// records go out before a partial failure's exit status, so a
		// caller learns what did go.
		if merged {
			if hubPath, err := hop.FindHub(fs, cwd); err == nil {
				checkBaseOverride(g, hubPath, baseOverride(cmd))
			}
			if dryRun {
				recs, refused := previewRemoveMerged(fs, g, cwd, force, noVerify, noPrompt, removeOptionsFor(cmd, cwd, deleteRemote))
				emitRemoveResult(cmd, markDryRun(recs))
				if refused > 0 {
					output.Fatal("[dry-run] would fail: %d merged worktree(s) could not be removed", refused)
				}
				return
			}
			recs, failed := runRemoveMerged(fs, g, cwd, force, noVerify, noPrompt, removeOptionsFor(cmd, cwd, deleteRemote))
			emitRemoveResult(cmd, recs)
			if failed > 0 {
				// Mirror single-target remove's exit convention: bubble
				// up a non-zero status when any candidate was blocked or
				// failed.
				output.Fatal("some merged worktrees could not be removed")
			}
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

			// A worktree made with plain git is in git's registry, not
			// hop.json: remove it all the same, under its branch name.
			if _, ok := hub.Config.Branches[target]; !ok {
				if wt, found := findUnregisteredWorktree(fs, g, hub, hubPath, cwd, target); found {
					target = wt.key()
					adoptTransiently(hub, target, wt.Path)
				}
			}

			// Check if target is a branch in the hub
			if _, ok := hub.Config.Branches[target]; ok {
				if deleteVolumes {
					output.FatalCode(exitUsage, "--delete-volumes applies only to removing a hub")
				}
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

				opts := removeOptionsFor(cmd, hubPath, deleteRemote)
				checkBaseOverride(g, hubPath, opts.Base)
				if dryRun {
					rec := previewRemoveBranch(fs, g, hub, hubPath, target, force, noVerify, noPrompt, opts)
					emitRemoveResult(cmd, markDryRun([]removeRecord{rec}))
					return
				}

				// Safety gate: probe the worktree and require --force /
				// --no-verify based on merged/pushed/dirty state. Skip
				// when the worktree is missing on disk (nothing to lose).
				gateRequired := false
				var checked compareTarget
				if _, err := fs.Stat(absWorktree); err == nil {
					checked = targetFor(g, hub, absWorktree, target, opts.Base)
					safety := inspectRemoveSafety(fs, g, hubPath, absWorktree, resolveWorktreeBranch(g, absWorktree, absWorktree, target), checked)
					if err := removeGate(safety, force, noVerify); err != nil {
						output.Fatal("%s", err.Error())
					}
					// Gate fired (and was satisfied by flags): the
					// confirmation prompt is appropriate.
					gateRequired = safety.risky()
				}

				// Only prompt when the operation is risky. Clean+merged
				// removals proceed silently per spec.
				if !noPrompt && gateRequired {
					branchConfig := hub.Config.Branches[target]
					worktreePath := config.ResolveWorktreePath(branchConfig.Path, hubPath)

					confirmed, err := output.ConfirmDeletionAnswer(target, []output.CardField{
						{Key: "Type", Value: "Branch worktree"},
						{Key: "Path", Value: worktreePath},
						{Key: "Hub", Value: hubPath},
					})
					if !resolveConfirmation(confirmed, err) {
						return
					}
				}

				rec, err := removeBranchWorktreeOpts(fs, g, hub, hubPath, target, opts)
				if err != nil {
					output.Fatal("%s", err.Error())
				}
				emitRemoveResult(cmd, []removeRecord{rec.withTarget(checked)})
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
			if baseOverride(cmd) != "" {
				output.FatalCode(exitUsage, "--base applies only to removing a branch worktree")
			}
			if dryRun {
				emitRemoveResult(cmd, markDryRun(previewRemoveHub(fs, targetPath, noPrompt, deleteVolumes)))
				return
			}
			if !noPrompt {
				// Load hub to get branch count
				hub, err := hop.LoadHub(fs, targetPath)
				if err == nil {
					branchCount := len(hub.Config.Branches)

					confirmed, err := output.ConfirmDeletionAnswer(targetPath, []output.CardField{
						{Key: "Type", Value: "Hub"},
						{Key: "Branches", Value: fmt.Sprintf("%d", branchCount)},
						{Key: "Repository", Value: fmt.Sprintf("%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)},
						volumesCardField(deleteVolumes),
					})
					if !resolveConfirmation(confirmed, err) {
						return
					}
				} else {
					// Fallback if we can't load hub config
					confirmed, err := output.ConfirmDeletionAnswer(targetPath, []output.CardField{
						{Key: "Type", Value: "Hub"},
						volumesCardField(deleteVolumes),
					})
					if !resolveConfirmation(confirmed, err) {
						return
					}
				}
			}

			emitRemoveResult(cmd, removeHub(fs, targetPath, deleteVolumes))
			return
		}

		// Target not found
		output.Fatal("Target %s not found or not supported yet", target)
	},
}

// exitPromptUnanswerable is the exit status for a confirmation prompt
// that could not be put to the user. It is a usage error: the invocation
// asked for interactive confirmation in an environment that cannot
// supply one, and the caller fixes it by passing --no-prompt.
const exitPromptUnanswerable = 129

// volumesCardField says, on the hub removal's confirmation card, what
// happens to its volume data.
func volumesCardField(deleteVolumes bool) output.CardField {
	if deleteVolumes {
		return output.CardField{Key: "Volume data", Value: "deleted (--delete-volumes)"}
	}
	return output.CardField{Key: "Volume data", Value: "kept (moved aside)"}
}

// resolveConfirmation collapses a prompt result into a single "proceed?"
// decision.
//
// An unanswerable prompt (non-interactive stdin) is fatal: it exits 129
// with a fatal: message rather than printing a cancellation that a
// batch script would read as a successful no-op. An explicit decline is
// a real user decision — it reports the cancellation and returns false
// so the caller can return normally and let deferred cleanup run.
func resolveConfirmation(confirmed bool, err error) bool {
	if err != nil {
		output.FatalCode(exitPromptUnanswerable, "%s", err.Error())
	}
	if !confirmed {
		output.Info("Cancelled.")
		return false
	}
	return true
}

// updateCurrentToDefault updates the "current" symlink to point to the defaultBranch worktree.
// Called after removing a branch so the symlink stays valid.
//
// A default branch with no worktree gives the link nowhere to go. When
// the link dangles, the removed worktree being what it pointed at, create
// is called to make the default branch's worktree (as 'git hop add' would)
// and the link moves to it. If that fails, or create is nil, the link is
// dropped with a warning naming the reason: a missing link is valid, a
// dangling one breaks every consumer that follows it. A link that still
// resolves is left alone.
func updateCurrentToDefault(fs afero.Fs, hub *hop.Hub, hubPath string, create defaultWorktreeCreator) error {
	defaultBranch := hub.Config.Repo.DefaultBranch
	branchCfg, ok := hub.Config.Branches[defaultBranch]
	if !ok {
		if currentDangles(fs, hubPath) {
			return createDefaultOrDropCurrent(fs, hub, hubPath, create)
		}
		return fmt.Errorf("default branch %q not found in hub", defaultBranch)
	}
	defaultPath := config.ResolveWorktreePath(branchCfg.Path, hubPath)
	return hop.UpdateCurrentSymlink(fs, hubPath, defaultPath)
}

// createDefaultOrDropCurrent makes the default branch's worktree and
// points "current" at it; when it cannot, it says why and drops "current".
func createDefaultOrDropCurrent(fs afero.Fs, hub *hop.Hub, hubPath string, create defaultWorktreeCreator) error {
	defaultBranch := hub.Config.Repo.DefaultBranch
	err := errors.New("no way to create it")
	if create != nil {
		if err = create(hub, hubPath); err == nil {
			branchCfg, ok := hub.Config.Branches[defaultBranch]
			if ok {
				defaultPath := config.ResolveWorktreePath(branchCfg.Path, hubPath)
				output.Info("Created worktree for default branch %q at %s and pointed 'current' at it", defaultBranch, defaultPath)
				return hop.UpdateCurrentSymlink(fs, hubPath, defaultPath)
			}
			err = errors.New("it was not recorded in the hub")
		}
	}
	output.Warn("Could not create a worktree for default branch %q: %v", defaultBranch, err)
	if err := hop.RemoveCurrentSymlink(fs, hubPath); err != nil {
		return err
	}
	output.Info("Removed 'current': it pointed at the removed worktree and default branch %q has no worktree", defaultBranch)
	return nil
}

// currentTarget is the absolute path the hub's "current" symlink names,
// and whether there is one.
func currentTarget(fs afero.Fs, hubPath string) (string, bool) {
	target, err := hop.GetCurrentSymlink(fs, hubPath)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(hubPath, target)
	}
	return target, true
}

// currentDangles reports whether the hub has a "current" symlink whose
// target is gone.
func currentDangles(fs afero.Fs, hubPath string) bool {
	target, ok := currentTarget(fs, hubPath)
	if !ok {
		return false
	}
	_, err := fs.Stat(target)
	return os.IsNotExist(err)
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
	_, err := removeBranchWorktreeWithRemote(fs, g, hub, hubPath, branch, false)
	return err
}

// removeBranchWorktreeWithRemote is removeBranchWorktree plus explicit
// control over remote-branch deletion.
//
// deleteRemote gates every origin-touching call. When false (the
// default) removal is fully local: no `git ls-remote` probe, no
// `git push --delete`. Probing origin on every removal made the command
// hang indefinitely whenever origin was unreachable, slow, or required
// credentials — a local worktree removal has no reason to wait on the
// network. Deleting the remote branch is a separate, destructive act
// the user must ask for via --delete-remote.
//
// The returned record says what the removal did; it is meaningful only
// when the error is nil.
func removeBranchWorktreeWithRemote(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, branch string, deleteRemote bool) (removeRecord, error) {
	return removeBranchWorktreeOpts(fs, g, hub, hubPath, branch, removeOptions{DeleteRemote: deleteRemote})
}

// removeBranchWorktreeOpts is removeBranchWorktreeWithRemote taking the
// whole of the removal's options (see removeOptions).
//
// The local branch deleted is the one the worktree has checked out, read
// before the worktree goes (the hop.json key when git does not say). It
// goes only after the worktree is removed and git's worktree metadata
// pruned, which would otherwise make git refuse it as "used by worktree".
// The safety gate the removal passed covers the deletion: a branch
// merged by content (squash, rebase) is not reachable from default, so
// the deletion forces (-D) where git branch -d would refuse. Whatever
// leaves the branch in place is said, in the human lines and in the
// record's branch_kept_reason.
func removeBranchWorktreeOpts(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, branch string, opts removeOptions) (removeRecord, error) {
	deleteRemote := opts.DeleteRemote
	output.Info("Removing branch %s from hub...", branch)

	// Get the worktree path from the hub config BEFORE removing from config
	branchConfig := hub.Config.Branches[branch]
	// Resolve the worktree path (may be relative like "hops/branch")
	worktreePath := config.ResolveWorktreePath(branchConfig.Path, hubPath)
	rec := removeRecord{Kind: removeKindWorktree, Branch: branch, Path: worktreePath}

	repoID := repoid.For(hubPath, hub.Config.Repo)

	// The branch the worktree holds is read before anything runs in it: a
	// git flow finish (the detector's pre-remove) leaves the worktree on
	// the branch it merged into, which is not the one to delete.
	absBasePath := removalBasePath(fs, hub, hubPath, branch)
	localBranch := resolveWorktreeBranch(g, absBasePath, worktreePath, branch)

	// Create detector manager and register detectors
	detectorMgr := newBranchDetectors(fs, g, hubPath)

	// Execute pre-remove (detector OnRemove)
	detectorCtx := context.Background()
	branchInfo, err := detectorMgr.ExecutePreRemove(detectorCtx, branch, hubPath, worktreePath)
	if err != nil {
		return rec, fmt.Errorf("branch type detector failed: %v", err)
	}

	// Execute pre-worktree-remove hook with detector env vars
	hookRunner := hooks.NewRunner(fs).ForRepo(hub.Config.Repo.URI, hubPath)
	detectorEnv := detectorMgr.GetDetectorEnvVars(branchInfo)
	if _, err := hookRunner.ExecuteHookWithDetector("pre-worktree-remove", worktreePath, repoID, branch, detectorEnv); err != nil {
		return rec, fmt.Errorf("hook pre-worktree-remove failed: %v", err)
	}

	// Only `git worktree remove` deletes a worktree's files. When it
	// cannot, or the directory is not a worktree git has registered and
	// holds anything, the removal stops here: the files, the hop.json
	// entry and the branch all stay, and the error says why.
	output.Info("Removing worktree directory: %s", worktreePath)
	if err := removeWorktreeFiles(fs, g, absBasePath, worktreePath); err != nil {
		return rec, err
	}
	rec.Removed = true
	output.Info("Successfully removed worktree directory")

	// Remove parent dir (e.g. feat/, fix/) if now empty.
	cleanupMgr := hop.NewCleanupManager(fs, g)
	if err := cleanupMgr.RemoveEmptyParent(worktreePath, hubPath); err != nil {
		output.Warn("Failed to remove empty parent directory: %v", err)
	}

	// Git's metadata for the removed worktree goes before its branch:
	// while it stays, git refuses the branch as used by a worktree.
	if err := hop.NewCleanupManager(fs, g).PruneWorktrees(hubPath); err != nil {
		output.Warn("Failed to prune worktrees: %v", err)
	}

	outcome := planBranchDeletion(g, absBasePath, localBranch, hub.Config.Repo.DefaultBranch, opts)
	if outcome.Delete {
		if err := g.DeleteLocalBranch(absBasePath, outcome.Branch); err != nil {
			outcome.Delete = false
			outcome.Failed = true
			outcome.KeptReason = "git branch -D failed: " + errorCause(err)
		}
	}
	applyBranchOutcome(&rec, outcome)
	reportKeptBranch(outcome)

	// Remote deletion is opt-in. Both calls below reach the network, so
	// they run only when the user explicitly asked for them.
	if deleteRemote {
		if g.HasRemoteBranch(absBasePath, branch) {
			if err := g.DeleteRemoteBranch(absBasePath, branch); err != nil {
				output.Warn("Failed to delete remote branch: %v", err)
			} else {
				rec.RemoteDeleted = true
			}
		}
	}

	// Free the worktree's ports and volumes for other worktrees.
	if err := services.DropEnvEntry(fs, hop.ResolveHopspacePath(hubPath, hub.Config.Repo), hubPath, worktreePath, branch); err != nil {
		output.Warn("Failed to update ports and volumes: %v", err)
	}

	// Remove branch from hub config so it no longer appears in status
	if err := hub.RemoveBranch(branch); err != nil {
		output.Warn("Failed to update hub config: %v", err)
	}
	delete(hub.Config.Branches, branch) // an adopted-in-memory entry too

	// Load Hopspace to unregister
	hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
	if hopspace, err := hop.LoadHopspace(fs, hopspacePath); err == nil {
		// Unregister from hopspace (silent if branch doesn't exist)
		hopspace.UnregisterBranch(hubPath, branch, worktreePath)
	}

	// Update global state
	if err := state.Update(fs, func(st *state.State) error {
		return st.RemoveWorktreeAt(repoID, worktreePath)
	}); err != nil {
		output.Warn("Failed to update state: %v", err)
	}

	// Execute post-worktree-remove hook
	if _, err := hookRunner.ExecuteHookWithDetector("post-worktree-remove", worktreePath, repoID, branch, detectorEnv); err != nil {
		output.Warn("Hook post-worktree-remove failed: %v", err)
	}

	// Update current symlink to point back to defaultBranch worktree
	if err := updateCurrentToDefault(fs, hub, hubPath, newDefaultWorktreeCreator(fs, g)); err != nil {
		output.Warn("Failed to update current symlink: %v", err)
	}

	// The branch is already out of hub.Config above, so restating the set
	// here is what actually retires the removed path from the shell
	// integration's cache. Left stale, the handler keeps announcing a
	// worktree that no longer exists on every cd into its old location.
	refreshRootsCache(fs, hub, hubPath)

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

	if outcome.Failed {
		output.Warn("Removed worktree %s; local branch %s kept: %s", branch, outcome.Branch, outcome.KeptReason)
	} else {
		output.Info("Successfully removed %s", branch)
	}
	return rec, nil
}

// removalBasePath is a live directory to run removing branch's git
// commands (worktree remove, branch -D) in: the default branch's
// worktree, else any other worktree of the hub that is on disk, else
// hubPath itself (the bare repository), so git always has a working
// directory even when every recorded worktree is missing.
func removalBasePath(fs afero.Fs, hub *hop.Hub, hubPath, branch string) string {
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
		if info, err := fs.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return hubPath
}

// isWorktreeRegistered reports whether git's worktree registry contains
// an entry for worktreePath.
//
// This is the structural signal that separates "already gone" from "real
// failure" when removing a worktree. `git worktree remove` fails with
// "is not a working tree" for exactly one reason: the path is absent
// from the registry. Every other failure mode (locked worktree, dirty
// tree, permission denied, I/O error) happens on a path that IS
// registered. Probing membership first therefore skips only the doomed-
// and-benign call, leaving every genuine failure to surface normally.
//
// Deliberately structural rather than a match on git's stderr: message
// wording varies across git versions and is translated under non-English
// locales, so string inspection would silently stop working. Registry
// membership is stable porcelain v1 output.
//
// A registry that cannot be read at all (git failure) is treated as
// "registered" so the removal is still attempted and any error is
// reported — failing toward visibility, never toward silence. An entry
// whose directory has since been deleted is still listed (marked
// prunable) and `git worktree remove` still succeeds on it, so no
// special handling is needed for that case.
func isWorktreeRegistered(g git.GitInterface, basePath, worktreePath string) bool {
	out, err := g.WorktreeListPorcelain(basePath)
	if err != nil {
		return true
	}

	// git lists resolved paths (macOS /var is /private/var); a path
	// recorded through a symlink still names the same worktree.
	for _, wt := range parseWorktreeListPorcelain(out) {
		if state.SamePath(wt.Path, worktreePath) {
			return true
		}
	}
	return false
}

// mergedCandidate captures one row in the candidate list built by --merged.
// reason is non-empty when the branch was excluded; included candidates
// have reason == "".
type mergedCandidate struct {
	Branch       string
	WorktreePath string
	Skip         bool
	Reason       string
	// GitBranch is the branch checked out in the worktree, which the gate
	// probes and the removal deletes; Branch is the name it is reported under.
	GitBranch string
	// Unregistered marks a worktree git lists and hop.json does not.
	Unregistered bool
	// Target is the branch the candidate's merge was checked against.
	Target compareTarget
}

// adopt shows the candidate to the removal pipeline as a hub entry, when
// hop.json has none (see adoptTransiently).
func (c mergedCandidate) adopt(hub *hop.Hub) {
	if c.Unregistered {
		adoptTransiently(hub, c.Branch, c.WorktreePath)
	}
}

// collectMergedCandidates iterates hub branches, skips the default branch
// and the currently-active worktree, and probes each remaining branch
// with inspectBranchSafety. Branches whose tip has zero commits ahead of
// the default branch (Merged=true) are returned for removal. Branches
// missing on disk are reported as skipped with a prune suggestion.
//
// The hub map is read but never mutated here — callers act on a stable
// snapshot.
func collectMergedCandidates(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, cwd, override string) (toRemove []mergedCandidate, skipped []mergedCandidate) {
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
				Reason:       "worktree path missing; run 'git hop prune' first",
			})
			continue
		}

		// Probe merge state. inspectBranchSafety is read-only.
		gitBranch := resolveWorktreeBranch(g, absWorktree, absWorktree, branch)
		target := targetFor(g, hub, absWorktree, branch, override)
		safety := inspectBranchSafety(g, absWorktree, gitBranch, target.Branch)
		if !safety.Merged {
			continue
		}

		toRemove = append(toRemove, mergedCandidate{
			Branch:       branch,
			WorktreePath: worktreePath,
			GitBranch:    gitBranch,
			Target:       target,
		})
	}

	// Worktrees made with plain git are in git's registry and not in
	// hop.json; they are candidates like any other.
	for _, wt := range unregisteredWorktrees(fs, g, hub, hubPath) {
		c := mergedCandidate{Branch: wt.key(), WorktreePath: wt.Path, GitBranch: wt.Branch, Unregistered: true}
		absWorktree, _ := filepath.Abs(wt.Path)
		switch {
		case wt.Branch == defaultBranch:
			continue
		case absCwd == absWorktree || strings.HasPrefix(absCwd, absWorktree+string(filepath.Separator)):
			c.Skip, c.Reason = true, "currently inside this worktree"
		case wt.Missing:
			c.Skip, c.Reason = true, "worktree path missing; run 'git hop prune' first"
		case wt.Detached:
			c.Skip, c.Reason = true, "detached HEAD; no branch to compare with the default branch"
		default:
			c.Target = resolveMergeTargetFor(g, absWorktree, hub.Config, config.HubBranch{}, override, wt.Branch)
			if !inspectBranchSafety(g, absWorktree, wt.Branch, c.Target.Branch).Merged {
				continue
			}
			toRemove = append(toRemove, c)
			continue
		}
		skipped = append(skipped, c)
	}
	return toRemove, skipped
}

// runRemoveMerged drives the --merged removal flow. Order of operations:
// snapshot the candidate list, prompt (unless --no-prompt), then loop
// each candidate through removeGate + removeBranchWorktree.
//
// It returns one record per candidate and pre-iteration skip, in branch
// order, and how many candidates the safety gate refused or failed to
// remove; the caller turns a non-zero count into the exit status.
func runRemoveMerged(fs afero.Fs, g git.GitInterface, cwd string, force, noVerify, noPrompt bool, opts removeOptions) ([]removeRecord, int) {
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
	toRemove, preSkipped := collectMergedCandidates(fs, g, hub, hubPath, cwd, opts.Base)

	// Surface pre-iteration skips (cwd-inside, missing path) up front so
	// the user knows why something they expected to disappear didn't.
	recs := skippedRemoveRecords(preSkipped)
	for _, sk := range preSkipped {
		output.Info("Skipping %s: %s", sk.Branch, sk.Reason)
	}

	if len(toRemove) == 0 {
		output.Info("No merged worktrees to remove.")
		return recs, 0
	}

	// Show the candidate list.
	output.Info("Merged worktrees to remove:")
	for _, c := range toRemove {
		output.Info("  %s  %s  (merged into %s)", c.Branch, c.WorktreePath, c.Target)
		if c.Target.Note != "" {
			output.Info("    %s", c.Target.Note)
		}
	}

	if !noPrompt {
		confirmed, err := output.ConfirmAnswer(fmt.Sprintf("Remove %d merged worktree(s)?", len(toRemove)))
		if !resolveConfirmation(confirmed, err) {
			return nil, 0
		}
	}

	removed := 0
	skippedReasons := []string{}
	for _, c := range toRemove {
		// Re-check the gate per-candidate. The earlier collection only
		// confirmed Merged=true; the gate also enforces the dirty-state
		// rule when --no-verify isn't set.
		safety := inspectRemoveSafety(fs, g, hubPath, c.WorktreePath, c.GitBranch, c.Target)
		if err := removeGate(safety, force, noVerify); err != nil {
			reason := fmt.Sprintf("%s: %s", c.Branch, err.Error())
			output.Warn("Skipping %s", reason)
			skippedReasons = append(skippedReasons, reason)
			recs = append(recs, keptRemoveRecord(c, err.Error()))
			continue
		}

		c.adopt(hub)
		rec, err := removeBranchWorktreeOpts(fs, g, hub, hubPath, c.Branch, opts)
		if err != nil {
			delete(hub.Config.Branches, c.Branch)
			reason := fmt.Sprintf("%s: %s", c.Branch, err.Error())
			output.Warn("Failed to remove %s", reason)
			skippedReasons = append(skippedReasons, reason)
			recs = append(recs, keptRemoveRecord(c, err.Error()))
			continue
		}
		recs = append(recs, rec.withTarget(c.Target))
		removed++
	}

	output.Info("Removed %d, skipped %d (%d reasons)", removed, len(skippedReasons), len(skippedReasons))

	sortRemoveRecords(recs)
	return recs, len(skippedReasons)
}

// keptRemoveRecord is the record of a --merged candidate left in place,
// and why.
func keptRemoveRecord(c mergedCandidate, reason string) removeRecord {
	return removeRecord{Kind: removeKindWorktree, Branch: c.Branch, Path: c.WorktreePath, Reason: reason}.withTarget(c.Target)
}

// skippedRemoveRecords are the records of the candidates --merged set
// aside before removing anything.
func skippedRemoveRecords(skipped []mergedCandidate) []removeRecord {
	recs := make([]removeRecord, 0, len(skipped))
	for _, sk := range skipped {
		recs = append(recs, keptRemoveRecord(sk, sk.Reason))
	}
	return recs
}

func init() {
	cli.RootCmd.AddCommand(removeCmd)
	removeCmd.Flags().Bool("no-prompt", false, "Skip the confirmation prompt only; does NOT bypass the safety gate")
	removeCmd.Flags().Bool("no-verify", false, "Allow removal despite uncommitted/untracked files or unpushed commits; does NOT bypass the not-merged check (needs --force)")
	removeCmd.Flags().Bool("merged", false, "Remove all worktrees whose branch is merged into the branch it was made from (its base; see --base) (skips the default branch itself and the active worktree)")
	removeCmd.Flags().String("base", "", "Branch to check every candidate's merge against, in place of each worktree's own base (compareBranch, then the default branch, when it has none)")
	removeCmd.Flags().Bool("keep-branch", false, "Keep the local branch after removing its worktree; without it the branch goes with the worktree (default via hop.remove.keepBranch)")
	removeCmd.Flags().Bool("delete-remote", false, "Also delete the branch on origin; without it removal stays local and never contacts the network")
	removeCmd.Flags().Bool("delete-volumes", false, "Removing a hub, also delete its volume data; without it the data is kept, moved aside under the data home")
	removeCmd.ValidArgsFunction = completeBranchNames
}

// defaultWorktreeCreator creates the default branch's worktree in the
// hub, registering it as 'git hop add' does. Injected so tests can stand
// in for it.
type defaultWorktreeCreator func(hub *hop.Hub, hubPath string) error

// defaultBranchHasSource reports whether the default branch exists to
// make a worktree from: locally, or as origin/<default>. No fetch.
func defaultBranchHasSource(g git.GitInterface, hubPath, defaultBranch string) bool {
	return defaultBranch != "" && (localBranchExists(g, hubPath, defaultBranch) ||
		refExists(g, hubPath, "refs/remotes/origin/"+defaultBranch))
}

// newDefaultWorktreeCreator is the real creator: createHopWorktree, the
// code 'git hop add <default>' runs from the hub root, with the settings
// add resolves when no flag is given. The branch comes from the local
// default branch, else origin/<default>; nothing is fetched. A default
// branch found in neither is refused here, since add's own fallback would
// start a new branch from HEAD.
func newDefaultWorktreeCreator(fs afero.Fs, g git.GitInterface) defaultWorktreeCreator {
	return func(hub *hop.Hub, hubPath string) error {
		def := hub.Config.Repo.DefaultBranch
		if !defaultBranchHasSource(g, hubPath, def) {
			return fmt.Errorf("branch %q exists neither locally nor on origin", def)
		}
		hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
		hopspace, err := hop.LoadHopspace(fs, hopspacePath)
		if err != nil {
			return fmt.Errorf("cannot load the hopspace at %s: %v", hopspacePath, err)
		}
		globalConfig := config.NewGlobalLoader().Load()
		start, err := resolveAddStartPoint(g, hubPath, hubPath, hop.StartPointDefaultBranch,
			"removing the worktree 'current' named", def)
		if err != nil {
			return err
		}
		_, err = createHopWorktree(context.Background(), addCreateParams{
			FS:            fs,
			Git:           g,
			Docker:        docker.New(),
			Hub:           hub,
			HubPath:       hubPath,
			Hopspace:      hopspace,
			HopspacePath:  hopspacePath,
			HookRunner:    hooks.NewRunner(fs).ForRepo(hub.Config.Repo.URI, hubPath),
			WM:            hop.NewWorktreeManager(fs, g),
			GlobalConfig:  globalConfig,
			RepoID:        repoid.For(hubPath, hub.Config.Repo),
			Branch:        def,
			WorktreePath:  expandAddWorktreePath(hub, hubPath, def, globalConfig),
			StartPoint:    start.Spec,
			Start:         start,
			DefaultBranch: def,
			MiseMode:      miseTrustMode(config.NewGitConfig(), nil),
			EnvStart:      cli.DecideAutoEnvStart(nil, globalConfig),
		})
		return err
	}
}
