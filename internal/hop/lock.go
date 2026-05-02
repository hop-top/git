package hop

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileLock is an OS-level advisory lock on a file. Acquire blocks until
// the lock is held or fails immediately when TryAcquire is used.
//
// The lock file itself is created on first acquire and is left in place
// on Release — subsequent runs reuse it. Callers Release via defer; an
// abandoned process loses the lock when its file handle closes.
type FileLock struct {
	path string
	file *os.File
}

// NewFileLock creates a lock object for path. The file is not opened
// until Acquire/TryAcquire is called.
func NewFileLock(path string) *FileLock {
	return &FileLock{path: path}
}

// TryAcquire attempts to acquire the lock without blocking. Returns
// (true, nil) on success, (false, nil) if another process holds it,
// or (false, err) on a real error (e.g. permission denied, mkdir failure).
func (l *FileLock) TryAcquire() (bool, error) {
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return false, fmt.Errorf("create lock dir: %w", err)
	}
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
	l.file = f
	return true, nil
}

// Release drops the lock. Safe to call on a never-acquired lock.
func (l *FileLock) Release() error {
	if l.file == nil {
		return nil
	}
	if err := unflock(l.file); err != nil {
		_ = l.file.Close()
		l.file = nil
		return err
	}
	err := l.file.Close()
	l.file = nil
	return err
}
