package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/filelock"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
)

var (
	repairUndoFlag        string
	repairListBackupsFlag bool
	repairNoBackup        bool
	repairForceDirty      bool
	repairProgress        output.ProgressWhen
	repairColor           = output.ColorAuto
	repairBaseFlag        bool
	repairDryRunFlag      bool
)

var repairCmd = &cobra.Command{
	Use:     "repair [<pathspec>...]",
	Args:    cobra.ArbitraryArgs,
	GroupID: cli.GroupManagement,
	Short:   "Safely repair stale worktree metadata",
	Long: `Repair stale worktree metadata (gitdir pointers, hop.json, git registry)
in a recoverable way: detects issues, takes a backup, applies fixes,
verifies post-state with doctor, and supports --undo.

The default invocation MUTATES with safety nets (backup, dirty-check,
lock). Use -n/--dry-run to preview without changes.

Pathspec arguments restrict the operation to specific worktrees,
mirroring 'git add -- <pathspec>'.`,
	RunE: runRepair,
}

func init() {
	cli.RootCmd.AddCommand(repairCmd)
	f := repairCmd.Flags()
	f.StringVar(&repairUndoFlag, "undo", "", "restore from backup (use --undo without value for most recent)")
	f.Lookup("undo").NoOptDefVal = "@latest"
	f.BoolVar(&repairListBackupsFlag, "list-backups", false, "list available backups")
	f.BoolVar(&repairNoBackup, "no-backup", false, "skip backup (requires --force)")
	f.BoolVar(&repairForceDirty, "force-dirty", false, "allow repair when worktrees have uncommitted changes")
	output.BindProgressFlags(f, &repairProgress)
	f.Var(&repairColor, "color", "color output: always|auto|never (bare --color: always)")
	f.Lookup("color").NoOptDefVal = string(output.ColorAlways)
	f.BoolVar(&repairBaseFlag, "base", false, "infer and record HubBranch.Base for legacy entries (best-effort heuristic; use --dry-run to preview)")
	// Local --dry-run shadows the global persistent flag, so repair handles
	// its own preview. The runRepair logic reads
	// cmd.Flags().GetBool("dry-run"), which resolves to this local flag
	// (cobra prefers local over inherited persistent flags). A shadowing
	// flag must repeat the global -n shorthand or -n stops working here.
	f.BoolVarP(&repairDryRunFlag, "dry-run", "n", false, "preview changes without applying")
	declareOutputSchema(repairCmd, &[]repairRecord{})
}

// exit codes follow git porcelain convention: 0 success, 1 op failure,
// 128 fatal git/repo error, 129 usage error.
const (
	exitOK    = 0
	exitOp    = 1
	exitFatal = 128
	exitUsage = 129
)

func runRepair(cmd *cobra.Command, args []string) error {
	fs := afero.NewOsFs()
	g := git.New()

	// The declared result is the repair plan. A backup listing or a
	// restore is not a plan action, and folding them into the plan record
	// would give one field set three meanings, so these views refuse the
	// structured modes -- before --undo restores anything -- rather than
	// print text where a document was asked for.
	if output.IsStructured() && (repairListBackupsFlag || repairUndoFlag != "") {
		view := "--list-backups"
		if repairUndoFlag != "" {
			view = "--undo"
		}
		output.FatalCode(exitUsage, "structured output is not supported for 'git hop repair %s'", view)
	}
	if repairListBackupsFlag {
		return repairListBackups(fs)
	}
	if repairUndoFlag != "" {
		return repairUndo(fs, repairUndoFlag)
	}
	return repairRun(cmd, fs, g, args)
}

func repairListBackups(fs afero.Fs) error {
	hubPath, err := resolveHubPath(fs)
	if err != nil {
		return fatal(err.Error())
	}
	b := hop.NewRepairBackup(fs, hubPath)
	list, err := b.List()
	if err != nil {
		return fatal("list backups: " + err.Error())
	}
	if len(list) == 0 {
		fmt.Println("(no backups)")
		return nil
	}
	for _, m := range list {
		fmt.Printf("%s\t%s\t%d action(s)\n", m.ID, m.Timestamp.Format("2006-01-02T15:04:05Z"), len(m.Actions))
	}
	return nil
}

