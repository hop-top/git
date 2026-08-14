package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// twoRepoState builds a state holding two unrelated repositories under
// different path roots — the shape of the 2026-07-18 repro, where a prune
// run inside repo A dropped repo B's registration.
//
// Repo A ("github.com/test/a") is the repo the user stands in. Its hub
// lives at hubA and is present on disk; it has one live worktree and one
// orphaned worktree. Repo B ("github.com/other/b") lives under an entirely
// different root, its hub directory is gone, and its worktree is gone too
// — so an unscoped prune deletes both of B's rows.
func twoRepoState(hubA, hubB string) *state.State {
	return &state.State{
		Version: "1.0.0",
		Repositories: map[string]*state.RepositoryState{
			"github.com/test/a": {
				URI:           "git@github.com:test/a.git",
				Org:           "test",
				Repo:          "a",
				DefaultBranch: "main",
				Worktrees: map[string]*state.WorktreeState{
					"main":      {Path: filepath.Join(hubA, "hops", "main"), Type: "linked", HubPath: hubA},
					"feat/gone": {Path: filepath.Join(hubA, "hops", "feat", "gone"), Type: "linked", HubPath: hubA},
				},
				Hubs: []*state.HubState{{Path: hubA, Mode: "local"}},
			},
			"github.com/other/b": {
				URI:           "git@github.com:other/b.git",
				Org:           "other",
				Repo:          "b",
				DefaultBranch: "main",
				Worktrees: map[string]*state.WorktreeState{
					"main": {Path: filepath.Join(hubB, "hops", "main"), Type: "linked", HubPath: hubB},
				},
				Hubs: []*state.HubState{{Path: hubB, Mode: "local"}},
			},
		},
	}
}

// TestResolvePruneScope_CurrentRepoOnly is the core regression: standing in
// repo A's hub, prune must see only repo A. Before the fix every prune pass
// ranged over all of st.Repositories regardless of cwd.
func TestResolvePruneScope_CurrentRepoOnly(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubA := "/roots/a/hub"
	hubB := "/elsewhere/b/hub"
	writePruneHubFor(t, fs, hubA, "test", "a", []string{"main", "feat/gone"}, []string{"main"})

	st := twoRepoState(hubA, hubB)

	scoped, err := resolvePruneScope(fs, st, filepath.Join(hubA, "hops", "main"), false /* all */)
	require.NoError(t, err)
	require.NotNil(t, scoped)

	assert.Len(t, scoped.Repositories, 1, "scoped prune must see exactly one repository")
	assert.Contains(t, scoped.Repositories, "github.com/test/a")
	assert.NotContains(t, scoped.Repositories, "github.com/other/b",
		"an unrelated repository must never be in prune's scope")
}

// TestResolvePruneScope_AllSweepsEverything guards against over-correcting:
// --all must still hand every repository to the prune passes.
func TestResolvePruneScope_AllSweepsEverything(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubA := "/roots/a/hub"
	hubB := "/elsewhere/b/hub"
	writePruneHubFor(t, fs, hubA, "test", "a", []string{"main"}, []string{"main"})

	st := twoRepoState(hubA, hubB)

	scoped, err := resolvePruneScope(fs, st, filepath.Join(hubA, "hops", "main"), true /* all */)
	require.NoError(t, err)
	require.NotNil(t, scoped)

	assert.Len(t, scoped.Repositories, 2, "--all must sweep every repository")
	assert.Contains(t, scoped.Repositories, "github.com/test/a")
	assert.Contains(t, scoped.Repositories, "github.com/other/b")
	assert.Same(t, st, scoped, "--all must operate on the live state so saves persist")
}

