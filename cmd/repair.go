package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

var (
	repairUndoFlag        string
	repairListBackupsFlag bool
	repairNoBackup        bool
	repairForceDirty      bool
	repairProgressFlag    bool
	repairNoProgressFlag  bool
	repairColor           string
	repairBaseFlag        bool
	repairDryRunFlag      bool
)

var repairCmd = &cobra.Command{
	Use:   "repair [<pathspec>...]",
	Short: "Safely repair stale worktree metadata",
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
	f.BoolVar(&repairProgressFlag, "progress", false, "force progress to stderr")
	f.BoolVar(&repairNoProgressFlag, "no-progress", false, "force progress off")
	f.StringVar(&repairColor, "color", "auto", "color output: always|auto|never")
	f.BoolVar(&repairBaseFlag, "base", false, "infer and record HubBranch.Base for legacy entries (best-effort heuristic; use --dry-run to preview)")
	// Local --dry-run shadows the global persistent flag so we can attach
	// the -n shorthand that the Long help advertises. The runRepair logic
	// reads cmd.Flags().GetBool("dry-run"), which resolves to this local
	// flag (cobra prefers local over inherited persistent flags).
	f.BoolVarP(&repairDryRunFlag, "dry-run", "n", false, "preview changes without applying")
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

func repairRun(cmd *cobra.Command, fs afero.Fs, g git.GitInterface, pathspec []string) error {
	hubPath, err := resolveHubPath(fs)
	if err != nil {
		return fatal(err.Error())
	}

	// 1. Acquire lock.
	lock := hop.NewFileLock(filepath.Join(hubPath, ".hop", "repair.lock"))
	ok, err := lock.TryAcquire()
	if err != nil {
		return fatal("acquire lock: " + err.Error())
	}
	if !ok {
		return fatal("another repair is in progress")
	}
	defer lock.Release()

	// 2. Detect / build plan.
	plan, err := hop.NewPlanner(fs, g).WithBaseInference(repairBaseFlag).Build(hubPath, pathspec)
	if err != nil {
		return fatal("plan: " + err.Error())
	}

	// 3. Dirty-check.
	if !repairForceDirty {
		if dirty := dirtyWorktrees(g, plan); len(dirty) > 0 {
			for _, p := range dirty {
				fmt.Fprintf(os.Stderr, "error: %s has uncommitted changes\n", p)
			}
			return opErr("dirty worktrees; use --force-dirty to override")
		}
	}

	// 4. Print plan.
	porcelainMode, _ := cmd.Flags().GetBool("porcelain")
	printPlan(plan, porcelainMode)

	// 5. Dry-run shortcut.
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		return nil
	}

	if !plan.HasMutations() {
		return nil
	}

	// 6. pre-repair hook. Blocking: a non-zero exit aborts before any
	// mutation, so nothing has been backed up or applied yet.
	if abort := firePreRepairHook(fs, hubPath); abort != nil {
		return opErr("pre-repair hook aborted: " + abort.Error())
	}

	// 7. Backup.
	var backupID string
	forceFlag, _ := cmd.Flags().GetBool("force")
	if !(repairNoBackup && forceFlag) {
		b := hop.NewRepairBackup(fs, hubPath)
		manifest, err := b.Snapshot(plan)
		if err != nil {
			return fatal("backup: " + err.Error())
		}
		backupID = manifest.ID
	}

	// 8. Apply.
	applier := hop.NewApplier(fs, g)
	mutations, err := applier.Apply(plan)
	if err != nil {
		if backupID != "" {
			fmt.Fprintf(os.Stderr, "error: apply failed; backup at .hop/backups/%s\n", backupID)
		}
		return opErr(err.Error())
	}

	// 9. Verify globally — re-run planner; if doctor-equivalent diff has
	// new issues not in the original plan, auto-restore.
	postPlan, err := hop.NewPlanner(fs, g).Build(hubPath, nil)
	if err == nil && postPlan.HasMutations() {
		// Did any post-state action fall outside the original plan's targets?
		if introducedNewIssue(plan, postPlan) {
			if backupID != "" {
				if _, rerr := hop.NewRepairBackup(fs, hubPath).Restore(backupID); rerr == nil {
					fmt.Fprintf(os.Stderr, "error: repair introduced new issues, restored from backup %s\n", backupID)
					return opErr("repair introduced new issues, restored")
				}
			}
			return opErr("repair introduced new issues; manual recovery required")
		}
	}

	// 10. post-repair hook (advisory, ignore exit).
	_ = firePostRepairHook(fs, hubPath)

	if backupID != "" {
		fmt.Fprintf(os.Stderr, "hint: backup written to .hop/backups/%s\n", backupID)
	}
	if mutations > 0 {
		output.Success("Repaired %d worktree(s)", mutations)
	}
	return nil
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

func dirtyWorktrees(g git.GitInterface, plan *hop.Plan) []string {
	var dirty []string
	for _, a := range plan.Actions {
		if a.Kind == hop.ActionNoOp {
			continue
		}
		out, err := g.RunInDir(a.WorktreePath, "git", "status", "--porcelain")
		if err != nil {
			continue
		}
		if strings.TrimSpace(out) != "" {
			dirty = append(dirty, a.WorktreePath)
		}
	}
	return dirty
}

func printPlan(plan *hop.Plan, porcelainMode bool) {
	if porcelainMode {
		for _, a := range plan.Actions {
			status := "ok"
			if a.Kind != hop.ActionNoOp {
				status = "repaired"
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", status, a.WorktreePath, a.Kind.String(), a.OldValue, a.NewValue)
		}
		return
	}
	fmt.Printf("Repair plan for %s:\n", plan.HubPath)
	if len(plan.Actions) == 0 && len(plan.Warnings) == 0 {
		fmt.Println("  (nothing to do)")
		return
	}
	for _, a := range plan.Actions {
		fmt.Printf("  %-15s %s — %s\n", a.Kind.String(), a.WorktreePath, a.Reason)
	}
	for _, w := range plan.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
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
//   - GIT_HOP_REPO_ID       — "github.com/<org>/<repo>" when resolvable,
//     otherwise empty. See repairHookRepoID.
//
// The repo ID is resolved at dispatch time rather than passed in, which
// makes the pre/post asymmetry fall out naturally: pre-repair reads the
// possibly-damaged hub config, post-repair reads the repaired one.
func runRepairHook(fs afero.Fs, name, hubPath string) error {
	_, err := hooks.NewRunner(fs).ExecuteHook(name, hubPath, repairHookRepoID(fs, hubPath), "")
	return err
}

// repairHookRepoID builds the 3-part repo identifier hooks.Runner needs
// for hopspace-level hook lookup, reading the hub config as it stands at
// the moment of the call.
//
// Returns "" when the hub config is missing, unreadable, or lacks org or
// repo. A partial ID is worse than none: the runner splits on "/" and
// needs at least three segments, so "github.com//" would fail hopspace
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
	org, repo := hub.Config.Repo.Org, hub.Config.Repo.Repo
	if org == "" || repo == "" {
		return ""
	}
	return fmt.Sprintf("github.com/%s/%s", org, repo)
}

// fatal returns an error that the cobra layer surfaces with exit 128.
func fatal(msg string) error {
	fmt.Fprintf(os.Stderr, "fatal: %s\n", msg)
	os.Exit(exitFatal)
	return nil
}

// opErr formats a non-fatal operation failure for exit 1 and returns
// the cobra-friendly error so cobra also reports it through SilenceUsage.
func opErr(msg string) error {
	fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	os.Exit(exitOp)
	return nil
}
