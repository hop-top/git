package hop

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

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

// Recording a hub a second time changes nothing: no entry is rewritten,
// not even its timestamps, and the plan is empty.
func TestRegisterNewHub_Idempotent(t *testing.T) {
	isolateRegisterPaths(t)
	fs := afero.NewMemMapFs()
	h := NewHub{
		Org: "acme", Repo: "widget", DefaultBranch: "main", HubPath: "/hub",
		WorktreePath: "/hub/hops/main", WorktreeType: WorktreeTypeBare,
		Linked: map[string]string{"feature": "/hub/hops/feature"},
	}
	_, err := RegisterNewHub(fs, h)
	require.NoError(t, err)
	statePath := filepath.Join(state.GetStateHome(), "state.json")
	first, err := afero.ReadFile(fs, statePath)
	require.NoError(t, err)

	plan, err := RegisterNewHub(fs, h)
	require.NoError(t, err)

	assert.True(t, plan.Empty(), "nothing left to record: %+v", plan)
	again, err := afero.ReadFile(fs, statePath)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(again), "state is not rewritten")
}

// Entries state already holds for the repository are merged with, never
// overwritten: another hub of the same repository stays with its
// worktrees, including its own main; this hub's main is recorded next to
// it; and a worktree already recorded keeps its entry as it is. The
// state is seeded in the branch-keyed format of earlier releases, so the
// merge also runs over a migrated file.
func TestRegisterNewHub_MergesWithExistingEntries(t *testing.T) {
	isolateRegisterPaths(t)
	fs := afero.NewMemMapFs()
	created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	st := state.NewState()
	st.AddRepository("github.com/acme/widget", &state.RepositoryState{
		Org: "acme", Repo: "widget", DefaultBranch: "main", URI: "keep-me",
		Hubs: []*state.HubState{{Path: "/other", Mode: state.HubModeLocal, CreatedAt: created}},
		Worktrees: map[string]*state.WorktreeState{
			"main":    {Path: "/other/hops/main", Type: WorktreeTypeBare, HubPath: "/other", CreatedAt: created},
			"feature": {Path: "/hub/hops/feature", Type: "linked", HubPath: "/hub", CreatedAt: created},
		},
	})
	require.NoError(t, state.SaveState(fs, st))

	plan, err := RegisterNewHub(fs, NewHub{
		URI: "new-uri", Org: "acme", Repo: "widget", DefaultBranch: "main", HubPath: "/hub",
		WorktreePath: "/hub/hops/main", WorktreeType: WorktreeTypeBare,
		Linked: map[string]string{"feature": "/hub/hops/feature", "extra": "/hub/hops/extra"},
	})
	require.NoError(t, err)

	assert.False(t, plan.Repo)
	assert.True(t, plan.Hub)
	assert.Equal(t, []string{"extra", "main"}, plan.Branches())

	got, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := got.Repositories["github.com/acme/widget"]
	require.NotNil(t, repo)
	assert.Equal(t, "keep-me", repo.URI, "the repository entry is kept")
	require.Len(t, repo.Hubs, 2)
	assert.Equal(t, "/other", repo.Hubs[0].Path)
	assert.True(t, repo.Hubs[0].CreatedAt.Equal(created))
	assert.Equal(t, "/hub", repo.Hubs[1].Path)
	require.Len(t, repo.Worktrees, 4)

	other, ok := repo.Worktree("/other", "main")
	require.True(t, ok, "the other hub's main is kept")
	assert.True(t, other.CreatedAt.Equal(created))
	mine, ok := repo.Worktree("/hub", "main")
	require.True(t, ok, "this hub's main is recorded next to it")
	assert.Equal(t, "/hub/hops/main", mine.Path)
	feature, ok := repo.Worktree("/hub", "feature")
	require.True(t, ok)
	assert.True(t, feature.CreatedAt.Equal(created), "a worktree already recorded is kept as is")
	extra, ok := repo.Worktree("/hub", "extra")
	require.True(t, ok)
	assert.Equal(t, "/hub/hops/extra", extra.Path)
}

// A state file that cannot be read is left as it is, rather than replaced
// by one that holds only this hub.
func TestRegisterNewHub_UnreadableStateUntouched(t *testing.T) {
	isolateRegisterPaths(t)
	fs := afero.NewMemMapFs()
	statePath := filepath.Join(state.GetStateHome(), "state.json")
	require.NoError(t, afero.WriteFile(fs, statePath, []byte("{not json"), 0o644))

	_, err := RegisterNewHub(fs, NewHub{Org: "acme", Repo: "widget", DefaultBranch: "main", HubPath: "/hub"})

	assert.Error(t, err)
	data, readErr := afero.ReadFile(fs, statePath)
	require.NoError(t, readErr)
	assert.Equal(t, "{not json", string(data))
}

// Hubs registered at once (clones and inits running side by side) are
// all recorded: none saves over another.
func TestRegisterNewHub_ConcurrentHubsLoseNothing(t *testing.T) {
	isolateRegisterPaths(t)
	fs := afero.NewMemMapFs()

	const hubs = 24
	var want []string
	var wg sync.WaitGroup
	for i := 0; i < hubs; i++ {
		hubPath := fmt.Sprintf("/hubs/h%02d", i)
		want = append(want, hubPath)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := RegisterNewHub(fs, NewHub{
				Org: "acme", Repo: "widget", DefaultBranch: "main", HubPath: hubPath,
				WorktreePath: hubPath + "/hops/main", WorktreeType: WorktreeTypeBare,
			})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	st, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := st.Repositories["github.com/acme/widget"]
	require.NotNil(t, repo)
	var got []string
	for _, h := range repo.Hubs {
		got = append(got, h.Path)
	}
	sort.Strings(got)
	assert.Equal(t, want, got)
	assert.Len(t, repo.Worktrees, hubs)
}
