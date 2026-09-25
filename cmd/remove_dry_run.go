package cmd

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/afero"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// previewRemoveBranch reports what `git hop remove <branch>` would do
// without doing any of it: no detector action, hook, git, filesystem,
// hop.json, hopspace, state or symlink write. The safety gate is evaluated
// exactly as the real run evaluates it, and a removal it would refuse
// fails here too. It returns the record the removal would produce.
func previewRemoveBranch(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, branch string, force, noVerify, noPrompt, deleteRemote bool) removeRecord {
	worktreePath := config.ResolveWorktreePath(hub.Config.Branches[branch].Path, hubPath)
	absWorktree, _ := filepath.Abs(worktreePath)

	gateRequired := false
	if _, err := fs.Stat(absWorktree); err == nil {
		safety := inspectBranchSafety(g, absWorktree, branch, hub.Config.Repo.DefaultBranch)
		output.Info("[dry-run] Safety check for '%s': %s", branch, describeSafety(safety))
		if err := removeGate(safety, force, noVerify); err != nil {
			refuseDryRun(fmt.Sprintf("remove '%s'", branch), err)
		}
		gateRequired = !safety.Merged || !safety.Clean
	} else {
		output.Info("[dry-run] Worktree for '%s' is missing on disk; safety check skipped", branch)
	}

	if !noPrompt && gateRequired {
		output.Info("[dry-run] Would ask for confirmation before removing '%s' (--no-prompt skips it)", branch)
	}

	return previewBranchRemoval(fs, g, hub, hubPath, branch, deleteRemote)
}

// previewBranchRemoval reports the steps removeBranchWorktreeWithRemote
// would take for branch, in the order it takes them, and returns the
// record it would produce.
func previewBranchRemoval(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, branch string, deleteRemote bool) removeRecord {
	worktreePath := config.ResolveWorktreePath(hub.Config.Branches[branch].Path, hubPath)
	repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
	runner := hooks.NewRunner(fs).ForRepo(hub.Config.Repo.URI)

	if err := previewDetector(g, branch, hubPath, "finish"); err != nil {
		refuseDryRun(fmt.Sprintf("remove '%s'", branch), fmt.Errorf("branch type detector failed: %v", err))
	}
	cli.PreviewHook(runner, "pre-worktree-remove", worktreePath, repoID)
	output.Info("[dry-run] Would remove worktree at %s", worktreePath)
	localExists := g.LocalBranchExists(hubPath, branch)
	previewBranchDeletion(branch, localExists, deleteRemote)
	output.Info("[dry-run] Would remove '%s' from hop.json, hopspace and state", branch)
	cli.PreviewHook(runner, "post-worktree-remove", worktreePath, repoID)
	output.Info("[dry-run] Would point 'current' at '%s'", hub.Config.Repo.DefaultBranch)
	return removeRecord{
		Kind:          removeKindWorktree,
		Branch:        branch,
		Path:          worktreePath,
		Removed:       true,
		BranchDeleted: localExists,
		RemoteDeleted: deleteRemote,
	}
}

// previewRemoveMerged is runRemoveMerged's preview: the same candidate
// list and per-candidate gate, with nothing removed. It returns the
// records the run would produce and how many candidates it would refuse;
// the caller fails when the real run would, i.e. when any would be.
func previewRemoveMerged(fs afero.Fs, g git.GitInterface, cwd string, force, noVerify, noPrompt, deleteRemote bool) ([]removeRecord, int) {
	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		output.Fatal("Not in a hub: %v", err)
	}
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	toRemove, preSkipped := collectMergedCandidates(fs, g, hub, hubPath, cwd)
	recs := skippedRemoveRecords(preSkipped)
	for _, sk := range preSkipped {
		output.Info("Skipping %s: %s", sk.Branch, sk.Reason)
	}
	if len(toRemove) == 0 {
		output.Info("No merged worktrees to remove.")
		sortRemoveRecords(recs)
		return recs, 0
	}
	sort.Slice(toRemove, func(i, j int) bool { return toRemove[i].Branch < toRemove[j].Branch })

	if !noPrompt {
		output.Info("[dry-run] Would ask for confirmation before removing %d merged worktree(s) (--no-prompt skips it)", len(toRemove))
	}

	refused := 0
	for _, c := range toRemove {
		safety := inspectBranchSafety(g, c.WorktreePath, c.Branch, hub.Config.Repo.DefaultBranch)
		output.Info("[dry-run] Safety check for '%s': %s", c.Branch, describeSafety(safety))
		if err := removeGate(safety, force, noVerify); err != nil {
			output.Info("[dry-run] Would skip %s: %v", c.Branch, err)
			recs = append(recs, keptRemoveRecord(c, err.Error()))
			refused++
			continue
		}
		recs = append(recs, previewBranchRemoval(fs, g, hub, hubPath, c.Branch, deleteRemote))
	}

	sortRemoveRecords(recs)
	return recs, refused
}

// previewRemoveHub reports what removing the hub at hubPath would delete,
// and returns the records removeHub would produce.
func previewRemoveHub(fs afero.Fs, hubPath string, noPrompt bool) []removeRecord {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}
	if !noPrompt {
		output.Info("[dry-run] Would ask for confirmation before removing hub %s (--no-prompt skips it)", hubPath)
	}

	branches := make([]string, 0, len(hub.Config.Branches))
	for name := range hub.Config.Branches {
		branches = append(branches, name)
	}
	sort.Strings(branches)
	recs := make([]removeRecord, 0, len(branches)+2)
	for _, name := range branches {
		worktreePath := config.ResolveWorktreePath(hub.Config.Branches[name].Path, hubPath)
		output.Info("[dry-run] Would remove worktree for branch '%s' at %s", name, worktreePath)
		recs = append(recs, removeRecord{Kind: removeKindWorktree, Branch: name, Path: worktreePath, Removed: true})
	}

	output.Info("[dry-run] Would remove hub directory %s", hubPath)
	recs = append(recs, removeRecord{Kind: removeKindHub, Path: hubPath, Removed: true})
	repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
	st, stErr := state.LoadState(fs)
	if stErr == nil && st.Repositories[repoID] != nil {
		output.Info("[dry-run] Would remove %s", stateRemoval(repoID, hubPath, otherHubsInState(st, repoID, hubPath)))
	}
	for _, key := range hop.LoadRegistry(fs).HubKeys(hubPath, hubWorktreePaths(hub, hubPath)) {
		output.Info("[dry-run] Would remove '%s' from the hops registry", key)
	}
	d := dataHomeHopspaceFor(fs, st, stErr, hub, hubPath)
	switch {
	case !d.exists:
	case d.remove():
		output.Info("[dry-run] Would remove hopspace data at %s: %s", d.path, d.reason())
	default:
		output.Info("[dry-run] Would keep hopspace data at %s: %s", d.path, d.reason())
	}
	if d.exists {
		recs = append(recs, d.record(d.remove()))
	}
	return recs
}
