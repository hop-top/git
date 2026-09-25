package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// now stamps a move's trash folder; tests fix it.
	now func() time.Time
}

// NewTrash creates a new trash instance
func NewTrash(fs afero.Fs) *Trash {
	t := &Trash{fs: fs, now: time.Now}
	if _, ok := fs.(*afero.OsFs); ok {
		t.rename = os.Rename
	}
	return t
}

// backupDirLayout names a move's trash folder under the backups
// directory: the second it was made, then "-<n>" for a later move made
// in the same second.
const backupDirLayout = "20060102-150405"

// Move moves a path to the backup/trash directory
// Returns the backup path where the file was moved
func (t *Trash) Move(path string) (string, error) {
	backupDir, err := t.newBackupDir()
	if err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	destPath := filepath.Join(backupDir, filepath.Base(path))

	if t.rename == nil || t.rename(path, destPath) != nil {
		if err := t.moveByCopy(path, destPath); err != nil {
			// Drops the folder only when the copy left it empty.
			_ = t.fs.Remove(backupDir)
			return "", err
		}
	}

	return destPath, nil
}

// newBackupDir creates a trash folder of its own for one move, named
// for the current second (backupDirLayout). A folder already there, from
// an earlier move in the same second, is never reused: the next free
// "-<n>" suffix is taken, so two moves of the same name never meet. The
// folder is created with Mkdir, which fails when it exists, so two runs
// cannot take the same one either.
func (t *Trash) newBackupDir() (string, error) {
	base := filepath.Join(hop.GetGitHopDataHome(), "backups")
	if err := t.fs.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	stamp := t.now().Format(backupDirLayout)
	for n := 0; n < 10000; n++ {
		name := stamp
		if n > 0 {
			name = fmt.Sprintf("%s-%d", stamp, n)
		}
		dir := filepath.Join(base, name)
		err := t.fs.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if exists, _ := afero.DirExists(t.fs, dir); !exists {
			return "", err
		}
	}
	return "", fmt.Errorf("no free trash folder for %s", stamp)
}

