package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
)

var (
	doctorFix bool
)

// doctorOpts carries the flags that decide whether a check may mutate.
//
// fix and dryRun are deliberately separate rather than a tri-state: --fix
// alone repairs, --fix --dry-run previews the same repairs, and --dry-run
// alone is a no-op because a run without --fix never mutates anyway.
type doctorOpts struct {
	fix    bool
	dryRun bool
}

// mutating reports whether a repair may actually be applied. Every
// mutation site in doctor gates on this, never on opts.fix — the whole
// point of the flag is that --dry-run reports without applying.
func (o doctorOpts) mutating() bool { return o.fix && !o.dryRun }

// planning reports whether doctor is previewing repairs instead of
// applying them, i.e. whether output should be phrased as "would".
func (o doctorOpts) planning() bool { return o.fix && o.dryRun }

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Args:    cobra.NoArgs,
	Aliases: []string{"check"},
	Short:   "Check and repair the environment",
	Long: `Run diagnostics on git-hop installation and project setup.

Checks:
- Path configuration (data home, config home, cache home)
- Hub configuration and symlinks
- Hopspace existence and consistency
- Worktree state (orphaned directories)
- Orphaned worktrees in state

Use --fix to automatically repair issues. In the current hub, --fix also
drops hop.json branch entries whose worktree directory is gone (the rows
'git hop status' reports as Missing), backing hop.json up to the per-hub
repair state dir ($XDG_STATE_HOME/git-hop/repair/<hub>/backups/) first so
the change can be undone with 'git hop repair --undo'.

Combine --fix with --dry-run to preview every repair without applying any
of it: no directories created, no worktrees recreated, no dependencies
touched, no state or hop.json rewritten, and no backup snapshot taken.`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		r := runDoctor(fs, git.New(), cwd, doctorOpts{fix: doctorFix, dryRun: dryRun})
		if output.IsStructured() {
			emitResult(cmd, r.records)
		}
	},
}

func init() {
	cli.RootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Automatically fix issues")
	declareOutputSchema(doctorCmd, &[]doctorRecord{})
}

// Checks doctor runs, as reported in doctorRecord.Check.
const (
	doctorCheckPaths        = "paths"
	doctorCheckHub          = "hub"
	doctorCheckDependencies = "dependencies"
	doctorCheckWorktrees    = "worktrees"
	doctorCheckState        = "state"
)

// Kinds of doctor record; see doctorRecord.Kind.
const (
	doctorKindIssue    = "issue"
	doctorKindWarning  = "warning"
	doctorKindFixed    = "fixed"
	doctorKindWouldFix = "would-fix"
	doctorKindFailed   = "failed"
)

// doctorReport accumulates the verdict across every check.
//
// fixed counts repairs that genuinely landed; under --dry-run it counts
// repairs that would land. The summary distinguishes the two so a preview
// never claims to have fixed anything.
//
// records is the structured result: every problem found and every repair
// attempted, in the order the checks ran. The human report is printed as
// the checks go; records carry the same findings for --format/--json/
// --porcelain, where that report is suppressed.
type doctorReport struct {
	issuesFound bool
	fixed       int
	records     []doctorRecord
}

// record appends one record to the structured result.
func (r *doctorReport) record(kind, check, subject, format string, args ...any) {
	r.records = append(r.records, doctorRecord{
		Kind:    kind,
		Check:   check,
		Subject: subject,
		Message: fmt.Sprintf(format, args...),
	})
}

// issue records a problem that makes the installation unhealthy.
func (r *doctorReport) issue(check, subject, format string, args ...any) {
	r.issuesFound = true
	r.record(doctorKindIssue, check, subject, format, args...)
}

// repaired records and counts a repair that landed, or under --dry-run
// would land. format names the repair itself ("create data directory");
// the kind says whether it happened.
func (r *doctorReport) repaired(opts doctorOpts, check, subject, format string, args ...any) {
	kind := doctorKindFixed
	if opts.planning() {
		kind = doctorKindWouldFix
	}
	r.record(kind, check, subject, format, args...)
	r.fixed++
}

// failed records a repair --fix attempted and could not make.
func (r *doctorReport) failed(check, subject, format string, args ...any) {
	r.record(doctorKindFailed, check, subject, format, args...)
}

