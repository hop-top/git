package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// retentionFixture lays out three repair snapshots in the state-dir root
// of one hub, aged 100 days, 10 days, and just now, with
// hop.repair.backupRetention answering value ("" leaves it unset).
type retentionFixture struct {
	fs                afero.Fs
	g                 *mocks.MockGit
	hub               string
	old, mid, current string
}

func newRetentionFixture(t *testing.T, value string) retentionFixture {
	t.Helper()
	withStateHome(t, "/xdg-state")
	f := retentionFixture{fs: afero.NewMemMapFs(), g: mocks.NewMockGit(), hub: "/hubs/repo"}
	root := hop.RepairBackupRoot(f.hub)
	f.old = filepath.Join(root, "repair-20200101T000000Z")
	f.mid = filepath.Join(root, "repair-20260101T000000Z")
	f.current = filepath.Join(root, "repair-20990101T000000Z")
	for _, dir := range []string{f.old, f.mid, f.current} {
		require.NoError(t, f.fs.MkdirAll(dir, 0o755))
	}
	ageDir(t, f.fs, f.old)
	tenDays := time.Now().Add(-10 * 24 * time.Hour)
	require.NoError(t, f.fs.Chtimes(f.mid, tenDays, tenDays))
	if value != "" {
		f.g.Runner.Responses[f.hub+":git config --get hop.repair.backupRetention"] = value
	}
	return f
}

func (f retentionFixture) prune(dryRun bool) []pruneRecord {
	return pruneRepairBackups(f.fs, f.g, singleHubState(f.hub), dryRun)
}

func (f retentionFixture) assertPresent(t *testing.T, want map[string]bool) {
	t.Helper()
	for dir, present := range want {
		exists, _ := afero.DirExists(f.fs, dir)
		assert.Equal(t, present, exists, "%s present", filepath.Base(dir))
	}
}

// TestPruneRepairBackups_NonPositiveRetentionDisablesPruning: the docs
// promise that 0 disables pruning of repair backups, the same as 0 on
// hop.backup.maxBackups / hop.backup.cleanupAgeDays. A cutoff of
// now-minus-zero deleted every snapshot, including the one just taken;
// a negative duration pushed the cutoff into the future and did the same.
func TestPruneRepairBackups_NonPositiveRetentionDisablesPruning(t *testing.T) {
	for _, value := range []string{"0", "0s", "-1h"} {
		t.Run(value, func(t *testing.T) {
			f := newRetentionFixture(t, value)

			assert.Empty(t, f.prune(true), "dry-run must list no repair backup")
			assert.Empty(t, f.prune(false), "no repair backup may be pruned")
			f.assertPresent(t, map[string]bool{f.old: true, f.mid: true, f.current: true})
		})
	}
}

// TestPruneRepairBackups_NonPositiveRetentionStillRetiresLegacyDir: with
// pruning off, a legacy snapshot is kept, and moved out of the hub the
// same as any snapshot retention left in place.
func TestPruneRepairBackups_NonPositiveRetentionStillRetiresLegacyDir(t *testing.T) {
	f := newRetentionFixture(t, "0")
	id := "repair-20190101T000000Z"
	legacy := filepath.Join(f.hub, ".hop", "backups", id)
	require.NoError(t, f.fs.MkdirAll(legacy, 0o755))
	manifest := `{"version":1,"id":"` + id + `","hubPath":"` + f.hub + `","files":{}}`
	require.NoError(t, afero.WriteFile(f.fs, filepath.Join(legacy, "manifest.json"), []byte(manifest), 0o644))
	ageDir(t, f.fs, legacy)

	assert.Empty(t, f.prune(false))

	exists, _ := afero.DirExists(f.fs, filepath.Join(hop.RepairBackupRoot(f.hub), id))
	assert.True(t, exists, "legacy snapshot must be moved, not deleted")
	exists, _ = afero.Exists(f.fs, filepath.Join(f.hub, ".hop"))
	assert.False(t, exists, ".hop/ must be retired once empty")
}

// TestPruneRepairBackups_RetentionIsMaxAge pins the positive case: the
// setting is a max age, not a count. Snapshots older than it go, newer
// ones stay, however many there are.
func TestPruneRepairBackups_RetentionIsMaxAge(t *testing.T) {
	f := newRetentionFixture(t, "168h")

	dry := f.prune(true)
	require.Len(t, dry, 2, "dry-run lists both snapshots older than 7 days")
	f.assertPresent(t, map[string]bool{f.old: true, f.mid: true, f.current: true})

	pruned := f.prune(false)
	require.Len(t, pruned, 2)
	assert.ElementsMatch(t, []string{f.old, f.mid}, []string{pruned[0].Path, pruned[1].Path})
	f.assertPresent(t, map[string]bool{f.old: false, f.mid: false, f.current: true})
}

// TestPruneRepairBackups_InvalidRetentionUsesDefault: an unparseable value
// takes the 30-day default, as an unparseable hop.backup.maxBackups or
// hop.backup.cleanupAgeDays takes its default. Unset behaves the same.
func TestPruneRepairBackups_InvalidRetentionUsesDefault(t *testing.T) {
	for name, value := range map[string]string{"unset": "", "bogus": "bogus", "bare number": "30"} {
		t.Run(name, func(t *testing.T) {
			f := newRetentionFixture(t, value)

			pruned := f.prune(false)
			require.Len(t, pruned, 1)
			assert.Equal(t, f.old, pruned[0].Path)
			f.assertPresent(t, map[string]bool{f.old: false, f.mid: true, f.current: true})
		})
	}
}
