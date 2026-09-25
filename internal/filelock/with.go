package filelock

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/afero"
)

// A file several git-hop runs rewrite (hop.json, state.json) is
// rewritten as a load-modify-save. Without serialising them, a run that
// loaded the file before another run saved it writes its stale copy
// back, undoing the other run.
//
// Guard serialises them: on disk with an OS advisory lock (Lock) on a
// lock file next to the guarded file, which the OS drops when its holder
// exits, so a crashed run never leaves the file locked. Any other
// filesystem (in-memory, a dry run's copy-on-write view) is private to
// this process and gets an in-process mutex instead, keeping the lock
// file off the real disk.
//
// Callers load the guarded file afresh inside fn and hold the lock only
// around that load-modify-save: never across git commands, hooks or
// anything else that may run git-hop, which would wait on the lock this
// run holds.

// Guard describes one lock file and how long to wait for it.
type Guard struct {
	// Path is the lock file.
	Path string
	// Timeout bounds the wait for a lock another live process holds.
	// The hold is one load-modify-save, so reaching it means that
	// process is stuck.
	Timeout time.Duration
	// Busy is the error, wrapped, that Do returns when the lock is still
	// held after Timeout; ErrBusy when nil.
	Busy error
}

// ErrBusy is Guard.Do's error for a lock still held after the timeout,
// when the Guard names no other.
var ErrBusy = errors.New("lock is held by another process")

// The first wait between attempts; it doubles up to maxPoll.
const (
	firstPoll = time.Millisecond
	maxPoll   = 50 * time.Millisecond
)

var memLocks sync.Map // cleaned lock path -> *sync.Mutex

// Do runs fn while holding the exclusive lock g describes. fn's error is
// returned as is. fn must not take the same lock again.
func (g Guard) Do(fs afero.Fs, fn func() error) error {
	if _, onDisk := fs.(*afero.OsFs); !onDisk {
		mu, _ := memLocks.LoadOrStore(filepath.Clean(g.Path), &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()
		return fn()
	}

	lock := New(g.Path)
	deadline := time.Now().Add(g.Timeout)
	for wait := firstPoll; ; wait = min(wait*2, maxPoll) {
		ok, err := lock.TryAcquire()
		if err != nil {
			return fmt.Errorf("lock %s: %w", g.Path, err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			busy := g.Busy
			if busy == nil {
				busy = ErrBusy
			}
			return fmt.Errorf("%w: %s still held after %s", busy, g.Path, g.Timeout)
		}
		time.Sleep(wait)
	}
	fnErr := fn()
	if err := lock.Release(); err != nil && fnErr == nil {
		return fmt.Errorf("unlock %s: %w", g.Path, err)
	}
	return fnErr
}
