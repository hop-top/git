package hop

import (
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
