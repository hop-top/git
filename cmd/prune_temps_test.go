package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/filelock"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// A save of hop.json or state.json that dies before renaming its temp
// file into place leaves the temp file behind. prune removes those an
// hour old, only those named as a save names them, and none while a
// save holds the file's lock.

// tempFiles is a stale and a fresh temp file in a directory, and stale
// files there whose names no save gives its temp files.
type tempFiles struct {
	stale, fresh string
	others       []string
}

// writeTempFiles lays out tempFiles in dir: temp names are
// <prefix><digits>.tmp; the others are named after it but differ.
func writeTempFiles(t *testing.T, fs afero.Fs, dir, prefix string) tempFiles {
	t.Helper()
	f := tempFiles{
		stale: filepath.Join(dir, prefix+"111111111.tmp"),
		fresh: filepath.Join(dir, prefix+"222222222.tmp"),
		others: []string{
			filepath.Join(dir, prefix+"tmp"),
			filepath.Join(dir, prefix+"333.tmp.bak"),
			filepath.Join(dir, "x"+prefix+"444.tmp"),
			filepath.Join(dir, "notes.tmp"),
		},
	}
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	old := time.Now().Add(-2 * filelock.StaleTempAge)
	for _, p := range append([]string{f.stale, f.fresh}, f.others...) {
		require.NoError(t, afero.WriteFile(fs, p, []byte("{}"), 0o600))
		if p != f.fresh {
			require.NoError(t, fs.Chtimes(p, old, old))
		}
	}
	return f
}

// assertKept fails for each file of f but the stale one that is gone,
// and reports whether the stale one is present.
func (f tempFiles) assertKept(t *testing.T, fs afero.Fs) bool {
	t.Helper()
	for _, p := range append([]string{f.fresh}, f.others...) {
		ok, _ := afero.Exists(fs, p)
		assert.True(t, ok, "%s removed", filepath.Base(p))
	}
	ok, _ := afero.Exists(fs, f.stale)
	return ok
}

func TestPrune_SweepsStaleHubTemps(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hub := "/hubs/repo"
	writePruneHub(t, fs, hub, []string{"main"}, []string{"main"})
	f := writeTempFiles(t, fs, hub, "hop-config-")
	st := stateWithHub(hub)

	var edits stateEdits
	counts := runPruneAll(fs, mocks.NewMockGit(), st, true, &edits)
	assert.Equal(t, 1, counts.tempFiles)
	assert.Equal(t, []pruneRecord{{Action: "would-prune", Kind: pruneKindTempFile, Repository: "github.com/test/repo", Path: f.stale}}, counts.records)
	assert.True(t, f.assertKept(t, fs), "dry run removed the stale temp file")

	counts = runPruneAll(fs, mocks.NewMockGit(), st, false, &edits)
	assert.Equal(t, 1, counts.tempFiles)
	assert.Equal(t, []pruneRecord{{Action: "pruned", Kind: pruneKindTempFile, Repository: "github.com/test/repo", Path: f.stale}}, counts.records)
	assert.False(t, f.assertKept(t, fs), "stale temp file kept")
	assert.Equal(t, 1, counts.total())
}

// A --global hub's hopspace, in the data home, holds hop.json,
// ports.json and volumes.json, and their temp files, too.
func TestPrune_SweepsStaleHopspaceTemps(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hub := "/hubs/repo"
	hopspace := writeGlobalHub(t, fs, hub)
	require.NoError(t, fs.MkdirAll(hopspace, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(hopspace, "hop.json"), []byte("{}"), 0o644))
	f := writeTempFiles(t, fs, hopspace, "hop-config-")

	var edits stateEdits
	counts := runPruneAll(fs, mocks.NewMockGit(), stateWithHub(hub), false, &edits)
	assert.Equal(t, 1, counts.tempFiles)
	assert.False(t, f.assertKept(t, fs), "stale hopspace temp file kept")
}

