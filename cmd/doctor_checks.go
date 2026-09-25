package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// checkHub validates the hub, its hopspace, and each branch worktree.
// Returns the hub path ("" when not in a hub) for the later checks, and
// the missing worktrees the hub check could not recreate, which the
// state repair must keep (see checkBranchWorktrees).
func checkHub(fs afero.Fs, g git.GitInterface, cwd string, opts doctorOpts, r *doctorReport) (string, keptWorktrees) {
	output.Info("\n=== Checking Hub ===")
	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		output.Info("Not in a hub. Skipping hub-specific checks.")
		return "", nil
	}

	output.Info("Hub found at: %s", hubPath)
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Error("Failed to load hub config: %v", err)
		r.unfixableIssue(doctorCheckHub, hubPath, "failed to load hub config: %v", err)
		return hubPath, nil
	}

	hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
	output.Info("Hopspace: %s", hopspacePath)
	warnStaleHopspaceCopy(fs, hub, r)
	checkOriginFetchRefspec(g, hubPath, r)

	// Only a hub marked global can lack its hopspace: an unmarked hub's
	// hopspace is its own hop.json, loaded above.
	exists, _ := afero.Exists(fs, filepath.Join(hopspacePath, "hop.json"))
	switch {
	case !exists && r.misplaced[filepath.Clean(hopspacePath)]:
		// Reported by the hop.dataLayout check, with what to do; a new
		// hopspace here would strand the real one.
		output.Info("Hopspace not at %s: see the hop.dataLayout warning above.", hopspacePath)
	case !exists:
		r.fixableIssue(doctorCheckHub, hopspacePath, "hopspace does not exist")
		createMissingHopspace(fs, hub, hubPath, hopspacePath, opts, r)
	default:
		reconcileHopspaceBranches(fs, hub, hubPath, hopspacePath, opts, r)
	}

	return hubPath, checkBranchWorktrees(fs, g, hub, hopspacePath, opts, r)
}

// checkOriginFetchRefspec reports a hub whose origin has no fetch
// refspec, which leaves refs/remotes/origin/* frozen on every fetch.
// 'git hop repair' restores it; --fix leaves that to repair, which
// fetches and verifies afterwards, so the issue stays until it runs.
func checkOriginFetchRefspec(g git.GitInterface, hubPath string, r *doctorReport) {
	if !hop.MissingOriginFetchRefspec(g, hubPath) {
		return
	}
	const msg = "origin has no remote.origin.fetch refspec; fetches leave refs/remotes/origin/* stale"
	output.Error("%s", msg)
	output.Hint("run 'git hop repair' to restore %s", hop.OriginFetchRefspec)
	r.unfixableIssue(doctorCheckHub, hubPath, "%s; run 'git hop repair' to restore it", msg)
}

// warnStaleHopspaceCopy reports a data-home hop.json left beside an
// unmarked hub. It is never read, so it is a warning, not an issue, and
// --fix leaves it alone; the record's check lets a cleanup find it.
func warnStaleHopspaceCopy(fs afero.Fs, hub *hop.Hub, r *doctorReport) {
	stale := hop.StaleHopspaceCopy(fs, hub.Path, hub.Config.Repo)
	if stale == "" {
		return
	}
	const msg = "stale hopspace copy at %s is ignored (hub is local); remove it if unused"
	output.Warn(msg, stale)
	r.record(doctorKindWarning, doctorCheckHopspace, stale, msg, stale)
}

// createMissingHopspace initializes the absent data-home hopspace of a hub
// marked global and registers the hub's branches into it.
func createMissingHopspace(fs afero.Fs, hub *hop.Hub, hubPath, hopspacePath string, opts doctorOpts, r *doctorReport) {
	if !opts.fix {
		output.Error("Hopspace does not exist at %s", hopspacePath)
		return
	}
	if !opts.mutating() {
		output.Info("[dry-run] Would create hopspace at %s (registering %d branch(es))",
			hopspacePath, len(hub.Config.Branches))
		r.repaired(opts, doctorCheckHub, hopspacePath, "create hopspace and register %d branch(es)", len(hub.Config.Branches))
		return
	}

	output.Info("Creating missing hopspace...")
	defaultBranch := hub.Config.Repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	hopspace, err := hop.InitHopspace(fs, hopspacePath, hub.Config.Repo.URI,
		hub.Config.Repo.Org, hub.Config.Repo.Repo, defaultBranch)
	if err != nil {
		output.Error("Failed to initialize hopspace: %v", err)
		r.failed(doctorCheckHub, hopspacePath, "create hopspace: %v", err)
		return
	}

	for _, branchName := range sortedBranchNames(hub) {
		branchWorktreePath := config.ResolveWorktreePath(hub.Config.Branches[branchName].Path, hubPath)
		if err := hopspace.RegisterBranch(branchName, branchWorktreePath); err != nil {
			output.Error("Failed to register branch %s: %v", branchName, err)
			r.failed(doctorCheckHub, branchName, "register branch in hopspace: %v", err)
		}
	}
	output.Info("Created hopspace")
	r.repaired(opts, doctorCheckHub, hopspacePath, "create hopspace and register %d branch(es)", len(hub.Config.Branches))
}

