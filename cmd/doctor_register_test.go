package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// A hub converted before conversions recorded themselves in state has a
// hop.json but no state entry: list, status --all and prune --all do not
// see it. doctor reports it, and --fix records it the way init and clone
// record a new hub.

func stateFileExists(t *testing.T, fs afero.Fs) bool {
	t.Helper()
	exists, err := afero.Exists(fs, filepath.Join(state.GetStateHome(), "state.json"))
	require.NoError(t, err)
	return exists
}

func TestDoctor_UnregisteredHub_ReportedAsIssue(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat"}, []string{"main", "feat"})

	r := runDoctor(fs, newRegistryGit(fs), hubPath, doctorOpts{})

	issues := recordMessages(r, doctorKindIssue, hubPath)
	require.Len(t, issues, 1, "records: %+v", r.records)
	assert.Contains(t, issues[0], "not registered")
	assert.Error(t, doctorResult(r), "an unregistered hub is an issue: exit 1")
	assert.False(t, stateFileExists(t, fs), "a report writes nothing")
}

func TestDoctorFix_UnregisteredHub_Registered(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat", "gone"}, []string{"main", "feat"})
	g := newRegistryGit(fs)
	mergedInto(g, hubPath, "main", "gone", "main")

	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.NotEmpty(t, recordMessages(r, doctorKindFixed, hubPath), "records: %+v", r.records)
	assert.NoError(t, doctorResult(r), "every issue was fixed: %+v", r.records)

	st, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := st.Repositories[registerRepoID]
	require.NotNil(t, repo, "the repository is recorded")
	require.Len(t, repo.Hubs, 1)
	assert.Equal(t, hubPath, repo.Hubs[0].Path)
	assert.Equal(t, state.HubModeGlobal, repo.Hubs[0].Mode, "the hub's hop.json marks it global")
	require.Len(t, repo.Worktrees, 2, "the worktrees on disk are recorded; the missing one is not")
	main, ok := repo.Worktree(hubPath, "main")
	require.True(t, ok)
	assert.Equal(t, worktreeDir(hubPath, "main"), main.Path)
	assert.Equal(t, hop.WorktreeTypeBare, main.Type)
	feat, ok := repo.Worktree(hubPath, "feat")
	require.True(t, ok)
	assert.Equal(t, worktreeDir(hubPath, "feat"), feat.Path)
	assert.Equal(t, "linked", feat.Type)

	list, status, hubs := stateRecords(t, fs)
	assert.Len(t, list, 2, "list sees the hub's worktrees")
	assert.Len(t, status, 2, "status --all sees them")
	assert.Equal(t, []string{hubPath}, hubs, "prune --all visits the hub")

	again := runDoctor(fs, g, hubPath, doctorOpts{})
	assert.Empty(t, recordMessages(again, doctorKindIssue, hubPath), "a registered hub is healthy")
	assert.NoError(t, doctorResult(again), "a rerun exits 0: %+v", again.records)
}

func TestDoctorDryRun_UnregisteredHub_WritesNothing(t *testing.T) {
	p := isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat"}, []string{"main", "feat"})

	r := runDoctor(fs, newRegistryGit(fs), hubPath, doctorOpts{fix: true, dryRun: true})

	wouldFix := strings.Join(recordMessages(r, doctorKindWouldFix, hubPath), "\n")
	assert.Contains(t, wouldFix, "register hub")
	assert.NoError(t, doctorResult(r), "the issue would be fixed: %+v", r.records)
	assert.False(t, stateFileExists(t, fs), "a preview writes no state")
	exists, _ := afero.Exists(fs, filepath.Join(p.configHome, "git-hop", "hops.json"))
	assert.False(t, exists, "a preview writes no registry")
}

