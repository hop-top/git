package cmd

import (
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
		r.issuesFound = true
		return hubPath
	}

	hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(),
		hub.Config.Repo.Org, hub.Config.Repo.Repo)
	output.Info("Expected hopspace: %s", hopspacePath)

	if exists, _ := afero.Exists(fs, filepath.Join(hopspacePath, "hop.json")); !exists {
		r.issuesFound = true
		createMissingHopspace(fs, hub, hubPath, hopspacePath, opts, r)
	} else {
		output.Info("✓ Hopspace exists")
		reconcileHopspaceBranches(fs, hub, hubPath, hopspacePath, opts, r)
	}

	checkBranchWorktrees(fs, g, hub, hopspacePath, opts, r)
	return hubPath
}

// createMissingHopspace initializes an absent hopspace and registers the
// hub's branches into it.
func createMissingHopspace(fs afero.Fs, hub *hop.Hub, hubPath, hopspacePath string, opts doctorOpts, r *doctorReport) {
	if !opts.fix {
		output.Error("Hopspace does not exist at %s", hopspacePath)
		return
	}
	if !opts.mutating() {
		output.Info("[dry-run] Would create hopspace at %s (registering %d branch(es))",
			hopspacePath, len(hub.Config.Branches))
		r.fixed++
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
		return
	}

	for branchName, branch := range hub.Config.Branches {
		branchWorktreePath := config.ResolveWorktreePath(branch.Path, hubPath)
		if err := hopspace.RegisterBranch(branchName, branchWorktreePath); err != nil {
			output.Error("Failed to register branch %s: %v", branchName, err)
		}
	}
	output.Info("✓ Created hopspace")
	r.fixed++
}

// reconcileHopspaceBranches registers hub branches missing from the
// hopspace.
func reconcileHopspaceBranches(fs afero.Fs, hub *hop.Hub, hubPath, hopspacePath string, opts doctorOpts, r *doctorReport) {
	hopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		output.Error("Failed to load hopspace: %v", err)
		r.issuesFound = true
		return
	}

	for branchName := range hub.Config.Branches {
		if _, ok := hopspace.Config.Branches[branchName]; ok {
			continue
		}
		r.issuesFound = true

		if !opts.fix {
			output.Error("Branch %s in hub but not in hopspace", branchName)
			continue
		}
		if !opts.mutating() {
			output.Info("[dry-run] Would register branch %s in hopspace", branchName)
			r.fixed++
			continue
		}

		branchWorktreePath := config.ResolveWorktreePath(hub.Config.Branches[branchName].Path, hubPath)
		if err := hopspace.RegisterBranch(branchName, branchWorktreePath); err != nil {
			output.Error("Failed to register branch %s: %v", branchName, err)
		} else {
			output.Info("✓ Registered branch %s in hopspace", branchName)
			r.fixed++
		}
	}
}

