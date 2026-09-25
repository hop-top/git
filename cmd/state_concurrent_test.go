package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// prune and doctor --fix load state, scan (git commands, prompts) and
// then save. Another git-hop run can save state during the scan; what it
// recorded or dropped must survive their save.

// gitDuringScan runs during once, the first time the scan asks git for
// its worktrees: another run saving state while prune or doctor works.
type gitDuringScan struct {
	*mocks.MockGit
	once   sync.Once
	during func()
}

func (g *gitDuringScan) WorktreeListPorcelain(dir string) (string, error) {
	g.once.Do(g.during)
	return g.MockGit.WorktreeListPorcelain(dir)
}

// concurrentScanFixture records a hub with a worktree whose directory
// is gone ("gone"), one that exists ("other") and a hub that is gone.
// It returns the fs, the hub, the state as loaded before the scan, and
// the change another run saves during it: it records a worktree and a
// hub, and drops "other".
func concurrentScanFixture(t *testing.T) (afero.Fs, string, *state.State, func()) {
	t.Helper()
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "other"}, []string{"main", "other"})
	wtPath := func(b string) string { return filepath.Join(hubPath, config.MakeWorktreePath(b)) }

	st := stateWithHub(hubPath)
	repo := st.Repositories[registerRepoID]
	repo.Hubs = append(repo.Hubs, &state.HubState{Path: "/hubs/gone", Mode: state.HubModeLocal})
	for _, b := range []string{"gone", "other"} {
		require.NoError(t, st.PutWorktree(registerRepoID, &state.WorktreeState{Path: wtPath(b), Branch: b, Type: "linked", HubPath: hubPath}))
	}
	require.NoError(t, state.SaveState(fs, st))
	loaded, err := state.LoadState(fs)
	require.NoError(t, err)

	otherRun := func() {
		require.NoError(t, state.Update(fs, func(st *state.State) error {
			if err := st.AddHub(registerRepoID, &state.HubState{Path: "/hubs/new", Mode: state.HubModeLocal}); err != nil {
				return err
			}
			if err := st.PutWorktree(registerRepoID, &state.WorktreeState{Path: "/hubs/new/hops/added", Branch: "added", Type: "linked", HubPath: "/hubs/new"}); err != nil {
				return err
			}
			return st.RemoveWorktreeAt(registerRepoID, wtPath("other"))
		}))
	}
	return fs, hubPath, loaded, otherRun
}

func savedHubs(t *testing.T, fs afero.Fs) []string {
	t.Helper()
	st, err := state.LoadState(fs)
	require.NoError(t, err)
	var paths []string
	for _, h := range st.Repositories[registerRepoID].Hubs {
		paths = append(paths, h.Path)
	}
	sort.Strings(paths)
	return paths
}

func savedBranches(t *testing.T, fs afero.Fs) []string {
	t.Helper()
	st, err := state.LoadState(fs)
	require.NoError(t, err)
	return stateBranches(st.Repositories[registerRepoID])
}

func TestPrune_KeepsStateSavedDuringScan(t *testing.T) {
	fs, hubPath, loaded, otherRun := concurrentScanFixture(t)
	g := &gitDuringScan{MockGit: mocks.NewMockGit(), during: otherRun}

	counts, err := pruneAndSave(fs, g, loaded, loaded, true, false)
	require.NoError(t, err)

	assert.Equal(t, 1, counts.worktrees, "gone is pruned")
	assert.Equal(t, 1, counts.hubs, "/hubs/gone is pruned")
	assert.Equal(t, []string{"added"}, savedBranches(t, fs),
		"gone is pruned; added, recorded during the scan, stays; other, dropped during it, stays dropped")
	assert.Equal(t, []string{"/hubs/new", hubPath}, savedHubs(t, fs),
		"/hubs/gone is pruned; /hubs/new, recorded during the scan, stays")
}

