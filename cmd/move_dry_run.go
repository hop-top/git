package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// movePlan is everything `git hop move` has decided before its first write.
type movePlan struct {
	hub                  *hop.Hub
	hubPath, repoID      string
	oldBranch, newBranch string
	oldPath, newPath     string
}

// previewMove reports what `git hop move` would do for p without doing any
// of it: no detector action, hook, branch rename, worktree move, hop.json,
// hopspace, state, ports, volumes or symlink write. A move the real run
// would reject fails here too.
func previewMove(fs afero.Fs, g git.GitInterface, p movePlan) {
	refuse := func(err error) {
		refuseDryRun(fmt.Sprintf("move '%s'", p.oldBranch), err)
	}

	if _, exists := p.hub.Config.Branches[p.newBranch]; exists {
		refuse(fmt.Errorf("branch '%s' already exists", p.newBranch))
	}
	if _, err := hop.LoadHopspace(fs, p.hubPath); err != nil {
		hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(), p.hub.Config.Repo.Org, p.hub.Config.Repo.Repo)
		if _, err := hop.LoadHopspace(fs, hopspacePath); err != nil {
			refuse(fmt.Errorf("failed to load hopspace: %v", err))
		}
	}
	if err := previewDetector(fs, g, p.oldBranch, p.hubPath, "start"); err != nil {
		refuse(fmt.Errorf("detector failed: %v", err))
	}

	runner := hooks.NewRunner(fs)
	cli.PreviewHook(runner, "pre-worktree-move", p.oldPath, p.repoID)

	if g.LocalBranchExists(p.hubPath, p.newBranch) {
		output.Info("[dry-run] Would keep existing local branch '%s'", p.newBranch)
	} else {
		output.Info("[dry-run] Would rename branch '%s' to '%s'", p.oldBranch, p.newBranch)
	}
	output.Info("[dry-run] Would move worktree %s -> %s", p.oldPath, p.newPath)
	output.Info("[dry-run] Would rekey '%s' to '%s' in hop.json, hopspace, state, ports and volumes", p.oldBranch, p.newBranch)

	if target, err := hop.GetCurrentSymlink(fs, p.hubPath); err == nil {
		absTarget, _ := filepath.Abs(filepath.Join(p.hubPath, target))
		absOld, _ := filepath.Abs(p.oldPath)
		if absTarget == absOld {
			output.Info("[dry-run] Would point 'current' at %s", p.newPath)
		}
	}

	// Repo-level hooks travel with the worktree, so the post hook is
	// resolved where they live now.
	cli.PreviewHook(runner, "post-worktree-move", p.oldPath, p.repoID)
}
