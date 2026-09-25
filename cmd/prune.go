package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

var pruneCmd = &cobra.Command{
	Use:     "prune",
	Args:    cobra.NoArgs,
	GroupID: cli.GroupManagement,
	Aliases: []string{"cleanup", "clean"},
	Short:   "Remove orphaned worktrees and hubs from state and hop.json",
	Long: `Remove worktrees and hubs that no longer exist on the filesystem.

By default only the current repository is pruned: the one whose hub
contains the working directory. Pass --all to sweep every repository in
the state file. Outside any known repository prune refuses to run rather
than defaulting to a global sweep; use --all there if that is the intent.

For the repositories in scope this command reads the state file and each
hub's hop.json, removing:
  - Worktrees whose paths no longer exist
  - Hubs whose directories have been deleted
  - hop.json branch entries whose worktree directory is gone
    (the rows 'git hop status' reports as Missing)
  - Repair backups older than hop.repair.backupRetention
  - Conversion backups 'git hop init' kept, beyond hop.backup.maxBackups
    per repository or older than hop.backup.cleanupAgeDays (backups of
    failed conversions are never removed)
  - Temp files a save of hop.json left beside it when its run died
    before renaming them into place, once an hour old; with --all, a
    save of the state file's too. A save running at the time (it holds
    the file's lock) leaves them for a later prune

A worktree git has locked ('git worktree lock') is kept even when its
directory is missing, as 'git worktree prune' keeps it: prune reports
its state entry and hop.json row as skipped and names the lock reason.

Every line naming a pruned entry is prefixed with the repository it
belongs to, so a --all sweep shows exactly which repositories it touched.

hop.json is backed up to the per-hub repair state dir
($XDG_STATE_HOME/git-hop/repair/<hub>/backups/repair-<timestamp>Z) before
any entry is dropped, so a prune can be undone with 'git hop repair --undo'.
Removals from the state file are not recoverable from the CLI.

Hubs still carrying the .hop/ directory an earlier release created for
its lock and backups are tidied on the way: surviving snapshots move to
the state dir, a stale lock file is removed, and .hop/ itself is removed
only once it is completely empty.

Use the global --dry-run flag to preview what would be pruned without
making changes.
`,
	Run: runPrune,
}