// TestResolvePruneScope_OutsideRepoRequiresAll pins requirement 3: run from
// nowhere in particular, prune must refuse rather than silently going global.
func TestResolvePruneScope_OutsideRepoRequiresAll(t *testing.T) {
	fs := afero.NewMemMapFs()
	st := twoRepoState("/roots/a/hub", "/elsewhere/b/hub")
	require.NoError(t, fs.MkdirAll("/tmp/nowhere", 0o755))

	scoped, err := resolvePruneScope(fs, st, "/tmp/nowhere", false /* all */)

	require.Error(t, err, "outside a hub, prune must error instead of going global")
	assert.Nil(t, scoped, "no scope may be returned when the repo cannot be resolved")
	assert.Contains(t, err.Error(), "--all",
		"the error must point the user at the opt-in flag")
}

// TestResolvePruneScope_UnregisteredHubErrors covers standing in a hub whose
// repo ID has no state entry: still not a licence to sweep every other repo.
func TestResolvePruneScope_UnregisteredHubErrors(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubA := "/roots/a/hub"
	writePruneHubFor(t, fs, hubA, "test", "unregistered", []string{"main"}, []string{"main"})

	st := twoRepoState("/roots/other/hub", "/elsewhere/b/hub")

	scoped, err := resolvePruneScope(fs, st, hubA, false)

	require.Error(t, err)
	assert.Nil(t, scoped)
}

// TestRunPruneAll_ScopedLeavesOtherRepoUntouched is the wiring test the
// method notes demand: it drives the same runPruneAll sequence runPrune
// drives, through the resolved scope, and asserts repo B survives in both
// state.json and its own hop.json.
//
// Reverting the scoping at any of the five call sites must turn this red,
// which is why it asserts on post-prune state rather than on a helper's
// return value.
func TestRunPruneAll_ScopedLeavesOtherRepoUntouched(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubA := "/roots/a/hub"
	hubB := "/elsewhere/b/hub"

	// Both hubs are real on disk with a stale hop.json row each.
	writePruneHubFor(t, fs, hubA, "test", "a", []string{"main", "feat/gone"}, []string{"main"})
	writePruneHubFor(t, fs, hubB, "other", "b", []string{"main", "feat/b-gone"}, []string{"main"})

	// Both hubs carry an expired repair backup.
	oldBackupA := filepath.Join(hubA, ".hop", "backups", "repair-20200101T000000Z")
	oldBackupB := filepath.Join(hubB, ".hop", "backups", "repair-20200101T000000Z")
	require.NoError(t, fs.MkdirAll(oldBackupA, 0o755))
	require.NoError(t, fs.MkdirAll(oldBackupB, 0o755))
	ancient := veryOld()
	require.NoError(t, fs.Chtimes(oldBackupA, ancient, ancient))
	require.NoError(t, fs.Chtimes(oldBackupB, ancient, ancient))

	st := twoRepoState(hubA, hubB)
	st.Repositories["github.com/other/b"].Worktrees["feat/b-gone"] =
		&state.WorktreeState{Path: filepath.Join(hubB, "hops", "feat", "b-gone"), Type: "linked", HubPath: hubB}
	// Repo B also carries a hub whose directory is gone — the exact shape
	// of the 2026-07-18 repro, where a prune inside repo A emitted
	// "Pruning orphaned hub: github.com/ExoFramework/monolith". Without
	// this row pruneOrphanedHubs has nothing to wrongly delete and the
	// site's scoping would be untested.
	st.Repositories["github.com/other/b"].Hubs = append(
		st.Repositories["github.com/other/b"].Hubs,
		&state.HubState{Path: "/elsewhere/b/deleted-hub", Mode: "local"})

	scoped, err := resolvePruneScope(fs, st, filepath.Join(hubA, "hops", "main"), false)
	require.NoError(t, err)

	counts := runPruneAll(fs, mocks.NewMockGit(), scoped, false)

	// Repo A was pruned.
	assert.Equal(t, 1, counts.worktrees, "repo A's orphaned worktree pruned")
	assert.Equal(t, 0, counts.hubs, "repo A has no orphaned hub")
	assert.Equal(t, 1, counts.hopJSONEntries, "repo A's stale hop.json row pruned")
	assert.Equal(t, 1, counts.repairBackups, "only repo A's expired backup pruned")
	assert.NotContains(t, st.Repositories["github.com/test/a"].Worktrees, "feat/gone")
	assert.ElementsMatch(t, []string{"main"}, hubBranchKeys(t, fs, hubA))

	// Repo B is untouched across every surface.
	assert.Contains(t, st.Repositories["github.com/other/b"].Worktrees, "feat/b-gone",
		"site 3: unrelated repo's state.json worktree rows must survive")
	assert.Len(t, st.Repositories["github.com/other/b"].Hubs, 2,
		"site 4: unrelated repo's state.json hub rows must survive, "+
			"including the one whose directory is gone")
	assert.ElementsMatch(t, []string{"main", "feat/b-gone"}, hubBranchKeys(t, fs, hubB),
		"site 5: unrelated repo's hop.json must not be rewritten")
	existsB, _ := afero.DirExists(fs, oldBackupB)
	assert.True(t, existsB, "site 1: unrelated repo's repair backups must not be deleted")
	backupDirB, _ := afero.DirExists(fs, filepath.Join(hubB, ".hop", "backups"))
	assert.True(t, backupDirB)
}

