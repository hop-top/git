package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/services"
)

// doctor --fix renames hopspace directories. A git-hop run changing the
// hopspace's hop.json or ports.json at the same time must either finish
// before the rename, its change moving with the data, or start after it:
// never save at the old path once the data has gone, recreating files
// there (config.Writer creates the directory) and leaving the moved copy
// without its change. These run on disk: only there are the locks file
// locks, and the lock files part of the directory being moved.

// lockMoveEnv is a misplaced hopspace on disk, at alt, that belongs at
// current.
type lockMoveEnv struct {
	fs           afero.Fs
	alt, current string
}

func newLockMoveEnv(t *testing.T) lockMoveEnv {
	t.Helper()
	root := t.TempDir()
	e := lockMoveEnv{
		fs:      afero.NewOsFs(),
		alt:     filepath.Join(root, "data", "example.com", "acme", "widgets"),
		current: filepath.Join(root, "data", "acme", "widgets"),
	}
	w := config.NewWriter(e.fs)
	require.NoError(t, w.WriteHopspaceConfig(e.alt, &config.HopspaceConfig{
		Branches: map[string]config.HopspaceBranch{"main": {Exists: true, Path: "/w/main"}},
	}))
	require.NoError(t, w.WritePortsConfig(e.alt, &config.PortsConfig{
		Branches: map[string]config.BranchPorts{"main": {Ports: map[string]int{"web": 10000}}},
	}))
	writeTestFile(t, e.fs, filepath.Join(e.alt, "hooks", "post-worktree-add"), "#!/bin/sh\n")
	return e
}

// lockedWriter is a git-hop run changing one file of the hopspace at dir:
// it takes the file's lock, loads the file, and adds the "writer" branch.
type lockedWriter struct {
	file string
	lock func(fs afero.Fs, dir string, fn func() error) error
	add  func(fs afero.Fs, dir string) (save func() error, err error)
}

var hopJSONWriter = lockedWriter{
	file: "hop.json",
	lock: hop.WithHopJSONLock,
	add: func(fs afero.Fs, dir string) (func() error, error) {
		cfg, err := config.NewLoader(fs).LoadHopspaceConfig(dir)
		if err != nil {
			return nil, err
		}
		cfg.Branches["writer"] = config.HopspaceBranch{Exists: true, Path: "/w/writer"}
		return func() error { return config.NewWriter(fs).WriteHopspaceConfig(dir, cfg) }, nil
	},
}

var portsJSONWriter = lockedWriter{
	file: "ports.json",
	lock: services.WithEnvLock,
	add: func(fs afero.Fs, dir string) (func() error, error) {
		cfg, err := config.NewLoader(fs).LoadPortsConfig(dir)
		if err != nil {
			return nil, err
		}
		cfg.Branches["writer"] = config.BranchPorts{Ports: map[string]int{"web": 10010}}
		return func() error { return config.NewWriter(fs).WritePortsConfig(dir, cfg) }, nil
	},
}

// holdWhile runs w in the background, paused between its load and its
// save until the returned resume is called. It returns once w holds the
// lock; done yields w's error.
func (w lockedWriter) holdWhile(t *testing.T, fs afero.Fs, dir string) (resume func(), done <-chan error) {
	t.Helper()
	held, release, errc := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		errc <- w.lock(fs, dir, func() error {
			save, err := w.add(fs, dir)
			close(held)
			<-release
			if err != nil {
				return err
			}
			return save()
		})
	}()
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("writer never took the lock")
	}
	return func() { close(release) }, errc
}

// startMove runs move in the background and returns its result channel.
func startMove(move func() error) <-chan error {
	errc := make(chan error, 1)
	go func() { errc <- move() }()
	return errc
}

// assertWaiting fails when move finishes within a short grace period: it
// must wait for the lock the writer holds.
func assertWaiting(t *testing.T, move <-chan error) {
	t.Helper()
	select {
	case err := <-move:
		t.Fatalf("the move ran while a writer held the hopspace's lock (err=%v)", err)
	case <-time.After(300 * time.Millisecond):
	}
}

func recv(t *testing.T, c <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-c:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never finished", what)
		return nil
	}
}

func assertNoLockFiles(t *testing.T, fs afero.Fs, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		for _, name := range hopspaceLockNames {
			ok, _ := afero.Exists(fs, filepath.Join(dir, name))
			assert.False(t, ok, "lock file %s left behind", filepath.Join(dir, name))
		}
	}
}

