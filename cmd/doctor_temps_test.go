package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/filelock"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// doctor reports the temp files crashed saves left as warnings (exit
// 0), and --fix removes them with prune's sweep.

// tempRecordsOf returns the records doctor made about path.
func tempRecordsOf(r doctorReport, path string) []doctorRecord {
	var out []doctorRecord
	for _, rec := range r.records {
		if rec.Subject == path {
			out = append(out, rec)
		}
	}
	return out
}

// healthyDoctorFs is an installation doctor finds nothing wrong with,
// but for the temp files the test adds.
func healthyDoctorFs(t *testing.T) afero.Fs {
	t.Helper()
	p := isolateDoctorPaths(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(p.dataHome, 0o755))
	return fs
}

func TestDoctor_StaleStateTempsAreWarnings(t *testing.T) {
	fs := healthyDoctorFs(t)
	f := writeTempFiles(t, fs, state.GetStateHome(), "state.json.")

	r := runDoctor(fs, mocks.NewMockGit(), "/nowhere", doctorOpts{})

	recs := tempRecordsOf(r, f.stale)
	require.Len(t, recs, 1)
	assert.Equal(t, doctorKindWarning, recs[0].Kind)
	assert.Equal(t, doctorCheckState, recs[0].Check)
	assert.Contains(t, recs[0].Message, "git hop doctor --fix")
	for _, p := range append([]string{f.fresh}, f.others...) {
		assert.Empty(t, tempRecordsOf(r, p), "%s reported", filepath.Base(p))
	}
	assert.False(t, r.issuesFound)
	assert.NoError(t, doctorResult(r), "a warning leaves the exit status 0")
	assert.True(t, f.assertKept(t, fs), "doctor without --fix removed a file")
}

func TestDoctorFix_RemovesStaleStateTemps(t *testing.T) {
	fs := healthyDoctorFs(t)
	f := writeTempFiles(t, fs, state.GetStateHome(), "state.json.")

	r := runDoctor(fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true, dryRun: true})
	assert.Equal(t, []string{doctorKindWarning, doctorKindWouldFix}, recordKinds(tempRecordsOf(r, f.stale)))
	assert.True(t, f.assertKept(t, fs), "--fix --dry-run removed the stale temp file")
	assert.NoError(t, doctorResult(r))

	r = runDoctor(fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true})
	assert.Equal(t, []string{doctorKindWarning, doctorKindFixed}, recordKinds(tempRecordsOf(r, f.stale)))
	assert.False(t, f.assertKept(t, fs), "--fix kept the stale temp file")
	assert.NoError(t, doctorResult(r))

	r = runDoctor(fs, mocks.NewMockGit(), "/nowhere", doctorOpts{})
	assert.Empty(t, tempRecordsOf(r, f.stale), "reported after --fix removed it")
}

func TestDoctorFix_RemovesStaleHubTemps(t *testing.T) {
	fs := healthyDoctorFs(t)
	hub := "/hubs/widgets"
	writeOriginHub(t, fs, hub, "git@github.com:acme/widgets.git")
	f := writeTempFiles(t, fs, hub, "hop-config-")

	r := runDoctor(fs, mocks.NewMockGit(), hub, doctorOpts{})
	recs := tempRecordsOf(r, f.stale)
	require.Len(t, recs, 1)
	assert.Equal(t, doctorKindWarning, recs[0].Kind)
	assert.Equal(t, doctorCheckHub, recs[0].Check)
	for _, p := range append([]string{f.fresh}, f.others...) {
		assert.Empty(t, tempRecordsOf(r, p), "%s reported", filepath.Base(p))
	}
	assert.True(t, f.assertKept(t, fs), "doctor without --fix removed a file")

	r = runDoctor(fs, mocks.NewMockGit(), hub, doctorOpts{fix: true})
	assert.Equal(t, []string{doctorKindWarning, doctorKindFixed}, recordKinds(tempRecordsOf(r, f.stale)))
	assert.False(t, f.assertKept(t, fs), "--fix kept the stale temp file")
}

// While a save holds the lock, --fix leaves the temp files, and the run
// still exits 0: they are warnings.
func TestDoctorFix_TempsLeftWhileLockHeld(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fs := afero.NewOsFs()
	hub := filepath.Join(t.TempDir(), "hub")
	writeOriginHub(t, fs, hub, "git@github.com:acme/widgets.git")
	hubTemps := writeTempFiles(t, fs, hub, "hop-config-")
	stateTemps := writeTempFiles(t, fs, state.GetStateHome(), "state.json.")
	for _, lock := range []string{filepath.Join(hub, hop.HopJSONLockName), filepath.Join(state.GetStateHome(), state.LockName)} {
		l := filelock.New(lock)
		ok, err := l.TryAcquire()
		require.NoError(t, err)
		require.True(t, ok)
		t.Cleanup(func() { _ = l.Release() })
	}

	r := doctorReport{}
	checkStaleTemps(fs, hub, doctorOpts{fix: true}, &r)

	assert.True(t, hubTemps.assertKept(t, fs), "hub temp file removed under a held lock")
	assert.True(t, stateTemps.assertKept(t, fs), "state temp file removed under a held lock")
	assert.Equal(t, []string{doctorKindWarning, doctorKindWarning}, recordKinds(r.records))
	assert.NoError(t, doctorResult(r))
}

// A preview says what the real run would do: with the lock held, that
// is nothing.
func TestDoctorFixDryRun_TempsLeftWhileLockHeld(t *testing.T) {
	root := t.TempDir()
	for env, dir := range map[string]string{
		"XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_CACHE_HOME": "cache",
		"XDG_STATE_HOME": "state", "GIT_HOP_DATA_HOME": "githop-data",
	} {
		t.Setenv(env, filepath.Join(root, dir))
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	fs := afero.NewOsFs()
	require.NoError(t, fs.MkdirAll(hop.GetGitHopDataHome(), 0o755))
	stateTemps := writeTempFiles(t, fs, state.GetStateHome(), "state.json.")
	l := filelock.New(filepath.Join(state.GetStateHome(), state.LockName))
	ok, err := l.TryAcquire()
	require.NoError(t, err)
	require.True(t, ok)
	defer l.Release()

	r := runDoctor(fs, mocks.NewMockGit(), root, doctorOpts{fix: true, dryRun: true})

	assert.Equal(t, []string{doctorKindWarning}, recordKinds(tempRecordsOf(r, stateTemps.stale)))
	assert.True(t, stateTemps.assertKept(t, fs))
}

func recordKinds(recs []doctorRecord) []string {
	kinds := make([]string, len(recs))
	for i, rec := range recs {
		kinds[i] = rec.Kind
	}
	return kinds
}
