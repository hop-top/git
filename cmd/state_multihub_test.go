package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// Two hubs of one repository, /hubs/a and /hubs/b, each with its own
// worktree of main and of feat: state records all four.

func twoHubRepoState(t *testing.T, fs afero.Fs) *state.State {
	t.Helper()
	st := state.NewState()
	st.AddRepository(registerRepoID, &state.RepositoryState{Org: "test", Repo: "repo", DefaultBranch: "main"})
	for _, hub := range []string{"/hubs/a", "/hubs/b"} {
		require.NoError(t, st.AddHub(registerRepoID, &state.HubState{Path: hub, Mode: state.HubModeLocal}))
		for _, branch := range []string{"main", "feat"} {
			path := filepath.Join(hub, "hops", branch)
			require.NoError(t, fs.MkdirAll(path, 0o755))
			require.NoError(t, st.PutWorktree(registerRepoID, &state.WorktreeState{Path: path, Branch: branch, Type: "linked", HubPath: hub}))
		}
	}
	return st
}

func TestListAndStatusAll_ShowEveryHubsWorktrees(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	st := twoHubRepoState(t, fs)
	require.NoError(t, state.SaveState(fs, st))

	list, status, hubs := stateRecords(t, fs)

	require.Len(t, list, 4)
	require.Len(t, status, 4)
	var got []string
	for _, r := range list {
		got = append(got, r.Branch+"@"+r.Hub)
	}
	assert.Equal(t, []string{"feat@/hubs/a", "feat@/hubs/b", "main@/hubs/a", "main@/hubs/b"}, got)
	for i, r := range status {
		assert.Equal(t, list[i].Hub, r.Hub, "status --all carries the hub too")
	}
	assert.Equal(t, []string{"/hubs/a", "/hubs/b"}, hubs, "prune --all visits both hubs")
}

// prune --all prunes a missing worktree of either hub and keeps the
// other hub's worktree of the same branch.
func TestPruneAll_TwoHubsSameBranch(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	st := twoHubRepoState(t, fs)
	require.NoError(t, fs.RemoveAll("/hubs/b/hops/feat"))

	counts := runPruneAll(fs, mocks.NewMockGit(), st, false)

	assert.Equal(t, 1, counts.worktrees)
	repo := st.Repositories[registerRepoID]
	_, ok := repo.Worktree("/hubs/b", "feat")
	assert.False(t, ok, "hub b's missing feat is pruned")
	_, ok = repo.Worktree("/hubs/a", "feat")
	assert.True(t, ok, "hub a's feat stays")
}

// add in a hub of a repository state already tracks from another hub
// records the hub too, and leaves the other hub's worktree of the same
// branch alone.
func TestRecordAddedWorktree_SecondHubOfTrackedRepo(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	st := state.NewState()
	st.AddRepository(registerRepoID, &state.RepositoryState{Org: "test", Repo: "repo", DefaultBranch: "main"})
	require.NoError(t, st.AddHub(registerRepoID, &state.HubState{Path: "/hubs/a"}))
	require.NoError(t, st.PutWorktree(registerRepoID, &state.WorktreeState{Path: "/hubs/a/hops/feat", Branch: "feat", HubPath: "/hubs/a"}))
	require.NoError(t, state.SaveState(fs, st))

	hub := &hop.Hub{Path: "/hubs/b", Config: &config.HubConfig{Repo: config.RepoConfig{Org: "test", Repo: "repo", DefaultBranch: "main", Mode: config.RepoModeGlobal}}}
	recordAddedWorktree(fs, hub, registerRepoID, "/hubs/b", "feat", "/hubs/b/hops/feat")

	got, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := got.Repositories[registerRepoID]
	var hubs []string
	for _, h := range repo.Hubs {
		hubs = append(hubs, h.Path+":"+h.Mode)
	}
	assert.Equal(t, []string{"/hubs/a:", "/hubs/b:" + state.HubModeGlobal}, hubs, "the second hub is recorded")
	a, ok := repo.Worktree("/hubs/a", "feat")
	require.True(t, ok, "hub a's feat stays")
	assert.Equal(t, "/hubs/a/hops/feat", a.Path)
	b, ok := repo.Worktree("/hubs/b", "feat")
	require.True(t, ok)
	assert.Equal(t, "linked", b.Type)
}

// add never replaces a state file it cannot read.
func TestRecordAddedWorktree_UnreadableStateUntouched(t *testing.T) {
	p := isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	path := filepath.Join(p.stateHome, "state.json")
	require.NoError(t, afero.WriteFile(fs, path, []byte("{not json"), 0o644))

	hub := &hop.Hub{Path: "/hubs/b", Config: &config.HubConfig{Repo: config.RepoConfig{Org: "test", Repo: "repo"}}}
	recordAddedWorktree(fs, hub, registerRepoID, "/hubs/b", "feat", "/hubs/b/hops/feat")

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, "{not json", string(data))
}

func writeStateBackup(t *testing.T, fs afero.Fs, name string) string {
	t.Helper()
	path := filepath.Join(state.BackupDir(), name)
	require.NoError(t, afero.WriteFile(fs, path, []byte("{}"), 0o644))
	return path
}

// State backups age out after hop.repair.backupRetention (30 days by
// default); --dry-run reports without removing, and 0 keeps them all.
func TestPruneStateBackups(t *testing.T) {
	stamp := func(age time.Duration) string {
		return state.BackupPrefix + time.Now().Add(-age).UTC().Format("20060102T150405Z") + state.BackupSuffix
	}
	for _, tc := range []struct {
		name      string
		retention string
		dryRun    bool
		wantGone  bool
	}{
		{"default retention", "", false, true},
		{"dry-run", "", true, false},
		{"retention 0 keeps all", "0", false, false},
		{"longer retention keeps it", "2000h", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateDoctorPaths(t)
			fs := afero.NewMemMapFs()
			st := twoHubRepoState(t, fs)
			g := mocks.NewMockGit()
			if tc.retention != "" {
				g.Runner.Responses["/hubs/a:git config --get hop.repair.backupRetention"] = tc.retention
			}
			old := writeStateBackup(t, fs, stamp(40*24*time.Hour))
			recent := writeStateBackup(t, fs, stamp(time.Hour))
			other := writeStateBackup(t, fs, "notes.txt")

			records := pruneStateBackups(fs, g, st, tc.dryRun)

			exists, _ := afero.Exists(fs, old)
			assert.Equal(t, !tc.wantGone, exists)
			for _, keep := range []string{recent, other} {
				exists, _ := afero.Exists(fs, keep)
				assert.True(t, exists, keep)
			}
			if tc.retention == "0" || tc.retention == "2000h" {
				assert.Empty(t, records)
				return
			}
			require.Len(t, records, 1)
			assert.Equal(t, pruneKindStateBackup, records[0].Kind)
			assert.Equal(t, old, records[0].Path)
			want := "pruned"
			if tc.dryRun {
				want = "would-prune"
			}
			assert.Equal(t, want, records[0].Action)
		})
	}
}
