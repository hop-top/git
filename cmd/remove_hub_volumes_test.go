package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/services"
)

// pgVersion is a file of volume data: what a database leaves in its
// bind-mounted directory.
const pgVersion = "16\n"

// writeVolumeData puts a file of volume data in dir.
func writeVolumeData(t *testing.T, fs afero.Fs, dir string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PG_VERSION"), []byte(pgVersion), 0o644))
}

// orphanedVolumes returns the directories removals moved volume data of
// test/repo to, named name-<UTC>.
func orphanedVolumes(t *testing.T, fs afero.Fs, name string) []string {
	t.Helper()
	base := hop.GetHopspacePath(filepath.Join(hop.GetGitHopDataHome(), orphanedVolumesDir), hop.RepoRef{Org: "test", Repo: "repo"})
	matches, err := afero.Glob(fs, filepath.Join(base, name+"-*"))
	require.NoError(t, err)
	return matches
}

// recordOf returns the record of kind from recs.
func recordOf(t *testing.T, recs []removeRecord, kind string) removeRecord {
	t.Helper()
	for _, r := range recs {
		if r.Kind == kind {
			return r
		}
	}
	t.Fatalf("no %s record in %+v", kind, recs)
	return removeRecord{}
}

// A default hub keeps its volumes in itself. Removing the hub used to
// delete them with it; the data now survives, moved aside, and a hint
// says where and how to delete it.
func TestRemoveHub_KeepsVolumeDataByDefault(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hub := "/hubs/app"
	hubFixture(t, fs, hub, false)
	writeVolumeData(t, fs, filepath.Join(hub, "volumes", "hop_main_db"))

	var recs []removeRecord
	_, stderr := captureRemoveOutput(t, func() { recs = removeHub(fs, hub, false) })

	gone, _ := afero.DirExists(fs, hub)
	assert.False(t, gone, "the hub directory goes")
	dirs := orphanedVolumes(t, fs, services.HubKey(hub))
	require.Len(t, dirs, 1, "the volume data is moved aside")
	data, err := afero.ReadFile(fs, filepath.Join(dirs[0], "volumes", "hop_main_db", "PG_VERSION"))
	require.NoError(t, err, "the volume data survives")
	assert.Equal(t, pgVersion, string(data))

	assert.Contains(t, stderr, "hint: kept the volume data at:")
	assert.Contains(t, stderr, filepath.Join(dirs[0], "volumes"))
	assert.Contains(t, stderr, "--delete-volumes")

	rec := recordOf(t, recs, removeKindHub)
	assert.Equal(t, volumesKept, rec.Volumes)
	assert.Equal(t, []string{filepath.Join(dirs[0], "volumes")}, rec.VolumePaths)
}

// --delete-volumes deletes the volume data with the hub.
func TestRemoveHub_DeleteVolumes(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hub := "/hubs/app"
	hubFixture(t, fs, hub, false)
	vol := filepath.Join(hub, "volumes", "hop_main_db")
	writeVolumeData(t, fs, vol)

	var recs []removeRecord
	_, stderr := captureRemoveOutput(t, func() { recs = removeHub(fs, hub, true) })

	gone, _ := afero.DirExists(fs, hub)
	assert.False(t, gone)
	assert.Empty(t, orphanedVolumes(t, fs, services.HubKey(hub)), "nothing is moved aside")
	assert.NotContains(t, stderr, "kept the volume data")

	rec := recordOf(t, recs, removeKindHub)
	assert.Equal(t, volumesDeleted, rec.Volumes)
	assert.Equal(t, []string{filepath.Join(hub, "volumes")}, rec.VolumePaths)
}

// A hub without volume data goes whole, as before, and its record says
// nothing about volumes. An empty volumes directory holds no data.
func TestRemoveHub_NoVolumeData(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hub := "/hubs/app"
	hubFixture(t, fs, hub, false)
	require.NoError(t, fs.MkdirAll(filepath.Join(hub, "volumes", "hop_main_db"), 0o755))

	var recs []removeRecord
	_, stderr := captureRemoveOutput(t, func() { recs = removeHub(fs, hub, false) })

	gone, _ := afero.DirExists(fs, hub)
	assert.False(t, gone)
	assert.Empty(t, orphanedVolumes(t, fs, services.HubKey(hub)))
	assert.NotContains(t, stderr, "volume data")
	rec := recordOf(t, recs, removeKindHub)
	assert.Empty(t, rec.Volumes)
	assert.Empty(t, rec.VolumePaths)
}

