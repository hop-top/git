package services

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// OverrideDir is a compose override cache directory no worktree uses.
type OverrideDir struct {
	Path string
	// Root is the repository's cache directory it lies in; removal
	// prunes the empty directories it leaves up to there.
	Root string
	Size int64
}

// OrphanedOverrideDirs returns the compose override cache directories,
// under the cache directory of each repository recs knows, that no
// worktree of any hub in recs uses: not the one an entry records, nor,
// for a worktree whose entry records none (or that has no entry), the
// repository-wide one of its branch, which env start falls back to. A
// directory counts only if it holds the override metadata git-hop
// writes; nothing else in the cache is considered.
func OrphanedOverrideDirs(fs afero.Fs, recs *EnvRecords) ([]OverrideDir, error) {
	used := map[string]bool{}
	use := func(dir string) {
		if dir != "" {
			used[state.ResolvePath(dir)] = true
		}
	}
	roots := map[string]bool{}
	cache := hop.GetGitHopCacheHome()
	for _, c := range recs.Claims {
		if c.Org != "" && c.Repo != "" {
			roots[filepath.Join(cache, c.Org, c.Repo)] = true
		}
		use(c.Entry.OverrideDir)
	}
	for _, h := range recs.Hubs {
		if h.Org == "" || h.Repo == "" {
			continue
		}
		roots[filepath.Join(cache, h.Org, h.Repo)] = true
		for branch, wt := range h.Branches {
			if self, found := recs.Self(h.Hopspace, h.Path, wt, branch); !found || self.Entry.OverrideDir == "" {
				use(legacyOverrideDir(h.Org, h.Repo, branch))
			}
		}
	}

	var out []OverrideDir
	for root := range roots {
		err := afero.Walk(fs, root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if info.IsDir() || info.Name() != overrideMetaFileName {
				return nil
			}
			dir := filepath.Dir(path)
			if used[state.ResolvePath(dir)] {
				return nil
			}
			var size int64
			for _, name := range []string{overrideFileName, overrideMetaFileName} {
				if fi, err := fs.Stat(filepath.Join(dir, name)); err == nil {
					size += fi.Size()
				}
			}
			out = append(out, OverrideDir{Path: dir, Root: root, Size: size})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// RemoveOverrideDir deletes the override and its metadata from d, then d
// and the directories above it, up to d.Root, that this leaves empty.
// Nothing else in d is touched.
func RemoveOverrideDir(fs afero.Fs, d OverrideDir) error {
	for _, name := range []string{overrideFileName, overrideMetaFileName} {
		if err := fs.Remove(filepath.Join(d.Path, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for dir := d.Path; isBelow(dir, d.Root); dir = filepath.Dir(dir) {
		if empty, err := afero.IsEmpty(fs, dir); err != nil || !empty {
			break
		}
		if err := fs.Remove(dir); err != nil {
			break
		}
	}
	return nil
}