// parseBackupDir returns the time a trash folder's name records, and
// false for a name backupDirLayout (plus an optional "-<n>") does not
// fit. The name is local time, as Move stamps it.
func parseBackupDir(name string) (time.Time, bool) {
	if len(name) < len(backupDirLayout) {
		return time.Time{}, false
	}
	stamp, suffix := name[:len(backupDirLayout)], name[len(backupDirLayout):]
	if suffix != "" {
		n := strings.TrimPrefix(suffix, "-")
		if n == suffix || n == "" || strings.Trim(n, "0123456789") != "" {
			return time.Time{}, false
		}
	}
	ts, err := time.ParseInLocation(backupDirLayout, stamp, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// moveByCopy moves src to dst, which does not exist, when a rename
// cannot: it copies src to dst (copyPath) and then removes src
// (removeCopied). If the copy fails, the partial copy is removed and src
// is left as it was.
func (t *Trash) moveByCopy(src, dst string) error {
	if err := t.copyPath(src, dst); err != nil {
		if rerr := t.removeTree(dst); rerr != nil {
			return fmt.Errorf("failed to copy %s: %w (partial copy left at %s: %v)", src, err, dst, rerr)
		}
		return fmt.Errorf("failed to copy %s: %w", src, err)
	}
	return t.removeCopied(src, dst)
}

// removeCopied removes src once it has been copied to dst.
//
// A read-only directory (a Go module cache, say) cannot have its entries
// removed, so the directories in src are made writable first: only those
// inside src, never through a symlink. When that fails (a directory
// another user owns), nothing has been removed yet: the modes changed so
// far are put back, the copy is removed and src is left whole.
//
// A removal that still fails can have removed part of src already; what
// it removed exists only in the copy. It is rolled back: what is missing
// from src is copied back from dst, the directory modes are put back,
// and the copy is removed. Only when copying back fails too is the copy
// kept, so nothing is lost, and the error says where it is.
func (t *Trash) removeCopied(src, dst string) error {
	changed, err := t.makeDirsWritable(src)
	if err != nil {
		t.restoreModes(changed)
		if rerr := t.removeTree(dst); rerr != nil {
			return fmt.Errorf("failed to remove %s: %w; it is left in place, and its copy at %s could not be removed: %v", src, err, dst, rerr)
		}
		return fmt.Errorf("failed to remove %s: %w; it is left in place", src, err)
	}
	removeErr := t.fs.RemoveAll(src)
	if removeErr == nil {
		return nil
	}
	if err := t.copyMissing(dst, src); err != nil {
		t.restoreModes(changed)
		return fmt.Errorf("failed to remove %s: %w; it is partly removed, and could not be restored (%v): its copy is kept at %s", src, removeErr, err, dst)
	}
	t.restoreModes(changed)
	if err := t.removeTree(dst); err != nil {
		return fmt.Errorf("failed to remove %s: %w; it is restored, and its copy at %s could not be removed: %v", src, removeErr, dst, err)
	}
	return fmt.Errorf("failed to remove %s: %w; it is restored", src, removeErr)
}

// dirMode is a directory's mode before makeDirsWritable changed it.
type dirMode struct {
	path string
	mode os.FileMode
}

// makeDirsWritable gives the owner write permission on root, when it is
// a directory, and on every directory under it, without following
// symlinks. It returns the directories it changed, with their former
// modes, including when it fails partway.
func (t *Trash) makeDirsWritable(root string) ([]dirMode, error) {
	var changed []dirMode
	var walk func(path string) error
	walk = func(path string) error {
		info, err := lstat(t.fs, path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		mode := info.Mode()
		if mode.Perm()&0o200 == 0 {
			if err := t.fs.Chmod(path, chmodBits(mode)|0o200); err != nil {
				return err
			}
			changed = append(changed, dirMode{path: path, mode: mode})
		}
		entries, err := afero.ReadDir(t.fs, path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if err := walk(filepath.Join(path, entry.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return changed, walk(root)
}

// restoreModes puts back the modes makeDirsWritable changed, deepest
// first, on the directories still there.
func (t *Trash) restoreModes(changed []dirMode) {
	for i := len(changed) - 1; i >= 0; i-- {
		_ = t.fs.Chmod(changed[i].path, chmodBits(changed[i].mode))
	}
}

// chmodBits is the part of mode Chmod sets.
func chmodBits(mode os.FileMode) os.FileMode {
	return mode & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
}

// copyMissing copies back into dst what src has and dst lacks, entry by
// entry through directories both have; what dst still has is left as it
// is.
func (t *Trash) copyMissing(src, dst string) error {
	dstInfo, err := lstat(t.fs, dst)
	if os.IsNotExist(err) {
		return t.copyPath(src, dst)
	}
	if err != nil {
		return err
	}
	srcInfo, err := lstat(t.fs, src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() || !dstInfo.IsDir() {
		return nil
	}
	entries, err := afero.ReadDir(t.fs, src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := t.copyMissing(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// removeTree removes path and everything under it, read-only directories
// included: they are made writable first, never through a symlink.
func (t *Trash) removeTree(path string) error {
	if _, err := t.makeDirsWritable(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return t.fs.RemoveAll(path)
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
		if err := t.moveByCopy(backupPath, originalPath); err != nil {
			return fmt.Errorf("failed to restore from backup: %w", err)
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
		timestamp, ok := parseBackupDir(entry.Name())
		if !ok {
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
		timestamp, ok := parseBackupDir(entry.Name())
		if !ok {
			continue
		}

		// Delete if older than cutoff
		if timestamp.Before(cutoffTime) {
			backupDir := filepath.Join(backupBase, entry.Name())
			size := t.getDirSize(backupDir)

			if err := t.removeTree(backupDir); err != nil {
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
