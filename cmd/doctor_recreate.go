package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
)

// recreateBlocker reports what would make `git worktree add` refuse to
// check out branch at path again, or "" when nothing would. It asks the
// same questions git does, so a --dry-run preview promises a recreate
// only when the real run would make it, and the real run reports the
// same reason instead of git's error:
//
//   - path is registered and git will not let go of it: locked, or not
//     prunable. A prunable registration is fine; clearStaleRegistration
//     drops it just before the add.
//   - branch is checked out in another worktree, even one whose own
//     directory is gone: git refuses a second checkout.
//   - something is in the way: path exists in a form Stat does not see
//     (a dangling symlink), or a parent of it is not a directory.
//   - branch does not exist, locally or as origin/<branch>, the start
//     point the recreate falls back to.
//
// registry is `git worktree list --porcelain`; gitDir is where the add
// runs.
func recreateBlocker(fs afero.Fs, g git.GitInterface, registry, gitDir, branch, path string) string {
	want := resolvedPath(path)
	for _, wt := range parseWorktreeList(registry) {
		if resolvedPath(wt.path) != want {
			if wt.branch() == branch {
				return fmt.Sprintf("branch %s is already checked out at %s", branch, wt.path)
			}
			continue
		}
		if reason, locked := wt.attrs["locked"]; locked {
			if reason == "" {
				return fmt.Sprintf("git has %s locked", path)
			}
			return fmt.Sprintf("git has %s locked (%s)", path, reason)
		}
		if _, prunable := wt.attrs["prunable"]; !prunable {
			return fmt.Sprintf("git still registers %s and does not mark it prunable", path)
		}
	}
	if blocked := pathInTheWay(fs, path); blocked != "" {
		return blocked + " is in the way"
	}
	if !branchResolves(g, gitDir, branch) {
		return fmt.Sprintf("branch %s does not exist (neither refs/heads/%s nor origin/%s)", branch, branch, branch)
	}
	return ""
}

// pathInTheWay returns what keeps a directory from being created at
// path, which Stat reports missing: path itself when something is there
// anyway (a dangling symlink), or the nearest existing parent when that
// is not a directory. "" when nothing is in the way.
func pathInTheWay(fs afero.Fs, path string) string {
	if _, err := lstat(fs, path); err == nil {
		return path
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if info, err := lstat(fs, dir); err == nil {
			if info.IsDir() {
				return ""
			}
			// A symlink is fine only when it leads to a directory.
			if target, err := fs.Stat(dir); err == nil && target.IsDir() {
				return ""
			}
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// lstat is fs's Lstat where it has one, Stat otherwise.
func lstat(fs afero.Fs, path string) (os.FileInfo, error) {
	if l, ok := fs.(afero.Lstater); ok {
		info, _, err := l.LstatIfPossible(path)
		return info, err
	}
	return fs.Stat(path)
}

// branchResolves reports whether branch can be checked out in the
// repository at dir: it exists locally, or as origin/<branch>, the
// start point the recreate falls back to.
func branchResolves(g git.GitInterface, dir, branch string) bool {
	for _, ref := range []string{"refs/heads/" + branch, "origin/" + branch} {
		if _, err := g.RunInDir(dir, "git", "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			return true
		}
	}
	return false
}
