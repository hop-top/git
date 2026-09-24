package hop

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/state"
)

func isolateRegisterPaths(t *testing.T) {
	t.Helper()
	root := "/xdg"
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("GIT_HOP_DATA_HOME", filepath.Join(root, "githop-data"))
}

// A bare repository git-hop adopts before any worktree exists still gets
// a data home and a hub record, and no worktree record for a directory
// that is not there.
func TestRegisterNewHub_NoInitialWorktree(t *testing.T) {
	isolateRegisterPaths(t)
	fs := afero.NewMemMapFs()

	RegisterNewHub(fs, NewHub{
		Org: "acme", Repo: "widget", DefaultBranch: "main", HubPath: "/hub",
	})

	exists, _ := afero.DirExists(fs, GetGitHopDataHome())
	assert.True(t, exists, "data home must exist")

	st, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := st.Repositories["github.com/acme/widget"]
	require.NotNil(t, repo)
	require.Len(t, repo.Hubs, 1)
	assert.Equal(t, "/hub", repo.Hubs[0].Path)
	assert.Empty(t, repo.Worktrees)
}

// Worktrees the hub already has besides the default branch's are
// recorded as linked ones, next to the initial worktree.
func TestRegisterNewHub_LinkedWorktrees(t *testing.T) {
	isolateRegisterPaths(t)
	fs := afero.NewMemMapFs()

	RegisterNewHub(fs, NewHub{
		Org: "acme", Repo: "widget", DefaultBranch: "main", HubPath: "/hub",
		WorktreePath: "/hub/hops/main", WorktreeType: WorktreeTypeBare,
		Linked: map[string]string{"feature": "/hub/hops/feature"},
	})

	st, err := state.LoadState(fs)
	require.NoError(t, err)
	wts := st.Repositories["github.com/acme/widget"].Worktrees
	require.Len(t, wts, 2)
	assert.Equal(t, "main", wts["/hub/hops/main"].Branch)
	assert.Equal(t, WorktreeTypeBare, wts["/hub/hops/main"].Type)
	assert.Equal(t, "feature", wts["/hub/hops/feature"].Branch)
	assert.Equal(t, "linked", wts["/hub/hops/feature"].Type)
	assert.Equal(t, "/hub", wts["/hub/hops/feature"].HubPath)
}
