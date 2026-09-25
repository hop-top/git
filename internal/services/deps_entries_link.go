package services

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// depsDirKind is what a worktree's DepsDir holds.
type depsDirKind int

const (
	// depsDirNone: nothing.
	depsDirNone depsDirKind = iota
	// depsDirLink: a link to a store install, as earlier releases made.
	depsDirLink
	// depsDirEntries: links into a store install, entry by entry.
	depsDirEntries
	// depsDirLocal: a local install (LocalInstallMarker, or one that
	// could not be shared anyway), which the package manager updates in
	// place.
	depsDirLocal
	// depsDirFolder: anything else, such as an install the package
	// manager made in the worktree (npm install replaces the links of the
	// per-entry layout with package directories).
	depsDirFolder
)

// depsDirState is what readDepsDir found at a worktree's DepsDir.
type depsDirState struct {
	kind depsDirKind
	// install is the store install a link or the per-entry layout points
	// into.
	install string
}

// readDepsDir classifies the DepsDir of pm at depsDir in worktreePath.
func (m *DepsManager) readDepsDir(depsDir, worktreePath string, pm PackageManager) (depsDirState, error) {
	if target, ok := readSymlink(m.fs, depsDir); ok {
		return depsDirState{kind: depsDirLink, install: target}, nil
	}
	info, err := lstat(m.fs, depsDir)
	if errors.Is(err, os.ErrNotExist) {
		return depsDirState{kind: depsDirNone}, nil
	}
	if err != nil {
		return depsDirState{}, fmt.Errorf("failed to check worktree deps path: %w", err)
	}
	if !info.IsDir() {
		return depsDirState{kind: depsDirFolder}, nil
	}
	if _, ok := readLocalMarker(m.fs, depsDir); ok {
		return depsDirState{kind: depsDirLocal}, nil
	}
	if install, ok := m.entryLinksInstall(depsDir, pm); ok {
		if check, err := m.checkEntryLinks(install, depsDir); err == nil && len(check.Shadowed) > 0 {
			return depsDirState{kind: depsDirFolder, install: install}, nil
		}
		return depsDirState{kind: depsDirEntries, install: install}, nil
	}
	if reason, err := localReasonOf(m.fs, depsDir, worktreePath); err == nil && reason != "" {
		return depsDirState{kind: depsDirLocal}, nil
	}
	return depsDirState{kind: depsDirFolder}, nil
}

// linkEntriesInto lays depsDir out entry by entry for the store install
// at depsPath, from whatever cur found there. A layout already complete
// for it is left alone; one for another install, or with links missing,
// is relinked in place, keeping the worktree's own entries (tool caches).
// A link is replaced, and put back if laying fails. A local install or
// folder goes to the trash first.
func (m *DepsManager) linkEntriesInto(depsPath, depsDir string, cur depsDirState) error {
	switch cur.kind {
	case depsDirEntries:
		if cur.install == depsPath {
			if check, err := m.checkEntryLinks(depsPath, depsDir); err == nil && len(check.Missing) == 0 {
				return nil
			}
		}
		return m.layEntryLinks(depsPath, depsDir)
	case depsDirLink:
		if err := m.fs.Remove(depsDir); err != nil {
			return fmt.Errorf("failed to remove link %s: %w", depsDir, err)
		}
		if err := m.layEntryLinks(depsPath, depsDir); err != nil {
			return m.restoreAfterFailure(fmt.Errorf("failed to link deps: %w", err), depsDir, cur)
		}
		return nil
	case depsDirLocal:
		if err := m.cleanWorktreeDepsPath(depsDir); err != nil {
			return err
		}
	case depsDirFolder:
		if err := m.takeDownDepsDir(depsDir, cur); err != nil {
			return err
		}
	}
	return m.layEntryLinks(depsPath, depsDir)
}

// takeDownDepsDir clears depsDir before an install runs there, since the
// install command writes ./<DepsDir> of the worktree: through a link it
// would write into, or wipe, the install the link points to. A link is
// removed. Links into the store are only pointers and are removed too,
// never trashed; what is left of a per-entry layout or folder (a tool's
// cache, packages the worktree installed) goes to the trash. A local
// install stays: the package manager updates it in place.
func (m *DepsManager) takeDownDepsDir(depsDir string, cur depsDirState) error {
	switch cur.kind {
	case depsDirLink:
		if err := m.fs.Remove(depsDir); err != nil {
			return fmt.Errorf("failed to remove stale symlink: %w", err)
		}
	case depsDirEntries, depsDirFolder:
		if isRealDir(m.fs, depsDir) {
			if err := m.clearEntryLinks(depsDir); err != nil {
				return fmt.Errorf("failed to remove links in %s: %w", depsDir, err)
			}
			if empty, err := afero.IsEmpty(m.fs, depsDir); err == nil && empty {
				return m.fs.Remove(depsDir)
			}
		}
		return m.cleanWorktreeDepsPath(depsDir)
	}
	return nil
}

// restoreAfterFailure puts back what linkDeps took down at depsDir (cur)
// before a step that then failed with cause, and returns cause: the link,
// or the per-entry layout for the install it pointed into, if that is
// still there. What the failed step left at depsDir goes first. A failure
// to restore is reported with cause, but not wrapping it, so callers
// matching cause (ErrBinaryNotFound) do not mistake the pair for a
// skippable failure.
func (m *DepsManager) restoreAfterFailure(cause error, depsDir string, cur depsDirState) error {
	switch cur.kind {
	case depsDirLink:
		return m.relinkAfterFailure(cause, depsDir, cur.install, true)
	case depsDirEntries:
		err := m.fs.RemoveAll(depsDir)
		if ok, _ := afero.DirExists(m.fs, cur.install); err == nil && ok {
			err = m.layEntryLinks(cur.install, depsDir)
		}
		if err != nil {
			return fmt.Errorf("%v; and could not restore the links in %s into %s: %w", cause, depsDir, cur.install, err)
		}
	}
	return cause
}