// runDoctor executes every diagnostic and, when opts.fix is set, the
// repairs. It is the wiring layer the tests drive: the cobra Run closure
// does nothing but resolve flags and call it, so a test exercising
// runDoctor exercises the same path a user gets — including whether each
// call site honours --dry-run.
func runDoctor(fs afero.Fs, g git.GitInterface, cwd string, opts doctorOpts) doctorReport {
	// Never nil, so a healthy run renders as [] rather than null.
	r := doctorReport{records: []doctorRecord{}}

	output.Info("Running git-hop diagnostics...")
	if opts.planning() {
		output.Info("[dry-run] Previewing repairs; no changes will be applied.")
	}

	checkPaths(fs, opts, &r)
	hubPath := checkHub(fs, g, cwd, opts, &r)
	checkDependencies(fs, hubPath, opts, &r)
	checkWorktreeState(fs, g, hubPath, opts, &r)
	checkState(fs, g, hubPath, opts, &r)

	summarizeDoctor(opts, r)
	return r
}

// checkPaths verifies the XDG-derived directories exist, creating them
// under --fix.
func checkPaths(fs afero.Fs, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking Paths ===")
	dataHome := hop.GetGitHopDataHome()
	configHome := hop.GetConfigHome()
	cacheHome := hop.GetCacheHome()

	output.Info("Data home:   %s", dataHome)
	output.Info("Config home: %s", configHome)
	output.Info("Cache home:  %s", cacheHome)

	for _, dir := range []struct {
		name string
		path string
	}{
		{"data", filepath.Join(dataHome, "git-hop")},
		{"config", filepath.Join(configHome, "git-hop")},
		{"cache", filepath.Join(cacheHome, "git-hop")},
	} {
		if exists, _ := afero.DirExists(fs, dir.path); exists {
			continue
		}
		r.issue(doctorCheckPaths, dir.path, "%s directory does not exist", dir.name)

		if !opts.fix {
			output.Error("%s directory does not exist: %s", dir.name, dir.path)
			continue
		}
		if !opts.mutating() {
			output.Info("[dry-run] Would create %s directory: %s", dir.name, dir.path)
			r.repaired(opts, doctorCheckPaths, dir.path, "create %s directory", dir.name)
			continue
		}
		if err := fs.MkdirAll(dir.path, 0755); err != nil {
			output.Error("Failed to create %s directory: %v", dir.name, err)
			r.failed(doctorCheckPaths, dir.path, "create %s directory: %v", dir.name, err)
		} else {
			output.Info("Created %s directory", dir.name)
			r.repaired(opts, doctorCheckPaths, dir.path, "create %s directory", dir.name)
		}
	}
}

// checkWorktreeState detects hopspace directories with no corresponding
// worktree and, under --fix, deletes them.
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

	hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(),
		hub.Config.Repo.Org, hub.Config.Repo.Repo)

	hopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		output.Error("Failed to load hopspace: %v", err)
		r.issue(doctorCheckWorktrees, hopspacePath, "failed to load hopspace: %v", err)
		return
	}

	validator := hop.NewStateValidator(fs, g)
	cleanup := hop.NewCleanupManager(fs, g)

	orphanedDirs, err := validator.DetectOrphanedDirectories(hopspace)
	if err != nil {
		output.Error("Failed to detect orphaned directories: %v", err)
		r.record(doctorKindIssue, doctorCheckWorktrees, hopspacePath, "failed to detect orphaned directories: %v", err)
		return
	}
	if len(orphanedDirs) == 0 {
		output.Info("No orphaned directories found")
		return
	}

	output.Error("Found %d orphaned directories", len(orphanedDirs))
	for _, dir := range orphanedDirs {
		output.Error("  - %s", dir)
		fullPath := filepath.Join(hopspacePath, "hops", dir)
		r.issue(doctorCheckWorktrees, fullPath, "orphaned directory")
		if !opts.fix {
			continue
		}
		if !opts.mutating() {
			output.Info("    [dry-run] Would remove %s", fullPath)
			r.repaired(opts, doctorCheckWorktrees, fullPath, "remove orphaned directory")
			continue
		}
		output.Info("    Cleaning up...")
		if err := cleanup.CleanupOrphanedDirectory(fullPath); err != nil {
			output.Error("    Failed to remove: %v", err)
			r.failed(doctorCheckWorktrees, fullPath, "remove orphaned directory: %v", err)
		} else {
			output.Info("    Removed")
			r.repaired(opts, doctorCheckWorktrees, fullPath, "remove orphaned directory")
		}
	}
	if !opts.fix {
		output.Info("  Run 'git hop doctor --fix' to clean up orphaned directories")
	}
}

