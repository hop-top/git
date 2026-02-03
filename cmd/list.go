package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/state"
	"github.com/jadb/git-hop/internal/tui"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "all"},
	Short:   "List all managed worktrees",
	Long: `List all worktrees tracked by git-hop.

Shows worktrees from the state file with their paths, types, and last access times.
Can list worktrees for current repository or all repositories.
`,
	Run: runList,
}

func runList(cmd *cobra.Command, args []string) {
	fs := afero.NewOsFs()

	// Load state
	st, err := loadStateOrLegacy(fs)
	if err != nil {
		output.Fatal("Failed to load state: %v", err)
	}

	// Check if we're in a hub to filter by current repo
	cwd, _ := os.Getwd()
	var currentRepoID string
	if hubPath, err := hop.FindHub(fs, cwd); err == nil {
		// Try to determine repo ID from hub
		if hub, err := hop.LoadHub(fs, hubPath); err == nil {
			currentRepoID = fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
		}
	}

	if len(st.Repositories) == 0 {
		output.Info("No worktrees found.")
		output.Info("\nRun 'git hop migrate' if you have existing data to migrate.")
		return
	}

	// If in a specific repo, show detailed view
	if currentRepoID != "" && st.Repositories[currentRepoID] != nil {
		showRepositoryWorktrees(fs, currentRepoID, st.Repositories[currentRepoID])
		return
	}

	// Otherwise show all repositories
	showAllRepositories(fs, st)
}

func showRepositoryWorktrees(fs afero.Fs, repoID string, repo *state.RepositoryState) {
	output.Info("Repository: %s", repoID)
	output.Info("")

	if len(repo.Worktrees) == 0 {
		output.Info("No worktrees found.")
		return
	}

	t := tui.NewTable([]interface{}{"Branch", "Type", "Path", "Status"})

	for branch, wt := range repo.Worktrees {
		status := "missing"
		if exists, _ := afero.DirExists(fs, wt.Path); exists {
			status = "active"
		}

		t.AddRow(branch, wt.Type, wt.Path, status)
	}

	t.Render()
}

func showAllRepositories(fs afero.Fs, st *state.State) {
	output.Info("All Repositories:")
	output.Info("")

	t := tui.NewTable([]interface{}{"Repository", "Branch", "Type", "Path", "Status"})

	// Sort repositories for consistent output
	var repoIDs []string
	for repoID := range st.Repositories {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)

	for _, repoID := range repoIDs {
		repo := st.Repositories[repoID]

		// Sort branches
		var branches []string
		for branch := range repo.Worktrees {
			branches = append(branches, branch)
		}
		sort.Strings(branches)

		for _, branch := range branches {
			wt := repo.Worktrees[branch]
			status := "missing"
			if exists, _ := afero.DirExists(fs, wt.Path); exists {
				status = "active"
			}

			t.AddRow(repoID, branch, wt.Type, wt.Path, status)
		}
	}

	t.Render()
}

// loadStateOrLegacy loads state.json, or falls back to legacy registry
func loadStateOrLegacy(fs afero.Fs) (*state.State, error) {
	// Try to load new state first
	st, err := state.LoadState(fs)
	if err == nil && len(st.Repositories) > 0 {
		return st, nil
	}

	// Fall back to legacy registry and auto-migrate
	registry := hop.LoadRegistry(fs)
	if registry.Config != nil && len(registry.Config.Hops) > 0 {
		output.Warn("Found legacy data. Auto-migrating...")
		newState := state.NewState()
		if err := hop.MigrateRegistry(fs, registry, newState); err != nil {
			return nil, fmt.Errorf("auto-migration failed: %w", err)
		}

		// Save migrated state
		if err := state.SaveState(fs, newState); err != nil {
			return nil, fmt.Errorf("failed to save migrated state: %w", err)
		}

		output.Success("Auto-migration complete.")
		return newState, nil
	}

	// Return empty state if nothing found
	return state.NewState(), nil
}

func init() {
	cli.RootCmd.AddCommand(listCmd)
}
