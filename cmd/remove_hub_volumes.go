package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
)

// orphanedVolumesDir is the data-home directory the volume data of a
// removed hub or hopspace is moved to, under the repository's layout
// path: <data>/orphaned-volumes/<org>/<repo>/<name>-<UTC>.
const orphanedVolumesDir = "orphaned-volumes"

// orphanedVolumesStamp is the UTC second in a move-aside directory's name.
const orphanedVolumesStamp = "20060102T150405Z"

// Values of removeRecord.Volumes.
const (
	volumesKept    = "kept"
	volumesDeleted = "deleted"
)

// volumeDataIn returns the volume data directories inside root, the tree
// a removal deletes, in root's spelling: the volumes directory of the
// hopspace at hopspacePath, the basePath its volumes.json names and every
// directory that records, when inside root. Only a directory holding
// something counts, and one inside another is left to it. root itself
// never counts: keeping it would keep everything.
func volumeDataIn(fsys afero.Fs, hopspacePath, root string) []string {
	candidates := []string{filepath.Join(hopspacePath, "volumes")}
	if cfg, err := config.NewLoader(fsys).LoadVolumesConfig(hopspacePath); err == nil {
		candidates = append(candidates, cfg.BasePath)
		for _, b := range cfg.Branches {
			for _, dir := range b.Volumes {
				candidates = append(candidates, dir)
			}
		}
	}
	var found []string
	for _, c := range candidates {
		p, ok := below(c, root)
		if !ok || !holdsData(fsys, p) {
			continue
		}
		found = append(found, p)
	}
	return outermost(found)
}

// below returns path spelled under root when it lies below root, compared
// resolved (state.ResolvePath).
func below(path, root string) (string, bool) {
	if path == "" {
		return "", false
	}
	rel, err := filepath.Rel(state.ResolvePath(root), state.ResolvePath(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(root, rel), true
}

// holdsData reports whether the directory at path holds any file. One
// that cannot be read through counts: what it holds is unknown.
func holdsData(fsys afero.Fs, path string) bool {
	info, err := lstat(fsys, path)
	if err != nil || !info.IsDir() {
		return false
	}
	errFound := errors.New("found")
	err = afero.Walk(fsys, path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return errFound
		}
		if !info.IsDir() {
			return errFound
		}
		return nil
	})
	return err != nil
}

