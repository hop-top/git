package hop

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/filelock"
)

// hop.json is rewritten by every git-hop run that changes a hub, and
// several runs can work in one hub at once. Each rewrite is a
// load-modify-save; without serialising them, a run that loaded the file
// before another run saved it writes its stale copy back, undoing the
// other run (a removed branch reappears, an added one disappears).
//
// WithHopJSONLock serialises them with a filelock.Guard on hop.json.lock
// next to hop.json. Callers load hop.json afresh inside fn and hold the
// lock only around that load-modify-save: never across git commands,
// hooks or anything else that may run git-hop, which would wait on the
// lock this run holds.

// HopJSONLockName is the lock file WithHopJSONLock takes next to hop.json.
const HopJSONLockName = "hop.json.lock"

// hopJSONLockTimeout bounds the wait for a lock another live process
// holds. The hold is one load-modify-save, so reaching it means that
// process is stuck.
var hopJSONLockTimeout = 30 * time.Second

// ErrHopJSONLocked is returned when the lock stays held past the timeout.
var ErrHopJSONLocked = errors.New("hop.json is locked by another git-hop process")

// WithHopJSONLock runs fn while holding the exclusive lock on the
// hop.json in dir. fn must not take the same lock again.
func WithHopJSONLock(fs afero.Fs, dir string, fn func() error) error {
	return hopJSONGuard(dir).Do(fs, fn)
}

// writeHopJSONLocked writes data as the hop.json in dir under its lock.
// For writers that build the whole file themselves (clone, init, repair
// undo) rather than modifying the one on disk.
func writeHopJSONLocked(fs afero.Fs, dir string, data []byte) error {
	return WithHopJSONLock(fs, dir, func() error {
		return afero.WriteFile(fs, filepath.Join(dir, "hop.json"), data, 0644)
	})
}

// StaleHopJSONTemps lists the temp files a write of the hop.json in dir
// (or of ports.json or volumes.json beside it, which name theirs the
// same way) left behind: named as config.IsTempName names them and
// unmodified for filelock.StaleTempAge. It takes no lock.
func StaleHopJSONTemps(fs afero.Fs, dir string) ([]string, error) {
	return filelock.StaleFiles(fs, dir, config.IsTempName, staleTempCutoff())
}

// SweepHopJSONTemps removes the temp files StaleHopJSONTemps lists,
// holding the hop.json lock in dir so that no hop.json save runs
// meanwhile. When another process holds the lock it removes nothing and
// returns ErrHopJSONLocked (wrapped). Under dryRun it removes nothing and
// returns what it would remove. See filelock.Guard.SweepStale.
//
// A save of ports.json or volumes.json holds their own lock
// (ports.json.lock), not this one; its temp file is safe from the sweep
// by age alone, being renamed within moments of its creation.
func SweepHopJSONTemps(fs afero.Fs, dir string, dryRun bool) ([]string, error) {
	return hopJSONGuard(dir).SweepStale(fs, dir, config.IsTempName, staleTempCutoff(), dryRun)
}

// staleTempCutoff is the modification time before which a temp file
// counts as left behind.
func staleTempCutoff() time.Time {
	return time.Now().Add(-filelock.StaleTempAge)
}

// hopJSONGuard is the lock on the hop.json in dir.
func hopJSONGuard(dir string) filelock.Guard {
	return filelock.Guard{
		Path:    filepath.Join(dir, HopJSONLockName),
		Timeout: hopJSONLockTimeout,
		Busy:    ErrHopJSONLocked,
	}
}
