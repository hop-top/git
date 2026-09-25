package state

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
)

// state.json is rewritten by every git-hop run that records or drops a
// repository, hub or worktree, and several runs can do so at once. A
// run that loads the file, works (runs git, hooks, prompts), then saves
// what it loaded writes back a stale copy, undoing whatever another run
// saved in between. Update serialises the rewrites: it takes the state
// lock, loads state.json afresh, applies the caller's change and saves,
// all under the lock. Readers need no lock: a save replaces the file by
// renaming a complete copy over it.

// LockName is the lock file Update and SaveState take next to
// state.json.
const LockName = "state.json.lock"

// lockTimeout bounds the wait for a lock another live process holds.
// The hold is one load-modify-save, so reaching it means that process
// is stuck.
var lockTimeout = 30 * time.Second

// ErrLocked is returned when the state lock stays held past the timeout.
var ErrLocked = errors.New("state.json is locked by another git-hop process")

// ErrSkipSave, returned by an Update callback, leaves state.json as it
// is: Update then returns nil.
var ErrSkipSave = errors.New("skip saving state")

// Update loads state.json afresh under the state lock, applies fn to it
// and saves the result, so a change never undoes one another run saved
// since this run last read the file. When fn returns an error nothing is
// saved and the error is returned, except ErrSkipSave, which only skips
// the save.
//
// fn runs with the lock held: it must only change st, never run git,
// hooks or anything else that may run git-hop, which would wait on the
// lock this run holds, nor take the lock again (Update, SaveState). A
// run that decides what to change from a slow scan records its decisions
// and replays them in fn, against the state as it is by then.
//
// Loading migrates the file in memory as LoadState does, and saving
// backs it up first as SaveState does; since both happen under the lock,
// runs that load an unmigrated file at once back it up once.
func Update(fs afero.Fs, fn func(st *State) error) error {
	return withStateLock(fs, func() error {
		st, err := LoadState(fs)
		if err != nil {
			return err
		}
		if err := fn(st); err != nil {
			if errors.Is(err, ErrSkipSave) {
				return nil
			}
			return err
		}
		return saveLocked(fs, st)
	})
}

// withStateLock runs fn holding the lock on state.json.
func withStateLock(fs afero.Fs, fn func() error) error {
	return stateGuard().Do(fs, fn)
}

// replaceFile atomically replaces path with data: it writes a temp file
// of its own in the same directory and renames it over path. The temp
// file's name is unique, so saves that overlap (another process, or a
// writer that bypassed the lock) never write into each other's copy.
func replaceFile(fs afero.Fs, path string, data []byte) (err error) {
	f, err := afero.TempFile(fs, filepath.Dir(path), filepath.Base(path)+tempSuffixPattern)
	if err != nil {
		return fmt.Errorf("failed to create temp state file: %w", err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = fs.Remove(tmp)
		}
	}()

	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("failed to write temp state file: %w", werr)
	}
	// The temp file is created private (0600); state.json never was.
	if err := fs.Chmod(tmp, 0o644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}
	if err := fs.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to save state file: %w", err)
	}
	return nil
}
