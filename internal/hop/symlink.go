package hop

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// createSymlink creates a symlink
func createSymlink(fs afero.Fs, target, link string) error {
	// Try to use SymlinkIfPossible if the filesystem supports it
	type Symlinker interface {
		SymlinkIfPossible(oldname, newname string) error
	}

	if s, ok := fs.(Symlinker); ok {
		return s.SymlinkIfPossible(target, link)
	}

	// Fallback to os.Symlink for real filesystems
	// Note: This won't work with mock filesystems in tests

	// Create parent directories for the symlink if they don't exist
	// This is needed when branch names contain slashes (e.g., feat/my-feature)
	linkDir := filepath.Dir(link)
	if err := fs.MkdirAll(linkDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for symlink: %w", err)
	}

	// Verify directory was created
	if exists, _ := afero.DirExists(fs, linkDir); !exists {
		return fmt.Errorf("parent directory was not created: %s", linkDir)
	}

	// Create the symlink using os.Symlink
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	return nil
}
