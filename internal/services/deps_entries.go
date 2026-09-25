package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

// A worktree's DepsDir is a real directory holding one link per entry of
// the store install it uses (the per-entry layout), not a link to the
// install itself. Anything that empties the DepsDir (npm ci,
// rm -rf node_modules/*) then removes the worktree's links and nothing in
// the store.
//
// Each entry directly in the install is linked by name, with two
// exceptions whose children are linked one by one, so a package manager
// adding to them writes into the worktree: a scope (@scope/<package>) and
// .bin (.bin/<command>). Hidden files directly in the install (npm's
// .package-lock.json, yarn's .yarn-integrity or .yarn-state.yml) are
// copied, never linked: package managers rewrite them in place, which
// through a link would write into the store. Other hidden entries (tool
// caches such as .cache or .vite) are not linked: each worktree keeps its
// own. Packages in the store resolve their dependencies from the store:
// Node follows the links to their real paths.
//
// Only links into the hopspace's store (and the files git-hop copied) are
// ever replaced or removed from a DepsDir; anything else in it belongs to
// the worktree.

// EntryLinksMarker names the file git-hop writes into a DepsDir it linked
// entry by entry. It records the install the links point into and the
// hidden files copied from it.
const EntryLinksMarker = ".git-hop-links"

// entryLinksRecord is the content of EntryLinksMarker.
type entryLinksRecord struct {
	Install string   `json:"install"`
	Copied  []string `json:"copied,omitempty"`
}

// entryLink is a link the per-entry layout puts in a DepsDir.
type entryLink struct {
	// Rel is the link's path below the DepsDir: "a", "@scope/b", ".bin/c".
	Rel string
	// Target is what it points to in the install.
	Target string
}

// linksChildren reports whether the entry name, directly in an install,
// has its children linked one by one instead of being linked itself.
func linksChildren(name string) bool {
	return name == ".bin" || strings.HasPrefix(name, "@")
}

// installEntries returns the links the per-entry layout of install has,
// and the names of the hidden files it copies from it.
func installEntries(fs afero.Fs, install string) ([]entryLink, []string, error) {
	infos, err := afero.ReadDir(fs, install)
	if err != nil {
		return nil, nil, err
	}
	var links []entryLink
	var copies []string
	for _, info := range infos {
		name := info.Name()
		switch {
		case linksChildren(name) && info.IsDir():
			children, err := afero.ReadDir(fs, filepath.Join(install, name))
			if err != nil {
				return nil, nil, err
			}
			for _, child := range children {
				rel := filepath.Join(name, child.Name())
				links = append(links, entryLink{Rel: rel, Target: filepath.Join(install, rel)})
			}
		case strings.HasPrefix(name, "."):
			if info.Mode().IsRegular() {
				copies = append(copies, name)
			}
		default:
			links = append(links, entryLink{Rel: name, Target: filepath.Join(install, name)})
		}
	}
	return links, copies, nil
}

// layEntryLinks lays depsDir out entry by entry for install, creating it
// if needed. The links into the store and the copied files already there
// are replaced; anything else (a tool's cache, a package the worktree
// installed itself, a link elsewhere) is left alone, and a name it takes
// is not linked. A dangling link is replaced. The record is written last.
func (m *DepsManager) layEntryLinks(install, depsDir string) error {
	links, copies, err := installEntries(m.fs, install)
	if err != nil {
		return fmt.Errorf("failed to list install %s: %w", install, err)
	}
	if err := m.fs.MkdirAll(depsDir, 0o755); err != nil {
		return err
	}
	if err := m.clearEntryLinks(depsDir); err != nil {
		return err
	}
	for _, link := range links {
		path := filepath.Join(depsDir, link.Rel)
		if err := m.fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if free, err := m.freeForLink(path); err != nil {
			return err
		} else if !free {
			continue
		}
		if err := m.createSymlink(link.Target, path); err != nil {
			return fmt.Errorf("failed to link %s: %w", path, err)
		}
	}
	copied := []string{}
	for _, name := range copies {
		path := filepath.Join(depsDir, name)
		if info, err := lstat(m.fs, path); err == nil && !info.Mode().IsRegular() {
			continue
		}
		src := filepath.Join(install, name)
		info, err := m.fs.Stat(src)
		if err != nil {
			return err
		}
		if err := copyFile(m.fs, src, path, info.Mode().Perm()); err != nil {
			return fmt.Errorf("failed to copy %s: %w", src, err)
		}
		copied = append(copied, name)
	}
	return writeEntryLinksRecord(m.fs, depsDir, entryLinksRecord{Install: install, Copied: copied})
}

