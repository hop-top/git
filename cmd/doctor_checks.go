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
// Returns the hub path ("" when not in a hub) for the later checks.
func checkHub(fs afero.Fs, g git.GitInterface, cwd string, opts doctorOpts, r *doctorReport) string {
	output.Info("\n=== Checking Hub ===")
	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		output.Info("Not in a hub. Skipping hub-specific checks.")
		return ""
	}

	output.Info("Hub found at: %s", hubPath)
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Error("Failed to load hub config: %v", err)
		r.issue(doctorCheckHub, hubPath, "failed to load hub config: %v", err)
		return hubPath
	}

	hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
	output.Info("Hopspace: %s", hopspacePath)
	warnStaleHopspaceCopy(fs, hub, r)

	// Only a hub marked global can lack its hopspace: an unmarked hub's
	// hopspace is its own hop.json, loaded above.
	if exists, _ := afero.Exists(fs, filepath.Join(hopspacePath, "hop.json")); !exists {
		r.issue(doctorCheckHub, hopspacePath, "hopspace does not exist")
		createMissingHopspace(fs, hub, hubPath, hopspacePath, opts, r)
	} else {
		reconcileHopspaceBranches(fs, hub, hubPath, hopspacePath, opts, r)
	}

	checkBranchWorktrees(fs, g, hub, hopspacePath, opts, r)
	return hubPath
}

// warnStaleHopspaceCopy reports a data-home hop.json left beside an
// unmarked hub. It is never read, so it is a warning, not an issue, and
// --fix leaves it alone; the record's check lets a cleanup find it.
func warnStaleHopspaceCopy(fs afero.Fs, hub *hop.Hub, r *doctorReport) {
	stale := hop.StaleHopspaceCopy(fs, hub.Config.Repo)
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
		r.issue(doctorCheckHub, hopspacePath, "failed to load hopspace: %v", err)
		return
	}

	for _, branchName := range sortedBranchNames(hub) {
		if _, ok := hopspace.Config.Branches[branchName]; ok {
			continue
		}
		r.issue(doctorCheckHub, branchName, "branch in hub but not in hopspace")

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
	globalConfig, err := globalLoader.Load()
	if err != nil {
		globalConfig = globalLoader.GetDefaults()
	}

	depsManager, err := services.NewDepsManager(fs, hopspacePath, globalConfig)
	if err != nil {
		output.Error("Failed to initialize dependency manager: %v", err)
		r.issue(doctorCheckDependencies, hopspacePath, "failed to initialize dependency manager: %v", err)
		return
	}

	worktrees := hub.WorktreePaths()

	issues, err := depsManager.Audit(worktrees)
	if err != nil {
		output.Error("Failed to audit dependencies: %v", err)
		r.issue(doctorCheckDependencies, hopspacePath, "failed to audit dependencies: %v", err)
		return
	}

	if len(issues) == 0 {
		output.Info("All dependencies are properly configured")
		reportOrphanedDeps(fs, depsManager, hopspacePath, "  ", r)
		return
	}

	// Only error-severity issues make the installation unhealthy. Stale
	// symlinks are warnings: the deps still work and the next install
	// refreshes them, so they must not on their own drive the "issues
	// found" verdict — while staying visible in the report.
	if hasErrorSeverity(issues) {
		r.issuesFound = true
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
			output.Error("  %s: broken symlink %s -> %s (missing)", issue.Branch, issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget))
			msg = fmt.Sprintf("broken symlink %s -> %s (missing)", issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget))
		case services.IssueStaleSymlink:
			output.Warn("  %s: stale symlink %s -> %s (lockfile changed to %s); refreshed by the next install", issue.Branch, issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget), issue.ExpectedHash[:6])
			msg = fmt.Sprintf("stale symlink %s -> %s (lockfile changed to %s); refreshed by the next install", issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget), issue.ExpectedHash[:6])
		case services.IssueMissingDeps:
			output.Error("  %s: missing %s", issue.Branch, issue.PM.DepsDir)
			msg = fmt.Sprintf("missing %s", issue.PM.DepsDir)
		}
		kind := doctorKindIssue
		if issue.Type.Severity() != services.SeverityError {
			kind = doctorKindWarning
		}
		r.record(kind, doctorCheckDependencies, issue.Branch, "%s", msg)
	}

	if totalReclaimableSize > 0 {
		output.Info("\nPotential space savings: %.1fMB", float64(totalReclaimableSize)/1024/1024)
	}

	if opts.fix {
		fixDependencies(depsManager, issues, totalReclaimableSize, opts, r)
	}

	reportOrphanedDeps(fs, depsManager, hopspacePath, "\n  ", r)
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
		orphanedSize += getDirSize(fs, filepath.Join(hopspacePath, "deps", depsKey))
	}
	output.Info("%s%d orphaned dependencies (%.1fMB)", indent, len(orphaned), float64(orphanedSize)/1024/1024)
	output.Info("    Run 'git hop env gc' to reclaim space")
	r.record(doctorKindWarning, doctorCheckDependencies, filepath.Join(hopspacePath, "deps"),
		"%d orphaned dependencies (%.1fMB); run 'git hop env gc' to reclaim space", len(orphaned), float64(orphanedSize)/1024/1024)
}
