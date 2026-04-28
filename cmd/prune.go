package cmd

import (
	"hop.top/git/internal/cli"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:     "prune",
	Aliases: []string{"cleanup", "clean"},
	Short:   "Remove orphaned worktrees and hubs from state",
	Long: `Remove worktrees and hubs that no longer exist on the filesystem.

This command scans the state file and removes entries for:
  - Worktrees whose paths no longer exist
  - Hubs whose directories have been deleted
  - Orphaned entries that have been cleaned up

Use --dry-run to preview what would be pruned without making changes.
`,
	Run: runPrune,
}

func init() {
	cli.RootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) {
	fs := afero.NewOsFs()

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

	worktreesPruned, hubsPruned := runPruneFS(fs, st, dryRun)

	if !dryRun && (worktreesPruned > 0 || hubsPruned > 0) {
		if err := state.SaveState(fs, st); err != nil {
			output.Fatal("Failed to save state: %v", err)
		}
	}

	switch {
	case worktreesPruned == 0 && hubsPruned == 0:
		output.Success("No orphaned entries found.")
	case dryRun:
		output.Success("[dry-run] Would prune %d worktree(s) and %d hub(s)", worktreesPruned, hubsPruned)
	default:
		output.Success("Pruned %d worktree(s) and %d hub(s)", worktreesPruned, hubsPruned)
	}
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
