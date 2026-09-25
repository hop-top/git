package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// LegacyDepsStore is a deps store an earlier release wrote
// (legacyDepsStorePath) that is still on disk.
type LegacyDepsStore struct {
	Path string
	// Links are the symlinks found in worktrees that point into the store.
	Links []string
	Size  int64
}

// Linked reports whether any worktree still links into the store.
func (s LegacyDepsStore) Linked() bool { return len(s.Links) > 0 }

// FindLegacyDepsStores returns the legacy deps stores of hopspaces that
// exist on disk, sorted by path, each with the symlinks under worktrees
// that point into it.
//
// Hubs whose paths share a tail at the same length shared one legacy
// store, so the answer is only as good as the scope: hopspaces and
// worktrees must cover every hub recorded, not just one. Every worktree
// is walked in full (symlinks are not followed) since a link may sit
// anywhere below it. A worktree that cannot be walked completely makes
// the result an error: a store it might link into cannot be shown
// unlinked.
func FindLegacyDepsStores(fs afero.Fs, hopspaces, worktrees []string) ([]LegacyDepsStore, error) {
	current := make(map[string]bool, len(hopspaces))
	for _, hs := range hopspaces {
		current[state.ResolvePath(DepsStorePath(hs))] = true
	}

	byPath := map[string]*LegacyDepsStore{}
	for _, hs := range hopspaces {
		path := legacyDepsStorePath(hs)
		if path == "" || byPath[path] != nil || current[state.ResolvePath(path)] {
			continue
		}
		// A hop.json beside it makes its parent a hopspace, and the store
		// that hopspace's own; the slicing only happened to name it.
		if ok, _ := afero.Exists(fs, filepath.Join(filepath.Dir(path), "hop.json")); ok {
			continue
		}
		if ok, _ := afero.DirExists(fs, path); !ok {
			continue
		}
		byPath[path] = &LegacyDepsStore{Path: path}
	}
	if len(byPath) == 0 {
		return nil, nil
	}

	for _, wt := range uniquePaths(worktrees) {
		if err := collectLegacyLinks(fs, wt, byPath); err != nil {
			return nil, err
		}
	}

	stores := make([]LegacyDepsStore, 0, len(byPath))
	for _, s := range byPath {
		s.Size = dirSize(fs, s.Path)
		stores = append(stores, *s)
	}
	sort.Slice(stores, func(i, j int) bool { return stores[i].Path < stores[j].Path })
	return stores, nil
}

// RemoveLegacyDepsStore deletes a legacy store and the directories above
// it, up to the data home, that it leaves empty. It refuses a store with
// links; callers pass one FindLegacyDepsStores has just found unlinked.
func RemoveLegacyDepsStore(fs afero.Fs, store LegacyDepsStore) error {
	if store.Linked() {
		return fmt.Errorf("%s is still linked from %s", store.Path, store.Links[0])
	}
	if err := fs.RemoveAll(store.Path); err != nil {
		return err
	}
	dataHome := filepath.Clean(hop.GetGitHopDataHome())
	for dir := filepath.Dir(store.Path); isBelow(dir, dataHome); dir = filepath.Dir(dir) {
		if empty, err := afero.IsEmpty(fs, dir); err != nil || !empty {
			break
		}
		if err := fs.Remove(dir); err != nil {
			break
		}
	}
	return nil
}

// collectLegacyLinks walks root and records every symlink pointing into
// one of stores. A root that is gone has nothing to record.
func collectLegacyLinks(fs afero.Fs, root string, stores map[string]*LegacyDepsStore) error {
	dirs := make([]string, 0, len(stores))
	for path := range stores {
		dirs = append(dirs, path)
	}
	return collectLinks(fs, root, dirs, func(dir, link string) {
		stores[dir].Links = append(stores[dir].Links, link)
	})
}

// collectLinks walks root (symlinks not followed, .git skipped) and calls
// found(dir, link) for every symlink below it that points into one of
// dirs. A root that is gone has nothing to find; any other part of it
// that cannot be walked or read is an error, since a link there would go
// unseen.
func collectLinks(fs afero.Fs, root string, dirs []string, found func(dir, link string)) error {
	if _, err := lstat(fs, root); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	reader, ok := fs.(afero.LinkReader)
	if !ok {
		return fmt.Errorf("cannot read symlinks to check %s", root)
	}
	return afero.Walk(fs, root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("cannot check %s for links into old dependency installs: %w", path, err)
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := reader.ReadlinkIfPossible(path)
		if err != nil {
			return fmt.Errorf("cannot read link %s: %w", path, err)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		for _, dir := range dirs {
			if pointsInto(target, dir) {
				found(dir, path)
			}
		}
		return nil
	})
}

// pointsInto reports whether target is dir or below it, compared as
// written and with symlinks resolved, so a link spelled through another
// path to the same directory still counts.
func pointsInto(target, dir string) bool {
	return isWithin(filepath.Clean(target), filepath.Clean(dir)) ||
		isWithin(state.ResolvePath(target), state.ResolvePath(dir))
}

func isWithin(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

func isBelow(path, dir string) bool {
	return path != dir && isWithin(path, dir)
}

// uniquePaths returns paths resolved (state.ResolvePath), each once.
func uniquePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		key := state.ResolvePath(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		// Resolved, so a worktree path that is itself a symlink is
		// walked rather than read as one link.
		out = append(out, key)
	}
	return out
}

func dirSize(fs afero.Fs, path string) int64 {
	var size int64
	_ = afero.Walk(fs, path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
