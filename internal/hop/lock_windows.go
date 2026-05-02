//go:build windows

package hop

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryFlock attempts a non-blocking exclusive lock on the entire file using
// LockFileEx. Returns (true, nil) when acquired, (false, nil) when held by
// another process (ERROR_LOCK_VIOLATION), and a real error otherwise.
func tryFlock(f *os.File) (bool, error) {
	ol := new(windows.Overlapped)
	const flags = windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, err
}

func unflock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
