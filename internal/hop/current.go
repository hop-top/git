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

	// Replace the existing symlink; anything else there is left alone.
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

// RemoveCurrentSymlink removes the "current" symlink from the hub
// (idempotent). Anything else named current is left alone, with an
// error (see CheckCurrentSymlink).
func RemoveCurrentSymlink(fs afero.Fs, hubPath string) error {
	currentPath := filepath.Join(hubPath, currentSymlinkName)
	return removeSymlinkIfExists(fs, currentPath)
}

// CheckCurrentSymlink reports why the hub's "current" cannot be pointed
// anywhere: something other than a symlink is there, a file or a
// directory the user made. git-hop never replaces or deletes it; the
// error says so, with a hint. Nothing there, or a symlink, is fine.
func CheckCurrentSymlink(fs afero.Fs, hubPath string) error {
	return checkSymlinkOrAbsent(fs, filepath.Join(hubPath, currentSymlinkName))
}

// checkSymlinkOrAbsent is CheckCurrentSymlink for the path itself.
func checkSymlinkOrAbsent(fs afero.Fs, path string) error {
	info, err := lstat(fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	kind := "a file"
	if info.IsDir() {
		kind = "a directory"
	}
	return fmt.Errorf("'%s' is %s, not a symlink; git-hop leaves it as it is\n"+
		"hint: rename it: while it is there, git-hop cannot keep the 'current' link", path, kind)
}

// removeSymlinkIfExists removes the symlink at path if there is one
// (idempotent). Anything else at path is refused, never removed.
func removeSymlinkIfExists(fs afero.Fs, path string) error {
	if err := checkSymlinkOrAbsent(fs, path); err != nil {
		return err
	}
	if _, err := lstat(fs, path); os.IsNotExist(err) {
		return nil
	}
	if err := fs.Remove(path); err != nil {
		return fmt.Errorf("failed to remove symlink: %w", err)
	}

	return nil
}
