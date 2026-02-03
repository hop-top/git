package hop

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// createSymlink creates a symlink
func createSymlink(fs afero.Fs, target, link string) error {
	// afero.Fs doesn't support Symlink directly in the interface for all backends,
	// but OsFs does.
	// However, since we are using afero.Fs interface, we might need to type assert or use os package if we assume OsFs.
	// For now, let's assume we are running on a system where os.Symlink works and we can use the OsFs or similar.
	//
	// But `fs.SymlinkIfPossible` exists in some versions/extensions.
	//
	// Since we are targeting a real system, we can use `os.Symlink` if we are sure `fs` maps to OS.
	// But to be correct with `afero`, we should use a wrapper or check if it supports it.
	//
	// Let's use `os.Symlink` directly for now, assuming `link` is a real path.
	// But wait, if we are mocking `fs`, `os.Symlink` will create a real symlink on disk, which might fail or be wrong.
	//
	// Ideally `afero` should be used. `afero.Symlinker` interface?
	//
	// Let's check if fs implements Symlinker.

	type Symlinker interface {
		SymlinkIfPossible(oldname, newname string) error
	}

	if s, ok := fs.(Symlinker); ok {
		return s.SymlinkIfPossible(target, link)
	}

	// Fallback to os.Symlink if it's OsFs (which we can't easily check by type without import loop or specific type)
	// Or just use os.Symlink and hope for the best if we are not in a mock.
	//
	// Given this is a "Project Foundations" task, let's stick to `os.Symlink` for the real implementation
	// and note that mocks need to handle it or we need a better abstraction.
	//
	// Actually, `afero` has `SymlinkIfPossible` in the `OsFs` struct but not the interface.

	// Create parent directories for the symlink if they don't exist
	// This is needed when branch names contain slashes (e.g., feat/my-feature)
	linkDir := filepath.Dir(link)
	fmt.Printf("DEBUG: Creating parent directory: %s\n", linkDir)
	if err := fs.MkdirAll(linkDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for symlink: %w", err)
	}

	// Verify directory was created
	if exists, _ := afero.DirExists(fs, linkDir); !exists {
		return fmt.Errorf("parent directory was not created: %s", linkDir)
	}
	fmt.Printf("DEBUG: Parent directory confirmed: %s\n", linkDir)

	// For now, let's just use os.Symlink.
	fmt.Printf("DEBUG: Creating symlink: %s -> %s\n", link, target)
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	fmt.Printf("DEBUG: Symlink created successfully\n")
	return nil
}
