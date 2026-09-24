package cmd

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// registerRepoID is the repository the doctor test hubs belong to.
const registerRepoID = "github.com/test/repo"

// stateBranches returns the branch of every worktree repo records, for
// asserting on a state keyed by path.
func stateBranches(repo *state.RepositoryState) []string {
	var branches []string
	for _, wt := range repo.SortedWorktrees() {
		branches = append(branches, wt.Branch)
	}
	return branches
}

// stateRecords returns what list and status --all report and the hubs
// prune --all visits.
func stateRecords(t *testing.T, fs afero.Fs) (list []listRecord, status []statusRecord, hubs []string) {
	t.Helper()
	st, err := state.LoadState(fs)
	require.NoError(t, err)
	g := mocks.NewMockGit()
	for _, h := range hubPathsFromState(st) {
		hubs = append(hubs, h.path)
	}
	return listRecords(fs, g, st, sortedRepoIDs(st)), systemStatusRecords(fs, g), hubs
}
