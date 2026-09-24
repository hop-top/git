package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// ErrRestoreTargetNotEmpty is returned by RestoreToOriginal when the
// location a backup was taken from holds something (typically the hub
// the conversion produced) and the caller did not ask to replace it.
var ErrRestoreTargetNotEmpty = errors.New("original location exists and is not empty")

// RestoreToOriginal restores the conversion backup at backupPath to the
// location it was taken from, as recorded in its metadata, and returns
// that location. The process cwd plays no part.
//
// A missing or empty location is restored into. Anything else is
// refused with ErrRestoreTargetNotEmpty unless replace is set, in which
// case it is deleted first. A backup that lies inside the location is
// refused either way: replacing the location would delete it.
func (c *Converter) RestoreToOriginal(backupPath string, replace bool) (string, error) {
	mgr, err := LoadBackupManager(c.fs, c.git, backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to load backup: %w", err)
	}

	target := mgr.metadata.OriginalPath
	if target == "" {
		return "", fmt.Errorf("backup metadata %s records no original location to restore to",
			filepath.Join(backupPath, backupMetadataFile))
	}

	if pathWithin(backupPath, target) {
		return target, fmt.Errorf("backup %s lies inside %s, which the restore replaces; move the backup elsewhere first",
			backupPath, target)
	}

	if !replace {
		occupied, err := pathOccupied(c.fs, target)
		if err != nil {
			return target, fmt.Errorf("failed to inspect %s: %w", target, err)
		}
		if occupied {
			return target, fmt.Errorf("%w: %s", ErrRestoreTargetNotEmpty, target)
		}
	}

	if err := mgr.Restore(target); err != nil {
		return target, fmt.Errorf("failed to restore from backup: %w", err)
	}
	return target, nil
}

// pathOccupied reports whether path exists as anything other than an
// empty directory. A symlink counts as occupied, whatever it points at.
func pathOccupied(fs afero.Fs, path string) (bool, error) {
	var info os.FileInfo
	var err error
	if l, ok := fs.(afero.Lstater); ok {
		info, _, err = l.LstatIfPossible(path)
	} else {
		info, err = fs.Stat(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return true, nil
	}
	empty, err := afero.IsEmpty(fs, path)
	if err != nil {
		return false, err
	}
	return !empty, nil
}