// reconcileHopspaceBranches registers hub branches missing from the
// hopspace.
func reconcileHopspaceBranches(fs afero.Fs, hub *hop.Hub, hubPath, hopspacePath string, opts doctorOpts, r *doctorReport) {
	hopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		output.Error("Failed to load hopspace: %v", err)
		r.unfixableIssue(doctorCheckHub, hopspacePath, "failed to load hopspace: %v", err)
		return
	}

	for _, branchName := range sortedBranchNames(hub) {
		if _, ok := hopspace.Config.Branches[branchName]; ok {
			continue
		}
		r.fixableIssue(doctorCheckHub, branchName, "branch in hub but not in hopspace")

		if !opts.fix {
			output.Error("Branch %s in hub but not in hopspace", branchName)
			continue
		}
		if !opts.mutating() {
			output.Info("[dry-run] Would register branch %s in hopspace", branchName)
			r.repaired(opts, doctorCheckHub, branchName, "register branch in hopspace")
			continue
		}

		branchWorktreePath := config.ResolveWorktreePath(hub.Config.Branches[branchName].Path, hubPath)
		if err := hopspace.RegisterBranch(branchName, branchWorktreePath); err != nil {
			output.Error("Failed to register branch %s: %v", branchName, err)
			r.failed(doctorCheckHub, branchName, "register branch in hopspace: %v", err)
		} else {
			output.Info("Registered branch %s in hopspace", branchName)
			r.repaired(opts, doctorCheckHub, branchName, "register branch in hopspace")
		}
	}
}

// checkDependencies audits per-worktree dependency directories and, under
// --fix, repairs them.
func checkDependencies(fs afero.Fs, hubPath string, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking Dependencies ===")
	if hubPath == "" {
		output.Info("Not in a hub. Skipping dependency checks.")
		return
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return
	}

	hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)

	globalLoader := config.NewGlobalLoader()
	globalConfig := globalLoader.Load()

	depsManager, err := services.NewDepsManager(fs, hopspacePath, globalConfig)
	if err != nil {
		output.Error("Failed to initialize dependency manager: %v", err)
		r.unfixableIssue(doctorCheckDependencies, hopspacePath, "failed to initialize dependency manager: %v", err)
		return
	}

	issues, err := depsManager.Audit(auditableWorktrees(fs, hub))
	if err != nil {
		output.Error("Failed to audit dependencies: %v", err)
		r.unfixableIssue(doctorCheckDependencies, hopspacePath, "failed to audit dependencies: %v", err)
		return
	}

	if len(issues) == 0 {
		output.Info("All dependencies are properly configured")
		reportOrphanedDeps(fs, depsManager, hopspacePath, "  ", r)
		return
	}

	output.Info("\nDependency Issues:")

	var totalReclaimableSize int64
	for _, issue := range issues {
		var msg string
		switch issue.Type {
		case services.IssueLocalFolder:
			sizeMB := float64(issue.Size) / 1024 / 1024
			output.Error("  %s: local %s (%.1fMB) instead of symlink", issue.Branch, issue.PM.DepsDir, sizeMB)
			msg = fmt.Sprintf("local %s (%.1fMB) instead of symlink", issue.PM.DepsDir, sizeMB)
			totalReclaimableSize += issue.Size
		case services.IssueBrokenSymlink:
			output.Error("  %s: broken symlink %s -> %s (missing)", issue.Branch, issue.PM.DepsDir, issue.TargetName())
			msg = fmt.Sprintf("broken symlink %s -> %s (missing)", issue.PM.DepsDir, issue.TargetName())
		case services.IssueStaleSymlink:
			output.Warn("  %s: stale symlink %s -> %s (lockfile changed to %s); refreshed by the next install", issue.Branch, issue.PM.DepsDir, issue.TargetName(), issue.ExpectedHash[:6])
			msg = fmt.Sprintf("stale symlink %s -> %s (lockfile changed to %s); refreshed by the next install", issue.PM.DepsDir, issue.TargetName(), issue.ExpectedHash[:6])
		case services.IssueOldLayout:
			output.Warn("  %s: symlink %s -> %s is in the old store layout, which Node cannot resolve from; relinked by the next install", issue.Branch, issue.PM.DepsDir, issue.TargetName())
			msg = fmt.Sprintf("symlink %s -> %s is in the old store layout, which Node cannot resolve from; relinked by the next install", issue.PM.DepsDir, issue.TargetName())
		case services.IssueMissingDeps:
			output.Error("  %s: missing %s", issue.Branch, issue.PM.DepsDir)
			msg = fmt.Sprintf("missing %s", issue.PM.DepsDir)
		}
		// Only error-severity issues make the installation unhealthy, and
		// --fix repairs every one (fixDependencies). Stale and old-layout
		// symlinks are warnings: the next install refreshes them, so they
		// must not on their own drive the "issues found" verdict, while
		// staying visible in the report.
		if issue.Type.Severity() == services.SeverityError {
			r.fixableIssue(doctorCheckDependencies, issue.Branch, "%s", msg)
		} else {
			r.record(doctorKindWarning, doctorCheckDependencies, issue.Branch, "%s", msg)
		}
	}

	if totalReclaimableSize > 0 {
		output.Info("\nPotential space savings: %.1fMB", float64(totalReclaimableSize)/1024/1024)
	}

	if opts.fix {
		fixDependencies(depsManager, issues, totalReclaimableSize, opts, r)
	}

	reportOrphanedDeps(fs, depsManager, hopspacePath, "\n  ", r)
}