// freeForLink reports whether a link can be made at path: nothing is
// there, or a dangling link, which it removes.
func (m *DepsManager) freeForLink(path string) (bool, error) {
	info, err := lstat(m.fs, path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	if ok, _ := afero.Exists(m.fs, path); ok {
		return false, nil
	}
	return true, m.fs.Remove(path)
}

// clearEntryLinks removes from depsDir what the per-entry layout put
// there: links into the store (directly in it, or in a scope or .bin,
// which go too once empty), the files it copied, and its record.
// Symlinks are removed, never followed.
func (m *DepsManager) clearEntryLinks(depsDir string) error {
	record, _ := readEntryLinksRecord(m.fs, depsDir)
	for _, name := range record.Copied {
		path := filepath.Join(depsDir, filepath.Base(name))
		if info, err := lstat(m.fs, path); err == nil && info.Mode().IsRegular() {
			if err := m.fs.Remove(path); err != nil {
				return err
			}
		}
	}
	if err := m.fs.Remove(filepath.Join(depsDir, EntryLinksMarker)); err != nil && !os.IsNotExist(err) {
		return err
	}
	infos, err := afero.ReadDir(m.fs, depsDir)
	if err != nil {
		return err
	}
	for _, info := range infos {
		path := filepath.Join(depsDir, info.Name())
		if linksChildren(info.Name()) && info.IsDir() {
			if err := m.removeStoreLinksIn(path); err != nil {
				return err
			}
			if empty, err := afero.IsEmpty(m.fs, path); err == nil && empty {
				if err := m.fs.Remove(path); err != nil {
					return err
				}
			}
			continue
		}
		if m.isStoreLink(path, info) {
			if err := m.fs.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeStoreLinksIn removes the links into the store directly in dir.
func (m *DepsManager) removeStoreLinksIn(dir string) error {
	infos, err := afero.ReadDir(m.fs, dir)
	if err != nil {
		return err
	}
	for _, info := range infos {
		path := filepath.Join(dir, info.Name())
		if m.isStoreLink(path, info) {
			if err := m.fs.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

// isStoreLink reports whether path (lstat info) is a link into the store.
func (m *DepsManager) isStoreLink(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, ok := readSymlink(m.fs, path)
	return ok && filepath.IsAbs(target) && isBelow(filepath.Clean(target), DepsStorePath(m.RepoPath))
}

// entryLinksInstall returns the store install depsDir, a real directory,
// is linked into entry by entry: the one its record names, else the one
// its first link into the store points into. ok is false when depsDir is
// not in the per-entry layout.
func (m *DepsManager) entryLinksInstall(depsDir string, pm PackageManager) (string, bool) {
	keys := entryLinkedKeys(m.fs, m.RepoPath, depsDir, pm)
	if len(keys) == 0 {
		return "", false
	}
	return m.getDepsPath(keys[0]), true
}

// entryLinkedKeys returns the keys of the installs in repoPath's store
// that depsDir, a real directory, links into entry by entry: the one its
// record names first, then any other a link in it points into. A store
// install stays in use while any worktree links into it this way.
func entryLinkedKeys(fs afero.Fs, repoPath, depsDir string, pm PackageManager) []string {
	var keys []string
	seen := map[string]bool{}
	add := func(install string) {
		if key, ok := depsKeyOf(repoPath, install, pm); ok && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	if record, ok := readEntryLinksRecord(fs, depsDir); ok {
		add(record.Install)
	}
	store := DepsStorePath(repoPath)
	visit := func(dir string, depth int) []os.FileInfo {
		infos, _ := afero.ReadDir(fs, dir)
		for _, info := range infos {
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, ok := readSymlink(fs, filepath.Join(dir, info.Name()))
			if !ok || !filepath.IsAbs(target) || !isBelow(filepath.Clean(target), store) {
				continue
			}
			install := filepath.Clean(target)
			for range depth {
				install = filepath.Dir(install)
			}
			add(install)
		}
		return infos
	}
	for _, info := range visit(depsDir, 1) {
		if linksChildren(info.Name()) && info.IsDir() {
			visit(filepath.Join(depsDir, info.Name()), 2)
		}
	}
	return keys
}

// entryLinksCheck is what checkEntryLinks finds of the links a DepsDir
// should have into an install.
type entryLinksCheck struct {
	// Shadowed are the entries where something real stands in for a
	// link: the package manager installed into the worktree (npm install
	// replaces every link with a package directory).
	Shadowed []string
	// Missing are the entries with no link, a dangling one, or one into
	// another place in the store.
	Missing []string
}

// checkEntryLinks compares depsDir with the per-entry layout of install.
// A link to somewhere outside the store (npm link) is the worktree's own
// and counts as present.
func (m *DepsManager) checkEntryLinks(install, depsDir string) (entryLinksCheck, error) {
	var check entryLinksCheck
	links, _, err := installEntries(m.fs, install)
	if err != nil {
		return check, err
	}
	for _, link := range links {
		path := filepath.Join(depsDir, link.Rel)
		info, err := lstat(m.fs, path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			check.Missing = append(check.Missing, link.Rel)
		case err != nil:
			return check, err
		case info.Mode()&os.ModeSymlink == 0:
			check.Shadowed = append(check.Shadowed, link.Rel)
		default:
			target, _ := readSymlink(m.fs, path)
			live, _ := afero.Exists(m.fs, path)
			if !live || (m.isStoreLink(path, info) && filepath.Clean(target) != link.Target) {
				check.Missing = append(check.Missing, link.Rel)
			}
		}
	}
	sort.Strings(check.Missing)
	sort.Strings(check.Shadowed)
	return check, nil
}

// readEntryLinksRecord reads the record of the per-entry layout in depsDir.
func readEntryLinksRecord(fs afero.Fs, depsDir string) (entryLinksRecord, bool) {
	var record entryLinksRecord
	data, err := afero.ReadFile(fs, filepath.Join(depsDir, EntryLinksMarker))
	if err != nil || json.Unmarshal(data, &record) != nil || record.Install == "" {
		return entryLinksRecord{}, false
	}
	return record, true
}

func writeEntryLinksRecord(fs afero.Fs, depsDir string, record entryLinksRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return afero.WriteFile(fs, filepath.Join(depsDir, EntryLinksMarker), append(data, '\n'), 0o644)
}
