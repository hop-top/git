package state

import (
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/afero"

	"hop.top/git/internal/filelock"
)

// A save writes state.json.<digits>.tmp next to state.json and renames
// it over the file (replaceFile). A run that dies in between leaves the
// temp file behind; SweepTemps removes such leftovers.

// tempSuffixPattern follows a saved file's name in its temp file's name
// pattern: the * becomes the random digits.
const tempSuffixPattern = ".*.tmp"

// tempName matches the names replaceFile gives state.json's temp files,
// and nothing else.
var tempName = regexp.MustCompile(`^state\.json\.[0-9]+\.tmp$`)

// IsTempName reports whether name is the name of a temp file a save of
// state.json writes.
func IsTempName(name string) bool {
	return tempName.MatchString(name)
}

// StaleTemps lists the temp files next to state.json that a save left
// behind: those named as a save names them and unmodified for
// filelock.StaleTempAge. It takes no lock.
func StaleTemps(fs afero.Fs) ([]string, error) {
	return filelock.StaleFiles(fs, GetStateHome(), IsTempName, staleCutoff())
}

// SweepTemps removes the temp files StaleTemps lists, holding the state
// lock so that no save runs meanwhile. When another process holds the
// lock it removes nothing and returns ErrLocked (wrapped). Under dryRun
// it removes nothing and returns what it would remove. See
// filelock.Guard.SweepStale.
func SweepTemps(fs afero.Fs, dryRun bool) ([]string, error) {
	return stateGuard().SweepStale(fs, GetStateHome(), IsTempName, staleCutoff(), dryRun)
}

// staleCutoff is the modification time before which a temp file counts
// as left behind.
func staleCutoff() time.Time {
	return time.Now().Add(-filelock.StaleTempAge)
}

// stateGuard is the lock on state.json.
func stateGuard() filelock.Guard {
	return filelock.Guard{
		Path:    filepath.Join(GetStateHome(), LockName),
		Timeout: lockTimeout,
		Busy:    ErrLocked,
	}
}
