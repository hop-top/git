package hop

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/afero"
)

// hop.json is rewritten by every git-hop run that changes a hub, and
// several runs can work in one hub at once. Each rewrite is a
// load-modify-save; without serialising them, a run that loaded the file
// before another run saved it writes its stale copy back, undoing the
// other run (a removed branch reappears, an added one disappears).
//
// WithHopJSONLock serialises them: on disk with an OS advisory lock on
// hop.json.lock next to hop.json (FileLock), which the OS drops when its
// holder exits, so a crashed run never leaves the hub locked. Any other
// filesystem (in-memory, a dry run's copy-on-write view) is private to
// this process and gets an in-process mutex instead, keeping the lock
// file off the real disk.
//
// Callers load hop.json afresh inside fn and hold the lock only around
// that load-modify-save: never across git commands, hooks or anything
// else that may run git-hop, which would wait on the lock this run holds.

// HopJSONLockName is the lock file WithHopJSONLock takes next to hop.json.
const HopJSONLockName = "hop.json.lock"

// hopJSONLockTimeout bounds the wait for a lock another live process
// holds. The hold is one load-modify-save, so reaching it means that
// process is stuck.
var hopJSONLockTimeout = 30 * time.Second

// hopJSONLockPoll is the first wait between attempts; it doubles up to
// hopJSONLockMaxPoll.
const (
	hopJSONLockPoll    = time.Millisecond
	hopJSONLockMaxPoll = 50 * time.Millisecond
)

// ErrHopJSONLocked is returned when the lock stays held past the timeout.
var ErrHopJSONLocked = errors.New("hop.json is locked by another git-hop process")

var memLocks sync.Map // cleaned dir -> *sync.Mutex

// WithHopJSONLock runs fn while holding the exclusive lock on the
// hop.json in dir. fn must not take the same lock again.
func WithHopJSONLock(fs afero.Fs, dir string, fn func() error) error {
	if _, onDisk := fs.(*afero.OsFs); !onDisk {
		mu, _ := memLocks.LoadOrStore(filepath.Clean(dir), &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()
		return fn()
	}

	path := filepath.Join(dir, HopJSONLockName)
	lock := NewFileLock(path)
	deadline := time.Now().Add(hopJSONLockTimeout)
	for wait := hopJSONLockPoll; ; wait = min(wait*2, hopJSONLockMaxPoll) {
		ok, err := lock.TryAcquire()
		if err != nil {
			return fmt.Errorf("lock %s: %w", path, err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s still held after %s", ErrHopJSONLocked, path, hopJSONLockTimeout)
		}
		time.Sleep(wait)
	}
	fnErr := fn()
	if err := lock.Release(); err != nil && fnErr == nil {
		return fmt.Errorf("unlock %s: %w", path, err)
	}
	return fnErr
}

// writeHopJSONLocked writes data as the hop.json in dir under its lock.
// For writers that build the whole file themselves (clone, init, repair
// undo) rather than modifying the one on disk.
func writeHopJSONLocked(fs afero.Fs, dir string, data []byte) error {
	return WithHopJSONLock(fs, dir, func() error {
		return afero.WriteFile(fs, filepath.Join(dir, "hop.json"), data, 0644)
	})
}