// checkBranchWorktrees reports branches whose worktree directory is gone
// and, under --fix, recreates them.
func checkBranchWorktrees(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hopspacePath string, opts doctorOpts, r *doctorReport) {
	for name, b := range hub.Config.Branches {
		linkPath := config.ResolveWorktreePath(b.Path, hub.Path)
		if _, err := fs.Stat(linkPath); err == nil {
			continue
		}

		output.Error("Broken link for branch %s: %s", name, linkPath)
		r.issuesFound = true
		if !opts.fix {
			continue
		}

		// Feasibility is checked before branching on dry-run so a preview
		// reports the same "cannot fix" verdicts a real run would hit,
		// rather than promising a repair that would fail.
		hopspace, err := hop.LoadHopspace(fs, hopspacePath)
		if err != nil {
			output.Error("Cannot fix: failed to load hopspace: %v", err)
			continue
		}
		if _, ok := hopspace.Config.Branches[b.HopspaceBranch]; !ok {
			output.Error("Cannot fix: branch %s not found in hopspace", b.HopspaceBranch)
			continue
		}

		if !opts.mutating() {
			output.Info("[dry-run] Would recreate worktree for branch %s at %s", name, linkPath)
			r.fixed++
			continue
		}

		output.Info("Attempting to fix broken worktree for branch %s...", name)
		if err := fs.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
			output.Error("Failed to create parent directory: %v", err)
			continue
		}
		if err := g.CreateWorktree(hopspacePath, b.HopspaceBranch, linkPath, "", false, "origin/"+b.HopspaceBranch); err != nil {
			output.Error("Failed to recreate worktree: %v", err)
			continue
		}
		if err := hopspace.RegisterBranch(b.HopspaceBranch, linkPath); err != nil {
			output.Error("Failed to update hopspace: %v", err)
			// Continue anyway as the worktree was created.
		}
		if _, err := fs.Stat(linkPath); err == nil {
			output.Info("✓ Fixed worktree for branch %s", name)
			r.fixed++
		} else {
			output.Error("Worktree creation appeared to succeed but path still not accessible")
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

	hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(),
		hub.Config.Repo.Org, hub.Config.Repo.Repo)

	globalLoader := config.NewGlobalLoader()
	globalConfig, err := globalLoader.Load()
	if err != nil {
		globalConfig = globalLoader.GetDefaults()
	}

	depsManager, err := services.NewDepsManager(fs, hopspacePath, globalConfig)
	if err != nil {
		output.Error("Failed to initialize dependency manager: %v", err)
		r.issuesFound = true
		return
	}

	worktrees := make(map[string]string, len(hub.Config.Branches))
	for branchName, branch := range hub.Config.Branches {
		worktrees[branchName] = config.ResolveWorktreePath(branch.Path, hubPath)
	}

	issues, err := depsManager.Audit(worktrees)
	if err != nil {
		output.Error("Failed to audit dependencies: %v", err)
		r.issuesFound = true
		return
	}

	if len(issues) == 0 {
		output.Info("✓ All dependencies are properly configured")
		reportOrphanedDeps(fs, depsManager, hopspacePath, "  ")
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
		switch issue.Type {
		case services.IssueLocalFolder:
			sizeMB := float64(issue.Size) / 1024 / 1024
			output.Error("  ⚠ %s: local %s (%.1fMB) instead of symlink", issue.Branch, issue.PM.DepsDir, sizeMB)
			totalReclaimableSize += issue.Size
		case services.IssueBrokenSymlink:
			output.Error("  ✗ %s: broken symlink %s → %s (missing)", issue.Branch, issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget))
		case services.IssueStaleSymlink:
			output.Warn("  %s: stale symlink %s → %s (lockfile changed to %s); refreshed by the next install", issue.Branch, issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget), issue.ExpectedHash[:6])
		case services.IssueMissingDeps:
			output.Error("  ✗ %s: missing %s", issue.Branch, issue.PM.DepsDir)
		}
	}

	if totalReclaimableSize > 0 {
		output.Info("\nPotential space savings: %.1fMB", float64(totalReclaimableSize)/1024/1024)
	}

	if opts.fix {
		fixDependencies(depsManager, issues, totalReclaimableSize, opts, r)
	}

	reportOrphanedDeps(fs, depsManager, hopspacePath, "\n  ")
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
		r.fixed += len(issues)
		return
	}

	output.Info("\nFixing dependency issues...")
	if err := depsManager.Fix(issues, false); err != nil {
		output.Error("Failed to fix some issues: %v", err)
		return
	}
	output.Info("✓ Fixed %d dependency issue(s)", len(issues))
	r.fixed += len(issues)
	if reclaimable > 0 {
		output.Info("✓ Reclaimed %.1fMB", float64(reclaimable)/1024/1024)
	}
}

// reportOrphanedDeps prints the orphaned-deps hint. Read-only: reclaiming
// them is 'git hop env gc', never doctor.
func reportOrphanedDeps(fs afero.Fs, depsManager *services.DepsManager, hopspacePath, indent string) {
	orphaned := depsManager.Registry.GetOrphaned()
	if len(orphaned) == 0 {
		return
	}
	var orphanedSize int64
	for _, depsKey := range orphaned {
		orphanedSize += getDirSize(fs, filepath.Join(hopspacePath, "deps", depsKey))
	}
	output.Info("%s⚠ %d orphaned dependencies (%.1fMB)", indent, len(orphaned), float64(orphanedSize)/1024/1024)
	output.Info("    Run 'git hop env gc' to reclaim space")
}
