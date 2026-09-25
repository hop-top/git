package hop

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// OrphanKind says what an orphaned directory under hops/ holds, and so
// whether it may be removed.
type OrphanKind int

const (
	// OrphanEmpty is an empty directory: nothing is lost by removing it.
	OrphanEmpty OrphanKind = iota
	// OrphanNotEmpty holds files git-hop knows nothing about; only the
	// user can say whether they matter.
	OrphanNotEmpty
	// OrphanWorktree is, or holds, a worktree git has registered, with
	// whatever work is in it; it is never removed.
	OrphanWorktree
)

// OrphanDir is an orphaned directory under hops/ (see
// DetectOrphanedDirectories) with what it holds.
type OrphanDir struct {
	// Rel is the path relative to hops/, as DetectOrphanedDirectories
	// reports it.
	Rel string
	// Path is the absolute path.
	Path string
	Kind OrphanKind
	// Worktrees are the worktrees git has registered at or below Path
	// that exist on disk, as git lists them; set only for OrphanWorktree.
	Worktrees []string
}

// ClassifyOrphanedDirectories says what each orphaned directory rels
// (relative to hopspace's hops/) holds. A directory is a worktree when git
// lists a worktree at it or below it whose directory exists; a directory
// that cannot be read counts as not empty. When git's list
// cannot be read, no directory is taken for a worktree: those that hold
// anything are still not empty, and an empty one holds no work.
func (v *StateValidator) ClassifyOrphanedDirectories(hopspace *Hopspace, hubPath string, rels []string) []OrphanDir {
	var registered []string
	if out, err := v.git.WorktreeListPorcelain(hubPath); err == nil {
		registered = parsePorcelainWorktrees(out)
	}

	hopsDir := filepath.Join(hopspace.Path, "hops")
	dirs := make([]OrphanDir, 0, len(rels))
	for _, rel := range rels {
		d := OrphanDir{Rel: rel, Path: filepath.Join(hopsDir, rel)}
		for _, wt := range registered {
			if !samePath(wt, d.Path) && !isStrictlyUnder(wt, d.Path) {
				continue
			}
			// A registration whose directory is gone holds no work:
			// 'git worktree prune' clears it.
			if exists, _ := afero.Exists(v.fs, wt); exists {
				d.Worktrees = append(d.Worktrees, wt)
			}
		}
		switch {
		case len(d.Worktrees) > 0:
			d.Kind = OrphanWorktree
		case isEmptyDir(v.fs, d.Path):
			d.Kind = OrphanEmpty
		default:
			d.Kind = OrphanNotEmpty
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// isEmptyDir reports whether path is a directory with nothing in it.
func isEmptyDir(fs afero.Fs, path string) bool {
	empty, err := afero.IsEmpty(fs, path)
	return err == nil && empty
}

// RemoveEmptyDirectory removes path only if it is an empty directory; one
// that is gone already is not an error. Whatever holds anything is
// refused, never cleared, and the removal itself fails on a directory
// something was put in since it was found empty.
func (c *CleanupManager) RemoveEmptyDirectory(path string) error {
	info, err := c.fs.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || !isEmptyDir(c.fs, path) {
		return fmt.Errorf("%s is not an empty directory", path)
	}
	return c.fs.Remove(path)
}
