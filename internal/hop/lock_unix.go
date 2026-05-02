//go:build !windows

package hop

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryFlock attempts a non-blocking exclusive flock. Returns (true, nil)
// when acquired, (false, nil) when held by another process, and a real
// error for anything else.
func tryFlock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

func unflock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