// writeGlobalHub writes a --global hub's hop.json at hub and returns its
// hopspace, which it does not create.
func writeGlobalHub(t *testing.T, fs afero.Fs, hub string) string {
	t.Helper()
	t.Setenv("GIT_HOP_DATA_HOME", "/data")
	require.NoError(t, fs.MkdirAll(hub, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(hub, "hop.json"), []byte(
		`{"repo": {"uri": "git@github.com:acme/widgets.git", "org": "acme", "repo": "widgets", "defaultBranch": "main", "mode": "global"}}`), 0o644))
	cfg, err := hop.LoadHub(fs, hub)
	require.NoError(t, err)
	hopspace := hop.ResolveHopspacePath(hub, cfg.Config.Repo)
	require.NotEqual(t, hub, hopspace)
	return hopspace
}

// A directory without hop.json holds no hop.json temp files: prune
// leaves its files, a hub's or a hopspace's.
func TestPrune_HubTempsNeedHopJSON(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	plain := writeTempFiles(t, fs, "/hubs/plain", "hop-config-")
	hopspace := writeGlobalHub(t, fs, "/hubs/global")
	bare := writeTempFiles(t, fs, hopspace, "hop-config-")

	var edits stateEdits
	st := stateWithHub("/hubs/plain")
	st.Repositories["github.com/test/repo"].Hubs = append(st.Repositories["github.com/test/repo"].Hubs,
		&state.HubState{Path: "/hubs/global", Mode: "global"})
	counts := runPruneAll(fs, mocks.NewMockGit(), st, false, &edits)
	assert.Zero(t, counts.tempFiles)
	assert.True(t, plain.assertKept(t, fs))
	assert.True(t, bare.assertKept(t, fs))
}

// state.json's temp files belong to no one repository: only prune --all
// sweeps them, as it does state backups.
func TestPrune_SweepsStaleStateTempsWithAll(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hub := "/hubs/repo"
	writePruneHub(t, fs, hub, []string{"main"}, []string{"main"})
	f := writeTempFiles(t, fs, state.GetStateHome(), "state.json.")
	st := stateWithHub(hub)

	counts, err := pruneAndSave(fs, mocks.NewMockGit(), st, st, false, false)
	require.NoError(t, err)
	assert.Zero(t, counts.tempFiles)
	assert.True(t, f.assertKept(t, fs), "prune without --all removed a state temp file")

	counts, err = pruneAndSave(fs, mocks.NewMockGit(), st, st, true, true)
	require.NoError(t, err)
	assert.Equal(t, 1, counts.tempFiles)
	assert.Equal(t, []pruneRecord{{Action: "would-prune", Kind: pruneKindTempFile, Path: f.stale}}, counts.records)
	assert.True(t, f.assertKept(t, fs), "dry run removed the stale temp file")

	counts, err = pruneAndSave(fs, mocks.NewMockGit(), st, st, true, false)
	require.NoError(t, err)
	assert.Equal(t, 1, counts.tempFiles)
	assert.False(t, f.assertKept(t, fs), "stale state temp file kept")
}

// A save holding the lock is running now: prune leaves the temp files.
func TestPrune_TempsSkippedWhileLockHeld(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", home)
	fs := afero.NewOsFs()
	hub := filepath.Join(t.TempDir(), "hub")
	writePruneHub(t, fs, hub, []string{"main"}, []string{"main"})
	hubTemps := writeTempFiles(t, fs, hub, "hop-config-")
	stateTemps := writeTempFiles(t, fs, state.GetStateHome(), "state.json.")
	for _, lock := range []string{filepath.Join(hub, hop.HopJSONLockName), filepath.Join(state.GetStateHome(), state.LockName)} {
		l := filelock.New(lock)
		ok, err := l.TryAcquire()
		require.NoError(t, err)
		require.True(t, ok)
		t.Cleanup(func() { _ = l.Release() })
	}
	st := stateWithHub(hub)

	for _, dryRun := range []bool{true, false} {
		counts, err := pruneAndSave(fs, mocks.NewMockGit(), st, st, true, dryRun)
		require.NoError(t, err)
		assert.Zero(t, counts.tempFiles, "dryRun=%v", dryRun)
		assert.True(t, hubTemps.assertKept(t, fs), "hub temp file removed under a held lock")
		assert.True(t, stateTemps.assertKept(t, fs), "state temp file removed under a held lock")
	}
	_, err := os.Stat(hubTemps.stale)
	require.NoError(t, err)
}
