package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// checkBranchWorktrees reports hub branches whose worktree directory is
// gone and, under --fix, repairs each one. The repair depends on the
// branch:
//
//   - merged into the default branch: the worktree is not recreated. Its
//     work is already on the default branch and its directory is already
//     gone, so the repair is cleanup. The state check that runs next
//     drops the branch's state entry and hop.json row, as it always has
//     for merged branches; recreating the worktree here would only have
//     that check undo it.
//   - otherwise: the worktree is recreated, bringing the branch's
//     unmerged work back on disk.
//
// Either way git's registration of the vanished directory is cleared
// first (clearStaleRegistration). `git worktree add` refuses a path git
// still registers, and a registration left behind by cleanup would block
// a later `git hop add` of the same branch.
//
// A worktree git has locked is none of these: it is reported as a
// warning and left alone in every mode (warnLockedWorktree).
//
// A path something other than a directory occupies (a file, a dangling
// symlink) is not the worktree either: it is reported as such and goes
// through the same repair, which recreateBlocker then refuses, since git
// cannot check a worktree out over it. hop.WorktreeAt is the test, the one
// the state check and prune use too.
//
// The hub check runs before the state check on purpose: a worktree it
// recreates is back on disk when the state check looks, so the state
// check only sees what the hub check left for cleanup.
//
// Returns the missing worktrees it could not recreate (or, under
// --dry-run, would not be able to). The state repair after it keeps
// their hop.json rows: the row is the only record left of an unmerged
// branch's worktree, and a repair that failed is no reason to drop it.
func checkBranchWorktrees(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hopspacePath string, opts doctorOpts, r *doctorReport) keptWorktrees {
	kept := keptWorktrees{}
	var registry *string // git's worktree list, read once a worktree is missing
	for _, name := range sortedBranchNames(hub) {
		b := hub.Config.Branches[name]
		linkPath := config.ResolveWorktreePath(b.Path, hub.Path)
		presence := hop.WorktreeAt(fs, linkPath)
		if presence == hop.WorktreePresent {
			continue
		}

		if registry == nil {
			list, _ := g.WorktreeListPorcelain(hub.Path)
			registry = &list
		}
		if reason, locked := worktreeLock(*registry, linkPath); locked {
			warnLockedWorktree(r, doctorCheckHub, name, linkPath, reason)
			continue
		}

		if presence == hop.WorktreeOccupied {
			output.Error("Worktree path for branch %s is occupied by a non-directory: %s", name, linkPath)
			// --fix cannot check a worktree out over what is there
			// (recreateBlocker); it can only drop a merged branch's row.
			if _, merged := mergedMissingBranch(g, hub, b.HopspaceBranch); merged {
				r.fixableIssue(doctorCheckHub, name, "worktree path occupied by a non-directory: %s", linkPath)
			} else {
				r.unfixableIssue(doctorCheckHub, name, "worktree path occupied by a non-directory: %s", linkPath)
			}
		} else {
			output.Error("Broken link for branch %s: %s", name, linkPath)
			r.fixableIssue(doctorCheckHub, name, "worktree directory missing: %s", linkPath)
		}
		if !opts.fix {
			continue
		}

		if base, merged := mergedMissingBranch(g, hub, b.HopspaceBranch); merged {
			output.Info("Branch %s is merged into %s; not recreating its worktree", name, base)
			clearStaleRegistration(fs, g, hopspacePath, linkPath, opts, r)
			continue
		}
		if !recreateWorktree(fs, g, *registry, name, b.HopspaceBranch, linkPath, hopspacePath, opts, r) {
			kept.add(linkPath)
		}
	}
	return kept
}

// warnLockedWorktree reports a missing worktree git has locked: a
// warning, not an issue, since git keeps the worktree on purpose and its
// directory may only be unavailable. Nothing is repaired or previewed
// for it; the hint says how to hand it back to doctor.
func warnLockedWorktree(r *doctorReport, check, subject, path, reason string) {
	msg := lockedMissingMessage(path, reason)
	output.Warn("%s", msg)
	output.Hint("%s", unlockHint(path))
	r.record(doctorKindWarning, check, subject, "%s; %s", msg, unlockHint(path))
}

// mergedMissingBranch reports whether branch is merged into the hub's
// default branch, and names that branch. It is the state check's test
// (mergedIntoDefault, run in the hub), so both checks agree on which
// missing worktrees are cleanup.
func mergedMissingBranch(g git.GitInterface, hub *hop.Hub, branch string) (string, bool) {
	base := hub.Config.Repo.DefaultBranch
	return base, mergedIntoDefault(g, hub.Path, branch, base)
}

// mergedIntoDefault reports whether branch's work is on defaultBranch,
// run in the repository at dir. It is how both the hub check and the
// state check decide that a missing worktree is cleanup rather than work
// to keep. The branch counts as merged when its tip is reachable from
// defaultBranch, or when its work landed there under rewritten commits
// (a squash- or rebase-merge; branchWorkLandedIn, the test remove and
// status share). The default branch never counts, since it is trivially
// merged into itself. An unknown default branch, a branch ref that no
// longer exists, or a merge test that fails answers false: keeping or
// recreating is the repair that loses nothing.
func mergedIntoDefault(g git.GitInterface, dir, branch, defaultBranch string) bool {
	if defaultBranch == "" || branch == defaultBranch {
		return false
	}
	return isBranchMerged(g, dir, branch, defaultBranch) ||
		branchWorkLandedIn(g, dir, branch, defaultBranch)
}