func init() {
	pruneCmd.Flags().Bool("all", false, "prune every repository in state, not just the current one")
	declareOutputSchema(pruneCmd, &[]pruneRecord{})
	cli.RootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) {
	fs := afero.NewOsFs()
	g := git.New()

	st, err := state.LoadState(fs)
	if err != nil {
		output.Fatal("Failed to load state: %v", err)
	}

	if len(st.Repositories) == 0 {
		if output.IsStructured() {
			emitResult(cmd, []pruneRecord{})
			return
		}
		output.Info("No repositories in state. Nothing to prune.")
		return
	}

	all, _ := cmd.Flags().GetBool("all")
	cwd, err := os.Getwd()
	if err != nil {
		output.Fatal("Failed to determine working directory: %v", err)
	}

	scoped, err := resolvePruneScope(fs, st, cwd, all)
	if err != nil {
		output.Fatal("%v", err)
	}

	if len(scoped.Repositories) == 0 {
		if output.IsStructured() {
			emitResult(cmd, []pruneRecord{})
			return
		}
		output.Info("Nothing in scope to prune.")
		return
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if all {
		output.Info("Scanning all repositories for orphaned entries...")
	} else {
		output.Info("Scanning %s for orphaned entries...", scopeRepoIDs(scoped)[0])
	}

	counts, err := pruneAndSave(fs, g, st, scoped, all, dryRun)
	if err != nil {
		output.Fatal("Failed to save state: %v", err)
	}

	if output.IsStructured() {
		emitResult(cmd, counts.records)
		return
	}

	switch {
	case counts.total() == 0:
		output.Success("No orphaned entries found.")
	case dryRun:
		output.Success("[dry-run] Would prune %d worktree(s), %d hub(s), %d hop.json entry(ies), %d repair backup(s), %d conversion backup(s), %d state backup(s), and %d temp file(s)",
			counts.worktrees, counts.hubs, counts.hopJSONEntries, counts.repairBackups, counts.conversionBackups, counts.stateBackups, counts.tempFiles)
	default:
		output.Success("Pruned %d worktree(s), %d hub(s), %d hop.json entry(ies), %d repair backup(s), %d conversion backup(s), %d state backup(s), and %d temp file(s)",
			counts.worktrees, counts.hubs, counts.hopJSONEntries, counts.repairBackups, counts.conversionBackups, counts.stateBackups, counts.tempFiles)
	}
}

// pruneAndSave runs every prune pass over scoped, the part of st in
// scope (resolvePruneScope), and saves what they removed from state.
//
// The passes run git and take their time, so they work on the state
// loaded before them, without the state lock. What they removed is then
// removed from state.json as it is by the time of the save (stateEdits),
// so entries another run recorded or dropped during the scan stay as
// that run left them.
func pruneAndSave(fs afero.Fs, g git.GitInterface, st, scoped *state.State, all, dryRun bool) (pruneCounts, error) {
	var edits stateEdits
	counts := runPruneAll(fs, g, scoped, dryRun, &edits)
	if all {
		// State backups belong to no one repository, so only a prune of
		// every repository ages them out.
		counts.addStateBackups(pruneStateBackups(fs, g, st, dryRun))
		counts.addTempFiles(pruneStateTemps(fs, dryRun))
	}
	return counts, edits.save(fs)
}

// resolvePruneScope narrows st to the repositories prune is allowed to
// mutate, and is the single guard covering every prune pass: each pass
// ranges over the *state.State it is handed, so scoping once here scopes
// all of them.
//
// prune deletes state, and state deletion is not undoable from the CLI
// (the hop.json half snapshots to the repair state dir, the state.json half does
// not). A repo-local invocation must therefore not reach a sibling
// repository: running prune inside repo A used to drop repo B's
// registration, which surfaced only later as "repository not found".
//
// With all set, st is returned as-is — the deliberate global sweep, and
// the same pointer so the caller's save persists the mutations.
//
// Without it the repository is resolved exactly as 'git hop list' does,
// through hop.ResolveHub: the enclosing hub's hop.json, or failing that a
// hub state records around cwd (a repository registered as-is has no
// hop.json). Not in a hub, or in a hub whose repo has no state entry, is
// an error — never a silent fallback to global.
//
// The returned state shares its *RepositoryState pointers with st, so the
// passes mutate the real entries; only the map of what is visible narrows.
func resolvePruneScope(fs afero.Fs, st *state.State, cwd string, all bool) (*state.State, error) {
	if all {
		return st, nil
	}

	ref, err := hop.ResolveHub(fs, st, cwd)
	if errors.Is(err, hop.ErrNotInHub) {
		return nil, fmt.Errorf("not inside a git-hop repository: %s\n"+
			"hint: run prune from a repository, or pass --all to prune every repository in state", cwd)
	}
	if err != nil {
		return nil, fmt.Errorf("%v\n"+
			"hint: pass --all to prune every repository in state", err)
	}

	repoID := ref.RepoID
	repo, ok := st.Repositories[repoID]
	if !ok || repo == nil {
		return nil, fmt.Errorf("repository %s is not registered in state\n"+
			"hint: pass --all to prune every repository in state", repoID)
	}

	return &state.State{
		Version:      st.Version,
		LastUpdated:  st.LastUpdated,
		Repositories: map[string]*state.RepositoryState{repoID: repo},
		Orphaned:     st.Orphaned,
	}, nil
}

// scopeRepoIDs returns the repository IDs in scope, sorted, so messages
// render deterministically.
func scopeRepoIDs(st *state.State) []string {
	ids := make([]string, 0, len(st.Repositories))
	for id := range st.Repositories {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// pruneCounts tallies each class of stale data prune reclaims. Every
// field is what genuinely landed (or, under dry-run, what would land) —
// the summary line is rendered straight from it, so an over-reported
// count here is a lie to the user. records lists the same entries, in the
// order the passes ran, as the command's structured result.
type pruneCounts struct {
	worktrees      int
	hubs           int
	hopJSONEntries int
	repairBackups  int
	// conversionBackups counts init's conversion backups aged out.
	conversionBackups int
	// stateBackups counts state.json backups aged out (prune --all).
	stateBackups int
	// tempFiles counts temp files crashed saves of hop.json (and, under
	// --all, of state.json) left behind.
	tempFiles int
	records   []pruneRecord
}

func (c pruneCounts) total() int {
	return c.worktrees + c.hubs + c.hopJSONEntries + c.repairBackups + c.conversionBackups + c.stateBackups + c.tempFiles
}

// addStateBackups adds the state backups a pass aged out.
func (c *pruneCounts) addStateBackups(records []pruneRecord) {
	c.stateBackups += len(records)
	c.records = append(c.records, records...)
}

// addTempFiles adds the temp files a sweep removed.
func (c *pruneCounts) addTempFiles(records []pruneRecord) {
	c.tempFiles += len(records)
	c.records = append(c.records, records...)
}

// runPruneAll performs every prune pass against st and returns the
// counts. Mutations to st are in-memory and recorded in edits; the
// caller persists them (stateEdits.save).
//
// Ordering matters: hop.json entries are pruned first because the hub
// entries in st are what tell us which hop.json files to visit, and
// pruneOrphanedHubs drops those same entries from st in the pass right
// after.
func runPruneAll(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool, edits *stateEdits) pruneCounts {
	hopJSON := pruneOrphanedHubBranches(fs, g, st, dryRun)
	worktrees, hubs := runPruneFS(fs, g, st, dryRun, edits)
	backups := pruneRepairBackups(fs, g, st, dryRun)
	conversions := pruneConversionBackups(fs, st, dryRun)
	temps := pruneHubTemps(fs, st, dryRun)

	records := make([]pruneRecord, 0, len(hopJSON)+len(worktrees)+len(hubs)+len(backups)+len(conversions)+len(temps))
	for _, pass := range [][]pruneRecord{hopJSON, worktrees, hubs, backups, conversions, temps} {
		records = append(records, pass...)
	}
	return pruneCounts{
		worktrees:         prunedCount(worktrees),
		hubs:              len(hubs),
		hopJSONEntries:    prunedCount(hopJSON),
		repairBackups:     len(backups),
		conversionBackups: len(conversions),
		tempFiles:         len(temps),
		records:           records,
	}
}

// Kinds of entry prune removes, as reported in pruneRecord.Kind.
const (
	pruneKindWorktree     = "worktree"
	pruneKindHub          = "hub"
	pruneKindHopJSONEntry = "hop-json-entry"
	pruneKindRepairBackup = "repair-backup"
	// pruneKindConversionBackup is a backup 'git hop init' took before
	// converting a repository.
	pruneKindConversionBackup = "conversion-backup"
	// pruneKindStateBackup is a copy of state.json taken before a save
	// migrated it to the current format.
	pruneKindStateBackup = "state-backup"
	// pruneKindTempFile is a temp file a save of hop.json or state.json
	// left behind when its run died before renaming it into place.
	pruneKindTempFile = "temp-file"
)

// pruneActionSkipped is the action of an entry prune left in place
// although its path is missing; pruneRecord.Reason says why.
const pruneActionSkipped = "skipped"

// newPruneRecord describes one entry a pass removed, or would remove
// under dryRun.
func newPruneRecord(kind, repoID, branch, path string, dryRun bool) pruneRecord {
	action := "pruned"
	if dryRun {
		action = "would-prune"
	}
	return pruneRecord{Action: action, Kind: kind, Repository: repoID, Branch: branch, Path: path}
}

// skipLockedEntry reports an entry prune leaves in place because git has
// its worktree locked (lockReason is git's reason, "" when none), and
// returns its record. label names the entry ("hop.json entry", "orphaned
// worktree") in the human line. The same happens under --dry-run: a
// preview skips it too.
func skipLockedEntry(kind, label, repoID, branch, path, lockReason string) pruneRecord {
	reason := "locked in git"
	if lockReason != "" {
		reason += ": " + lockReason
	}
	output.Info("Skipping %s: %s:%s (%s): %s", label, repoID, branch, path, reason)
	output.Hint("%s", unlockHint(path))
	return pruneRecord{Action: pruneActionSkipped, Kind: kind, Repository: repoID, Branch: branch, Path: path, Reason: reason}
}

// prunedCount is how many of records a pass removed (or would remove),
// leaving out the entries it skipped.
func prunedCount(records []pruneRecord) int {
	n := 0
	for _, rec := range records {
		if rec.Action != pruneActionSkipped {
			n++
		}
	}
	return n
}

// repairBackupRetention reads hop.repair.backupRetention from the first
// in-scope hub that has it configured, falling back to 30 days. A value
// that is not a Go duration is skipped. Zero or less is returned as is:
// it means pruning is off, which pruneRepairBackups honours. Ranging
// over the scoped state matters: reading the setting from an unrelated
// repository would silently apply repo B's retention to repo A's backups.
// Repositories are visited in sorted order so the answer is deterministic
// when several hubs configure it.
func repairBackupRetention(g git.GitInterface, st *state.State) time.Duration {
	const fallback = 30 * 24 * time.Hour
	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		for _, hub := range repo.Hubs {
			val, err := g.GetConfig(hub.Path, "hop.repair.backupRetention")
			if err != nil || val == "" {
				continue
			}
			if d, err := time.ParseDuration(strings.TrimSpace(val)); err == nil {
				return d
			}
		}
	}
	return fallback
}

// runPruneFS scans st for orphaned worktrees and hubs and returns them.
// When dryRun is false the orphans are removed from st in place and the
// removals recorded in edits; the caller is responsible for persisting.
// When dryRun is true st is left untouched.
func runPruneFS(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool, edits *stateEdits) (worktrees, hubs []pruneRecord) {
	worktrees = pruneOrphanedWorktrees(fs, g, st, dryRun, edits)
	hubs = pruneOrphanedHubs(fs, st, dryRun, edits)
	return
}

// pruneOrphanedWorktrees reports worktrees whose paths no longer exist,
// in repository then branch order (state.SortedWorktreeKeys). When dryRun is false it also removes
// them from st, recording each removal in edits.
//
// A worktree git has locked is kept and reported skipped
// (skipLockedEntry): like `git worktree prune`, prune leaves it for the
// user to unlock.
func pruneOrphanedWorktrees(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool, edits *stateEdits) []pruneRecord {
	var pruned []pruneRecord
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}

	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		for _, key := range repo.SortedWorktreeKeys() {
			wt := repo.Worktrees[key]
			branch := wt.Branch
			if !hop.WorktreeDirPresent(fs, wt.Path) {
				if reason, locked := stateWorktreeLock(fs, g, repoID, repo.URI, wt); locked {
					pruned = append(pruned, skipLockedEntry(pruneKindWorktree, "orphaned worktree", repoID, branch, wt.Path, reason))
					continue
				}
				output.Info("%s orphaned worktree: %s:%s (%s)", prefix, repoID, branch, wt.Path)
				if !dryRun {
					edits.dropWorktree(st, repoID, key)
				}
				pruned = append(pruned, newPruneRecord(pruneKindWorktree, repoID, branch, wt.Path, dryRun))
			}
		}
	}

	return pruned
}

// pruneOrphanedHubs reports hubs whose directories no longer exist.
// When dryRun is false it also removes them from st, recording each
// removal in edits.
func pruneOrphanedHubs(fs afero.Fs, st *state.State, dryRun bool, edits *stateEdits) []pruneRecord {
	var pruned []pruneRecord
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}

	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		var gone []string
		for _, hub := range repo.Hubs {
			if exists, _ := afero.DirExists(fs, hub.Path); !exists {
				output.Info("%s orphaned hub: %s (%s)", prefix, repoID, hub.Path)
				pruned = append(pruned, newPruneRecord(pruneKindHub, repoID, "", hub.Path, dryRun))
				gone = append(gone, hub.Path)
			}
		}

		if !dryRun {
			for _, path := range gone {
				edits.dropHub(st, repoID, path)
			}
		}
	}

	return pruned
}