// TestRunPruneAll_AllStillSweepsGlobally is the counterweight: with --all
// resolved scope, every repo is reached. A fix that scoped unconditionally
// would turn this red.
func TestRunPruneAll_AllStillSweepsGlobally(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubA := "/roots/a/hub"
	hubB := "/elsewhere/b/hub"

	writePruneHubFor(t, fs, hubA, "test", "a", []string{"main", "feat/gone"}, []string{"main"})
	writePruneHubFor(t, fs, hubB, "other", "b", []string{"main", "feat/b-gone"}, []string{"main"})

	oldBackupB := filepath.Join(hubB, ".hop", "backups", "repair-20200101T000000Z")
	require.NoError(t, fs.MkdirAll(oldBackupB, 0o755))
	ancient := veryOld()
	require.NoError(t, fs.Chtimes(oldBackupB, ancient, ancient))

	st := twoRepoState(hubA, hubB)
	st.Repositories["github.com/other/b"].Worktrees["feat/b-gone"] =
		&state.WorktreeState{Path: filepath.Join(hubB, "hops", "feat", "b-gone"), Type: "linked", HubPath: hubB}
	st.Repositories["github.com/other/b"].Hubs = append(
		st.Repositories["github.com/other/b"].Hubs,
		&state.HubState{Path: "/elsewhere/b/deleted-hub", Mode: "local"})

	scoped, err := resolvePruneScope(fs, st, filepath.Join(hubA, "hops", "main"), true /* all */)
	require.NoError(t, err)

	counts := runPruneAll(fs, mocks.NewMockGit(), scoped, false)

	assert.Equal(t, 2, counts.worktrees, "--all must prune both repos' orphaned worktrees")
	assert.Equal(t, 1, counts.hubs, "--all must prune repo B's orphaned hub")
	assert.Equal(t, 2, counts.hopJSONEntries, "--all must prune both repos' stale hop.json rows")
	assert.Equal(t, 1, counts.repairBackups, "--all must reach repo B's expired backup")
	assert.NotContains(t, st.Repositories["github.com/other/b"].Worktrees, "feat/b-gone")
	assert.Len(t, st.Repositories["github.com/other/b"].Hubs, 1,
		"--all must drop repo B's missing hub row")
	assert.ElementsMatch(t, []string{"main"}, hubBranchKeys(t, fs, hubB))
	existsB, _ := afero.DirExists(fs, oldBackupB)
	assert.False(t, existsB, "--all must still reclaim repo B's expired backup")
}

