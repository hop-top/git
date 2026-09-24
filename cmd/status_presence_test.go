package cmd

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// statusStates maps each record's branch to its state and sync status.
func statusStates(records []statusRecord) map[string][2]string {
	out := make(map[string][2]string, len(records))
	for _, r := range records {
		out[r.Branch] = [2]string{r.State, r.Status}
	}
	return out
}

// A regular file at a branch's worktree path is not the worktree. The hub
// view used to call it Linked (and probe git inside it); it now reports
// the path as Occupied, apart from a worktree that is simply gone.
func TestStatus_FileAtWorktreePath_ReportedOccupied(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "feat/file", "feat/gone"}, []string{"main"})
	require.NoError(t, afero.WriteFile(fs, worktreeDir(hubPath, "feat/file"), []byte("not a worktree"), 0o644))

	hub, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)

	got := statusStates(hubStatusRecords(fs, mocks.NewMockGit(), hub))
	assert.Equal(t, [2]string{statusStateLinked, "default"}, got["main"])
	assert.Equal(t, [2]string{statusStateOccupied, "-"}, got["feat/file"])
	assert.Equal(t, [2]string{statusStateMissing, "-"}, got["feat/gone"])
}

// status --all reads the same paths from state, and must agree with the
// hub view about each of them.
func TestStatusAll_FileAtWorktreePath_ReportedOccupied(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "feat/file", "feat/gone"}, []string{"main"})
	require.NoError(t, afero.WriteFile(fs, worktreeDir(hubPath, "feat/file"), []byte("not a worktree"), 0o644))

	st := stateWithHub(hubPath)
	for _, b := range []string{"main", "feat/file", "feat/gone"} {
		require.NoError(t, st.AddWorktree("github.com/test/repo", b, &state.WorktreeState{
			Path: worktreeDir(hubPath, b), Type: "linked", HubPath: hubPath,
		}))
	}
	require.NoError(t, state.SaveState(fs, st))

	got := statusStates(systemStatusRecords(fs, mocks.NewMockGit()))
	assert.Equal(t, [2]string{statusStateLinked, "default"}, got["main"])
	assert.Equal(t, [2]string{statusStateOccupied, "-"}, got["feat/file"])
	assert.Equal(t, [2]string{statusStateMissing, "-"}, got["feat/gone"])
}
