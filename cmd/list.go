package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"hop.top/git/internal/tui"
)

// displayBase renders the per-branch compare branch for the Base column.
// Returns "-" when the compare branch equals the repo default — the
// vast majority of rows fall into that case, and repeating the default
// branch name in every row adds noise without information. Only
// branches with an explicit non-default base show a value.
func displayBase(compare, defaultBranch string) string {
	if compare == "" || compare == defaultBranch {
		return "-"
	}
	return compare
}

// compareBranchesForRepo resolves the comparison branch for every
// worktree in repo.Worktrees by loading the hub(s) tracked in state. The
// result maps the worktree's key → compare branch (per
// resolveCompareBranch precedence). When the hub can't be loaded
// (deleted, corrupted) we fall back to repo.DefaultBranch — callers still
// get a usable label, just not the per-branch override.
func compareBranchesForRepo(fs afero.Fs, repo *state.RepositoryState) map[string]string {
	out := make(map[string]string, len(repo.Worktrees))
	type hubKey string
	cache := map[hubKey]*config.HubConfig{}
	for key, wt := range repo.Worktrees {
		if wt == nil {
			continue
		}
		branch := wt.Branch
		k := hubKey(wt.HubPath)
		hubCfg, seen := cache[k]
		if !seen {
			if hub, err := hop.LoadHub(fs, wt.HubPath); err == nil {
				hubCfg = hub.Config
			}
			cache[k] = hubCfg // may be nil
		}
		if hubCfg == nil {
			out[key] = repo.DefaultBranch
			continue
		}
		b, ok := hubCfg.Branches[branch]
		if !ok {
			out[key] = repo.DefaultBranch
			continue
		}
		out[key] = resolveCompareBranch(hubCfg, b)
	}
	return out
}