func TestDoctorFix_HopspaceMoveWaitsForWriters(t *testing.T) {
	for _, w := range []lockedWriter{hopJSONWriter, portsJSONWriter} {
		t.Run(w.file, func(t *testing.T) {
			e := newLockMoveEnv(t)
			resume, writerDone := w.holdWhile(t, e.fs, e.alt)

			var r doctorReport
			repo := &layoutRepo{ref: hop.RepoRef{Org: "acme", Repo: "widgets"}}
			move := startMove(func() error {
				reportMisplacedHopspace(e.fs, doctorOpts{fix: true}, &r, repo, e.alt, e.current)
				return nil
			})
			assertWaiting(t, move)
			resume()
			require.NoError(t, recv(t, writerDone, "writer"))
			require.NoError(t, recv(t, move, "move"))

			assert.Empty(t, r.failedRecords(), "the move must succeed once the writer is done")
			gone, _ := afero.Exists(e.fs, e.alt)
			assert.False(t, gone, "nothing may be left, or recreated, at the old path")
			body, err := afero.ReadFile(e.fs, filepath.Join(e.current, w.file))
			require.NoError(t, err)
			assert.Contains(t, string(body), "writer", "the writer's change must move with the hopspace")
			assert.Contains(t, string(body), "main", "the data the hopspace held must move too")
			assertNoLockFiles(t, e.fs, e.alt, e.current)
		})
	}
}

func TestDoctorFix_LegacyHooksMoveWaitsForWriters(t *testing.T) {
	e := newLockMoveEnv(t)
	legacy, newDir := filepath.Join(e.alt, "hooks"), filepath.Join(e.current, "hooks")
	resume, writerDone := hopJSONWriter.holdWhile(t, e.fs, e.alt)

	var r doctorReport
	move := startMove(func() error {
		reportLegacyHooksDir(e.fs, doctorOpts{fix: true}, &r, legacy, newDir)
		return nil
	})
	assertWaiting(t, move)
	resume()
	require.NoError(t, recv(t, writerDone, "writer"))
	require.NoError(t, recv(t, move, "move"))

	assert.Empty(t, r.failedRecords())
	ok, _ := afero.Exists(e.fs, filepath.Join(newDir, "post-worktree-add"))
	assert.True(t, ok, "the hooks must be at the new location")
	assertNoLockFiles(t, e.fs, e.alt, e.current, newDir)
}

// TestDoctorFix_HopspaceMoveRechecksUnderLock: what the move depends on
// can change while it waits for the locks. Checked again once they are
// held, it refuses rather than replace a directory that appeared at the
// destination (rename(2) replaces an empty one) or move a source that is
// no longer the hopspace.
func TestDoctorFix_HopspaceMoveRechecksUnderLock(t *testing.T) {
	t.Run("destination appeared", func(t *testing.T) {
		e := newLockMoveEnv(t)
		resume, writerDone := hopJSONWriter.holdWhile(t, e.fs, e.alt)
		move := startMove(func() error {
			return moveUnderHopspaceLocks(e.fs, e.alt, e.alt, e.current, func() bool { return true })
		})
		assertWaiting(t, move)
		require.NoError(t, e.fs.MkdirAll(e.current, 0o755))
		resume()
		require.NoError(t, recv(t, writerDone, "writer"))

		err := recv(t, move, "move")
		var exists *existsError
		require.ErrorAs(t, err, &exists)
		ok, _ := afero.Exists(e.fs, filepath.Join(e.alt, "ports.json"))
		assert.True(t, ok, "the hopspace must stay where it was")
		assertNoLockFiles(t, e.fs, e.alt, e.current)
	})

	t.Run("source moved meanwhile", func(t *testing.T) {
		e := newLockMoveEnv(t)
		elsewhere := filepath.Join(filepath.Dir(e.alt), "elsewhere")
		held, release := make(chan struct{}), make(chan struct{})
		holderDone := make(chan error, 1)
		go func() {
			// Another doctor --fix moving the hopspace first.
			holderDone <- withHopspaceLocks(e.fs, e.alt, func() error {
				close(held)
				<-release
				return e.fs.Rename(e.alt, elsewhere)
			})
		}()
		<-held
		present := func() bool { return len(presentMarkers(e.fs, e.alt)) > 0 }
		move := startMove(func() error {
			return moveUnderHopspaceLocks(e.fs, e.alt, e.alt, e.current, present)
		})
		assertWaiting(t, move)
		close(release)
		require.NoError(t, recv(t, holderDone, "other move"))

		require.ErrorIs(t, recv(t, move, "move"), errMovedMeanwhile)
		ok, _ := afero.Exists(e.fs, e.current)
		assert.False(t, ok, "an emptied source must not be moved into place")
	})
}

func (r doctorReport) failedRecords() []doctorRecord {
	var out []doctorRecord
	for _, rec := range r.records {
		if rec.Kind == doctorKindFailed {
			out = append(out, rec)
		}
	}
	return out
}
