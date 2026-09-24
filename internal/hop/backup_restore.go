package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
)

// ErrRestoreTargetNotEmpty is returned by RestoreToOriginal when the
// location a backup was taken from holds something (typically the hub
// the conversion produced) and the caller did not ask to replace it.
var ErrRestoreTargetNotEmpty = errors.New("original location exists and is not empty")

// RestoreOptions tunes RestoreToOriginal.
type RestoreOptions struct {
	// Replace moves an occupied original location aside instead of
	// refusing (init --restore --force).
	Replace bool
	// Clock stamps the moved-aside name; nil means time.Now.
	Clock func() time.Time
}

// RestoreResult says where RestoreToOriginal put things.
type RestoreResult struct {
	// Target is the location the backup was restored to: the one its
	// metadata records.
	Target string
	// MovedAside is where the previous occupant of Target was moved;
	// empty when Target was missing or empty.
	MovedAside string
}

// RestoreToOriginal restores the conversion backup at backupPath to the
// location it was taken from, as recorded in its metadata. The process
// cwd plays no part.
//
// A missing or empty location is restored into. Anything else is
// refused with ErrRestoreTargetNotEmpty unless opts.Replace is set, in which
// case it is moved aside to <location>.pre-restore-<UTC time> (see
// preRestorePath), never deleted. When that move fails, nothing has been
// touched. A backup that lies inside the location is refused either way:
// it would move aside along with everything else.
func (c *Converter) RestoreToOriginal(backupPath string, opts RestoreOptions) (RestoreResult, error) {
	mgr, err := LoadBackupManager(c.fs, c.git, backupPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("failed to load backup: %w", err)
	}

	res := RestoreResult{Target: mgr.metadata.OriginalPath}
	if res.Target == "" {
		return res, fmt.Errorf("backup metadata %s records no original location to restore to",
			filepath.Join(backupPath, backupMetadataFile))
	}

	if pathWithin(backupPath, res.Target) {
		return res, fmt.Errorf("backup %s lies inside %s, the location it restores to; move the backup elsewhere first",
			backupPath, res.Target)
	}

	occupied, err := pathOccupied(c.fs, res.Target)
	if err != nil {
		return res, fmt.Errorf("failed to inspect %s: %w", res.Target, err)
	}
	if occupied {
		if !opts.Replace {
			return res, fmt.Errorf("%w: %s", ErrRestoreTargetNotEmpty, res.Target)
		}
		aside, err := preRestorePath(c.fs, res.Target, opts.Clock)
		if err != nil {
			return res, fmt.Errorf("failed to choose where to move %s aside: %w; nothing was changed", res.Target, err)
		}
		if err := c.fs.Rename(res.Target, aside); err != nil {
			return res, fmt.Errorf("failed to move %s aside to %s: %w; nothing was changed", res.Target, aside, err)
		}
		res.MovedAside = aside
	}

	if err := mgr.Restore(res.Target); err != nil {
		if res.MovedAside != "" {
			return res, fmt.Errorf("failed to restore from backup: %w (the previous contents are kept at %s)", err, res.MovedAside)
		}
		return res, fmt.Errorf("failed to restore from backup: %w", err)
	}
	return res, nil
}

// preRestoreTimeFormat is a filesystem-safe UTC timestamp:
// 20260924T101500Z.
const preRestoreTimeFormat = "20060102T150405Z"

// preRestorePath is the first free <target>.pre-restore-<UTC time> name,
// with -1, -2, ... appended when restores land in the same second.
func preRestorePath(fs afero.Fs, target string, clock func() time.Time) (string, error) {
	now := time.Now
	if clock != nil {
		now = clock
	}
	base := target + ".pre-restore-" + now().UTC().Format(preRestoreTimeFormat)
	candidate := base
	for i := 1; ; i++ {
		taken, err := pathExists(fs, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// pathExists reports whether anything, a dangling symlink included, is
// at path.
func pathExists(fs afero.Fs, path string) (bool, error) {
	_, err := lstat(fs, path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func lstat(fs afero.Fs, path string) (os.FileInfo, error) {
	if l, ok := fs.(afero.Lstater); ok {
		info, _, err := l.LstatIfPossible(path)
		return info, err
	}
	return fs.Stat(path)
}

// pathOccupied reports whether path exists as anything other than an
// empty directory. A symlink counts as occupied, whatever it points at.
func pathOccupied(fs afero.Fs, path string) (bool, error) {
	info, err := lstat(fs, path)
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