func TestDoctorFix_KeepsStateSavedDuringScan(t *testing.T) {
	fs, hubPath, loaded, otherRun := concurrentScanFixture(t)
	g := &gitDuringScan{MockGit: mocks.NewMockGit(), during: otherRun}
	// gone is merged, so it is removed without a prompt.
	g.Runner.Responses = map[string]string{hubPath + ":git branch --merged main": "  gone\n* main\n"}

	var r doctorReport
	fixStateIssues(fs, g, loaded, hubPath, nil, doctorOpts{fix: true}, &r)

	assert.Equal(t, []string{"added"}, savedBranches(t, fs),
		"gone is removed; added, recorded during the scan, stays; other, dropped during it, stays dropped")
	assert.Equal(t, []string{"/hubs/new", hubPath}, savedHubs(t, fs),
		"/hubs/gone is pruned; /hubs/new, recorded during the scan, stays")
}

// Concurrent adds each record their worktree: none saves over another.
func TestRecordAddedWorktree_ConcurrentAddsLoseNothing(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hub := &hop.Hub{Path: "/hubs/a", Config: &config.HubConfig{Repo: config.RepoConfig{Org: "test", Repo: "repo", DefaultBranch: "main"}}}

	const adders, perAdder = 4, 15
	var want []string
	var wg sync.WaitGroup
	for a := 0; a < adders; a++ {
		for i := 0; i < perAdder; i++ {
			want = append(want, fmt.Sprintf("add-%d-%d", a, i))
		}
		wg.Add(1)
		go func(a int) {
			defer wg.Done()
			for i := 0; i < perAdder; i++ {
				branch := fmt.Sprintf("add-%d-%d", a, i)
				recordAddedWorktree(fs, hub, registerRepoID, "/hubs/a", branch, "/hubs/a/hops/"+branch)
			}
		}(a)
	}
	wg.Wait()

	got := savedBranches(t, fs)
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got)
}

// A relocation doctor records moves the entry in the state it loaded and
// in state.json as saved.
func TestStateEdits_RelocateReplaysOnFreshState(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	st := stateWithHub("/hubs/repo")
	require.NoError(t, st.PutWorktree(registerRepoID, &state.WorktreeState{Path: "/hubs/repo/hops/old", Branch: "feat", Type: "linked", HubPath: "/hubs/repo"}))
	require.NoError(t, state.SaveState(fs, st))
	loaded, err := state.LoadState(fs)
	require.NoError(t, err)

	var edits stateEdits
	key, wt, ok := loaded.Repositories[registerRepoID].WorktreeAt("/hubs/repo/hops/old")
	require.True(t, ok)
	edits.relocateWorktree(loaded, registerRepoID, key, *wt, "/elsewhere/feat")

	// Another run records a worktree meanwhile.
	require.NoError(t, state.Update(fs, func(st *state.State) error {
		return st.PutWorktree(registerRepoID, &state.WorktreeState{Path: "/hubs/repo/hops/other", Branch: "other", Type: "linked", HubPath: "/hubs/repo"})
	}))
	require.NoError(t, edits.save(fs))

	for name, s := range map[string]*state.State{"loaded": loaded, "saved": mustLoadState(t, fs)} {
		repo := s.Repositories[registerRepoID]
		_, _, old := repo.WorktreeAt("/hubs/repo/hops/old")
		assert.False(t, old, "%s: the old path is gone", name)
		_, moved, ok := repo.WorktreeAt("/elsewhere/feat")
		require.True(t, ok, "%s: the entry is at its new path", name)
		assert.Equal(t, "feat", moved.Branch, name)
	}
	assert.Equal(t, []string{"feat", "other"}, savedBranches(t, fs), "the other run's worktree stays")
}

func mustLoadState(t *testing.T, fs afero.Fs) *state.State {
	t.Helper()
	st, err := state.LoadState(fs)
	require.NoError(t, err)
	return st
}