// The volume directories volumes.json records count wherever in the hub
// they are, and so does its basePath.
func TestRemoveHub_KeepsRecordedVolumeDirs(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hub := "/hubs/app"
	hubFixture(t, fs, hub, false)
	custom := filepath.Join(hub, "data", "pg")
	base := filepath.Join(hub, "vols")
	writeVolumeData(t, fs, custom)
	writeVolumeData(t, fs, filepath.Join(base, "hop_main_cache"))
	require.NoError(t, config.NewWriter(fs).WriteVolumesConfig(hub, &config.VolumesConfig{
		BasePath: base,
		Branches: map[string]config.BranchVolumes{
			"main": {Volumes: map[string]string{"db": custom, "cache": filepath.Join(base, "hop_main_cache")}},
		},
	}))

	captureRemoveOutput(t, func() { removeHub(fs, hub, false) })

	dirs := orphanedVolumes(t, fs, services.HubKey(hub))
	require.Len(t, dirs, 1)
	for _, rel := range []string{"data/pg", "vols/hop_main_cache"} {
		_, err := afero.ReadFile(fs, filepath.Join(dirs[0], rel, "PG_VERSION"))
		assert.NoError(t, err, "%s survives", rel)
	}
	gone, _ := afero.DirExists(fs, hub)
	assert.False(t, gone)
}

// renameFailFs fails every rename below prefix, as a move to another
// filesystem does.
type renameFailFs struct {
	afero.Fs
	prefix string
}

func (f renameFailFs) Rename(from, to string) error {
	if isWithinPath(from, f.prefix) {
		return errors.New("invalid cross-device link")
	}
	return f.Fs.Rename(from, to)
}

// Volume data that cannot be moved aside stays where it is: the hub
// directory then holds it alone, and the hint names it there.
func TestRemoveHub_LeavesVolumeDataInPlaceWhenItCannotMove(t *testing.T) {
	isolateDoctorPaths(t)
	hub := "/hubs/app"
	vols := filepath.Join(hub, "volumes")
	fs := renameFailFs{afero.NewMemMapFs(), vols}
	hubFixture(t, fs, hub, false)
	writeVolumeData(t, fs, filepath.Join(vols, "hop_main_db"))

	var recs []removeRecord
	_, stderr := captureRemoveOutput(t, func() { recs = removeHub(fs, hub, false) })

	_, err := afero.ReadFile(fs, filepath.Join(vols, "hop_main_db", "PG_VERSION"))
	require.NoError(t, err, "the volume data survives in place")
	entries, err := afero.ReadDir(fs, hub)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the hub directory holds only its volumes")
	assert.Equal(t, "volumes", entries[0].Name())
	assert.Empty(t, orphanedVolumes(t, fs, services.HubKey(hub)), "no empty move-aside directory is left")
	assert.Contains(t, stderr, "warning: Cannot move volume data")
	assert.Contains(t, stderr, "hint:   "+vols)
	assert.Equal(t, []string{vols}, recordOf(t, recs, removeKindHub).VolumePaths)
}

// --dry-run shows both behaviours and touches nothing.
func TestPreviewRemoveHub_Volumes(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hub := "/hubs/app"
	hubFixture(t, fs, hub, false)
	vols := filepath.Join(hub, "volumes")
	writeVolumeData(t, fs, filepath.Join(vols, "hop_main_db"))

	var recs []removeRecord
	stdout, _ := captureRemoveOutput(t, func() { recs = previewRemoveHub(fs, hub, true, false) })
	assert.Contains(t, stdout, "[dry-run] Would keep volume data at "+vols+", moved to ")
	assert.Contains(t, stdout, orphanedVolumesDir)
	assert.Equal(t, volumesKept, recordOf(t, recs, removeKindHub).Volumes)

	stdout, _ = captureRemoveOutput(t, func() { recs = previewRemoveHub(fs, hub, true, true) })
	assert.Contains(t, stdout, "[dry-run] Would delete volume data at "+vols+" (--delete-volumes)")
	assert.Equal(t, volumesDeleted, recordOf(t, recs, removeKindHub).Volumes)

	_, err := afero.ReadFile(fs, filepath.Join(vols, "hop_main_db", "PG_VERSION"))
	assert.NoError(t, err, "dry-run touches nothing")
	assert.Empty(t, orphanedVolumes(t, fs, services.HubKey(hub)))
}

