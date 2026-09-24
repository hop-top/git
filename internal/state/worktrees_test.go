package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoHubState is one repository with hubs /a and /b, each with its own
// worktree of main and of feat: what the branch-keyed format could not
// hold.
func twoHubState(t *testing.T) *State {
	t.Helper()
	st := NewState()
	st.AddRepository("r", &RepositoryState{DefaultBranch: "main"})
	for _, hub := range []string{"/a", "/b"} {
		require.NoError(t, st.AddHub("r", &HubState{Path: hub}))
		for _, branch := range []string{"main", "feat"} {
			require.NoError(t, st.PutWorktree("r", &WorktreeState{Path: hub + "/hops/" + branch, Branch: branch, HubPath: hub}))
		}
	}
	return st
}

func TestPutWorktree_TwoHubsShareBranches(t *testing.T) {
	st := twoHubState(t)
	repo := st.Repositories["r"]

	assert.Len(t, repo.Worktrees, 4, "each hub keeps its own main and feat")
	for _, hub := range []string{"/a", "/b"} {
		for _, branch := range []string{"main", "feat"} {
			wt, ok := repo.Worktree(hub, branch)
			require.True(t, ok, "%s %s", hub, branch)
			assert.Equal(t, hub+"/hops/"+branch, wt.Path)
		}
		assert.Len(t, repo.HubWorktrees(hub), 2)
	}
}

// Recording a worktree again replaces its entry, whatever the path's
// spelling, and nothing else.
func TestPutWorktree_ReplacesSameWorktreeOnly(t *testing.T) {
	st := twoHubState(t)
	require.NoError(t, st.PutWorktree("r", &WorktreeState{Path: "/a/hops/../hops/main", Branch: "main", HubPath: "/a", Type: "bare"}))

	repo := st.Repositories["r"]
	assert.Len(t, repo.Worktrees, 4)
	wt, ok := repo.Worktree("/a", "main")
	require.True(t, ok)
	assert.Equal(t, "bare", wt.Type)
	other, _ := repo.Worktree("/b", "main")
	assert.Equal(t, "/b/hops/main", other.Path, "the other hub's main is untouched")
}

func TestPutWorktree_Rejects(t *testing.T) {
	st := twoHubState(t)
	assert.Error(t, st.PutWorktree("missing", &WorktreeState{Path: "/p", Branch: "b"}))
	assert.Error(t, st.PutWorktree("r", &WorktreeState{Branch: "b"}), "no path")
	assert.Error(t, st.PutWorktree("r", &WorktreeState{Path: "/p"}), "no branch")
}

func TestRemoveWorktreeAt_RemovesOnlyThatWorktree(t *testing.T) {
	st := twoHubState(t)
	require.NoError(t, st.RemoveWorktreeAt("r", "/b/hops/main"))

	repo := st.Repositories["r"]
	assert.Len(t, repo.Worktrees, 3)
	_, ok := repo.Worktree("/b", "main")
	assert.False(t, ok)
	_, ok = repo.Worktree("/a", "main")
	assert.True(t, ok, "hub /a's main stays")
}

// Removing a hub keeps the repository's other hub and its worktrees; the
// repository goes with its last hub.
func TestRemoveHub_KeepsOtherHubs(t *testing.T) {
	st := twoHubState(t)

	require.NoError(t, st.RemoveHub("r", "/b"))
	repo := st.Repositories["r"]
	require.NotNil(t, repo, "the repository has another hub")
	require.Len(t, repo.Hubs, 1)
	assert.Equal(t, "/a", repo.Hubs[0].Path)
	assert.Len(t, repo.Worktrees, 2)
	assert.Empty(t, repo.HubWorktrees("/b"))

	require.NoError(t, st.RemoveHub("r", "/a"))
	assert.NotContains(t, st.Repositories, "r", "no hub and no worktree left")
}

func TestSortedWorktreeKeys_Order(t *testing.T) {
	st := twoHubState(t)
	var got []string
	for _, wt := range st.Repositories["r"].SortedWorktrees() {
		got = append(got, wt.Branch+"@"+wt.HubPath)
	}
	assert.Equal(t, []string{"feat@/a", "feat@/b", "main@/a", "main@/b"}, got)
}
