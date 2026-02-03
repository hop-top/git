package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
)

// Trash provides safe file deletion with recovery capabilities
type Trash struct {
	fs afero.Fs
}

// NewTrash creates a new trash instance
func NewTrash(fs afero.Fs) *Trash {
	return &Trash{fs: fs}
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

	// Calculate destination path
	destPath := filepath.Join(backupDir, filepath.Base(path))

	// For OS filesystem, use os.Rename for efficiency
	if _, ok := t.fs.(*afero.OsFs); ok {
		if err := os.Rename(path, destPath); err != nil {
			// Fallback to copy+delete if rename fails (cross-device link)
			if err := t.copyPath(path, destPath); err != nil {
				return "", fmt.Errorf("failed to copy to backup: %w", err)
			}
			if err := t.fs.RemoveAll(path); err != nil {
				return "", fmt.Errorf("failed to remove original: %w", err)
			}
		}
	} else {
		// For other filesystems, use copy+delete
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

	// Ensure parent directory exists
	parentDir := filepath.Dir(originalPath)
	if err := t.fs.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// For OS filesystem, use os.Rename for efficiency
	if _, ok := t.fs.(*afero.OsFs); ok {
		if err := os.Rename(backupPath, originalPath); err != nil {
			// Fallback to copy+delete if rename fails
			if err := t.copyPath(backupPath, originalPath); err != nil {
				return fmt.Errorf("failed to copy from backup: %w", err)
			}
			if err := t.fs.RemoveAll(backupPath); err != nil {
				return fmt.Errorf("failed to remove backup: %w", err)
			}
		}
	} else {
		// For other filesystems, use copy+delete
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

// copyPath recursively copies a file or directory
func (t *Trash) copyPath(src, dst string) error {
	info, err := t.fs.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return t.copyDir(src, dst)
	}
	return t.copyFile(src, dst)
}

// copyFile copies a single file
func (t *Trash) copyFile(src, dst string) error {
	srcFile, err := t.fs.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get source file info for permissions
	srcInfo, err := t.fs.Stat(src)
	if err != nil {
		return err
	}

	dstFile, err := t.fs.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return t.fs.Chmod(dst, srcInfo.Mode())
}

// copyDir recursively copies a directory
func (t *Trash) copyDir(src, dst string) error {
	return afero.Walk(t.fs, src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate destination path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return t.fs.MkdirAll(destPath, info.Mode())
		}

		return t.copyFile(path, destPath)
	})
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
