package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
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

	// 6. pre-repair hook (best-effort: skip if hooks package can't be wired
	// from a non-worktree cwd; the existing hook runner is bound to git
	// repos, so a missing-hook is silent here).
	if abort := firePreRepairHook(hubPath); abort != nil {
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
	_ = firePostRepairHook(hubPath)

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

// firePreRepairHook invokes the pre-repair hook if installed at
// $XDG_CONFIG_HOME/git-hop/hooks/pre-repair. Returns non-nil error to
// abort. Runs synchronously; output flows to caller's stderr.
func firePreRepairHook(hubPath string) error {
	return runRepairHook("pre-repair", hubPath)
}

// firePostRepairHook fires the advisory post-repair hook. Errors are
// swallowed by the caller; we still return them for symmetry.
func firePostRepairHook(hubPath string) error {
	return runRepairHook("post-repair", hubPath)
}

func runRepairHook(name, hubPath string) error {
	hookPath := filepath.Join(hop.GetHooksDir(), name)
	info, err := os.Stat(hookPath)
	if err != nil || info.IsDir() {
		return nil
	}
	c := exec.Command(hookPath)
	c.Dir = hubPath
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	return c.Run()
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
