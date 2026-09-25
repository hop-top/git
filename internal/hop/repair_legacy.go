package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/filelock"
)

// LegacyCleanup describes one step taken (or, under dryRun, proposed) by
// CleanupLegacyRepairDir. Kind is one of:
//
//	"moved"    Path (a legacy backup dir) relocated to Dest
//	"removed"  Path unlinked or rmdir'ed
type LegacyCleanup struct {
	Kind string
	Path string
	Dest string
}

// CleanupLegacyRepairDir retires the hub-local <hub>/.hop/ footprint an
// earlier release left behind, without ever touching anything that is
// not git-hop's own:
//
//  1. Every remaining <hub>/.hop/backups/repair-* snapshot (i.e. one that
//     survived retention GC) is moved to the state-dir backup root, so it
//     stays restorable by the same id and stops pinning .hop/ in place.
//  2. A stale <hub>/.hop/repair.lock is unlinked, but only when no
//     process holds it.
//  3. <hub>/.hop/backups is removed when empty, then <hub>/.hop itself is
//     removed when — and only when — it is completely empty. A .hop/
//     holding anything else (another tool's state) is left alone.
//
// Under dryRun nothing is written; the returned steps describe what a
// real run would do. Errors on one step never abort the others: the
// function returns what it managed to do plus the first error seen.
func CleanupLegacyRepairDir(fs afero.Fs, hubPath string, dryRun bool) ([]LegacyCleanup, error) {
	var steps []LegacyCleanup
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	legacyDir := LegacyRepairDir(hubPath)
	if exists, _ := afero.DirExists(fs, legacyDir); !exists {
		return nil, nil
	}

	// 1. Migrate surviving snapshots.
	legacyRoot := LegacyRepairBackupRoot(hubPath)
	if entries, err := afero.ReadDir(fs, legacyRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), repairBackupPrefix) {
				continue
			}
			src := filepath.Join(legacyRoot, e.Name())
			dst := filepath.Join(RepairBackupRoot(hubPath), e.Name())
			if dryRun {
				steps = append(steps, LegacyCleanup{Kind: "moved", Path: src, Dest: dst})
				continue
			}
			if err := moveBackupDir(fs, src, dst); err != nil {
				note(fmt.Errorf("move %s: %w", src, err))
				continue
			}
			steps = append(steps, LegacyCleanup{Kind: "moved", Path: src, Dest: dst})
		}
	}

	// 2. Stale lock.
	lock := LegacyRepairLockPath(hubPath)
	if exists, _ := afero.Exists(fs, lock); exists && !filelock.Held(lock) {
		if dryRun {
			steps = append(steps, LegacyCleanup{Kind: "removed", Path: lock})
		} else if err := fs.Remove(lock); err != nil {
			note(fmt.Errorf("remove %s: %w", lock, err))
		} else {
			steps = append(steps, LegacyCleanup{Kind: "removed", Path: lock})
		}
	}

	// 3. Empty directories, innermost first.
	for _, dir := range []string{legacyRoot, legacyDir} {
		if !wouldBeEmpty(fs, dir, steps) {
			continue
		}
		if dryRun {
			steps = append(steps, LegacyCleanup{Kind: "removed", Path: dir})
			continue
		}
		if err := fs.Remove(dir); err != nil {
			note(fmt.Errorf("rmdir %s: %w", dir, err))
			continue
		}
		steps = append(steps, LegacyCleanup{Kind: "removed", Path: dir})
	}
	return steps, firstErr
}

// wouldBeEmpty reports whether dir exists and holds nothing beyond the
// entries that earlier steps already removed or moved (which matters
// under dryRun, where those entries are still physically present).
func wouldBeEmpty(fs afero.Fs, dir string, done []LegacyCleanup) bool {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return false
	}
	gone := make(map[string]struct{}, len(done))
	for _, s := range done {
		gone[filepath.Clean(s.Path)] = struct{}{}
	}
	for _, e := range entries {
		if _, ok := gone[filepath.Join(dir, e.Name())]; !ok {
			return false
		}
	}
	return true
}

// moveBackupDir relocates one snapshot directory. Copy-then-verify-then-
// remove rather than rename: the state dir may sit on another filesystem,
// and a snapshot must never be lost half-way. A destination that already
// holds a readable manifest is treated as already migrated.
func moveBackupDir(fs afero.Fs, src, dst string) error {
	if _, err := readManifest(fs, src); err != nil {
		return fmt.Errorf("unreadable manifest, left in place: %w", err)
	}
	if _, err := readManifest(fs, dst); err == nil {
		return fs.RemoveAll(src)
	}
	if _, err := copyTreeWithSum(fs, src, dst); err != nil {
		_ = fs.RemoveAll(dst)
		return err
	}
	if _, err := readManifest(fs, dst); err != nil {
		_ = fs.RemoveAll(dst)
		return fmt.Errorf("verify copy: %w", err)
	}
	return fs.RemoveAll(src)
}
