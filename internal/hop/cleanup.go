package hop

import (
	"path/filepath"

	"hop.top/git/internal/git"
	"github.com/spf13/afero"
)

// CleanupManager handles cleanup operations for orphaned artifacts
type CleanupManager struct {
	fs  afero.Fs
	git git.GitInterface
}

// NewCleanupManager creates a new CleanupManager
func NewCleanupManager(fs afero.Fs, g git.GitInterface) *CleanupManager {
	return &CleanupManager{
		fs:  fs,
		git: g,
	}
}

// CleanupOrphanedDirectory removes an orphaned directory from the filesystem
// Returns nil if the directory doesn't exist (already cleaned up)
func (c *CleanupManager) CleanupOrphanedDirectory(path string) error {
	// Check if directory exists
	exists, err := afero.Exists(c.fs, path)
	if err != nil {
		return err
	}

	// Already gone, nothing to do
	if !exists {
		return nil
	}

	// Remove the directory
	return c.fs.RemoveAll(path)
}

// RemoveEmptyParent removes the parent directory of a worktree path if it is
// empty and distinct from the hub root. This handles the case where branches
// use a prefix like feat/ or fix/ — after the last worktree using that prefix
// is deleted the now-empty prefix directory is also cleaned up.
//
// hubPath is used as a stop boundary: the parent is never removed if it equals
// hubPath (or any ancestor of it).
func (c *CleanupManager) RemoveEmptyParent(worktreePath, hubPath string) error {
	parent := filepath.Dir(worktreePath)

	// Never remove the hub root or anything above it.
	if parent == hubPath || parent == filepath.Dir(hubPath) || parent == "." || parent == "/" {
		return nil
	}

	// Resolve both to absolute paths for a reliable comparison.
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return err
	}
	absHub, err := filepath.Abs(hubPath)
	if err != nil {
		return err
	}
	if absParent == absHub {
		return nil
	}

	// Check the parent exists.
	exists, err := afero.DirExists(c.fs, absParent)
	if err != nil || !exists {
		return err
	}

	// List contents; only remove if truly empty.
	entries, err := afero.ReadDir(c.fs, absParent)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil // still has siblings
	}

	return c.fs.Remove(absParent)
}

// PruneWorktrees removes stale git worktree metadata
func (c *CleanupManager) PruneWorktrees(hopspace *Hopspace) error {
	// Find a valid base path to run git commands from
	var basePath string
	for _, branch := range hopspace.Config.Branches {
		if branch.Exists && branch.Path != "" {
			// Verify the path actually exists before using it
			exists, err := afero.DirExists(c.fs, branch.Path)
			if err == nil && exists {
				basePath = branch.Path
				break
			}
		}
	}

	if basePath == "" {
		// No valid worktrees to prune from - this is not an error
		return nil
	}

	return c.git.WorktreePrune(basePath)
}
