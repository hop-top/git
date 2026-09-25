// Package filelock provides an OS-level advisory lock on a file, and a
// helper that serialises a read-modify-write of a file shared by several
// git-hop processes.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Lock is an OS-level advisory lock on a file. TryAcquire fails
// immediately when another process holds the lock.
//
// The lock file exists only while the lock is held: TryAcquire creates
// it, Release unlinks it. A lock file that lingers after a run is
// indistinguishable from state to anything that scans the directory, so
// "released" means "gone". An abandoned process loses the lock when its
// handle closes; the stale file it leaves is harmless and is reclaimed
// by the next TryAcquire.
type Lock struct {
	path string
	file *os.File
}

// New creates a lock object for path. The file is not opened
// until TryAcquire is called.
func New(path string) *Lock {
	return &Lock{path: path}
}

// acquireAttempts bounds the open/lock/verify loop in TryAcquire. Each
// retry only happens when a concurrent Release unlinked the file between
// our open and our lock, which is rare and self-limiting.
const acquireAttempts = 5

// TryAcquire attempts to acquire the lock without blocking. Returns
// (true, nil) on success, (false, nil) if another process holds it,
// or (false, err) on a real error (e.g. permission denied, mkdir failure).
func (l *Lock) TryAcquire() (bool, error) {
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return false, fmt.Errorf("create lock dir: %w", err)
	}
	for i := 0; i < acquireAttempts; i++ {
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return false, fmt.Errorf("open lock file: %w", err)
		}
		ok, err := tryFlock(f)
		if err != nil {
			_ = f.Close()
			return false, err
		}
		if !ok {
			_ = f.Close()
			return false, nil
		}
		// Unlink-on-release opens a window: the file we locked may have
		// been removed by the previous holder between our open and our
		// lock, in which case we hold a lock on an orphaned inode that
		// nobody else can see. Verify the path still names our inode.
		if sameInode(f, l.path) {
			l.file = f
			return true, nil
		}
		_ = unflock(f)
		_ = f.Close()
	}
	return false, errors.New("lock file kept changing underneath us")
}

// Release drops the lock and removes the lock file. Safe to call on a
// never-acquired lock.
//
// The unlink happens while the lock is still held so no other process
// can acquire the old inode after we let go; a waiter that already
// opened it detects the swap in TryAcquire. Where the platform refuses
// to unlink an open file (Windows), the removal is retried after close.
//
// A lock file the holder moved (a rename of its directory) is no longer
// at the path: the file there, if any, is another process's lock, taken
// since, and unlinking it would let a third process take the lock while
// that one holds it. Release then only drops its own lock; the file it
// holds is the mover's to remove, at its new path, before releasing.
func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	removed := !sameInode(l.file, l.path) || removeIgnoringMissing(l.path) == nil
	unErr := unflock(l.file)
	clErr := l.file.Close()
	l.file = nil
	var rmErr error
	if !removed {
		rmErr = removeIgnoringMissing(l.path)
	}
	return errors.Join(unErr, clErr, rmErr)
}

// Held reports whether some process currently holds the lock at path.
// It never creates the file or its parent directory: a missing file is
// simply not held. Used by cleanup passes to tell a stale lock file from
// a live one before removing it.
func Held(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	ok, err := tryFlock(f)
	if err != nil {
		// Cannot tell; err on the side of "held" so nothing is removed.
		return true
	}
	if ok {
		_ = unflock(f)
		return false
	}
	return true
}

func sameInode(f *os.File, path string) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	pi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(fi, pi)
}

func removeIgnoringMissing(path string) error {
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
