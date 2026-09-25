package hop

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

const currentSymlinkName = "current"

// UpdateCurrentSymlink creates or updates the "current" symlink in the hub to point to the given worktree
// The symlink is relative for portability (e.g., "hops/main" not "/abs/path/to/hops/main")
func UpdateCurrentSymlink(fs afero.Fs, hubPath, worktreePath string) error {
	// Calculate relative path from hub to worktree
	relPath, err := filepath.Rel(hubPath, worktreePath)
	if err != nil {
		return fmt.Errorf("failed to calculate relative path: %w", err)
	}

	currentPath := filepath.Join(hubPath, currentSymlinkName)

	// Remove existing symlink if it exists
	if err := removeSymlinkIfExists(fs, currentPath); err != nil {
		return err
	}

	linker, ok := fs.(afero.Linker)
	if !ok {
		return fmt.Errorf("failed to create current symlink: %w", afero.ErrNoSymlink)
	}
	if err := linker.SymlinkIfPossible(relPath, currentPath); err != nil {
		return fmt.Errorf("failed to create current symlink: %w", err)
	}

	return nil
}

// GetCurrentSymlink returns the target of the "current" symlink (relative path)
func GetCurrentSymlink(fs afero.Fs, hubPath string) (string, error) {
	currentPath := filepath.Join(hubPath, currentSymlinkName)

	reader, ok := fs.(afero.LinkReader)
	if !ok {
		return "", fmt.Errorf("failed to read current symlink: %w", afero.ErrNoReadlink)
	}
	target, err := reader.ReadlinkIfPossible(currentPath)
	if err != nil {
		return "", fmt.Errorf("failed to read current symlink: %w", err)
	}

	return target, nil
}

// RemoveCurrentSymlink removes the "current" symlink from the hub (idempotent)
func RemoveCurrentSymlink(fs afero.Fs, hubPath string) error {
	currentPath := filepath.Join(hubPath, currentSymlinkName)
	return removeSymlinkIfExists(fs, currentPath)
}

// removeSymlinkIfExists removes a symlink if it exists (idempotent helper)
func removeSymlinkIfExists(fs afero.Fs, path string) error {
	if _, err := lstat(fs, path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat symlink: %w", err)
	}

	if err := fs.Remove(path); err != nil {
		return fmt.Errorf("failed to remove symlink: %w", err)
	}

	return nil
}
