package cmd

import (
	"fmt"
	"os"
	"sort"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"hop.top/git/internal/tui"
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
	g := git.New()

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
		showRepositoryWorktrees(fs, g, currentRepoID, st.Repositories[currentRepoID])
		return
	}

	// Otherwise show all repositories
	showAllRepositories(fs, g, st)
}

func showRepositoryWorktrees(fs afero.Fs, g git.GitInterface, repoID string, repo *state.RepositoryState) {
	if output.CurrentMode == output.ModeHuman {
		fmt.Println(output.RenderHeader("Repository: " + repoID))
		fmt.Println()
	} else {
		output.Info("Repository: %s", repoID)
		output.Info("")
	}

	if len(repo.Worktrees) == 0 {
		output.Info("No worktrees found.")
		return
	}

	if output.CurrentMode != output.ModeHuman {
		// Use old table for non-human modes
		t := tui.NewTable([]interface{}{"Branch", "Type", "Path", "State", "Status"})
		for branch, wt := range repo.Worktrees {
			state := "missing"
			sync := "-"
			if exists, _ := afero.DirExists(fs, wt.Path); exists {
				state = "active"
				sync = getBranchSyncStatus(g, wt.Path, branch, repo.DefaultBranch)
			}
			t.AddRow(branch, wt.Type, wt.Path, state, sync)
		}
		t.Render()
		return
	}

	// Enhanced table for human mode
	table := output.NewStatusTable("Branch", "Type", "Path", "State", "Status")

	// Sort branches for consistent output
	var branches []string
	for branch := range repo.Worktrees {
		branches = append(branches, branch)
	}
	sort.Strings(branches)

	activeCount := 0
	missingCount := 0

	for _, branch := range branches {
		wt := repo.Worktrees[branch]
		exists, _ := afero.DirExists(fs, wt.Path)

		status := "error"
		stateText := "missing"
		sync := "-"
		if exists {
			status = "success"
			stateText = "active"
			sync = getBranchSyncStatus(g, wt.Path, branch, repo.DefaultBranch)
			activeCount++
		} else {
			missingCount++
		}

		table.AddRow(status, branch, wt.Type, wt.Path, stateText, sync)
	}

	table.Print()

	// Summary
	fmt.Println()
	summary := fmt.Sprintf("Summary: %d worktrees", len(repo.Worktrees))
	if activeCount > 0 {
		summary += fmt.Sprintf(" · %d active", activeCount)
	}
	if missingCount > 0 {
		summary += output.StyleWarning.Render(fmt.Sprintf(" · %d missing", missingCount))
	}
	fmt.Println(summary)
}

func showAllRepositories(fs afero.Fs, g git.GitInterface, st *state.State) {
	if output.CurrentMode == output.ModeHuman {
		fmt.Println(output.RenderHeader("All Repositories"))
		fmt.Println()
	} else {
		output.Info("All Repositories:")
		output.Info("")
	}

	if output.CurrentMode != output.ModeHuman {
		// Use old table for non-human modes
		t := tui.NewTable([]interface{}{"Repository", "Branch", "Type", "Path", "State", "Status"})

		var repoIDs []string
		for repoID := range st.Repositories {
			repoIDs = append(repoIDs, repoID)
		}
		sort.Strings(repoIDs)

		for _, repoID := range repoIDs {
			repo := st.Repositories[repoID]
			var branches []string
			for branch := range repo.Worktrees {
				branches = append(branches, branch)
			}
			sort.Strings(branches)

			for _, branch := range branches {
				wt := repo.Worktrees[branch]
				state := "missing"
				sync := "-"
				if exists, _ := afero.DirExists(fs, wt.Path); exists {
					state = "active"
					sync = getBranchSyncStatus(g, wt.Path, branch, repo.DefaultBranch)
				}
				t.AddRow(repoID, branch, wt.Type, wt.Path, state, sync)
			}
		}
		t.Render()
		return
	}

	// Enhanced table for human mode
	table := output.NewStatusTable("Repository", "Branch", "Type", "State", "Status")

	// Sort repositories for consistent output
	var repoIDs []string
	for repoID := range st.Repositories {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)

	totalWorktrees := 0
	activeCount := 0
	missingCount := 0

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
			totalWorktrees++

			exists, _ := afero.DirExists(fs, wt.Path)
			status := "error"
			stateText := "missing"
			sync := "-"
			if exists {
				status = "success"
				stateText = "active"
				sync = getBranchSyncStatus(g, wt.Path, branch, repo.DefaultBranch)
				activeCount++
			} else {
				missingCount++
			}

			// Shorten repo ID for display
			shortRepo := repoID
			if len(shortRepo) > 30 {
				shortRepo = "..." + shortRepo[len(shortRepo)-27:]
			}

			table.AddRow(status, shortRepo, branch, wt.Type, stateText, sync)
		}
	}

	table.Print()

	// Summary
	fmt.Println()
	summary := fmt.Sprintf("Summary: %d worktrees across %d repositories", totalWorktrees, len(repoIDs))
	if activeCount > 0 {
		summary += fmt.Sprintf(" · %d active", activeCount)
	}
	if missingCount > 0 {
		summary += output.StyleWarning.Render(fmt.Sprintf(" · %d missing", missingCount))
	}
	fmt.Println(summary)

	// Legend
	fmt.Println()
	legend := output.Legend(map[string]string{
		output.ColorizeIcon(output.IconSuccess, "success"): "Active",
		output.ColorizeIcon(output.IconError, "error"):     "Missing",
	})
	fmt.Println(legend)
}

// loadStateOrLegacy loads state.json, returning an empty state if not found.
func loadStateOrLegacy(fs afero.Fs) (*state.State, error) {
	st, err := state.LoadState(fs)
	if err == nil && len(st.Repositories) > 0 {
		return st, nil
	}
	return state.NewState(), nil
}

func init() {
	cli.RootCmd.AddCommand(listCmd)
}
