package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// linkDeps lays worktreePath/<DepsDir> out entry by entry for the store's
// install for hash (GetDepsKey; see deps_entries.go), installing it first
// when the store has none intact and shareable. An install that cannot be
// shared (localReasonOf) stays in the worktree as a local install instead,
// marked with hash; one already marked with hash is left as it is.
//
// An install in the layout earlier releases wrote (flatDepsKey) is never
// reused or written: Node cannot resolve its packages from one another.
// A worktree still linking one is relinked here, and only here, once the
// new install is in place; the old install is left as it is.
//
// The install command writes ./<DepsDir> of the worktree, so whatever is
// there goes first (takeDownDepsDir): a link would let the install write
// into, or wipe, the install it points to. A local install stays: the
// package manager updates it in place. If the install or the links then
// fail, what was taken down is put back (restoreAfterFailure), so a failed
// attempt leaves the worktree linked as it was.
func (m *DepsManager) linkDeps(worktreePath, branch string, pm PackageManager, hash, lockfilePath string) error {
	depsKey := pm.GetDepsKey(hash)
	depsPath := m.getDepsPath(depsKey)
	depsDir := filepath.Join(worktreePath, pm.DepsDir)

	cur, err := m.readDepsDir(depsDir, worktreePath, pm)
	if err != nil {
		return err
	}
	if marked, ok := readLocalMarker(m.fs, depsDir); cur.kind == depsDirLocal && ok && marked == hash {
		return nil
	}

	// An install missing entries it was made with (emptied through a
	// worktree's link, or crashed mid-install) is treated as missing and
	// reinstalled, which repairs every worktree linked to it. One that
	// must not be shared is never linked again.
	shareable, err := m.installIntact(depsPath)
	if err != nil {
		return fmt.Errorf("failed to check deps existence: %w", err)
	}
	if shareable {
		reason, err := m.storeLocalReason(depsPath)
		if err != nil {
			return fmt.Errorf("failed to check %s for links: %w", depsPath, err)
		}
		shareable = reason == ""
	}

	if shareable {
		// A single link to this install, as earlier releases made, is
		// converted here: npm ci through it would empty the install for
		// every worktree. Only the link is replaced; the install is read.
		if err := m.linkEntriesInto(depsPath, depsDir, cur); err != nil {
			return err
		}
		m.Registry.AddUsage(depsKey, branch)
		return nil
	}

	if err := m.takeDownDepsDir(depsDir, cur); err != nil {
		return err
	}
	stayed, err := m.installDeps(depsKey, worktreePath, pm, hash)
	if err != nil {
		return m.restoreAfterFailure(fmt.Errorf("failed to install deps: %w", err), depsDir, cur)
	}
	if stayed {
		return nil
	}
	if err := m.writeInstallManifest(depsPath); err != nil {
		return m.restoreAfterFailure(err, depsDir, cur)
	}
	m.Registry.UpdateEntryMetadata(depsKey, hash, filepath.Base(lockfilePath))
	if err := m.layEntryLinks(depsPath, depsDir); err != nil {
		return m.restoreAfterFailure(fmt.Errorf("failed to link deps: %w", err), depsDir, cur)
	}
	m.Registry.AddUsage(depsKey, branch)
	return nil
}

// relinkAfterFailure puts back the link linkDeps took down before a step
// that then failed with cause, and returns cause. What the failed install
// left at symlinkPath goes first: cleanWorktreeDepsPath had cleared the
// path, so nothing else can be there. A failure to put the link back is
// reported with cause, but not wrapping it, so callers matching cause
// (ErrBinaryNotFound) do not mistake the pair for a skippable failure.
func (m *DepsManager) relinkAfterFailure(cause error, symlinkPath, previous string, linked bool) error {
	if !linked {
		return cause
	}
	err := m.fs.RemoveAll(symlinkPath)
	if err == nil {
		err = m.createSymlink(previous, symlinkPath)
	}
	if err != nil {
		return fmt.Errorf("%v; and could not restore %s -> %s: %w", cause, symlinkPath, previous, err)
	}
	return cause
}

