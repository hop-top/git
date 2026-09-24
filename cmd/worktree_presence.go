package cmd

import "github.com/spf13/afero"

// worktreePresence is what is at a worktree's path. doctor's hub and
// state checks and prune all ask worktreeAt, so they agree on which
// worktrees are there.
type worktreePresence int

const (
	// worktreeAbsent: nothing is at the path.
	worktreeAbsent worktreePresence = iota
	// worktreePresent: a directory is, or a symlink that leads to one.
	worktreePresent
	// worktreeOccupied: something other than a directory is: a file, a
	// symlink to one, or a dangling symlink. It is not the worktree, and
	// `git worktree add` cannot check one out over it (recreateBlocker).
	worktreeOccupied
)

// worktreeAt reports what is at path.
func worktreeAt(fs afero.Fs, path string) worktreePresence {
	if info, err := fs.Stat(path); err == nil {
		if info.IsDir() {
			return worktreePresent
		}
		return worktreeOccupied
	}
	if _, err := lstat(fs, path); err == nil {
		return worktreeOccupied
	}
	return worktreeAbsent
}

// worktreeDirPresent reports whether the worktree directory at path is
// there. An occupied path counts as missing: the worktree is gone.
func worktreeDirPresent(fs afero.Fs, path string) bool {
	return worktreeAt(fs, path) == worktreePresent
}
