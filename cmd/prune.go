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

	// Load state
	st, err := state.LoadState(fs)
	if err != nil {
		output.Fatal("Failed to load state: %v", err)
	}

	if len(st.Repositories) == 0 {
		output.Info("No repositories in state. Nothing to prune.")
		return
	}

	output.Info("Scanning for orphaned entries...")

	// Prune orphaned worktrees
	worktreesPruned := pruneOrphanedWorktrees(fs, st)

	// Prune orphaned hubs
	hubsPruned := pruneOrphanedHubs(fs, st)

	// Save updated state
	if worktreesPruned > 0 || hubsPruned > 0 {
		if err := state.SaveState(fs, st); err != nil {
			output.Fatal("Failed to save state: %v", err)
		}
	}

	// Report results
	if worktreesPruned == 0 && hubsPruned == 0 {
		output.Success("No orphaned entries found.")
	} else {
		output.Success("Pruned %d worktree(s) and %d hub(s)", worktreesPruned, hubsPruned)
	}
}

// pruneOrphanedWorktrees removes worktrees whose paths no longer exist
func pruneOrphanedWorktrees(fs afero.Fs, st *state.State) int {
	pruned := 0

	for repoID, repo := range st.Repositories {
		for branch, wt := range repo.Worktrees {
			if exists, _ := afero.DirExists(fs, wt.Path); !exists {
				output.Info("Pruning orphaned worktree: %s:%s (%s)", repoID, branch, wt.Path)
				delete(repo.Worktrees, branch)
				pruned++
			}
		}
	}

	return pruned
}

// pruneOrphanedHubs removes hubs whose directories no longer exist
func pruneOrphanedHubs(fs afero.Fs, st *state.State) int {
	pruned := 0

	for repoID, repo := range st.Repositories {
		var validHubs []*state.HubState

		for _, hub := range repo.Hubs {
			if exists, _ := afero.DirExists(fs, hub.Path); exists {
				validHubs = append(validHubs, hub)
			} else {
				output.Info("Pruning orphaned hub: %s (%s)", repoID, hub.Path)
				pruned++
			}
		}

		repo.Hubs = validHubs
	}

	return pruned
}
