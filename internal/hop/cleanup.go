package hop

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
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

// PruneWorktrees removes the stale worktree metadata of the hub's own
// repository: git runs in the hub, never in a worktree a shared
// --global hopspace records, which may be another hub's repository. A
// hub that is not on disk has nothing to prune.
func (c *CleanupManager) PruneWorktrees(hubPath string) error {
	if exists, err := afero.DirExists(c.fs, hubPath); err != nil || !exists {
		return nil
	}
	return c.git.WorktreePrune(hubPath)
}