// auditableWorktrees returns the worktree paths of hub, by branch, whose
// directory is there (hop.WorktreeAt): the ones doctor's dependency audit
// and env gc scan. A missing one has no dependencies. Neither has a path
// something else occupies, and looking for package-manager files below a
// file fails with ENOTDIR, which would abort the scan of every other
// worktree; it is skipped with a note.
func auditableWorktrees(fs afero.Fs, hub *hop.Hub) map[string]string {
	paths := hub.WorktreePaths()
	for _, name := range sortedBranchNames(hub) {
		switch hop.WorktreeAt(fs, paths[name]) {
		case hop.WorktreePresent:
			continue
		case hop.WorktreeOccupied:
			output.Info("Skipping %s: worktree path is not a directory: %s", name, paths[name])
		}
		delete(paths, name)
	}
	return paths
}

// fixDependencies applies (or, under --dry-run, previews) the dependency
// repairs. depsManager.Fix deletes local dep folders and rewrites
// symlinks, so it must never be reached in a preview.
func fixDependencies(depsManager *services.DepsManager, issues []services.Issue, reclaimable int64, opts doctorOpts, r *doctorReport) {
	if !opts.mutating() {
		output.Info("\n[dry-run] Would fix %d dependency issue(s)", len(issues))
		if reclaimable > 0 {
			output.Info("[dry-run] Would reclaim %.1fMB", float64(reclaimable)/1024/1024)
		}
		recordDependencyFixes(issues, opts, r)
		return
	}

	output.Info("\nFixing dependency issues...")
	if err := depsManager.Fix(issues, false); err != nil {
		output.Error("Failed to fix some issues: %v", err)
		r.failed(doctorCheckDependencies, depsManager.RepoPath, "fix dependency issues: %v", err)
		return
	}
	output.Info("Fixed %d dependency issue(s)", len(issues))
	recordDependencyFixes(issues, opts, r)
	if reclaimable > 0 {
		output.Info("Reclaimed %.1fMB", float64(reclaimable)/1024/1024)
	}
}

// recordDependencyFixes records one repair per dependency issue fixed (or,
// under --dry-run, that would be).
func recordDependencyFixes(issues []services.Issue, opts doctorOpts, r *doctorReport) {
	for _, issue := range issues {
		r.repaired(opts, doctorCheckDependencies, issue.Branch, "repair %s", issue.PM.DepsDir)
	}
}

// reportOrphanedDeps prints the orphaned-deps hint. Read-only: reclaiming
// them is 'git hop env gc', never doctor.
func reportOrphanedDeps(fs afero.Fs, depsManager *services.DepsManager, hopspacePath, indent string, r *doctorReport) {
	orphaned := depsManager.Registry.GetOrphaned()
	if len(orphaned) == 0 {
		return
	}
	var orphanedSize int64
	for _, depsKey := range orphaned {
		orphanedSize += getDirSize(fs, filepath.Join(services.DepsStorePath(hopspacePath), depsKey))
	}
	output.Info("%s%d orphaned dependencies (%.1fMB)", indent, len(orphaned), float64(orphanedSize)/1024/1024)
	output.Info("    Run 'git hop env gc' to reclaim space")
	r.record(doctorKindWarning, doctorCheckDependencies, services.DepsStorePath(hopspacePath),
		"%d orphaned dependencies (%.1fMB); run 'git hop env gc' to reclaim space", len(orphaned), float64(orphanedSize)/1024/1024)
}