// checkState reconciles the global state file (and the current hub's
// hop.json) against the filesystem.
func checkState(fs afero.Fs, g git.GitInterface, hubPath string, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking State ===")
	st, err := state.LoadState(fs)
	if err != nil {
		output.Warn("Could not load state: %v", err)
		output.Info("Run 'git hop migrate' if you have legacy data to migrate.")
		r.record(doctorKindWarning, doctorCheckState, "state", "could not load state: %v; run 'git hop migrate' if you have legacy data to migrate", err)
		return
	}
	if len(st.Repositories) == 0 {
		output.Info("No repositories in state. Skipping state checks.")
		return
	}

	stateIssues := missingStateWorktrees(fs, st)
	if len(stateIssues) == 0 {
		output.Info("State is consistent")
		return
	}

	output.Info("Found %d state consistency issue(s):", len(stateIssues))
	for _, issue := range stateIssues {
		output.Error("  %s", issue)
		r.issue(doctorCheckState, issue.repoID+":"+issue.branch, "worktree missing: %s", issue.path)
	}

	if opts.fix {
		r.fixed += fixStateIssues(fs, g, st, hubPath, opts, r)
	} else {
		output.Info("\nRun 'git hop doctor --fix' or 'git hop prune' to clean up orphaned entries.")
	}
}

// summarizeDoctor prints doctor's verdict. Under --dry-run the counts
// describe what a real --fix would do, so the wording must never claim a
// repair landed.
func summarizeDoctor(opts doctorOpts, r doctorReport) {
	output.Info("\n=== Summary ===")
	if !r.issuesFound {
		output.Info("No issues found. Your git-hop installation is healthy!")
		return
	}

	switch {
	case opts.planning():
		if r.fixed > 0 {
			output.Info("[dry-run] Would fix %d issue(s). Re-run without --dry-run to apply.", r.fixed)
		} else {
			output.Info("[dry-run] No issues could be automatically fixed. Please review the errors above.")
		}
	case opts.fix:
		if r.fixed > 0 {
			output.Info("Fixed %d issue(s).", r.fixed)
		}
		output.Info("Some issues could not be automatically fixed. Please review the errors above.")
	default:
		output.Info("Issues found. Run 'git hop doctor --fix' to automatically repair them.")
	}
}

// hasErrorSeverity reports whether any issue is severe enough to make the
// installation unhealthy. Warning-severity issues (stale symlinks) are
// still printed but do not flip doctor's verdict, because they describe a
// benign, self-healing state rather than something needing repair.
func hasErrorSeverity(issues []services.Issue) bool {
	for _, issue := range issues {
		if issue.Type.Severity() == services.SeverityError {
			return true
		}
	}
	return false
}

// getDirSize calculates the total size of a directory
func getDirSize(fs afero.Fs, path string) int64 {
	var size int64
	afero.Walk(fs, path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// checkStateConsistency verifies all worktrees in state exist on filesystem
func checkStateConsistency(fs afero.Fs, st *state.State) []string {
	var issues []string
	for _, issue := range missingStateWorktrees(fs, st) {
		issues = append(issues, issue.String())
	}
	return issues
}

// stateIssue is a worktree recorded in state whose directory is gone.
type stateIssue struct {
	repoID, branch, path string
}

func (i stateIssue) String() string {
	return "Worktree missing: " + i.repoID + ":" + i.branch + " at " + i.path
}

// missingStateWorktrees returns every worktree in st whose directory does
// not exist, in repository then branch order.
func missingStateWorktrees(fs afero.Fs, st *state.State) []stateIssue {
	var issues []stateIssue
	for _, repoID := range scopeRepoIDs(st) {
		for branch, wt := range st.Repositories[repoID].Worktrees {
			if exists, _ := afero.DirExists(fs, wt.Path); !exists {
				issues = append(issues, stateIssue{repoID: repoID, branch: branch, path: wt.Path})
			}
		}
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].repoID != issues[j].repoID {
			return issues[i].repoID < issues[j].repoID
		}
		return issues[i].branch < issues[j].branch
	})
	return issues
}
