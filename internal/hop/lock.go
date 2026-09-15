package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileLock is an OS-level advisory lock on a file. TryAcquire fails
// immediately when another process holds the lock.
//
// The lock file exists only while the lock is held: TryAcquire creates
// it, Release unlinks it. A lock file that lingers after a run is
// indistinguishable from state to anything that scans the directory, so
// "released" means "gone". An abandoned process loses the lock when its
// handle closes; the stale file it leaves is harmless and is reclaimed
// by the next TryAcquire.
type FileLock struct {
	path string
	file *os.File
}

// NewFileLock creates a lock object for path. The file is not opened
// until TryAcquire is called.
func NewFileLock(path string) *FileLock {
	return &FileLock{path: path}
}

// acquireAttempts bounds the open/lock/verify loop in TryAcquire. Each
// retry only happens when a concurrent Release unlinked the file between
// our open and our lock, which is rare and self-limiting.
const acquireAttempts = 5

// TryAcquire attempts to acquire the lock without blocking. Returns
// (true, nil) on success, (false, nil) if another process holds it,
// or (false, err) on a real error (e.g. permission denied, mkdir failure).
func (l *FileLock) TryAcquire() (bool, error) {
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
func (l *FileLock) Release() error {
	if l.file == nil {
		return nil
	}
	removed := removeIgnoringMissing(l.path) == nil
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