// TestRepairBackupRetention_IgnoresOutOfScopeRepo covers the quietest of
// the five sites. repairBackupRetention returns the first configured
// hop.repair.backupRetention it finds; ranging over global state let an
// unrelated repository's setting decide which of the current repo's
// backups were old enough to delete. Repo B configures a 1h retention
// that would sweep repo A's day-old backup; scoped to A (which configures
// nothing) the 30-day default must win and the backup must survive.
func TestRepairBackupRetention_IgnoresOutOfScopeRepo(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubA := "/roots/a/hub"
	hubB := "/elsewhere/b/hub"
	writePruneHubFor(t, fs, hubA, "test", "a", []string{"main"}, []string{"main"})

	backupA := filepath.Join(hubA, ".hop", "backups", "repair-20990101T000000Z")
	require.NoError(t, fs.MkdirAll(backupA, 0o755))
	dayOld := time.Now().Add(-24 * time.Hour)
	require.NoError(t, fs.Chtimes(backupA, dayOld, dayOld))

	g := mocks.NewMockGit()
	// Only repo B's hub answers the config query.
	g.Runner.Responses[hubB+":git config --get hop.repair.backupRetention"] = "1h"

	st := twoRepoState(hubA, hubB)

	// Sanity: unscoped, repo B's 1h retention reaches across and the
	// day-old backup is deemed expired.
	assert.Equal(t, time.Hour, repairBackupRetention(g, st),
		"precondition: repo B's setting is discoverable in global state")

	scoped, err := resolvePruneScope(fs, st, hubA, false)
	require.NoError(t, err)

	assert.Equal(t, 30*24*time.Hour, repairBackupRetention(g, scoped),
		"an out-of-scope repo's retention must not govern this prune")

	pruned := pruneRepairBackups(fs, g, scoped, false)
	assert.Equal(t, 0, pruned, "day-old backup must survive the 30-day default")
	exists, _ := afero.DirExists(fs, backupA)
	assert.True(t, exists, "site 2: cross-repo retention must not delete this backup")
}

// TestPruneOutput_NamesAffectedRepo pins requirement 4: every prune line
// identifies the repository it touched, so cross-repo effects under --all
// are visible rather than inferred from a bare path.
func TestPruneOutput_NamesAffectedRepo(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubA := "/roots/a/hub"
	hubB := "/elsewhere/b/hub"
	writePruneHubFor(t, fs, hubA, "test", "a", []string{"main", "feat/gone"}, []string{"main"})
	writePruneHubFor(t, fs, hubB, "other", "b", []string{"main", "feat/b-gone"}, []string{"main"})

	oldBackupB := filepath.Join(hubB, ".hop", "backups", "repair-20200101T000000Z")
	require.NoError(t, fs.MkdirAll(oldBackupB, 0o755))
	ancient := veryOld()
	require.NoError(t, fs.Chtimes(oldBackupB, ancient, ancient))

	st := twoRepoState(hubA, hubB)
	st.Repositories["github.com/other/b"].Worktrees["feat/b-gone"] =
		&state.WorktreeState{Path: filepath.Join(hubB, "hops", "feat", "b-gone"), Type: "linked", HubPath: hubB}
	// Orphan repo B's hub so pruneOrphanedHubs emits its line too.
	st.Repositories["github.com/other/b"].Hubs = append(
		st.Repositories["github.com/other/b"].Hubs,
		&state.HubState{Path: "/elsewhere/b/gone-hub", Mode: "local"})

	out := captureStdout(t, func() {
		runPruneAll(fs, mocks.NewMockGit(), st, false)
	})

	assert.Contains(t, out, "github.com/other/b",
		"hop.json prune lines must name the repository they affect")
	assert.Contains(t, out, "github.com/test/a",
		"prune lines for the current repo must name it too")

	// Every "Pruning" line must carry a repository ID.
	for _, line := range pruneLines(out) {
		assert.Regexp(t, `github\.com/[^ :)]+`, line,
			"prune line without a repository ID: %q", line)
	}
}
