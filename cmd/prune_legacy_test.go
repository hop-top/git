package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// withStateHome points XDG_STATE_HOME at dir for the test. Not
// parallel-safe: the xdg resolver reads the ambient process env.
func withStateHome(t *testing.T, dir string) {
	t.Helper()
	orig, had := os.LookupEnv("XDG_STATE_HOME")
	os.Setenv("XDG_STATE_HOME", dir)
	t.Cleanup(func() {
		if had {
			os.Setenv("XDG_STATE_HOME", orig)
		} else {
			os.Unsetenv("XDG_STATE_HOME")
		}
	})
}

func singleHubState(hubPath string) *state.State {
	return &state.State{
		Repositories: map[string]*state.RepositoryState{
			"github.com/test/repo": {Hubs: []*state.HubState{{Path: hubPath, Mode: "local"}}},
		},
	}
}

func ageDir(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	old := time.Now().Add(-100 * 24 * time.Hour)
	require.NoError(t, fs.Chtimes(path, old, old))
}

// TestPruneRepairBackups_RemovesOldBackupsInStateDir: retention GC follows
// the backups to their new home outside the hub.
func TestPruneRepairBackups_RemovesOldBackupsInStateDir(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	root := hop.RepairBackupRoot(hubPath)
	old := filepath.Join(root, "repair-20200101T000000Z")
	recent := filepath.Join(root, "repair-99990101T000000Z")
	require.NoError(t, fs.MkdirAll(old, 0755))
	require.NoError(t, fs.MkdirAll(recent, 0755))
	ageDir(t, fs, old)

	pruned := pruneRepairBackups(fs, mocks.NewMockGit(), singleHubState(hubPath), false)

	assert.Equal(t, 1, pruned)
	exists, _ := afero.DirExists(fs, old)
	assert.False(t, exists, "expired backup in state dir should be removed")
	exists, _ = afero.DirExists(fs, recent)
	assert.True(t, exists, "recent backup should remain")
}

// TestPruneRepairBackups_RemovesStaleLegacyLockAndEmptyDotHop: a hub
// polluted by an earlier repair (0-byte .hop/repair.lock plus an expired
// legacy snapshot) is left with no .hop/ at all.
func TestPruneRepairBackups_RemovesStaleLegacyLockAndEmptyDotHop(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	legacyOld := filepath.Join(hubPath, ".hop", "backups", "repair-20200101T000000Z")
	require.NoError(t, fs.MkdirAll(legacyOld, 0755))
	ageDir(t, fs, legacyOld)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(hubPath, ".hop", "repair.lock"), nil, 0644))

	pruned := pruneRepairBackups(fs, mocks.NewMockGit(), singleHubState(hubPath), false)

	assert.Equal(t, 1, pruned)
	exists, _ := afero.Exists(fs, filepath.Join(hubPath, ".hop"))
	assert.False(t, exists, "empty legacy .hop/ must be removed")
}

// TestPruneRepairBackups_LeavesDotHopWithOtherContent: .hop/ belongs to
// other tools too. Only git-hop's own artifacts go; anything else pins
// the directory in place.
func TestPruneRepairBackups_LeavesDotHopWithOtherContent(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	tlcDB := filepath.Join(hubPath, ".hop", "tlc", "db.sqlite")
	require.NoError(t, fs.MkdirAll(filepath.Dir(tlcDB), 0755))
	require.NoError(t, afero.WriteFile(fs, tlcDB, []byte("x"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(hubPath, ".hop", "repair.lock"), nil, 0644))
	stray := filepath.Join(hubPath, ".hop", "backups", "manual-not-managed")
	require.NoError(t, fs.MkdirAll(stray, 0755))

	pruneRepairBackups(fs, mocks.NewMockGit(), singleHubState(hubPath), false)

	exists, _ := afero.Exists(fs, filepath.Join(hubPath, ".hop", "repair.lock"))
	assert.False(t, exists, "stale legacy lock must be removed")
	exists, _ = afero.Exists(fs, tlcDB)
	assert.True(t, exists, ".hop/tlc must never be touched")
	exists, _ = afero.DirExists(fs, stray)
	assert.True(t, exists, "non repair- entries under .hop/backups must stay")
	exists, _ = afero.DirExists(fs, filepath.Join(hubPath, ".hop"))
	assert.True(t, exists, ".hop/ with other content must remain")
}

// TestPruneRepairBackups_MigratesUnexpiredLegacyBackups: a snapshot still
// inside retention moves to the state dir instead of pinning .hop/ for
// the rest of the retention window. It stays restorable by the same id.
func TestPruneRepairBackups_MigratesUnexpiredLegacyBackups(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	id := "repair-99990101T000000Z"
	legacy := filepath.Join(hubPath, ".hop", "backups", id)
	require.NoError(t, fs.MkdirAll(legacy, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(legacy, "hop.json"), []byte(`{"branches":{}}`), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(legacy, "manifest.json"), []byte(`{"version":1,"id":"`+id+`","hubPath":"`+hubPath+`","files":{}}`), 0644))

	pruned := pruneRepairBackups(fs, mocks.NewMockGit(), singleHubState(hubPath), false)

	assert.Equal(t, 0, pruned, "migration is not a prune")
	exists, _ := afero.Exists(fs, filepath.Join(hubPath, ".hop"))
	assert.False(t, exists, ".hop/ must be gone once its only content moved")
	moved := filepath.Join(hop.RepairBackupRoot(hubPath), id)
	data, err := afero.ReadFile(fs, filepath.Join(moved, "hop.json"))
	require.NoError(t, err, "payload must exist at the new location")
	assert.Equal(t, `{"branches":{}}`, string(data))
	list, err := hop.NewRepairBackup(fs, hubPath).List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, id, list[0].ID)
}

// TestPruneRepairBackups_DryRunLeavesLegacyIntact: preview must not
// move, unlink, or rmdir anything.
func TestPruneRepairBackups_DryRunLeavesLegacyIntact(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	id := "repair-99990101T000000Z"
	legacy := filepath.Join(hubPath, ".hop", "backups", id)
	require.NoError(t, fs.MkdirAll(legacy, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(legacy, "manifest.json"), []byte(`{"version":1,"id":"`+id+`","hubPath":"`+hubPath+`","files":{}}`), 0644))
	lock := filepath.Join(hubPath, ".hop", "repair.lock")
	require.NoError(t, afero.WriteFile(fs, lock, nil, 0644))

	pruneRepairBackups(fs, mocks.NewMockGit(), singleHubState(hubPath), true)

	exists, _ := afero.Exists(fs, lock)
	assert.True(t, exists, "dry-run must not unlink the legacy lock")
	exists, _ = afero.DirExists(fs, legacy)
	assert.True(t, exists, "dry-run must not move legacy backups")
	exists, _ = afero.DirExists(fs, hop.RepairBackupRoot(hubPath))
	assert.False(t, exists, "dry-run must not create the state dir")
}