// The data-home hopspace of the last --global hub goes with it, but its
// volume data is kept, moved aside, unless --delete-volumes.
func TestRemoveHub_GlobalHopspaceKeepsVolumeData(t *testing.T) {
	for _, deleteVolumes := range []bool{false, true} {
		name := "default"
		if deleteVolumes {
			name = "--delete-volumes"
		}
		t.Run(name, func(t *testing.T) {
			isolateDoctorPaths(t)
			fs := afero.NewMemMapFs()
			g1 := "/hubs/g1"
			hubFixture(t, fs, g1, true)
			hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true})
			writeVolumeData(t, fs, filepath.Join(hopspacePath, "volumes", services.HubKey(g1), "hop_main_db"))

			var recs []removeRecord
			_, stderr := captureRemoveOutput(t, func() { recs = removeHub(fs, g1, deleteVolumes) })

			exists, _ := afero.DirExists(fs, hopspacePath)
			assert.False(t, exists, "the hopspace goes with its last hub")
			rec := recordOf(t, recs, removeKindHopspace)
			assert.True(t, rec.Removed)
			dirs := orphanedVolumes(t, fs, "hopspace")
			if deleteVolumes {
				assert.Empty(t, dirs)
				assert.Equal(t, volumesDeleted, rec.Volumes)
				return
			}
			require.Len(t, dirs, 1)
			_, err := afero.ReadFile(fs, filepath.Join(dirs[0], "volumes", services.HubKey(g1), "hop_main_db", "PG_VERSION"))
			assert.NoError(t, err, "the hopspace's volume data survives")
			assert.Equal(t, volumesKept, rec.Volumes)
			assert.Equal(t, []string{filepath.Join(dirs[0], "volumes")}, rec.VolumePaths)
			assert.Contains(t, stderr, filepath.Join(dirs[0], "volumes"))
		})
	}
}

// A hopspace other hubs keep stays, and so does the removed hub's volume
// data in it, unless --delete-volumes; the other hubs' data is never
// touched.
func TestRemoveHub_SharedHopspaceHubVolumes(t *testing.T) {
	for _, deleteVolumes := range []bool{false, true} {
		name := "default"
		if deleteVolumes {
			name = "--delete-volumes"
		}
		t.Run(name, func(t *testing.T) {
			isolateDoctorPaths(t)
			fs := afero.NewMemMapFs()
			g1, g2 := "/hubs/g1", "/hubs/g2"
			hubFixture(t, fs, g1, true)
			hubFixture(t, fs, g2, true)
			hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, g2: true})
			mine := filepath.Join(hopspacePath, "volumes", services.HubKey(g1))
			theirs := filepath.Join(hopspacePath, "volumes", services.HubKey(g2))
			writeVolumeData(t, fs, filepath.Join(mine, "hop_main_db"))
			writeVolumeData(t, fs, filepath.Join(theirs, "hop_main_db"))

			var recs []removeRecord
			_, stderr := captureRemoveOutput(t, func() { recs = removeHub(fs, g1, deleteVolumes) })

			_, err := afero.ReadFile(fs, filepath.Join(theirs, "hop_main_db", "PG_VERSION"))
			assert.NoError(t, err, "the other hub's volume data is untouched")
			rec := recordOf(t, recs, removeKindHopspace)
			assert.False(t, rec.Removed)
			assert.Equal(t, []string{mine}, rec.VolumePaths)
			_, err = afero.ReadFile(fs, filepath.Join(mine, "hop_main_db", "PG_VERSION"))
			if deleteVolumes {
				assert.Error(t, err, "--delete-volumes deletes the hub's volume data")
				assert.Equal(t, volumesDeleted, rec.Volumes)
				return
			}
			assert.NoError(t, err, "the hub's volume data is kept")
			assert.Equal(t, volumesKept, rec.Volumes)
			assert.True(t, strings.Contains(stderr, "hint:   "+mine), stderr)
		})
	}
}

// removeAllExcept leaves the kept paths and the directories leading to
// them, and removes everything else.
func TestRemoveAllExcept(t *testing.T) {
	fs := afero.NewMemMapFs()
	for _, f := range []string{"/r/a/keep/x", "/r/a/drop/y", "/r/b/z", "/r/top"} {
		require.NoError(t, afero.WriteFile(fs, f, []byte("1"), 0o644))
	}
	require.NoError(t, removeAllExcept(fs, "/r", []string{"/r/a/keep"}))

	for f, want := range map[string]bool{"/r/a/keep/x": true, "/r/a/drop": false, "/r/b": false, "/r/top": false} {
		ok, _ := afero.Exists(fs, f)
		assert.Equal(t, want, ok, f)
	}

	require.NoError(t, removeAllExcept(fs, "/r", nil))
	ok, _ := afero.Exists(fs, "/r")
	assert.False(t, ok, "nothing kept: all of it goes")
}

// Two removals in the same second get directories of their own: a
// move-aside never lands in, or overwrites, an earlier one.
func TestNewOrphanedVolumesDir_NeverReuses(t *testing.T) {
	fs := afero.NewMemMapFs()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	first, err := newOrphanedVolumesDir(fs, "/data/orphaned-volumes/o/r/hub", now)
	require.NoError(t, err)
	second, err := newOrphanedVolumesDir(fs, "/data/orphaned-volumes/o/r/hub", now)
	require.NoError(t, err)
	assert.Equal(t, "/data/orphaned-volumes/o/r/hub-20260925T120000Z", first)
	assert.Equal(t, first+"-1", second)
}
