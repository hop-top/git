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

// previewMove reports what `git hop move` would do for p without doing any
// of it: no hook, branch rename, worktree move, hop.json, hopspace, state,
// ports, volumes or symlink write. A move the real run would reject fails
// here too, checked in the same order. It returns the result the move
// would produce.
func previewMove(fs afero.Fs, g git.GitInterface, p movePlan) moveResult {
	refuse := func(err error) {
		refuseDryRun(fmt.Sprintf("move '%s'", p.oldBranch), err)
	}

	if _, err := p.prepare(fs, g); err != nil {
		refuse(err)
	}
	if _, err := p.hookEnv(fs, g); err != nil {
		refuse(fmt.Errorf("detector failed: %v", err))
	}

	runner := hooks.NewRunner(fs).ForRepo(p.hub.Config.Repo.URI, p.hubPath)
	cli.PreviewHook(runner, "pre-worktree-move", p.oldPath, p.repoID)

	if g.LocalBranchExists(p.oldPath, p.newBranch) {
		output.Info("[dry-run] Would keep local branch '%s', already checked out in the worktree", p.newBranch)
	} else {
		output.Info("[dry-run] Would rename branch '%s' to '%s'", p.oldBranch, p.newBranch)
	}
	output.Info("[dry-run] Would move worktree %s -> %s", p.oldPath, p.newPath)
	output.Info("[dry-run] Would rekey '%s' to '%s' in hop.json, hopspace, state, ports and volumes", p.oldBranch, p.newBranch)

	res := moveResult{
		OldBranch: p.oldBranch,
		NewBranch: p.newBranch,
		OldPath:   p.oldPath,
		NewPath:   p.newPath,
		DryRun:    true,
	}
	if target, err := hop.GetCurrentSymlink(fs, p.hubPath); err == nil {
		absTarget, _ := filepath.Abs(filepath.Join(p.hubPath, target))
		absOld, _ := filepath.Abs(p.oldPath)
		if absTarget == absOld {
			output.Info("[dry-run] Would point 'current' at %s", p.newPath)
			res.CurrentUpdated = true
		}
	}

	// Repo-level hooks travel with the worktree, so the post hook is
	// resolved where they live now.
	cli.PreviewHook(runner, "post-worktree-move", p.oldPath, p.repoID)
	return res
}