// TargetName names the install the issue's symlink points to the way the
// store names installs: its key, e.g. "abc123/node_modules" in the current
// layout or "node_modules.abc123" in the old one.
func (i Issue) TargetName() string {
	depsDir := filepath.Clean(i.PM.DepsDir)
	if hashDir, ok := strings.CutSuffix(i.SymlinkTarget, string(filepath.Separator)+depsDir); ok && i.PM.DepsDir != "" {
		return filepath.ToSlash(filepath.Join(filepath.Base(hashDir), depsDir))
	}
	return filepath.Base(i.SymlinkTarget)
}

// flatDepsPaths returns where an install for hash in the layout earlier
// releases wrote may be: this store, then the legacy store when the
// hopspace has one (legacyDepsStorePath).
func (m *DepsManager) flatDepsPaths(pm PackageManager, hash string) []string {
	key := pm.flatDepsKey(hash)
	paths := []string{m.getDepsPath(key)}
	if legacy := legacyDepsStorePath(m.RepoPath); legacy != "" {
		paths = append(paths, filepath.Join(legacy, key))
	}
	return paths
}

// SetLinkScope gives the worktrees of every hub git-hop knows of, which
// GarbageCollect checks for links before it removes an old-layout install
// (isFlatDepsKey). Until it is set, old-layout installs are never
// collected: hubs whose paths end the same way could share one with the
// hub collecting, so its own worktrees alone cannot show one unused.
func (m *DepsManager) SetLinkScope(worktrees []string) {
	m.linkScope = append([]string{}, worktrees...)
	m.linkScopeSet = true
}

// orphaned returns the keys of this store's installs no worktree links to:
// those the registry, rebuilt from worktrees, has no user for, less the
// old-layout ones a link found by a walk of worktrees and the link scope
// still points into.
func (m *DepsManager) orphaned(worktrees map[string]string) ([]string, error) {
	if err := m.Registry.RebuildFromWorktrees(m.fs, worktrees, m.PackageManagers, m.RepoPath); err != nil {
		return nil, fmt.Errorf("failed to rebuild registry: %w", err)
	}

	var orphaned, flat []string
	for _, key := range m.Registry.GetOrphaned() {
		if isFlatDepsKey(key) {
			flat = append(flat, key)
			continue
		}
		orphaned = append(orphaned, key)
	}
	if len(flat) == 0 || !m.linkScopeSet {
		return orphaned, nil
	}

	linked := make(map[string]bool, len(flat))
	paths := make([]string, 0, len(flat))
	for _, key := range flat {
		paths = append(paths, m.getDepsPath(key))
	}
	roots := append([]string{}, m.linkScope...)
	for _, wt := range worktrees {
		roots = append(roots, wt)
	}
	for _, root := range uniquePaths(roots) {
		err := collectLinks(m.fs, root, paths, func(path, _ string) { linked[path] = true })
		if err != nil {
			return nil, err
		}
	}
	for _, key := range flat {
		if !linked[m.getDepsPath(key)] {
			orphaned = append(orphaned, key)
		}
	}
	return orphaned, nil
}

// removeInstall deletes the install depsKey names, and for a key in the
// current layout the hash directory above it once that is empty.
func (m *DepsManager) removeInstall(depsKey string) error {
	path := m.getDepsPath(depsKey)
	if err := m.fs.RemoveAll(path); err != nil {
		return err
	}
	if isFlatDepsKey(depsKey) {
		return nil
	}
	if err := m.fs.Remove(InstallManifestPath(path)); err != nil && !os.IsNotExist(err) {
		return err
	}
	store := DepsStorePath(m.RepoPath)
	for dir := filepath.Dir(path); isBelow(dir, store); dir = filepath.Dir(dir) {
		empty, err := afero.IsEmpty(m.fs, dir)
		if err != nil || !empty {
			break
		}
		if err := m.fs.Remove(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
