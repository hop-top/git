package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// Before SaveState replaces a state file that still holds branch-keyed
// entries, the file is copied to BackupDir as state-<UTC stamp>.json. A
// backup is written once and never overwritten; `git hop prune` ages
// backups out after hop.repair.backupRetention, like repair backups.

// BackupPrefix and BackupSuffix frame the name of every state backup.
const (
	BackupPrefix = "state-"
	BackupSuffix = ".json"
)

// backupTimeFormat is the UTC stamp in a backup's name: compact, sortable,
// the format repair backups use.
const backupTimeFormat = "20060102T150405Z"

// BackupDir is where state backups are written.
func BackupDir() string {
	return filepath.Join(GetStateHome(), "backups")
}

// IsBackupName reports whether name is a state backup's file name.
func IsBackupName(name string) bool {
	return strings.HasPrefix(name, BackupPrefix) && strings.HasSuffix(name, BackupSuffix)
}

// BackupTime returns when the backup named name was taken.
func BackupTime(name string) (time.Time, bool) {
	stamp := strings.TrimSuffix(strings.TrimPrefix(name, BackupPrefix), BackupSuffix)
	stamp, _, _ = strings.Cut(stamp, "-")
	t, err := time.Parse(backupTimeFormat, stamp)
	return t, err == nil
}

// backupLegacyState writes data to a new backup file and returns its
// path. The file is created exclusively: an existing backup is never
// overwritten, and a second backup in the same second gets a suffix.
func backupLegacyState(fs afero.Fs, data []byte, now time.Time) (string, error) {
	dir := BackupDir()
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := BackupPrefix + now.UTC().Format(backupTimeFormat)
	for i := 0; i < 100; i++ {
		name := base + BackupSuffix
		if i > 0 {
			name = fmt.Sprintf("%s-%d%s", base, i, BackupSuffix)
		}
		path := filepath.Join(dir, name)
		f, err := fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil {
			return "", werr
		}
		return path, cerr
	}
	return "", fmt.Errorf("no free backup name for %s in %s", base, dir)
}
