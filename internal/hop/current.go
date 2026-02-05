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
	if err := removeSymlinkIfExists(currentPath); err != nil {
		return err
	}

	// Create new symlink (using os.Symlink for real filesystem)
	// Note: afero doesn't have full symlink support in the interface
	if err := os.Symlink(relPath, currentPath); err != nil {
		return fmt.Errorf("failed to create current symlink: %w", err)
	}

	return nil
}

// GetCurrentSymlink returns the target of the "current" symlink (relative path)
func GetCurrentSymlink(fs afero.Fs, hubPath string) (string, error) {
	currentPath := filepath.Join(hubPath, currentSymlinkName)

	// Read symlink target (using os.Readlink for real filesystem)
	target, err := os.Readlink(currentPath)
	if err != nil {
		return "", fmt.Errorf("failed to read current symlink: %w", err)
	}

	return target, nil
}

// RemoveCurrentSymlink removes the "current" symlink from the hub (idempotent)
func RemoveCurrentSymlink(fs afero.Fs, hubPath string) error {
	currentPath := filepath.Join(hubPath, currentSymlinkName)
	return removeSymlinkIfExists(currentPath)
}

// removeSymlinkIfExists removes a symlink if it exists (idempotent helper)
func removeSymlinkIfExists(path string) error {
	// Check if exists
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Doesn't exist, nothing to do
			return nil
		}
		return fmt.Errorf("failed to stat symlink: %w", err)
	}

	// Remove it
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove symlink: %w", err)
	}

	return nil
}