func repairUndo(fs afero.Fs, idArg string) error {
	hubPath, err := resolveHubPath(fs)
	if err != nil {
		return fatal(err.Error())
	}
	b := hop.NewRepairBackup(fs, hubPath)
	id := idArg
	if id == "@latest" {
		id = ""
	}
	manifest, err := b.Restore(id)
	if err != nil {
		return opErr("undo failed: " + err.Error())
	}
	fmt.Printf("Restored backup %s\n", manifest.ID)
	return nil
}

// repairOutcome is how the locked section of a repair reports back. It
// is mapped to an exit only AFTER the lock has been released: fatal and
// opErr call os.Exit, which skips deferred calls, so returning them from
// inside the locked section would leave the lock file behind on every
// handled failure (dirty worktrees, hook abort, apply error).
type repairOutcome struct {
	code int
	msg  string
}

func okOutcome() repairOutcome              { return repairOutcome{code: exitOK} }
func fatalOutcome(msg string) repairOutcome { return repairOutcome{code: exitFatal, msg: msg} }
func opOutcome(msg string) repairOutcome    { return repairOutcome{code: exitOp, msg: msg} }

// exit converts the outcome into the process exit the porcelain contract
// promises. Only ever called with the lock released.
func (o repairOutcome) exit() error {
	switch o.code {
	case exitOK:
		return nil
	case exitFatal:
		return fatal(o.msg)
	default:
		return opErr(o.msg)
	}
}

func repairRun(cmd *cobra.Command, fs afero.Fs, g git.GitInterface, pathspec []string) error {
	hubPath, err := resolveHubPath(fs)
	if err != nil {
		return fatal(err.Error())
	}

	// 1. Acquire lock. Lives in the per-hub state dir, never in the hub.
	lock := filelock.New(hop.RepairLockPath(hubPath))
	ok, err := lock.TryAcquire()
	if err != nil {
		return fatal("acquire lock: " + err.Error())
	}
	if !ok {
		return fatal("another repair is in progress")
	}

	outcome := repairLocked(cmd, fs, g, hubPath, pathspec)
	if err := lock.Release(); err != nil {
		output.Warn("release lock: %v", err)
	}
	return outcome.exit()
}

// repairLocked is the body of a repair run, executed while the lock is
// held. It must not exit the process; see repairOutcome.
func repairLocked(cmd *cobra.Command, fs afero.Fs, g git.GitInterface, hubPath string, pathspec []string) repairOutcome {
	showProgress := output.ShowProgress(repairProgress)

	// 2. Detect / build plan.
	plan, err := hop.NewPlanner(fs, g).WithBaseInference(repairBaseFlag).Build(hubPath, pathspec)
	if err != nil {
		return fatalOutcome("plan: " + err.Error())
	}

	// 3. Dirty-check.
	if !repairForceDirty {
		if dirty := dirtyWorktrees(g, plan, showProgress); len(dirty) > 0 {
			for _, p := range dirty {
				output.Error("%s has uncommitted changes", p)
			}
			return opOutcome("dirty worktrees; use --force-dirty to override")
		}
	}

	// 4. Print plan.
	if output.IsStructured() {
		emitResult(cmd, repairRecords(plan))
		printPlanWarnings(plan)
	} else {
		printPlan(plan)
	}

	// 5. Dry-run shortcut.
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		return okOutcome()
	}

	if !plan.HasMutations() {
		return okOutcome()
	}

	// 6. pre-repair hook. Blocking: a non-zero exit aborts before any
	// mutation, so nothing has been backed up or applied yet.
	if abort := firePreRepairHook(fs, hubPath); abort != nil {
		return opOutcome("pre-repair hook aborted: " + abort.Error())
	}

	// 7. Backup.
	var backupID, backupDir string
	forceFlag, _ := cmd.Flags().GetBool("force")
	if !(repairNoBackup && forceFlag) {
		b := hop.NewRepairBackup(fs, hubPath)
		manifest, err := b.Snapshot(plan)
		if err != nil {
			return fatalOutcome("backup: " + err.Error())
		}
		backupID = manifest.ID
		backupDir = b.Path(backupID)
	}

	// 8. Apply.
	meter := output.NewProgress(showProgress, "Applying repairs", len(plan.Actions))
	mutations, err := hop.NewApplier(fs, g).OnAction(meter.Tick).Apply(plan)
	meter.Stop()
	if err != nil {
		if backupID != "" {
			output.Error("apply failed; backup at %s", backupDir)
		}
		return opOutcome(err.Error())
	}

	// 9. Verify globally — re-run planner; if doctor-equivalent diff has
	// new issues not in the original plan, auto-restore.
	postPlan, err := hop.NewPlanner(fs, g).Build(hubPath, nil)
	if err == nil && postPlan.HasMutations() {
		// Did any post-state action fall outside the original plan's targets?
		if introducedNewIssue(plan, postPlan) {
			if backupID != "" {
				if _, rerr := hop.NewRepairBackup(fs, hubPath).Restore(backupID); rerr == nil {
					output.Error("repair introduced new issues, restored from backup %s", backupID)
					return opOutcome("repair introduced new issues, restored")
				}
			}
			return opOutcome("repair introduced new issues; manual recovery required")
		}
	}

	// 10. post-repair hook (advisory, ignore exit).
	_ = firePostRepairHook(fs, hubPath)

	if backupID != "" {
		output.Hint("backup written to %s", backupDir)
	}
	if mutations > 0 {
		output.Success("Repaired %d worktree(s)", mutations)
	}
	return okOutcome()
}

