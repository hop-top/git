package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/filelock"
)

// crashFs is a filesystem on which a save dies between writing its temp
// file and renaming it: the rename fails and the cleanup never runs.
type crashFs struct{ afero.Fs }

func (crashFs) Rename(string, string) error { return errors.New("crashed") }
func (crashFs) Remove(string) error         { return nil }

// A save that dies before its rename leaves a temp file, named as
// IsTempName expects, next to state.json.
func TestSaveState_CrashLeavesTempNamedAsIsTempName(t *testing.T) {
	isolateStateHome(t)
	mem := afero.NewMemMapFs()
	require.Error(t, SaveState(crashFs{mem}, NewState()))

	entries, err := afero.ReadDir(mem, GetStateHome())
	require.NoError(t, err)
	require.Len(t, entries, 1, "the crashed save's temp file")
	assert.True(t, IsTempName(entries[0].Name()), "%s", entries[0].Name())
}

func TestIsTempName(t *testing.T) {
	for name, want := range map[string]bool{
		"state.json.123456789.tmp":    true,
		"state.json.1.tmp":            true,
		"state.json":                  false,
		"state.json.lock":             false,
		"state.json.tmp":              false,
		"state.json..tmp":             false,
		"state.json.12a.tmp":          false,
		"state.json.123.tmp.x":        false,
		"xstate.json.123.tmp":         false,
		"hop.json.123.tmp":            false,
		"state-20200101T000000Z.json": false,
	} {
		assert.Equal(t, want, IsTempName(name), name)
	}
}

// stateTempFixture lays out, next to state.json on disk, a stale and a
// fresh temp file and a stale file of another name.
type stateTempFixture struct{ stale, fresh, other string }

func newStateTempFixture(t *testing.T) stateTempFixture {
	t.Helper()
	home := isolateStateHome(t)
	require.NoError(t, os.MkdirAll(home, 0o755))
	f := stateTempFixture{
		stale: filepath.Join(home, "state.json.111111111.tmp"),
		fresh: filepath.Join(home, "state.json.222222222.tmp"),
		other: filepath.Join(home, "state.json.bak"),
	}
	old := time.Now().Add(-2 * filelock.StaleTempAge)
	for _, p := range []string{f.stale, f.fresh, f.other} {
		require.NoError(t, os.WriteFile(p, []byte("{}"), 0o600))
		if p != f.fresh {
			require.NoError(t, os.Chtimes(p, old, old))
		}
	}
	return f
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestSweepTemps(t *testing.T) {
	f := newStateTempFixture(t)
	fs := afero.NewOsFs()

	listed, err := StaleTemps(fs)
	require.NoError(t, err)
	assert.Equal(t, []string{f.stale}, listed)

	would, err := SweepTemps(fs, true)
	require.NoError(t, err)
	assert.Equal(t, []string{f.stale}, would)
	assert.True(t, fileExists(f.stale), "dry run removed the stale temp file")

	removed, err := SweepTemps(fs, false)
	require.NoError(t, err)
	assert.Equal(t, []string{f.stale}, removed)
	assert.False(t, fileExists(f.stale), "stale temp file kept")
	assert.True(t, fileExists(f.fresh), "fresh temp file removed")
	assert.True(t, fileExists(f.other), "file of another name removed")
}

// While a save holds the state lock, the sweep removes nothing.
func TestSweepTemps_StateLockHeldSkips(t *testing.T) {
	f := newStateTempFixture(t)
	writer := filelock.New(filepath.Join(GetStateHome(), LockName))
	ok, err := writer.TryAcquire()
	require.NoError(t, err)
	require.True(t, ok)
	defer writer.Release()

	removed, err := SweepTemps(afero.NewOsFs(), false)
	assert.ErrorIs(t, err, ErrLocked)
	assert.Empty(t, removed)
	assert.True(t, fileExists(f.stale), "stale temp file removed under a held lock")
}
