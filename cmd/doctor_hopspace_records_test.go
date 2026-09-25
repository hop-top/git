package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// goneHubsFixture writes a shared hopspace recording a worktree of each
// of four hubs: the current one (/hubs/g1, on disk and in state), one
// state records whose directory is gone, one on disk state does not
// record, and one gone both ways. Only the last one's record is stale.
func goneHubsFixture(t *testing.T, fs afero.Fs) (hubPath, hopspacePath string) {
	t.Helper()
	isolateDoctorPaths(t)
	hubPath = "/hubs/g1"
	hopspacePath = sharedHopspaceFixture(t, fs, map[string]bool{hubPath: true, "/hubs/instate": true})
	for _, dir := range []string{hubPath, "/hubs/ondisk"} {
		require.NoError(t, fs.MkdirAll(dir, 0o755))
	}
	hs, err := hop.LoadHopspace(fs, hopspacePath)
	require.NoError(t, err)
	for _, hub := range []string{hubPath, "/hubs/instate", "/hubs/ondisk", "/hubs/gone"} {
		require.NoError(t, hs.RegisterBranch(hub, "main", filepath.Join(hub, "hops", "main")))
	}
	return hubPath, hopspacePath
}

func hopspaceKeys(t *testing.T, fs afero.Fs, hopspacePath string) []string {
	t.Helper()
	hs, err := hop.LoadHopspace(fs, hopspacePath)
	require.NoError(t, err)
	var keys []string
	for k := range hs.Config.Branches {
		keys = append(keys, k)
	}
	return keys
}

// A record is stale only when its hub is neither in state nor on disk.
// Without --fix it is a warning, which leaves the exit status alone; --fix
// --dry-run plans the drop and writes nothing; --fix drops only it.
func TestCheckGoneHubRecords(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath, hopspacePath := goneHubsFixture(t, fs)
	all := hopspaceKeys(t, fs, hopspacePath)
	stale := "/hubs/gone/hops/main"

	r := doctorReport{}
	checkGoneHubRecords(fs, hubPath, hopspacePath, doctorOpts{}, &r)
	require.Len(t, r.records, 1)
	assert.Equal(t, doctorKindWarning, r.records[0].Kind)
	assert.Equal(t, stale, r.records[0].Subject)
	assert.Contains(t, r.records[0].Message, "hub /hubs/gone, which no longer exists")
	assert.NoError(t, doctorResult(r), "a warning leaves the exit status alone")

	r = doctorReport{}
	checkGoneHubRecords(fs, hubPath, hopspacePath, doctorOpts{fix: true, dryRun: true}, &r)
	assert.Equal(t, 1, r.fixed)
	assert.Equal(t, doctorKindWouldFix, r.records[len(r.records)-1].Kind)
	assert.ElementsMatch(t, all, hopspaceKeys(t, fs, hopspacePath), "--dry-run drops nothing")

	r = doctorReport{}
	checkGoneHubRecords(fs, hubPath, hopspacePath, doctorOpts{fix: true}, &r)
	assert.Equal(t, 1, r.fixed)
	assert.Equal(t, doctorKindFixed, r.records[len(r.records)-1].Kind)
	left := hopspaceKeys(t, fs, hopspacePath)
	assert.NotContains(t, left, stale)
	assert.Len(t, left, len(all)-1)

	r = doctorReport{}
	checkGoneHubRecords(fs, hubPath, hopspacePath, doctorOpts{}, &r)
	assert.Empty(t, r.records)
}

// A hub's own hopspace (a local hub) is never checked, and neither is a
// shared one when state cannot be read: which hubs exist is unknown.
func TestCheckGoneHubRecords_Skips(t *testing.T) {
	t.Run("own hopspace", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		_, hopspacePath := goneHubsFixture(t, fs)
		r := doctorReport{}
		checkGoneHubRecords(fs, hopspacePath, hopspacePath, doctorOpts{fix: true}, &r)
		assert.Empty(t, r.records)
	})
	t.Run("state unreadable", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		hubPath, hopspacePath := goneHubsFixture(t, fs)
		require.NoError(t, afero.WriteFile(fs, filepath.Join(state.GetStateHome(), "state.json"), []byte("{not json"), 0o644))
		r := doctorReport{}
		checkGoneHubRecords(fs, hubPath, hopspacePath, doctorOpts{fix: true}, &r)
		assert.Empty(t, r.records)
		assert.Contains(t, hopspaceKeys(t, fs, hopspacePath), "/hubs/gone/hops/main")
	})
}
