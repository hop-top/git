package cmd

import "hop.top/git/internal/state"

// stateBranches returns the branch of every worktree repo records, for
// asserting on a state keyed by path.
func stateBranches(repo *state.RepositoryState) []string {
	var branches []string
	for _, wt := range repo.SortedWorktrees() {
		branches = append(branches, wt.Branch)
	}
	return branches
}
