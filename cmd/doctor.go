package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
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
	GroupID: cli.GroupManagement,
	Aliases: []string{"check"},
	Short:   "Check and repair the environment",
	Long: `Run diagnostics on git-hop installation and project setup.

Checks:
- Path configuration (data home; the config and cache homes are
  optional until something is written there)
- Hub configuration and symlinks
- The current hub's origin fetch refspec: an origin with no
  remote.origin.fetch (a plain 'git clone --bare') never updates
  refs/remotes/origin/* on fetch ('git hop repair' restores it)
- Hopspace existence and consistency
- Records in the data-home hopspace --global hubs share of worktrees of
  a hub that no longer exists (not in state, directory gone): a warning
  (--fix drops them)
- Hopspaces of --global hubs left where another hop.dataLayout put
  them (e.g. <org>/<repo> after a switch to {host}/{org}/{repo}): a
  warning (--fix renames the directory to the current location when
  nothing is there yet and no worktree lies inside it or links into it;
  with data at both, nothing is moved)
- Hopspace hooks left at the pre-hop.dataLayout location
  ($GIT_HOP_DATA_HOME/github.com/<org>/<repo>/hooks): they still fire,
  a warning (--fix moves the directory to the hop.dataLayout location
  when nothing is there yet; with hooks at both, nothing is moved), and
  an invalid hop.dataLayout value, a warning
- Worktree state: directories under hops/ that hop.json does not
  record. --fix removes only empty ones; a worktree git has registered
  ('git hop repair' records it) and any directory with something in it
  are reported for you to handle, never removed
- Orphaned worktrees in state
- Repositories state still keys by another host than their origin's,
  because the "<host>/<org>/<repo>" key their origin gives was already
  taken: an issue. Both entries are kept; --fix does not merge them, the
  hint says how to merge them by hand
- The current hub's record in state: a hub with hop.json that state does
  not record, or records without some of its worktrees, is invisible to
  list, status --all and prune --all (--fix records it, merging with
  state and never overwriting an entry)
- A managers.json that cannot be read or parsed: every command skips it
  with a warning, so its managers do not apply; an issue --fix cannot
  repair
- A config.json in the config directory: git-hop does not read it
  (settings live in git config hop.*); a warning, and --fix leaves the
  file for you to delete
- A hops.json in the config directory: the hub registry of earlier
  releases, which git-hop no longer writes or reads (state records every
  hub and worktree); a warning, and --fix leaves the file for you to
  delete
- --global hop.* keys the old global.json migration wrote but the user
  never set (--fix unsets them)
- Retired hop.* settings (hop.autoEnvStart, hop.bareRepo and the other
  settings git-hop no longer has) in --global or the current hub's
  config, whatever their value: never read; a warning (--fix unsets them)
- Temp files a save of the state file, or of the current hub's
  hop.json, left beside it when its run died before renaming them into
  place, once an hour old: a warning (--fix removes them, as 'git hop
  prune' does, unless a save holds the file's lock at the time)
- The current hub's repository format: extensions.worktreeConfig on at
  core.repositoryformatversion 0, which tools other than git may not
  honour; a warning (--fix sets version 1)

Use --fix to automatically repair issues. In the current hub, --fix also
drops hop.json branch entries whose worktree directory is gone (the rows
'git hop status' reports as Missing), backing hop.json up to the per-hub
repair state dir ($XDG_STATE_HOME/git-hop/repair/<hub>/backups/) first so
the change can be undone with 'git hop repair --undo'.

Combine --fix with --dry-run to preview every repair without applying any
of it: no directories created, no worktrees recreated, no dependencies
touched, no state or hop.json rewritten, and no backup snapshot taken.

Exit status, like git fsck: 0 when healthy or only warnings were found,
1 when an issue was reported and not fixed. With --fix, 0 only if every
issue was fixed; with --fix --dry-run, only if every issue would be.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if err := doctorResult(r); err != nil {
			// The report already says what is wrong; the status is the
			// only thing left to convey.
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			return err
		}
		return nil
	},
}

// errDoctorUnresolved is doctor's result when it leaves an issue behind.
var errDoctorUnresolved = errors.New("doctor found issues it did not fix")

// doctorResult maps a report to doctor's exit, like git fsck: an error
// (exit 1) when an issue was reported and not fixed, nil (exit 0) when the
// run was healthy or found only warnings. An issue counts as fixed when a
// fixed record (would-fix under --dry-run) names the same check and
// subject; any failed repair leaves the run unresolved.
func doctorResult(r doctorReport) error {
	type key struct{ check, subject string }
	resolved := map[key]bool{}
	for _, rec := range r.records {
		if rec.Kind == doctorKindFixed || rec.Kind == doctorKindWouldFix {
			resolved[key{rec.Check, rec.Subject}] = true
		}
	}
	for _, rec := range r.records {
		switch {
		case rec.Kind == doctorKindFailed:
			return errDoctorUnresolved
		case rec.Kind == doctorKindIssue && !resolved[key{rec.Check, rec.Subject}]:
			return errDoctorUnresolved
		}
	}
	return nil
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
	doctorCheckPorts        = "ports" // ports two worktrees hold, across every hub
	doctorCheckWorktrees    = "worktrees"
	doctorCheckState        = "state"
	doctorCheckHopspace     = "hopspace" // stale hopspace copies, hooks, layouts and records
	doctorCheckConfig       = "config"   // global git config left by the legacy migration or old releases
)

// doctorChecks lists every check above, in the order doctor runs them. It
// is the check enum of doctor's output schema (doctorRecord.
// JSONSchemaExtend); a test holds it to the constants.
var doctorChecks = []string{
	doctorCheckPaths,
	doctorCheckHub,
	doctorCheckHopspace,
	doctorCheckDependencies,
	doctorCheckPorts,
	doctorCheckWorktrees,
	doctorCheckState,
	doctorCheckConfig,
}

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
	// misplaced holds data-home hopspace paths whose hopspace sits, not
	// moved, at another hop.dataLayout's path (checkMisplacedHopspaces).
	// The hub check reports no such hopspace missing and creates none:
	// that would strand the real one.
	misplaced map[string]bool
}

// markMisplaced records that the hopspace belonging at path sits, not
// moved, elsewhere; see doctorReport.misplaced.
func (r *doctorReport) markMisplaced(path string) {
	if r.misplaced == nil {
		r.misplaced = map[string]bool{}
	}
	r.misplaced[filepath.Clean(path)] = true
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

// fixableIssue records a problem that makes the installation unhealthy
// and that --fix has a repair for. The repair can still fail when it
// runs; that is a failed record, not an unfixable issue.
func (r *doctorReport) fixableIssue(check, subject, format string, args ...any) {
	r.issue(true, check, subject, format, args...)
}

// unfixableIssue records a problem that makes the installation unhealthy
// and that --fix has no repair for: the error or hint printed with it
// says what to do instead. doctor never suggests --fix for it.
func (r *doctorReport) unfixableIssue(check, subject, format string, args ...any) {
	r.issue(false, check, subject, format, args...)
}

// issue records a problem, marked with whether --fix can repair it. Every
// check goes through fixableIssue or unfixableIssue, so none can report
// an issue without saying which it is.
func (r *doctorReport) issue(fixable bool, check, subject, format string, args ...any) {
	r.issuesFound = true
	r.records = append(r.records, doctorRecord{
		Kind:    doctorKindIssue,
		Check:   check,
		Subject: subject,
		Message: fmt.Sprintf(format, args...),
		Fixable: &fixable,
	})
}

// issueCounts returns how many of the issues reported are fixable and
// how many are not.
func (r doctorReport) issueCounts() (fixable, unfixable int) {
	for _, rec := range r.records {
		if rec.Kind != doctorKindIssue {
			continue
		}
		if rec.Fixable != nil && *rec.Fixable {
			fixable++
		} else {
			unfixable++
		}
	}
	return fixable, unfixable
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
		// A preview applies nothing, so a later check would judge the
		// disk as it was before the repairs an earlier check planned: a
		// worktree the hub check would recreate still looks missing to
		// the state check, which would then plan to prune it. The checks
		// run on a scratch copy-on-write layer instead, where previewDir
		// records what a real run would create. Nothing written to the
		// layer reaches disk.
		fs = afero.NewCopyOnWriteFs(fs, afero.NewMemMapFs())
	}

	checkPaths(fs, opts, &r)
	checkDataLayout(fs, cwd, opts, &r)
	hubPath, hubKept := checkHub(fs, g, cwd, opts, &r)
	checkDependencies(fs, hubPath, opts, &r)
	checkLegacyDepsStores(fs, hubPath, &r)
	checkPorts(fs, hubPath, &r)
	checkWorktreeState(fs, g, hubPath, opts, &r)
	checkState(fs, g, hubPath, hubKept, opts, &r)
	checkConfig(config.NewGlobalLoader(), hubPath, opts, &r)

	summarizeDoctor(opts, r)
	return r
}

// checkPaths verifies the data directory git-hop keeps its own data in
// exists, creating it under --fix.
//
// The config directory ($XDG_CONFIG_HOME/git-hop) is shown but not
// required. It holds only what the user (or a command acting for them)
// has configured, every writer creates it on demand (managers.json,
// the global hooks), and every reader treats its absence as
// "nothing configured". A fresh install without it is
// healthy, and --fix leaves creating it to the first writer.
//
// The cache directory ($XDG_CACHE_HOME/git-hop) is not required either,
// for the same reason. Everything in it is disposable and rebuilt on
// demand: its writers (conversion backups, the shell integration's
// worktree-roots file, compose override files) create it, and its
// readers (prune's backup sweep, the shell integration, the override
// check) treat a missing file or directory as empty.
func checkPaths(fs afero.Fs, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking Paths ===")
	dataHome := hop.GetGitHopDataHome()
	configHome := hop.GetConfigHome()
	cacheHome := hop.GetCacheHome()

	output.Info("Data home:   %s", dataHome)
	output.Info("Config home: %s", configHome)
	output.Info("Cache home:  %s", cacheHome)

	if exists, _ := afero.DirExists(fs, dataHome); exists {
		return
	}
	r.fixableIssue(doctorCheckPaths, dataHome, "data directory does not exist")

	switch {
	case !opts.fix:
		output.Error("data directory does not exist: %s", dataHome)
	case !opts.mutating():
		output.Info("[dry-run] Would create data directory: %s", dataHome)
		r.repaired(opts, doctorCheckPaths, dataHome, "create data directory")
	default:
		if err := fs.MkdirAll(dataHome, 0755); err != nil {
			output.Error("Failed to create data directory: %v", err)
			r.failed(doctorCheckPaths, dataHome, "create data directory: %v", err)
			return
		}
		output.Info("Created data directory")
		r.repaired(opts, doctorCheckPaths, dataHome, "create data directory")
	}
}

// checkState reconciles the global state file (and the current hub's
// hop.json) against the filesystem, first recording the current hub and
// its worktrees when state lacks them (checkHubRegistration). hubKept are
// the missing worktrees the hub check could not recreate; their hop.json
// rows are kept.
func checkState(fs afero.Fs, g git.GitInterface, hubPath string, hubKept keptWorktrees, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking State ===")
	checkHubRegistration(fs, hubPath, opts, r)
	st, stateIssues := inspectState(fs, g, r)
	checkRepoIDCollisions(fs, st, r)
	switch {
	case len(stateIssues) > 0 && opts.fix:
		r.fixed += fixStateIssues(fs, g, st, hubPath, hubKept, opts, r)
	case len(stateIssues) > 0:
		output.Info("\nRun 'git hop doctor --fix' or 'git hop prune' to clean up orphaned entries.")
	case opts.fix:
		// state.json has nothing to fix, but the hub's hop.json can still
		// list a worktree the hub check left for cleanup (a merged branch
		// whose directory is gone) when state never recorded it.
		r.fixed += pruneMissingHubRows(fs, g, hubPath, hubKept, opts, r)
	}
	checkStaleTemps(fs, hubPath, opts, r)
}

// inspectState loads the global state and reports every worktree it
// lists whose directory is gone. st is nil when state cannot be loaded.
//
// A missing worktree git has locked is reported as a warning and left
// out of the returned issues: git keeps it on purpose, and so does the
// repair (fixMissingWorktrees).
func inspectState(fs afero.Fs, g git.GitInterface, r *doctorReport) (*state.State, []stateIssue) {
	st, err := state.LoadState(fs)
	if err != nil {
		output.Warn("Could not load state: %v", err)
		output.Info("git-hop leaves the file as it is and will not save over it; repair it or move it aside.")
		r.record(doctorKindWarning, doctorCheckState, "state", "could not load state: %v; git-hop will not save over it: repair it or move it aside", err)
		return nil, nil
	}
	if len(st.Repositories) == 0 {
		output.Info("No repositories in state. Skipping state checks.")
		return st, nil
	}

	var stateIssues []stateIssue
	for _, issue := range missingStateWorktrees(fs, st) {
		wt := st.Repositories[issue.repoID].Worktrees[issue.key]
		if reason, locked := stateWorktreeLock(fs, g, issue.repoID, st.Repositories[issue.repoID].URI, wt); locked {
			warnLockedWorktree(r, doctorCheckState, stateWorktreeSubject(issue.repoID, issue.branch, issue.path), issue.path, reason)
			continue
		}
		stateIssues = append(stateIssues, issue)
	}
	if len(stateIssues) == 0 {
		output.Info("State is consistent")
		return st, nil
	}

	output.Info("Found %d state consistency issue(s):", len(stateIssues))
	for _, issue := range stateIssues {
		output.Error("  %s", issue)
		r.fixableIssue(doctorCheckState, stateWorktreeSubject(issue.repoID, issue.branch, issue.path), "worktree missing: %s (%s:%s)", issue.path, issue.repoID, issue.branch)
	}
	return st, stateIssues
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
		if doctorResult(r) != nil {
			output.Info("Some issues could not be automatically fixed. Please review the errors above.")
		}
	default:
		// Suggest --fix only for what it can repair: an issue it has no
		// repair for would still be there after it, and exit 1 again.
		fixable, unfixable := r.issueCounts()
		switch {
		case unfixable == 0:
			output.Info("Issues found. Run 'git hop doctor --fix' to automatically repair them.")
		case fixable == 0:
			output.Info("Issues found that 'git hop doctor --fix' cannot repair. Please review the errors and hints above.")
		default:
			output.Info("Issues found. Run 'git hop doctor --fix' to repair %d of them; for the other %d, review the errors and hints above.", fixable, unfixable)
		}
	}
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
// key is its entry's key in the repository's worktrees.
type stateIssue struct {
	repoID, key, branch, path string
}

func (i stateIssue) String() string {
	return "Worktree missing: " + i.repoID + ":" + i.branch + " at " + i.path
}

// missingStateWorktrees returns every worktree in st whose directory does
// not exist, in repository then branch order (state.SortedWorktreeKeys).
func missingStateWorktrees(fs afero.Fs, st *state.State) []stateIssue {
	var issues []stateIssue
	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		for _, key := range repo.SortedWorktreeKeys() {
			wt := repo.Worktrees[key]
			if !hop.WorktreeDirPresent(fs, wt.Path) {
				issues = append(issues, stateIssue{repoID: repoID, key: key, branch: wt.Branch, path: wt.Path})
			}
		}
	}
	return issues
}