// outermost drops the paths below another of paths, and duplicates, and
// sorts the rest.
func outermost(paths []string) []string {
	sort.Strings(paths)
	var out []string
	for _, p := range paths {
		if len(out) > 0 && isWithinPath(p, out[len(out)-1]) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// isWithinPath reports whether path is dir or below it, lexically.
func isWithinPath(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// removeAllExcept removes root and everything below it but the paths in
// keep and the directories leading to them. A symlink is removed, never
// followed.
func removeAllExcept(fsys afero.Fs, root string, keep []string) error {
	holds := false
	for _, k := range keep {
		if isWithinPath(root, k) {
			return nil
		}
		if isWithinPath(k, root) {
			holds = true
		}
	}
	if !holds {
		return fsys.RemoveAll(root)
	}
	if info, err := lstat(fsys, root); err != nil || !info.IsDir() {
		return fsys.RemoveAll(root)
	}
	entries, err := afero.ReadDir(fsys, root)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if err := removeAllExcept(fsys, filepath.Join(root, e.Name()), keep); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// volumeMove is the volume data of a tree a removal deletes, and what the
// removal does with it.
type volumeMove struct {
	// root is the tree removed; paths the volume data directories in it
	// (volumeDataIn).
	root  string
	paths []string
	// dest is the directory the data would be moved to, less its
	// "-<UTC>" suffix: <data>/orphaned-volumes/<layout>/<name>.
	dest string
	// delete is --delete-volumes: the data goes with the tree.
	delete bool
}

// newVolumeMove finds the volume data in root, the tree removed, whose
// hopspace is hopspacePath. The data would be moved to a directory named
// name under the repository ref's orphaned-volumes directory.
func newVolumeMove(fsys afero.Fs, hopspacePath, root string, ref hop.RepoRef, name string, deleteVolumes bool) volumeMove {
	return volumeMove{
		root:   root,
		paths:  volumeDataIn(fsys, hopspacePath, root),
		dest:   filepath.Join(hop.GetHopspacePath(filepath.Join(hop.GetGitHopDataHome(), orphanedVolumesDir), ref), name),
		delete: deleteVolumes,
	}
}

// state is the removal's record value: kept or deleted, or empty when
// root holds no volume data.
func (v volumeMove) state() string {
	switch {
	case len(v.paths) == 0:
		return ""
	case v.delete:
		return volumesDeleted
	default:
		return volumesKept
	}
}

// preview says what the removal of root would do with its volume data.
func (v volumeMove) preview() {
	for _, p := range v.paths {
		if v.delete {
			output.Info("[dry-run] Would delete volume data at %s (--delete-volumes)", p)
		} else {
			output.Info("[dry-run] Would keep volume data at %s, moved to %s-<UTC>", p, v.dest)
		}
	}
}

// moveAside moves the volume data out of root, unless it goes with it, to
// a new directory: dest plus the UTC second (and "-<n>" when that is
// taken; nothing is overwritten), keeping each directory's path relative
// to root. A directory it cannot move by rename (another filesystem)
// stays where it is, with a warning, and the removal leaves it. It
// returns where the data now is, and what the removal must leave.
func (v volumeMove) moveAside(fsys afero.Fs, now time.Time) (kept, leave []string) {
	if v.delete || len(v.paths) == 0 {
		return nil, nil
	}
	dir, err := newOrphanedVolumesDir(fsys, v.dest, now)
	if err != nil {
		output.Warn("Cannot create %s, leaving the volume data in place: %v", filepath.Dir(v.dest), err)
		return v.paths, v.paths
	}
	for _, p := range v.paths {
		rel, _ := filepath.Rel(v.root, p)
		to := filepath.Join(dir, rel)
		err := fsys.MkdirAll(filepath.Dir(to), 0o755)
		if err == nil {
			err = fsys.Rename(p, to)
		}
		if err != nil {
			output.Warn("Cannot move volume data %s to %s, leaving it in place: %v", p, to, err)
			kept = append(kept, p)
			leave = append(leave, p)
			continue
		}
		output.Info("Moved volume data %s to %s", p, to)
		kept = append(kept, to)
	}
	if len(leave) == len(v.paths) {
		_ = removeEmptyTree(fsys, dir)
	}
	return kept, leave
}

// newOrphanedVolumesDir creates a directory of its own at base plus the
// UTC second now names, or the next free "-<n>" after it. Mkdir fails
// when the directory exists, so two runs never share one.
func newOrphanedVolumesDir(fsys afero.Fs, base string, now time.Time) (string, error) {
	if err := fsys.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		return "", err
	}
	stamp := base + "-" + now.UTC().Format(orphanedVolumesStamp)
	for n := 0; n < 10000; n++ {
		dir := stamp
		if n > 0 {
			dir = fmt.Sprintf("%s-%d", stamp, n)
		}
		err := fsys.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, fs.ErrExist) && !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free directory name for %s", stamp)
}

// removeEmptyTree removes dir when it holds only empty directories.
func removeEmptyTree(fsys afero.Fs, dir string) error {
	if holdsData(fsys, dir) {
		return nil
	}
	return fsys.RemoveAll(dir)
}

// hintKeptVolumes tells where kept volume data is, and how to delete it.
func hintKeptVolumes(kept []string) {
	if len(kept) == 0 {
		return
	}
	output.Hint("kept the volume data at:\n  %s\ndelete it by hand once it is no longer needed; with --delete-volumes,\n'git hop remove' deletes it along with the hub", strings.Join(kept, "\n  "))
}

// hubVolumesIn is the directory of the hub at hubPath's volumes in the
// shared hopspace at hopspacePath: <basePath>/<hub key>, when it holds
// anything. No other hub writes there (services.HubKey).
func hubVolumesIn(fsys afero.Fs, hopspacePath, hubPath string) string {
	base := filepath.Join(hopspacePath, "volumes")
	if cfg, err := config.NewLoader(fsys).LoadVolumesConfig(hopspacePath); err == nil && cfg.BasePath != "" {
		base = cfg.BasePath
	}
	dir := filepath.Join(base, services.HubKey(hubPath))
	if !holdsData(fsys, dir) {
		return ""
	}
	return dir
}
