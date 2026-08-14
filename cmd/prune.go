package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
	Aliases: []string{"cleanup", "clean"},
	Short:   "Remove orphaned worktrees and hubs from state and hop.json",
	Long: `Remove worktrees and hubs that no longer exist on the filesystem.

By default only the current repository is pruned — the one whose hub
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

Every line naming a pruned entry is prefixed with the repository it
belongs to, so a --all sweep shows exactly which repositories it touched.

hop.json is backed up to .hop/backups/repair-<timestamp>Z before any
entry is dropped, so a prune can be undone with 'git hop repair --undo'.
Removals from the state file are not recoverable from the CLI.

Use the global --dry-run flag to preview what would be pruned without
making changes.
`,
	Run: runPrune,
}

func init() {
	pruneCmd.Flags().Bool("all", false, "prune every repository in state, not just the current one")
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
		output.Info("Nothing in scope to prune.")
		return
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if all {
		output.Info("Scanning all repositories for orphaned entries...")
	} else {
		output.Info("Scanning %s for orphaned entries...", scopeRepoIDs(scoped)[0])
	}

	counts := runPruneAll(fs, g, scoped, dryRun)

	if !dryRun && (counts.worktrees > 0 || counts.hubs > 0) {
		// scoped shares its *RepositoryState pointers with st, so the
		// in-place edits above are already visible in st; save the full
		// state so out-of-scope repositories are preserved verbatim.
		if err := state.SaveState(fs, st); err != nil {
			output.Fatal("Failed to save state: %v", err)
		}
	}

	switch {
	case counts.total() == 0:
		output.Success("No orphaned entries found.")
	case dryRun:
		output.Success("[dry-run] Would prune %d worktree(s), %d hub(s), %d hop.json entry(ies), and %d repair backup(s)",
			counts.worktrees, counts.hubs, counts.hopJSONEntries, counts.repairBackups)
	default:
		output.Success("Pruned %d worktree(s), %d hub(s), %d hop.json entry(ies), and %d repair backup(s)",
			counts.worktrees, counts.hubs, counts.hopJSONEntries, counts.repairBackups)
	}
}

// resolvePruneScope narrows st to the repositories prune is allowed to
// mutate, and is the single guard covering every prune pass: each pass
// ranges over the *state.State it is handed, so scoping once here scopes
// all of them.
//
// prune deletes state, and state deletion is not undoable from the CLI
// (the hop.json half snapshots to .hop/backups, the state.json half does
// not). A repo-local invocation must therefore not reach a sibling
// repository: running prune inside repo A used to drop repo B's
// registration, which surfaced only later as "repository not found".
//
// With all set, st is returned as-is — the deliberate global sweep, and
// the same pointer so the caller's save persists the mutations.
//
// Without it the repository is resolved exactly as 'git hop list' does:
// walk up from cwd to the enclosing hub, load its hop.json, and build the
// repo ID as github.com/<org>/<repo>. Not in a hub, or in a hub whose repo
// has no state entry, is an error — never a silent fallback to global.
//
// The returned state shares its *RepositoryState pointers with st, so the
// passes mutate the real entries; only the map of what is visible narrows.
func resolvePruneScope(fs afero.Fs, st *state.State, cwd string, all bool) (*state.State, error) {
	if all {
		return st, nil
	}

	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		return nil, fmt.Errorf("not inside a git-hop repository: %s\n"+
			"hint: run prune from a repository, or pass --all to prune every repository in state", cwd)
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read hub config at %s: %v\n"+
			"hint: pass --all to prune every repository in state", hubPath, err)
	}

	repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
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
// count here is a lie to the user.
type pruneCounts struct {
	worktrees      int
	hubs           int
	hopJSONEntries int
	repairBackups  int
}

func (c pruneCounts) total() int {
	return c.worktrees + c.hubs + c.hopJSONEntries + c.repairBackups
}

// runPruneAll performs every prune pass against st and returns the
// counts. Mutations to st are in-memory; the caller persists.
//
// Ordering matters: hop.json entries are pruned first because the hub
// entries in st are what tell us which hop.json files to visit, and
// pruneOrphanedHubs drops those same entries from st in the pass right
// after.
func runPruneAll(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool) pruneCounts {
	var c pruneCounts
	c.hopJSONEntries = pruneOrphanedHubBranches(fs, g, st, dryRun)
	c.worktrees, c.hubs = runPruneFS(fs, st, dryRun)
	c.repairBackups = pruneRepairBackups(fs, g, st, dryRun)
	return c
}

// pruneRepairBackups removes repair backup directories older than the
// configured retention. Returns the count removed (or that would be
// removed when dryRun is true).
//
// Retention is read from `git config --get hop.repair.backupRetention`
// from any in-scope hub; falls back to 30 days when unconfigured. The
// value uses Go duration syntax (e.g. "720h" for 30 days, "168h" for 7).
func pruneRepairBackups(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool) int {
	retention := repairBackupRetention(g, st)
	cutoff := time.Now().Add(-retention)
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}
	pruned := 0
	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		for _, hub := range repo.Hubs {
			backupsDir := filepath.Join(hub.Path, ".hop", "backups")
			entries, err := afero.ReadDir(fs, backupsDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "repair-") {
					continue
				}
				path := filepath.Join(backupsDir, entry.Name())
				if entry.ModTime().After(cutoff) {
					continue
				}
				output.Info("%s repair backup: %s (%s)", prefix, repoID, path)
				if !dryRun {
					_ = fs.RemoveAll(path)
				}
				pruned++
			}
		}
	}
	return pruned
}

// repairBackupRetention reads hop.repair.backupRetention from the first
// in-scope hub that has it configured, falling back to 30 days. Ranging
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

// runPruneFS scans st for orphaned worktrees and hubs and returns the counts.
// When dryRun is false the orphans are removed from st in place; the caller
// is responsible for persisting. When dryRun is true st is left untouched.
func runPruneFS(fs afero.Fs, st *state.State, dryRun bool) (worktrees, hubs int) {
	worktrees = pruneOrphanedWorktrees(fs, st, dryRun)
	hubs = pruneOrphanedHubs(fs, st, dryRun)
	return
}

// pruneOrphanedWorktrees reports worktrees whose paths no longer exist.
// When dryRun is false it also removes them from st.
func pruneOrphanedWorktrees(fs afero.Fs, st *state.State, dryRun bool) int {
	pruned := 0
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}

	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		for branch, wt := range repo.Worktrees {
			if exists, _ := afero.DirExists(fs, wt.Path); !exists {
				output.Info("%s orphaned worktree: %s:%s (%s)", prefix, repoID, branch, wt.Path)
				if !dryRun {
					delete(repo.Worktrees, branch)
				}
				pruned++
			}
		}
	}

	return pruned
}

// pruneOrphanedHubs reports hubs whose directories no longer exist.
// When dryRun is false it also removes them from st.
func pruneOrphanedHubs(fs afero.Fs, st *state.State, dryRun bool) int {
	pruned := 0
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}

	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		var validHubs []*state.HubState

		for _, hub := range repo.Hubs {
			if exists, _ := afero.DirExists(fs, hub.Path); exists {
				validHubs = append(validHubs, hub)
			} else {
				output.Info("%s orphaned hub: %s (%s)", prefix, repoID, hub.Path)
				pruned++
			}
		}

		if !dryRun {
			repo.Hubs = validHubs
		}
	}

	return pruned
}