// A hub state partly knows is completed, and what state holds is kept:
// another hub of the repository with its own main, and this hub's
// worktree state already records. This hub's main is recorded next to the
// other hub's. The state is seeded in the branch-keyed format of earlier
// releases.
func TestDoctorFix_PartiallyRecordedHub_Completed(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath, otherHub := "/hubs/repo", "/hubs/other"
	doctorHub(t, fs, hubPath, []string{"main", "feat", "extra"}, []string{"main", "feat", "extra"})
	otherMain := worktreeDir(otherHub, "main")
	require.NoError(t, fs.MkdirAll(otherMain, 0o755))

	created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	st := stateWithHub(otherHub)
	st.Repositories[registerRepoID].Worktrees = map[string]*state.WorktreeState{
		"main": {Path: otherMain, Type: hop.WorktreeTypeBare, HubPath: otherHub, CreatedAt: created},
		"feat": {Path: worktreeDir(hubPath, "feat"), Type: "linked", HubPath: hubPath, CreatedAt: created},
	}
	require.NoError(t, state.SaveState(fs, st))

	g := newRegistryGit(fs)
	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.NotEmpty(t, recordMessages(r, doctorKindFixed, hubPath), "records: %+v", r.records)
	for _, rec := range r.records {
		assert.NotEqual(t, doctorKindWarning, rec.Kind, "nothing is left over: %+v", rec)
	}
	assert.NoError(t, doctorResult(r), "records: %+v", r.records)

	got, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := got.Repositories[registerRepoID]
	require.NotNil(t, repo)
	var hubs []string
	for _, h := range repo.Hubs {
		hubs = append(hubs, h.Path)
	}
	assert.Equal(t, []string{otherHub, hubPath}, hubs, "the other hub is kept, this one added")
	require.Len(t, repo.Worktrees, 4)
	other, ok := repo.Worktree(otherHub, "main")
	require.True(t, ok, "the other hub's main is kept")
	assert.Equal(t, otherMain, other.Path)
	assert.True(t, other.CreatedAt.Equal(created))
	mine, ok := repo.Worktree(hubPath, "main")
	require.True(t, ok, "this hub's main is recorded next to it")
	assert.Equal(t, worktreeDir(hubPath, "main"), mine.Path)
	feat, ok := repo.Worktree(hubPath, "feat")
	require.True(t, ok)
	assert.True(t, feat.CreatedAt.Equal(created), "a recorded worktree is kept as is")
	extra, ok := repo.Worktree(hubPath, "extra")
	require.True(t, ok, "the unrecorded worktree is added")
	assert.Equal(t, worktreeDir(hubPath, "extra"), extra.Path)

	again := runDoctor(fs, g, hubPath, doctorOpts{})
	assert.Empty(t, again.records, "a rerun reports nothing")
	assert.NoError(t, doctorResult(again), "a rerun exits 0")
}

// A hub state records without one of its worktrees is an issue too, and
// --fix adds just that worktree.
func TestDoctorFix_RecordedHubMissingWorktree_Completed(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat"}, []string{"main", "feat"})
	st := stateWithHub(hubPath)
	require.NoError(t, st.PutWorktree(registerRepoID, &state.WorktreeState{
		Path: worktreeDir(hubPath, "main"), Branch: "main", Type: hop.WorktreeTypeBare, HubPath: hubPath,
	}))
	require.NoError(t, state.SaveState(fs, st))

	report := runDoctor(fs, newRegistryGit(fs), hubPath, doctorOpts{})
	issues := strings.Join(recordMessages(report, doctorKindIssue, hubPath), "\n")
	assert.Contains(t, issues, "feat")
	assert.Error(t, doctorResult(report))

	r := runDoctor(fs, newRegistryGit(fs), hubPath, doctorOpts{fix: true})
	assert.NoError(t, doctorResult(r), "records: %+v", r.records)
	got, err := state.LoadState(fs)
	require.NoError(t, err)
	repo := got.Repositories[registerRepoID]
	assert.Len(t, repo.Hubs, 1, "the hub is not recorded twice")
	feat, ok := repo.Worktree(hubPath, "feat")
	require.True(t, ok)
	assert.Equal(t, worktreeDir(hubPath, "feat"), feat.Path)
}
