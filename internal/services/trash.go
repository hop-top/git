package services

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

// Trash provides safe file deletion with recovery capabilities
type Trash struct {
	fs afero.Fs
	// rename moves a path in one step, as os.Rename does. It is set only
	// on the OS filesystem; when it is nil or fails (a move across
	// filesystems), the path is copied and the original removed.
	rename func(src, dst string) error
}

// NewTrash creates a new trash instance
func NewTrash(fs afero.Fs) *Trash {
	t := &Trash{fs: fs}
	if _, ok := fs.(*afero.OsFs); ok {
		t.rename = os.Rename
	}
	return t
}

// Move moves a path to the backup/trash directory
// Returns the backup path where the file was moved
func (t *Trash) Move(path string) (string, error) {
	// Create backup directory
	dataHome := hop.GetGitHopDataHome()
	timestamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(dataHome, "backups", timestamp)

	if err := t.fs.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	destPath := filepath.Join(backupDir, filepath.Base(path))

	if t.rename == nil || t.rename(path, destPath) != nil {
		if err := t.copyPath(path, destPath); err != nil {
			return "", fmt.Errorf("failed to copy to backup: %w", err)
		}
		if err := t.fs.RemoveAll(path); err != nil {
			return "", fmt.Errorf("failed to remove original: %w", err)
		}
	}

	return destPath, nil
}

// Restore restores a file from backup to its original location
func (t *Trash) Restore(backupPath, originalPath string) error {
	// Check if backup exists
	exists, err := afero.Exists(t.fs, backupPath)
	if err != nil {
		return fmt.Errorf("failed to check backup existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("backup not found at %s", backupPath)
	}

	// Check if original path already exists
	originalExists, err := afero.Exists(t.fs, originalPath)
	if err != nil {
		return fmt.Errorf("failed to check original path: %w", err)
	}
	if originalExists {
		return fmt.Errorf("original path already exists: %s", originalPath)
	}

	parentDir := filepath.Dir(originalPath)
	if err := t.fs.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	if t.rename == nil || t.rename(backupPath, originalPath) != nil {
		if err := t.copyPath(backupPath, originalPath); err != nil {
			return fmt.Errorf("failed to copy from backup: %w", err)
		}
		if err := t.fs.RemoveAll(backupPath); err != nil {
			return fmt.Errorf("failed to remove backup: %w", err)
		}
	}

	return nil
}

// List lists all backups in the trash directory
func (t *Trash) List() ([]BackupInfo, error) {
	dataHome := hop.GetGitHopDataHome()
	backupBase := filepath.Join(dataHome, "backups")

	// Check if backup directory exists
	exists, err := afero.DirExists(t.fs, backupBase)
	if err != nil {
		return nil, fmt.Errorf("failed to check backup directory: %w", err)
	}
	if !exists {
		return []BackupInfo{}, nil
	}

	// List all backup directories
	entries, err := afero.ReadDir(t.fs, backupBase)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	backups := []BackupInfo{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Parse timestamp from directory name
		timestamp, err := time.Parse("20060102-150405", entry.Name())
		if err != nil {
			continue
		}

		// List items in this backup
		backupDir := filepath.Join(backupBase, entry.Name())
		items, err := afero.ReadDir(t.fs, backupDir)
		if err != nil {
			continue
		}

		for _, item := range items {
			itemPath := filepath.Join(backupDir, item.Name())
			size := int64(0)
			if item.IsDir() {
				size = t.getDirSize(itemPath)
			} else {
				size = item.Size()
			}

			backups = append(backups, BackupInfo{
				Name:      item.Name(),
				Path:      itemPath,
				Timestamp: timestamp,
				Size:      size,
				IsDir:     item.IsDir(),
			})
		}
	}

	return backups, nil
}

// Clean removes old backups older than the specified duration
func (t *Trash) Clean(olderThan time.Duration) (int, int64, error) {
	dataHome := hop.GetGitHopDataHome()
	backupBase := filepath.Join(dataHome, "backups")

	// Check if backup directory exists
	exists, err := afero.DirExists(t.fs, backupBase)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to check backup directory: %w", err)
	}
	if !exists {
		return 0, 0, nil
	}

	// List all backup directories
	entries, err := afero.ReadDir(t.fs, backupBase)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read backup directory: %w", err)
	}

	cutoffTime := time.Now().Add(-olderThan)
	var deletedCount int
	var deletedSize int64

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Parse timestamp from directory name
		timestamp, err := time.Parse("20060102-150405", entry.Name())
		if err != nil {
			continue
		}

		// Delete if older than cutoff
		if timestamp.Before(cutoffTime) {
			backupDir := filepath.Join(backupBase, entry.Name())
			size := t.getDirSize(backupDir)

			if err := t.fs.RemoveAll(backupDir); err != nil {
				return deletedCount, deletedSize, fmt.Errorf("failed to delete backup %s: %w", entry.Name(), err)
			}

			deletedCount++
			deletedSize += size
		}
	}

	return deletedCount, deletedSize, nil
}

// BackupInfo represents information about a backup
type BackupInfo struct {
	Name      string
	Path      string
	Timestamp time.Time
	Size      int64
	IsDir     bool
}

// copyPath copies src to dst without following symlinks, for a move
// that cannot rename: a symlink is copied as a symlink with the same
// target (dangling or relative ones too), a directory entry by entry, a
// regular file byte for byte, each keeping its mode. Following a link
// would copy what it points at, a whole shared deps store say, and trash
// something other than what was there. Anything else (a FIFO, a socket,
// a device) is refused, so the original stays where it is.
func (t *Trash) copyPath(src, dst string) error {
	info, err := lstat(t.fs, src)
	if err != nil {
		return err
	}
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return t.copySymlink(src, dst)
	case mode.IsDir():
		return t.copyDir(src, dst, mode)
	case mode.IsRegular():
		if err := copyFile(t.fs, src, dst, mode.Perm()); err != nil {
			return err
		}
		return t.fs.Chmod(dst, mode)
	default:
		return fmt.Errorf("cannot copy %s: not a regular file, directory or symlink", src)
	}
}

// copySymlink recreates the symlink src at dst with the same target.
func (t *Trash) copySymlink(src, dst string) error {
	linker, ok := t.fs.(afero.Symlinker)
	if !ok {
		return fmt.Errorf("cannot copy symlink %s: the filesystem has no symlinks", src)
	}
	target, err := linker.ReadlinkIfPossible(src)
	if err != nil {
		return err
	}
	return linker.SymlinkIfPossible(target, dst)
}

// copyDir copies the directory src, entry by entry, to dst. dst stays
// writable while it is filled and gets src's mode last, so a read-only
// directory is copied too.
func (t *Trash) copyDir(src, dst string, mode os.FileMode) error {
	if err := t.fs.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	entries, err := afero.ReadDir(t.fs, src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := t.copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return t.fs.Chmod(dst, mode)
}

// getDirSize calculates the total size of a directory
func (t *Trash) getDirSize(path string) int64 {
	var size int64
	afero.Walk(t.fs, path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
