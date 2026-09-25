package cmd

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// doctor --fix moves hopspace data by renaming a directory: a whole
// hopspace left at another hop.dataLayout path, or a hooks dir left where
// releases before hop.dataLayout mirrored it. Other git-hop runs may be
// rewriting that hopspace's hop.json, ports.json or volumes.json at the
// same time; one that loaded a file before the rename and saves it after
// would write it back at the old path, or lose its change.
//
// So the rename runs under the source hopspace's locks, the ones every
// writer of those files takes: hop.json.lock, then ports.json.lock
// (hop.WithHopJSONLock, services.WithEnvLock). No other code holds both,
// so this order cannot deadlock (see internal/services/env_lock.go). A
// writer that was mid-change finishes first and the move carries its
// change; one that starts later waits for the move. Everything the move
// depends on is checked again under the locks: the source still holds
// the data, and nothing is at the destination.
//
// The locks are taken at the source only. Both lock files would have to
// exist at the destination to lock it there, which is the one thing the
// move requires to be absent. Writers at the destination need no lock:
// rename(2) refuses a non-empty destination, and a writer that has only
// created the empty directory then writes into the moved one, which is
// where its data belongs.
//
// Moving a whole hopspace moves the lock files, held, with it. They are
// unlinked at the destination before the locks go, the way a release
// unlinks them: a writer at the destination that opened one waits until
// then, and none is left behind. The release itself leaves the old path
// alone (filelock.Lock.Release unlinks only its own file), so a lock a
// waiter at the old path took since is not broken.

// hopspaceLockNames are the lock files of a hopspace, in the order
// withHopspaceLocks takes them.
var hopspaceLockNames = []string{hop.HopJSONLockName, services.PortsLockName}

// withHopspaceLocks runs fn holding the locks of the hopspace at dir, in
// the documented order: hop.json.lock, then ports.json.lock.
func withHopspaceLocks(fs afero.Fs, dir string, fn func() error) error {
	return hop.WithHopJSONLock(fs, dir, func() error {
		return services.WithEnvLock(fs, dir, fn)
	})
}

// errMovedMeanwhile is moveUnderHopspaceLocks's error when the data to
// move is no longer at the source once the locks are held.
var errMovedMeanwhile = errors.New("no longer there (moved or removed meanwhile)")

// moveUnderHopspaceLocks renames from to to while holding the locks of
// the hopspace at lockDir, which is from or holds it. present reports,
// under the locks, whether from still holds the data to move.
func moveUnderHopspaceLocks(fs afero.Fs, lockDir, from, to string, present func() bool) error {
	movesLocks := filepath.Clean(lockDir) == filepath.Clean(from)
	return withHopspaceLocks(fs, lockDir, func() error {
		if !present() {
			return errMovedMeanwhile
		}
		if err := moveDirNoClobber(fs, from, to); err != nil {
			return err
		}
		if movesLocks {
			dropMovedLockFiles(fs, to)
		}
		return nil
	})
}

// dropMovedLockFiles unlinks the lock files a hopspace move carried to
// dir, still held by this run: nobody else unlinks a held lock file, so
// the ones there are these. A failure leaves a stale, unheld lock file,
// which the next writer reuses; it is only warned about.
func dropMovedLockFiles(fs afero.Fs, dir string) {
	for _, name := range hopspaceLockNames {
		path := filepath.Join(dir, name)
		if err := fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			output.Warn("could not remove %s: %v", path, err)
		}
	}
}