// recreateWorktree checks out branch again at linkPath, the directory its
// hop.json row points at. registry is git's worktree list as the hub
// check read it. Returns whether the worktree was recreated (under
// --dry-run: would be); every failure is recorded.
func recreateWorktree(fs afero.Fs, g git.GitInterface, registry, name, branch, linkPath, hopspacePath string, opts doctorOpts, r *doctorReport) bool {
	// Feasibility is checked before branching on dry-run so a preview
	// reports the same "cannot fix" verdicts a real run would hit, rather
	// than promising a repair that would fail.
	hopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		output.Error("Cannot fix: failed to load hopspace: %v", err)
		r.failed(doctorCheckHub, name, "cannot recreate worktree: failed to load hopspace: %v", err)
		return false
	}
	if _, ok := hopspace.Config.Branches[branch]; !ok {
		output.Error("Cannot fix: branch %s not found in hopspace", branch)
		r.failed(doctorCheckHub, name, "cannot recreate worktree: branch %s not found in hopspace", branch)
		return false
	}
	if blocker := recreateBlocker(fs, g, registry, hopspacePath, branch, linkPath); blocker != "" {
		output.Error("Cannot fix: %s", blocker)
		r.failed(doctorCheckHub, name, "cannot recreate worktree: %s", blocker)
		return false
	}
	if !clearStaleRegistration(fs, g, hopspacePath, linkPath, opts, r) {
		return false
	}

	if !opts.mutating() {
		output.Info("[dry-run] Would recreate worktree for branch %s at %s", name, linkPath)
		r.repaired(opts, doctorCheckHub, name, "recreate worktree at %s", linkPath)
		previewDir(fs, linkPath)
		return true
	}

	output.Info("Attempting to fix broken worktree for branch %s...", name)
	if err := fs.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		output.Error("Failed to create parent directory: %v", err)
		r.failed(doctorCheckHub, name, "recreate worktree: create parent directory: %v", err)
		return false
	}
	if err := g.CreateWorktree(hopspacePath, branch, linkPath, "", false, "origin/"+branch); err != nil {
		output.Error("Failed to recreate worktree: %v", err)
		r.failed(doctorCheckHub, name, "recreate worktree: %v", err)
		return false
	}
	if err := hopspace.RegisterBranch(branch, linkPath); err != nil {
		output.Error("Failed to update hopspace: %v", err)
		// Continue anyway as the worktree was created.
	}
	if _, err := fs.Stat(linkPath); err != nil {
		output.Error("Worktree creation appeared to succeed but path still not accessible")
		r.failed(doctorCheckHub, name, "recreate worktree: path still not accessible")
		return false
	}
	output.Info("Fixed worktree for branch %s", name)
	r.repaired(opts, doctorCheckHub, name, "recreate worktree at %s", linkPath)
	return true
}

// clearStaleRegistration drops git's record of the worktree at path when
// git marks that record prunable, i.e. its directory is gone. A hand-run
// `rm -rf` of a worktree leaves exactly that, and `git worktree add`
// refuses such a path ("missing but already registered").
//
// The mechanism is `git worktree remove <path>` without --force, rather
// than the other ways out git's refusal names:
//   - `git worktree prune` takes no path; it would drop every other stale
//     registration in the repository too, which is more than this repair.
//   - `git worktree add -f` also overrides add's other safeguard, the
//     refusal to check out a branch already checked out elsewhere.
//
// remove acts on the named worktree only, deletes nothing but git's
// administrative files when the directory is missing, and without
// --force honours `git worktree lock` (git never marks a locked worktree
// prunable in the first place).
//
// Nothing is removed for a directory that exists: git marks a record
// prunable only when the directory is missing, and path is checked again
// here just before the call. A record that is not prunable (present,
// locked, or not registered at all) is left alone, as is a registry that
// cannot be read; the add that follows then reports whatever git refuses.
//
// Returns false when the registration could not be cleared; the failure
// is recorded.
func clearStaleRegistration(fs afero.Fs, g git.GitInterface, gitDir, path string, opts doctorOpts, r *doctorReport) bool {
	list, err := g.WorktreeListPorcelain(gitDir)
	if err != nil || !prunableWorktree(list, path) {
		return true
	}
	if _, err := fs.Stat(path); err == nil {
		return true
	}

	if !opts.mutating() {
		output.Info("[dry-run] Would clear stale worktree registration for %s", path)
		r.repaired(opts, doctorCheckHub, path, "clear stale worktree registration")
		return true
	}
	if err := g.WorktreeRemove(gitDir, path, false); err != nil {
		output.Error("Failed to clear stale worktree registration for %s: %v", path, err)
		r.failed(doctorCheckHub, path, "clear stale worktree registration: %v", err)
		return false
	}
	output.Info("Cleared stale worktree registration for %s", path)
	r.repaired(opts, doctorCheckHub, path, "clear stale worktree registration")
	return true
}

// prunableWorktree reports whether porcelain, the output of `git worktree
// list --porcelain`, marks the worktree at path prunable.
func prunableWorktree(porcelain, path string) bool {
	_, ok := worktreeAttr(porcelain, path, "prunable")
	return ok
}

// resolvedPath resolves symlinks in the longest prefix of p that exists.
// git records worktree paths resolved (on macOS the temp dir under /var
// is really under /private/var) while hop.json may not, and a missing
// worktree's own directory cannot be resolved, so filepath.EvalSymlinks
// on the whole path would fail.
func resolvedPath(p string) string {
	p = filepath.Clean(p)
	var rest []string
	for dir := p; ; {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
		dir = parent
	}
}

// previewDir marks path present on the scratch layer runDoctor gives a
// --dry-run, so the checks after this one see the directory a real run
// would have created. It writes only to that layer; on any other
// filesystem it does nothing, so a preview never creates a directory.
func previewDir(fs afero.Fs, path string) {
	if layer, ok := fs.(*afero.CopyOnWriteFs); ok {
		_ = layer.MkdirAll(path, 0o755)
	}
}
