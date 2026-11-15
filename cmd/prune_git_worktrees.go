package cmd

import (
	"github.com/spf13/afero"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// pruneGitWorktrees clears git's record of worktrees whose directory is
// gone and that hop.json never listed: those made with plain `git
// worktree add` and deleted from disk. `git worktree prune` is what drops
// them; this pass names each first, so the run says what it clears, and
// returns the records. A worktree git has locked is kept and reported
// skipped, as `git worktree prune` keeps it. The branch such a worktree
// held is not touched: with its directory gone there is no way to tell
// whether it holds unmerged work.
//
// It runs before the hop.json pass: the rows that pass drops are the ones
// hop.json lists, which this pass leaves out.
func pruneGitWorktrees(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool) []pruneRecord {
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}

	var pruned []pruneRecord
	for _, h := range hubPathsFromState(st) {
		hub, err := hop.LoadHub(fs, h.path)
		if err != nil {
			continue
		}
		list, err := g.WorktreeListPorcelain(h.path)
		if err != nil {
			continue
		}
		for _, wt := range unregisteredWorktrees(fs, g, hub, h.path) {
			if !wt.Missing {
				continue
			}
			if reason, locked := worktreeLock(list, wt.Path); locked {
				pruned = append(pruned, skipLockedEntry(pruneKindGitWorktree, "git worktree", h.repoID, wt.Branch, wt.Path, reason))
				continue
			}
			output.Info("%s git worktree: %s:%s (%s)", prefix, h.repoID, wt.Branch, wt.Path)
			pruned = append(pruned, newPruneRecord(pruneKindGitWorktree, h.repoID, wt.Branch, wt.Path, dryRun))
		}
		if dryRun || !gitHasPrunable(fs, list, h.path) {
			continue
		}
		// git drops the record of every worktree whose directory is gone,
		// those hop.json lists included: the hop.json pass that follows
		// removes their rows, and git's record of them goes with them.
		if err := hop.NewCleanupManager(fs, g).PruneWorktrees(h.path); err != nil {
			output.Warn("Failed to prune git worktrees in %s: %v", h.path, err)
		}
	}
	return pruned
}

// gitHasPrunable reports whether git's registry lists a worktree whose
// directory is gone and that is not locked: one `git worktree prune`
// drops.
func gitHasPrunable(fs afero.Fs, porcelain, hubPath string) bool {
	for _, wt := range parseWorktreeListPorcelain(porcelain) {
		if wt.Bare || state.SamePath(wt.Path, hubPath) || hop.WorktreeDirPresent(fs, wt.Path) {
			continue
		}
		if _, locked := worktreeLock(porcelain, wt.Path); !locked {
			return true
		}
	}
	return false
}
