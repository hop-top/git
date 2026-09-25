package cmd

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// checkWorktreeState detects directories under the hopspace's hops/ that
// hold no recorded worktree. --fix removes only the empty ones; the rest
// hold work doctor cannot judge and are reported for the user to handle:
// a worktree git has registered (recorded by 'git hop repair') and any
// other directory with something in it.
func checkWorktreeState(fs afero.Fs, g git.GitInterface, hubPath string, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking Worktree State ===")
	if hubPath == "" {
		output.Info("Not in a hub. Skipping worktree state checks.")
		return
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return
	}

	hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
	if r.misplaced[filepath.Clean(hopspacePath)] {
		output.Info("Hopspace not at %s. Skipping worktree state checks.", hopspacePath)
		return
	}

	hopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		output.Error("Failed to load hopspace: %v", err)
		r.unfixableIssue(doctorCheckWorktrees, hopspacePath, "failed to load hopspace: %v", err)
		return
	}

	validator := hop.NewStateValidator(fs, g)
	orphanedDirs, err := validator.DetectOrphanedDirectories(hopspace)
	if err != nil {
		output.Error("Failed to detect orphaned directories: %v", err)
		r.unfixableIssue(doctorCheckWorktrees, hopspacePath, "failed to detect orphaned directories: %v", err)
		return
	}
	if len(orphanedDirs) == 0 {
		output.Info("No orphaned directories found")
		return
	}

	output.Error("Found %d orphaned directories", len(orphanedDirs))
	removable := 0
	for _, dir := range validator.ClassifyOrphanedDirectories(hopspace, hubPath, orphanedDirs) {
		switch dir.Kind {
		case hop.OrphanWorktree:
			reportOrphanedWorktree(g, dir, r)
		case hop.OrphanNotEmpty:
			output.Error("  - %s: not empty; doctor removes only empty directories", dir.Rel)
			output.Hint("move out what you want to keep from %s, then delete it", dir.Path)
			r.unfixableIssue(doctorCheckWorktrees, dir.Path, "orphaned directory is not empty; doctor removes only empty ones: remove it by hand")
		default:
			removable++
			output.Error("  - %s", dir.Rel)
			r.fixableIssue(doctorCheckWorktrees, dir.Path, "orphaned directory")
			fixEmptyOrphan(fs, g, dir.Path, opts, r)
		}
	}
	if !opts.fix && removable > 0 {
		output.Info("  Run 'git hop doctor --fix' to remove the empty orphaned directories")
	}
}

// reportOrphanedWorktree reports an orphaned directory that is, or holds,
// a worktree git has registered: doctor never removes it, whatever is in
// it. 'git hop repair' records such a worktree in hop.json; one with
// uncommitted changes only with --force-dirty, so that is the command
// named then.
func reportOrphanedWorktree(g git.GitInterface, dir hop.OrphanDir, r *doctorReport) {
	wts := strings.Join(dir.Worktrees, ", ")
	if len(dir.Worktrees) == 1 && dir.Worktrees[0] == dir.Path {
		output.Error("  - %s: a worktree git has registered but hop.json does not record", dir.Rel)
	} else {
		output.Error("  - %s: holds a worktree git has registered but hop.json does not record: %s", dir.Rel, wts)
	}
	dirty := false
	for _, wt := range dir.Worktrees {
		dirty = dirty || repairSeesDirty(g, wt)
	}
	if dirty {
		output.Hint("doctor never removes a registered worktree; it has uncommitted changes, so\n" +
			"record it with 'git hop repair --force-dirty', or remove it with\n" +
			"'git worktree remove' once its work is safe")
		r.unfixableIssue(doctorCheckWorktrees, dir.Path,
			"orphaned directory holds a worktree git has registered (%s) with uncommitted changes; record it with 'git hop repair --force-dirty' or remove it with 'git worktree remove'", wts)
		return
	}
	output.Hint("doctor never removes a registered worktree; record it with 'git hop repair',\n" +
		"or remove it with 'git worktree remove' once its work is safe")
	r.unfixableIssue(doctorCheckWorktrees, dir.Path,
		"orphaned directory holds a worktree git has registered (%s); record it with 'git hop repair' or remove it with 'git worktree remove'", wts)
}

// fixEmptyOrphan removes the empty orphaned directory path under --fix,
// or says it would under --fix --dry-run.
func fixEmptyOrphan(fs afero.Fs, g git.GitInterface, path string, opts doctorOpts, r *doctorReport) {
	if !opts.fix {
		return
	}
	if !opts.mutating() {
		output.Info("    [dry-run] Would remove empty directory %s", path)
		r.repaired(opts, doctorCheckWorktrees, path, "remove empty orphaned directory")
		return
	}
	if err := hop.NewCleanupManager(fs, g).RemoveEmptyDirectory(path); err != nil {
		output.Error("    Failed to remove: %v", err)
		r.failed(doctorCheckWorktrees, path, "remove empty orphaned directory: %v", err)
		return
	}
	output.Info("    Removed empty directory %s", path)
	r.repaired(opts, doctorCheckWorktrees, path, "remove empty orphaned directory")
}
