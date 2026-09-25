package hop

import "github.com/spf13/afero"

// WorktreePresence is what is at a worktree's path. Every command that
// asks whether a worktree is there asks WorktreeAt, so they all agree on
// which worktrees are.
type WorktreePresence int

const (
	// WorktreeAbsent: nothing is at the path.
	WorktreeAbsent WorktreePresence = iota
	// WorktreePresent: a directory is, or a symlink that leads to one.
	WorktreePresent
	// WorktreeOccupied: something other than a directory is: a file, a
	// symlink to one, or a dangling symlink. It is not the worktree, and
	// `git worktree add` cannot check one out over it.
	WorktreeOccupied
)

// WorktreeAt reports what is at path.
func WorktreeAt(fs afero.Fs, path string) WorktreePresence {
	if info, err := fs.Stat(path); err == nil {
		if info.IsDir() {
			return WorktreePresent
		}
		return WorktreeOccupied
	}
	if _, err := lstat(fs, path); err == nil {
		return WorktreeOccupied
	}
	return WorktreeAbsent
}

// WorktreeDirPresent reports whether the worktree directory at path is
// there. An occupied path counts as missing: the worktree is gone.
func WorktreeDirPresent(fs afero.Fs, path string) bool {
	return WorktreeAt(fs, path) == WorktreePresent
}
