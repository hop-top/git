package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/afero"
)

// A guarded file is saved by writing a temp file next to it and renaming
// that over it. A writer that dies between the two leaves its temp file
// behind, and nothing else ever removes it. SweepStale removes such
// leftovers.

// StaleTempAge is how long a temp file must have gone unmodified before
// it counts as left behind: a save writes and renames its temp file
// within moments, so one this old belongs to no save still running.
const StaleTempAge = time.Hour

// StaleFiles lists, sorted, the files in dir whose name match accepts
// and that were last modified before cutoff. A missing dir lists none.
// It takes no lock: use it to report, and SweepStale to remove.
func StaleFiles(fs afero.Fs, dir string, match func(name string) bool, cutoff time.Time) ([]string, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var stale []string
	for _, e := range entries {
		if !e.Mode().IsRegular() || !match(e.Name()) || !e.ModTime().Before(cutoff) {
			continue
		}
		stale = append(stale, filepath.Join(dir, e.Name()))
	}
	sort.Strings(stale)
	return stale, nil
}

// SweepStale removes the files StaleFiles lists, holding the lock g
// describes so no save of the guarded file runs meanwhile. It does not
// wait for the lock: when another process holds it, SweepStale removes
// nothing and returns g.Busy (ErrBusy when nil), wrapped, for the caller
// to report and leave the files for a later sweep.
//
// Under dryRun it removes nothing and takes no lock (a lock file is a
// write): it returns what a real sweep would remove, or the same busy
// error when the lock is held now.
//
// It returns the files removed, or under dryRun those it would remove,
// even alongside an error: a file that cannot be removed is left out and
// its error joined into the one returned.
func (g Guard) SweepStale(fs afero.Fs, dir string, match func(name string) bool, cutoff time.Time, dryRun bool) ([]string, error) {
	// With nothing to remove, no lock is taken: that would create the
	// lock file, and its directory were dir gone.
	stale, err := StaleFiles(fs, dir, match, cutoff)
	if err != nil || len(stale) == 0 {
		return nil, err
	}
	if dryRun {
		if Held(g.Path) {
			return nil, g.busy()
		}
		return stale, nil
	}

	var removed []string
	g.Timeout = 0 // one attempt: a held lock means a save is running now
	err = g.Do(fs, func() error {
		// Listed again under the lock: a save may have finished since.
		stale, err := StaleFiles(fs, dir, match, cutoff)
		if err != nil {
			return err
		}
		var errs []error
		for _, path := range stale {
			if err := fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			removed = append(removed, path)
		}
		return errors.Join(errs...)
	})
	return removed, err
}

// busy is the error for a lock another process holds.
func (g Guard) busy() error {
	busy := g.Busy
	if busy == nil {
		busy = ErrBusy
	}
	return fmt.Errorf("%w: %s is held", busy, g.Path)
}
