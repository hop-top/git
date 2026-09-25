package cmd

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// repairTestCmd is a command carrying the flags repairLocked reads.
func repairTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "repair"}
	c.Flags().BoolP("dry-run", "n", false, "")
	c.Flags().Bool("force", false, "")
	return c
}

// A worktree git lists but hop.json lacks is added to hop.json by repair
// and recorded in state the way add records one: keyed by its path,
// under the branch git has checked out there, with its hub. Before, only
// hop.json got it, and doctor then reported the hub as registered in
// state without that worktree.
func TestRepair_UnrecordedWorktreeRecordedInState(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main"}, []string{"main"})
	hub, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)
	_, err = hop.RegisterNewHub(fs, hubFromConfig(fs, hub))
	require.NoError(t, err)

	extra := "/hubs/repo/hops/by-hand"
	require.NoError(t, fs.MkdirAll(extra, 0o755))
	g := newRegistryGit(fs)
	g.WorktreeListOut = "worktree " + hubPath + "\nbare\n\n" +
		porcelainEntry(worktreeDir(hubPath, "main"), "main") +
		porcelainEntry(extra, "feat/by-hand")

	outcome := repairLocked(repairTestCmd(), fs, g, hubPath, nil)
	require.Equal(t, exitOK, outcome.code, outcome.msg)

	reloaded, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)
	require.Contains(t, reloaded.Config.Branches, "feat/by-hand", "hop.json records the worktree")

	st, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := st.Repositories[registerRepoID]
	require.NotNil(t, repo)
	wt, ok := repo.Worktree(hubPath, "feat/by-hand")
	require.True(t, ok, "state records the worktree: %+v", repo.Worktrees)
	assert.Equal(t, extra, wt.Path)
	assert.Equal(t, "linked", wt.Type)
	assert.Equal(t, hubPath, wt.HubPath)
	assert.False(t, wt.CreatedAt.IsZero())
	assert.Contains(t, repo.Worktrees, state.WorktreeKey(extra), "keyed by path")

	r := runDoctor(fs, g, hubPath, doctorOpts{})
	for _, msg := range recordMessages(r, doctorKindIssue, hubPath) {
		assert.NotContains(t, msg, "without worktree", "doctor after repair: %+v", r.records)
	}
}

// A preview records nothing in state, as it writes nothing to hop.json.
func TestRepair_DryRunLeavesStateAlone(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main"}, []string{"main"})
	hub, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)
	_, err = hop.RegisterNewHub(fs, hubFromConfig(fs, hub))
	require.NoError(t, err)

	extra := "/hubs/repo/hops/by-hand"
	require.NoError(t, fs.MkdirAll(extra, 0o755))
	g := newRegistryGit(fs)
	g.WorktreeListOut = "worktree " + hubPath + "\nbare\n\n" +
		porcelainEntry(worktreeDir(hubPath, "main"), "main") +
		porcelainEntry(extra, "feat/by-hand")

	c := repairTestCmd()
	require.NoError(t, c.Flags().Set("dry-run", "true"))
	outcome := repairLocked(c, fs, g, hubPath, nil)
	require.Equal(t, exitOK, outcome.code, outcome.msg)

	st, err := state.LoadState(fs)
	require.NoError(t, err)
	_, ok := st.Repositories[registerRepoID].Worktree(hubPath, "feat/by-hand")
	assert.False(t, ok, "a preview records nothing")
}

// Recording follows hop.json after the apply: the default branch's
// worktree of a bare hub is recorded as such, and an action whose
// worktree hop.json does not list (a dropped row, or a branch hop.json
// has at another path) records nothing.
func TestRecordRepairedWorktrees_FollowsHopJSON(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat"}, []string{"main", "feat"})

	plan := &hop.Plan{HubPath: hubPath, Actions: []hop.Action{
		{Kind: hop.ActionUpdateHopJSON, WorktreePath: worktreeDir(hubPath, "main"), NewValue: "main"},
		{Kind: hop.ActionUpdateHopJSON, WorktreePath: "/hubs/repo/hops/elsewhere", NewValue: "feat"},
		{Kind: hop.ActionUpdateHopJSON, WorktreePath: "/hubs/repo/hops/gone"},
	}}
	recordRepairedWorktrees(fs, hubPath, plan)

	st, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := st.Repositories[registerRepoID]
	require.NotNil(t, repo)
	require.Len(t, repo.Worktrees, 1, "only the worktree hop.json lists there: %+v", repo.Worktrees)
	main, ok := repo.Worktree(hubPath, "main")
	require.True(t, ok)
	assert.Equal(t, hop.WorktreeTypeBare, main.Type)
	require.Len(t, repo.Hubs, 1)
	assert.Equal(t, state.HubModeGlobal, repo.Hubs[0].Mode, "the hub is recorded as add records it")
}