var listCmd = &cobra.Command{
	Use:     "list",
	Args:    cobra.NoArgs,
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

	// Inside a hub, scope to its repository.
	cwd, _ := os.Getwd()
	var currentRepoID string
	if ref, err := hop.ResolveHub(fs, st, cwd); err == nil {
		currentRepoID = ref.RepoID
	}

	if output.IsStructured() {
		// Inside a hub whose repository is tracked, the result is that
		// repository's worktrees -- the same scope as the human view.
		repoIDs := sortedRepoIDs(st)
		if currentRepoID != "" && st.Repositories[currentRepoID] != nil {
			repoIDs = []string{currentRepoID}
		}
		emitResult(cmd, listRecords(fs, g, st, repoIDs))
		return
	}

	if len(st.Repositories) == 0 {
		output.Info("No worktrees found.")
		output.Info("\nA hub git-hop does not list can be registered with 'git hop doctor --fix', run inside it.")
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

	compareMap := compareBranchesForRepo(fs, repo)

	if output.CurrentMode != output.ModeHuman {
		// Use old table for non-human modes
		t := tui.NewTable([]interface{}{"Branch", "Base", "Type", "Path", "State", "Status"})
		for _, key := range repo.SortedWorktreeKeys() {
			wt := repo.Worktrees[key]
			branch := wt.Branch
			state := "missing"
			sync := "-"
			compare := compareMap[key]
			if exists, _ := afero.DirExists(fs, wt.Path); exists {
				state = "active"
				sync = getBranchSyncStatus(g, wt.Path, branch, compare)
			}
			t.AddRow(branch, displayBase(compare, repo.DefaultBranch), wt.Type, wt.Path, state, sync)
		}
		t.Render()
		return
	}

	// Enhanced table for human mode
	table := output.NewStatusTable("Branch", "Base", "Type", "Path", "State", "Status")

	activeCount := 0
	missingCount := 0

	for _, key := range repo.SortedWorktreeKeys() {
		wt := repo.Worktrees[key]
		branch := wt.Branch
		exists, _ := afero.DirExists(fs, wt.Path)

		status := "error"
		stateText := "missing"
		sync := "-"
		compare := compareMap[key]
		if exists {
			status = "success"
			stateText = "active"
			sync = getBranchSyncStatus(g, wt.Path, branch, compare)
			activeCount++
		} else {
			missingCount++
		}

		table.AddRow(status, branch, displayBase(compare, repo.DefaultBranch), wt.Type, wt.Path, stateText, sync)
	}

	table.Print()

	// Summary
	fmt.Println()
	summary := fmt.Sprintf("Summary: %d worktrees", len(repo.Worktrees))
	if activeCount > 0 {
		summary += fmt.Sprintf(", %d active", activeCount)
	}
	if missingCount > 0 {
		summary += output.Colorize(fmt.Sprintf(", %d missing", missingCount), "warning")
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
		t := tui.NewTable([]interface{}{"Repository", "Branch", "Base", "Type", "Path", "State", "Status"})

		var repoIDs []string
		for repoID := range st.Repositories {
			repoIDs = append(repoIDs, repoID)
		}
		sort.Strings(repoIDs)

		for _, repoID := range repoIDs {
			repo := st.Repositories[repoID]
			compareMap := compareBranchesForRepo(fs, repo)
			for _, key := range repo.SortedWorktreeKeys() {
				wt := repo.Worktrees[key]
				branch := wt.Branch
				state := "missing"
				sync := "-"
				compare := compareMap[key]
				if exists, _ := afero.DirExists(fs, wt.Path); exists {
					state = "active"
					sync = getBranchSyncStatus(g, wt.Path, branch, compare)
				}
				t.AddRow(repoID, branch, displayBase(compare, repo.DefaultBranch), wt.Type, wt.Path, state, sync)
			}
		}
		t.Render()
		return
	}

	// Enhanced table for human mode
	table := output.NewStatusTable("Repository", "Branch", "Base", "Type", "State", "Status")

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
		compareMap := compareBranchesForRepo(fs, repo)

		for _, key := range repo.SortedWorktreeKeys() {
			wt := repo.Worktrees[key]
			branch := wt.Branch
			totalWorktrees++

			exists, _ := afero.DirExists(fs, wt.Path)
			status := "error"
			stateText := "missing"
			sync := "-"
			compare := compareMap[key]
			if exists {
				status = "success"
				stateText = "active"
				sync = getBranchSyncStatus(g, wt.Path, branch, compare)
				activeCount++
			} else {
				missingCount++
			}

			// Shorten repo ID for display
			shortRepo := repoID
			if len(shortRepo) > 30 {
				shortRepo = "..." + shortRepo[len(shortRepo)-27:]
			}

			table.AddRow(status, shortRepo, branch, displayBase(compare, repo.DefaultBranch), wt.Type, stateText, sync)
		}
	}

	table.Print()

	// Summary
	fmt.Println()
	summary := fmt.Sprintf("Summary: %d worktrees across %d repositories", totalWorktrees, len(repoIDs))
	if activeCount > 0 {
		summary += fmt.Sprintf(", %d active", activeCount)
	}
	if missingCount > 0 {
		summary += output.Colorize(fmt.Sprintf(", %d missing", missingCount), "warning")
	}
	fmt.Println(summary)

	// Legend
	fmt.Println()
	legend := output.Legend(map[string]string{
		"active":  "worktree on disk",
		"missing": "worktree directory not found",
	})
	fmt.Println(legend)
}

// listRecords builds one list record per worktree of the given
// repositories, in repository then branch order. The slice is never nil,
// so an empty result renders as [] rather than null.
func listRecords(fs afero.Fs, g git.GitInterface, st *state.State, repoIDs []string) []listRecord {
	records := []listRecord{}
	for _, repoID := range repoIDs {
		repo := st.Repositories[repoID]
		compareMap := compareBranchesForRepo(fs, repo)
		for _, key := range repo.SortedWorktreeKeys() {
			wt := repo.Worktrees[key]
			branch := wt.Branch
			r := listRecord{
				Repository: repoID,
				Branch:     branch,
				Base:       compareMap[key],
				Type:       wt.Type,
				Path:       wt.Path,
				State:      "missing",
				Status:     "-",
				Hub:        wt.HubPath,
			}
			if exists, _ := afero.DirExists(fs, wt.Path); exists {
				r.State = "active"
				r.Status = getBranchSyncStatus(g, wt.Path, branch, r.Base)
			}
			records = append(records, r)
		}
	}
	return records
}

// sortedRepoIDs returns the ids of every repository tracked in st, sorted.
func sortedRepoIDs(st *state.State) []string {
	ids := make([]string, 0, len(st.Repositories))
	for id := range st.Repositories {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
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
	declareOutputSchema(listCmd, &[]listRecord{})
	cli.RootCmd.AddCommand(listCmd)
}
