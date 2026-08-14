package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// writePruneHub writes a hub hop.json at hubPath listing branches,
// and creates on disk only the directories named in present.
func writePruneHub(t *testing.T, fs afero.Fs, hubPath string, branches []string, present []string) {
	t.Helper()
	cfg := &config.HubConfig{
		Repo: config.RepoConfig{
			URI:           "git@github.com:test/repo.git",
			Org:           "test",
			Repo:          "repo",
			DefaultBranch: "main",
		},
		Branches: map[string]config.HubBranch{},
	}
	for _, b := range branches {
		cfg.Branches[b] = config.HubBranch{
			Path:           config.MakeWorktreePath(b),
			HopspaceBranch: b,
		}
	}
	require.NoError(t, fs.MkdirAll(hubPath, 0o755))
	require.NoError(t, config.NewWriter(fs).WriteHubConfig(hubPath, cfg))
	for _, b := range present {
		require.NoError(t, fs.MkdirAll(filepath.Join(hubPath, config.MakeWorktreePath(b)), 0o755))
	}
}

func stateWithHub(hubPath string) *state.State {
	return &state.State{
		Version: "1.0.0",
		Repositories: map[string]*state.RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees:     map[string]*state.WorktreeState{},
				Hubs:          []*state.HubState{{Path: hubPath, Mode: "local"}},
			},
		},
	}
}

func hubBranchKeys(t *testing.T, fs afero.Fs, hubPath string) []string {
	t.Helper()
	hub, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)
	keys := make([]string, 0, len(hub.Config.Branches))
	for k := range hub.Config.Branches {
		keys = append(keys, k)
	}
	return keys
}

// TestPruneOrphanedHubBranches_DropsMissingHopJSONRows is the regression
// for prune reporting "Pruned N worktree(s)" while the hub hop.json rows
// survived — `git hop status` reads hop.json, so those rows kept showing
// up as State: Missing after every prune.
func TestPruneOrphanedHubBranches_DropsMissingHopJSONRows(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath,
		[]string{"main", "feat/gone", "feat/also-gone"},
		[]string{"main"})

	g := mocks.NewMockGit()
	st := stateWithHub(hubPath)

	pruned := pruneOrphanedHubBranches(fs, g, st, false)

	assert.Equal(t, 2, pruned, "both missing hop.json rows should be pruned")
	assert.ElementsMatch(t, []string{"main"}, hubBranchKeys(t, fs, hubPath),
		"hop.json must retain only branches whose worktree dir exists")
}

// TestPruneOrphanedHubBranches_DryRun proves -n reports without mutating
// hop.json.
func TestPruneOrphanedHubBranches_DryRun(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath,
		[]string{"main", "feat/gone"},
		[]string{"main"})

	g := mocks.NewMockGit()
	st := stateWithHub(hubPath)

	pruned := pruneOrphanedHubBranches(fs, g, st, true)

	assert.Equal(t, 1, pruned)
	assert.ElementsMatch(t, []string{"main", "feat/gone"}, hubBranchKeys(t, fs, hubPath),
		"dry-run must leave hop.json untouched")
	exists, _ := afero.DirExists(fs, filepath.Join(hubPath, ".hop", "backups"))
	assert.False(t, exists, "dry-run must not write a backup")
}

// TestPruneOrphanedHubBranches_BacksUpHopJSON confirms prune reuses the
// same backup mechanism repair uses before rewriting hop.json.
func TestPruneOrphanedHubBranches_BacksUpHopJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath,
		[]string{"main", "feat/gone"},
		[]string{"main"})

	g := mocks.NewMockGit()
	st := stateWithHub(hubPath)

	require.Equal(t, 1, pruneOrphanedHubBranches(fs, g, st, false))

	backups, err := hop.NewRepairBackup(fs, hubPath).List()
	require.NoError(t, err)
	require.Len(t, backups, 1, "one backup per mutating prune")
	restored, err := afero.ReadFile(fs,
		filepath.Join(hop.NewRepairBackup(fs, hubPath).Path(backups[0].ID), "hop.json"))
	require.NoError(t, err)
	assert.Contains(t, string(restored), "feat/gone",
		"backup must capture the pre-prune hop.json")
}

// TestPruneOrphanedHubBranches_NoOpWhenClean guarantees a healthy hub
// yields no count and no backup churn.
func TestPruneOrphanedHubBranches_NoOpWhenClean(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main"}, []string{"main"})

	g := mocks.NewMockGit()
	st := stateWithHub(hubPath)

	assert.Equal(t, 0, pruneOrphanedHubBranches(fs, g, st, false))
	exists, _ := afero.DirExists(fs, filepath.Join(hubPath, ".hop", "backups"))
	assert.False(t, exists, "clean hub must not accumulate backups")
}

// TestRunPruneAll_ClearsHopJSONRows pins the wiring: a full prune pass
// (the same sequence runPrune drives) must reach hop.json, not just
// state.json. Without the hop.json pass wired in, prune reported
// "Pruned N" while `git hop status` kept listing the rows as Missing.
func TestRunPruneAll_ClearsHopJSONRows(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath,
		[]string{"main", "feat/gone"},
		[]string{"main"})

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"main":      {Path: filepath.Join(hubPath, "hops", "main"), Type: "linked"},
		"feat/gone": {Path: filepath.Join(hubPath, "hops", "feat", "gone"), Type: "linked"},
	}

	counts := runPruneAll(fs, mocks.NewMockGit(), st, false)

	assert.Equal(t, 1, counts.hopJSONEntries, "hop.json pass must run")
	assert.Equal(t, 1, counts.worktrees, "state.json pass must still run")
	assert.ElementsMatch(t, []string{"main"}, hubBranchKeys(t, fs, hubPath),
		"the Missing row must be gone from hop.json after a full prune")
}

// TestPruneOrphanedHubBranches_SkipsHubsMissingHopJSON covers a state
// entry pointing at a directory that is not (or no longer) a hub.
func TestPruneOrphanedHubBranches_SkipsHubsMissingHopJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/not-a-hub"
	require.NoError(t, fs.MkdirAll(hubPath, 0o755))

	g := mocks.NewMockGit()
	st := stateWithHub(hubPath)

	assert.Equal(t, 0, pruneOrphanedHubBranches(fs, g, st, false))
}
