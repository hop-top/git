package hop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/filelock"
)

// hubTempFixture lays out, next to a hub's hop.json on disk, a stale and
// a fresh temp file and stale files of other names.
type hubTempFixture struct {
	hub, stale, fresh string
	others            []string
}

func newHubTempFixture(t *testing.T) hubTempFixture {
	t.Helper()
	hub := t.TempDir()
	f := hubTempFixture{
		hub:   hub,
		stale: filepath.Join(hub, "hop-config-111111111.tmp"),
		fresh: filepath.Join(hub, "hop-config-222222222.tmp"),
		others: []string{
			filepath.Join(hub, "hop.json"),
			filepath.Join(hub, "hop.json.123.tmp"),
			filepath.Join(hub, "notes.tmp"),
		},
	}
	old := time.Now().Add(-2 * filelock.StaleTempAge)
	for _, p := range append([]string{f.stale, f.fresh}, f.others...) {
		require.NoError(t, os.WriteFile(p, []byte("{}"), 0o600))
		if p != f.fresh {
			require.NoError(t, os.Chtimes(p, old, old))
		}
	}
	return f
}

func present(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestSweepHopJSONTemps(t *testing.T) {
	f := newHubTempFixture(t)
	fs := afero.NewOsFs()

	listed, err := StaleHopJSONTemps(fs, f.hub)
	require.NoError(t, err)
	assert.Equal(t, []string{f.stale}, listed)

	would, err := SweepHopJSONTemps(fs, f.hub, true)
	require.NoError(t, err)
	assert.Equal(t, []string{f.stale}, would)
	assert.True(t, present(f.stale), "dry run removed the stale temp file")

	removed, err := SweepHopJSONTemps(fs, f.hub, false)
	require.NoError(t, err)
	assert.Equal(t, []string{f.stale}, removed)
	assert.False(t, present(f.stale), "stale temp file kept")
	assert.True(t, present(f.fresh), "fresh temp file removed")
	for _, p := range f.others {
		assert.True(t, present(p), "%s removed", filepath.Base(p))
	}
}

// While a save holds the hop.json lock, the sweep removes nothing.
func TestSweepHopJSONTemps_LockHeldSkips(t *testing.T) {
	f := newHubTempFixture(t)
	writer := filelock.New(filepath.Join(f.hub, HopJSONLockName))
	ok, err := writer.TryAcquire()
	require.NoError(t, err)
	require.True(t, ok)
	defer writer.Release()

	removed, err := SweepHopJSONTemps(afero.NewOsFs(), f.hub, false)
	assert.ErrorIs(t, err, ErrHopJSONLocked)
	assert.Empty(t, removed)
	assert.True(t, present(f.stale), "stale temp file removed under a held lock")
}