// resolveHubPath finds the nearest hub from cwd. Returns an error
// suitable for a fatal exit (exit 128).
func resolveHubPath(fs afero.Fs) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		return "", fmt.Errorf("not in a hub: %w", err)
	}
	return hubPath, nil
}

// dirtyWorktrees runs git status in every worktree the plan touches,
// reporting progress over them when showProgress is set: on a large
// repository each status can take a while.
func dirtyWorktrees(g git.GitInterface, plan *hop.Plan, showProgress bool) []string {
	var targets []string
	for _, a := range plan.Actions {
		if a.Kind != hop.ActionNoOp {
			targets = append(targets, a.WorktreePath)
		}
	}
	meter := output.NewProgress(showProgress && len(targets) > 0, "Checking worktrees", len(targets))
	defer meter.Stop()

	var dirty []string
	for _, p := range targets {
		isDirty := repairSeesDirty(g, p)
		meter.Tick()
		if isDirty {
			dirty = append(dirty, p)
		}
	}
	return dirty
}

// repairSeesDirty reports whether repair's dirty check refuses the
// worktree at path without --force-dirty: `git status --porcelain`
// lists anything in it. A status git cannot read does not count.
func repairSeesDirty(g git.GitInterface, path string) bool {
	out, err := g.RunInDir(path, "git", "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// repairRecords is the plan as the command's structured result, one
// record per action in plan order. The slice is never nil, so an empty
// plan renders as [] rather than null.
func repairRecords(plan *hop.Plan) []repairRecord {
	records := make([]repairRecord, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		status := "ok"
		if a.Kind != hop.ActionNoOp {
			status = "repaired"
		}
		records = append(records, repairRecord{
			Status: status,
			Path:   a.WorktreePath,
			Kind:   a.Kind.String(),
			Old:    a.OldValue,
			New:    a.NewValue,
			Reason: a.Reason,
		})
	}
	return records
}

func printPlan(plan *hop.Plan) {
	fmt.Printf("Repair plan for %s:\n", plan.HubPath)
	if len(plan.Actions) == 0 && len(plan.Warnings) == 0 {
		fmt.Println("  (nothing to do)")
		return
	}
	for _, a := range plan.Actions {
		fmt.Printf("  %-15s %s: %s\n", a.Kind.String(), a.WorktreePath, a.Reason)
	}
	printPlanWarnings(plan)
}

func printPlanWarnings(plan *hop.Plan) {
	for _, w := range plan.Warnings {
		output.Warn("%s", w)
	}
}

// introducedNewIssue returns true when post contains an Action targeting
// a path that wasn't in the original plan, suggesting the repair created
// a fresh problem. Action equality is loose: same path + non-NoOp kind.
func introducedNewIssue(orig, post *hop.Plan) bool {
	planned := map[string]struct{}{}
	for _, a := range orig.Actions {
		if a.Kind != hop.ActionNoOp {
			planned[a.WorktreePath] = struct{}{}
		}
	}
	for _, a := range post.Actions {
		if a.Kind == hop.ActionNoOp {
			continue
		}
		if _, ok := planned[a.WorktreePath]; !ok {
			return true
		}
	}
	return false
}

// firePreRepairHook invokes the pre-repair hook. Returns non-nil error
// to abort the repair before any mutation. Runs synchronously.
func firePreRepairHook(fs afero.Fs, hubPath string) error {
	return runRepairHook(fs, "pre-repair", hubPath)
}

// firePostRepairHook fires the advisory post-repair hook. Errors are
// swallowed by the caller; we still return them for symmetry.
func firePostRepairHook(fs afero.Fs, hubPath string) error {
	return runRepairHook(fs, "post-repair", hubPath)
}

// runRepairHook dispatches a repair hook through the shared hooks.Runner,
// so repair resolves hooks exactly like every other command: repo-level
// (.git-hop/hooks/, searched up to the hub), then hopspace-level, then
// global. It also inherits the runner's name validation and its
// executable-bit check.
//
// Working directory: DELIBERATELY not set. Repair hooks inherit git-hop's
// cwd exactly like every other hook. An earlier hand-rolled dispatch ran
// them with cwd pinned to the hub; that was the only such exception in
// the tree and it has been aligned away on purpose — do not restore it
// thinking it was an oversight. A hook that wants the hub cds there
// itself via GIT_HOP_WORKTREE_PATH, the idiom every hook example uses.
//
// Hook environment. The runner always exports all four standard vars,
// empty ones included, so repair follows the export-empty convention
// rather than the omit-when-empty convention hooks.SwitchEnvVars uses for
// its optional extras. That distinction is load-bearing here: a hook
// author can tell "field known to be unresolvable" (variable is set and
// empty, `${GIT_HOP_REPO_ID+set}` is non-empty) from "field absent"
// (variable unset entirely, which for these four never happens).
//
//   - GIT_HOP_HOOK_NAME     — set by the runner.
//   - GIT_HOP_WORKTREE_PATH — the hub path. Repair operates on the hub,
//     not on one worktree; the hub is the directory the hook cares about
//     and the anchor for the repo-level hook search.
//   - GIT_HOP_BRANCH        — empty. A repair run spans every registered
//     branch, so no single branch is the subject. Empty is the honest
//     answer; naming an arbitrary branch would be worse.
//   - GIT_HOP_REPO_ID       — "<host>/<org>/<repo>" when resolvable,
//     otherwise empty. See repairHookRepoID.
//
// The repo ID is resolved at dispatch time rather than passed in, which
// makes the pre/post asymmetry fall out naturally: pre-repair reads the
// possibly-damaged hub config, post-repair reads the repaired one.
func runRepairHook(fs afero.Fs, name, hubPath string) error {
	_, err := hooks.NewRunner(fs).ForRepo(repairHookRepoURI(fs, hubPath), hubPath).
		ExecuteHook(name, hubPath, repairHookRepoID(fs, hubPath), "")
	return err
}

// repairHookRepoURI is the origin URL in the hub config at the moment of
// the call, "" when it cannot be read. The runner takes the host of the
// hopspace hooks directory from it.
func repairHookRepoURI(fs afero.Fs, hubPath string) string {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return ""
	}
	return hub.Config.Repo.URI
}

// repairHookRepoID builds the 3-part repo identifier hooks.Runner needs
// for hopspace-level hook lookup, reading the hub config as it stands at
// the moment of the call.
//
// Returns "" when the hub config is missing, unreadable, or lacks org or
// repo. A partial ID is worse than none: the runner splits on "/" and
// needs at least three segments, so "<host>//" would fail hopspace
// lookup anyway while looking like a real value to a hook. Empty is the
// truthful signal that the field could not be determined.
//
// The consequence differs by hook, and the difference is the point:
//
//   - pre-repair runs before the fix, when hop.json may be exactly as
//     broken as the reason repair was invoked. An empty repo ID here is
//     expected. Hopspace-level lookup silently falls through to global,
//     while repo-level and global hooks still resolve normally. Dispatch
//     is never skipped and never fails on account of a missing field.
//   - post-repair runs after a successful repair, so hop.json is valid by
//     construction and the full 3-part ID resolves, making hopspace-level
//     post-repair hooks work.
func repairHookRepoID(fs afero.Fs, hubPath string) string {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return ""
	}
	return repoid.For(hubPath, hub.Config.Repo)
}

// fatal returns an error that the cobra layer surfaces with exit 128.
func fatal(msg string) error {
	output.FatalCode(exitFatal, "%s", msg)
	return nil
}

// opErr formats a non-fatal operation failure for exit 1 and returns
// the cobra-friendly error so cobra also reports it through SilenceUsage.
func opErr(msg string) error {
	output.ErrorCode(exitOp, "%s", msg)
	return nil
}
