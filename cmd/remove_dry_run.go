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
)

// previewRemoveBranch reports what `git hop remove <branch>` would do
// without doing any of it: no detector action, hook, git, filesystem,
// hop.json, hopspace, state or symlink write. The safety gate is evaluated
// exactly as the real run evaluates it, and a removal it would refuse
// fails here too.
func previewRemoveBranch(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, branch string, force, noVerify, noPrompt, deleteRemote bool) {
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

	previewBranchRemoval(fs, g, hub, hubPath, branch, deleteRemote)
}

// previewBranchRemoval reports the steps removeBranchWorktreeWithRemote
// would take for branch, in the order it takes them.
func previewBranchRemoval(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, branch string, deleteRemote bool) {
	worktreePath := config.ResolveWorktreePath(hub.Config.Branches[branch].Path, hubPath)
	repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
	runner := hooks.NewRunner(fs)

	if err := previewDetector(g, branch, hubPath, "finish"); err != nil {
		refuseDryRun(fmt.Sprintf("remove '%s'", branch), fmt.Errorf("branch type detector failed: %v", err))
	}
	cli.PreviewHook(runner, "pre-worktree-remove", worktreePath, repoID)
	output.Info("[dry-run] Would remove worktree at %s", worktreePath)
	previewBranchDeletion(branch, g.LocalBranchExists(hubPath, branch), deleteRemote)
	output.Info("[dry-run] Would remove '%s' from hop.json, hopspace and state", branch)
	cli.PreviewHook(runner, "post-worktree-remove", worktreePath, repoID)
	output.Info("[dry-run] Would point 'current' at '%s'", hub.Config.Repo.DefaultBranch)
}

// previewRemoveMerged is runRemoveMerged's preview: the same candidate
// list and per-candidate gate, with nothing removed. It fails when the
// real run would, i.e. when any candidate would be refused.
func previewRemoveMerged(fs afero.Fs, g git.GitInterface, cwd string, force, noVerify, noPrompt, deleteRemote bool) {
	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		output.Fatal("Not in a hub: %v", err)
	}
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	toRemove, preSkipped := collectMergedCandidates(fs, g, hub, hubPath, cwd)
	for _, sk := range preSkipped {
		output.Info("Skipping %s: %s", sk.Branch, sk.Reason)
	}
	if len(toRemove) == 0 {
		output.Info("No merged worktrees to remove.")
		return
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
			refused++
			continue
		}
		previewBranchRemoval(fs, g, hub, hubPath, c.Branch, deleteRemote)
	}

	if refused > 0 {
		output.Fatal("[dry-run] would fail: %d merged worktree(s) could not be removed", refused)
	}
}

// previewRemoveHub reports what removing the hub at hubPath would delete.
func previewRemoveHub(fs afero.Fs, hubPath string, noPrompt bool) {
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
	for _, name := range branches {
		worktreePath := config.ResolveWorktreePath(hub.Config.Branches[name].Path, hubPath)
		output.Info("[dry-run] Would remove worktree for branch '%s' at %s", name, worktreePath)
	}

	output.Info("[dry-run] Would remove hub directory %s", hubPath)
	output.Info("[dry-run] Would remove 'github.com/%s/%s' from state", hub.Config.Repo.Org, hub.Config.Repo.Repo)
	hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(), hub.Config.Repo.Org, hub.Config.Repo.Repo)
	if exists, _ := afero.DirExists(fs, hopspacePath); exists {
		output.Info("[dry-run] Would remove hopspace data at %s", hopspacePath)
	}
}
