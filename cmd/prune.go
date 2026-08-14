package cmd

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

var pruneCmd = &cobra.Command{
	Use:     "prune",
	Aliases: []string{"cleanup", "clean"},
	Short:   "Remove orphaned worktrees and hubs from state and hop.json",
	Long: `Remove worktrees and hubs that no longer exist on the filesystem.

This command scans the state file and each hub's hop.json, removing:
  - Worktrees whose paths no longer exist
  - Hubs whose directories have been deleted
  - hop.json branch entries whose worktree directory is gone
    (the rows 'git hop status' reports as Missing)
  - Repair backups older than hop.repair.backupRetention

hop.json is backed up to .hop/backups/repair-<timestamp>Z before any
entry is dropped, so a prune can be undone with 'git hop repair --undo'.

Use --dry-run to preview what would be pruned without making changes.
`,
	Run: runPrune,
}

func init() {
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

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	output.Info("Scanning for orphaned entries...")

	counts := runPruneAll(fs, g, st, dryRun)

	if !dryRun && (counts.worktrees > 0 || counts.hubs > 0) {
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
	c.repairBackups = pruneRepairBackups(fs, st, dryRun)
	return c
}

// pruneRepairBackups removes repair backup directories older than the
// configured retention. Returns the count removed (or that would be
// removed when dryRun is true).
//
// Retention is read from `git config --get hop.repair.backupRetention`
// from any hub in state; falls back to 30 days when unconfigured. The
// value uses Go duration syntax (e.g. "720h" for 30 days, "168h" for 7).
func pruneRepairBackups(fs afero.Fs, st *state.State, dryRun bool) int {
	retention := repairBackupRetention(st)
	cutoff := time.Now().Add(-retention)
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}
	pruned := 0
	for _, repo := range st.Repositories {
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
				output.Info("%s repair backup: %s", prefix, path)
				if !dryRun {
					_ = fs.RemoveAll(path)
				}
				pruned++
			}
		}
	}
	return pruned
}

func repairBackupRetention(st *state.State) time.Duration {
	const fallback = 30 * 24 * time.Hour
	g := git.New()
	for _, repo := range st.Repositories {
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

	for repoID, repo := range st.Repositories {
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

	for repoID, repo := range st.Repositories {
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
