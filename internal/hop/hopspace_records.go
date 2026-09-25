package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// How a hopspace's hop.json keys its worktree records (branches):
//
//   - A hub's own hopspace (the default) is the hub's hop.json, whose
//     branches member the hub keys by branch; its records share those
//     entries, so they are keyed by branch too.
//   - A hopspace several --global hubs share keys each record by the
//     worktree's path (state.WorktreeKey) and names the branch and hub in
//     it, since every hub can have a worktree of the same branch. An
//     entry keyed by a branch there is either one an earlier release
//     wrote (migrateSharedEntries moves it to its path) or branch-level
//     settings with no worktree (packageManagers).
//
// ports.json and volumes.json key the same way: every record of a
// hopspace, in any of its files, is keyed by HopspaceKey.

// HopspaceKey is the key the worktree at worktreePath, on branch, of the
// hub at hubPath is recorded under in the hopspace at hopspacePath, in
// its hop.json, ports.json and volumes.json: the branch in a hub's own
// hopspace, where a branch has one worktree, and the worktree's path in
// a hopspace several hubs share (SharedHopspace), where each hub has its
// own.
func HopspaceKey(hopspacePath, hubPath, worktreePath, branch string) string {
	if !SharedHopspace(hopspacePath, hubPath) {
		return branch
	}
	return state.WorktreeKey(worktreePath)
}

// SharedHopspace reports whether the hopspace at hopspacePath is not the
// hub at hubPath itself but a --global hub's data-home hopspace, which
// every --global hub of the repository shares, so its records are keyed
// by worktree path (HopspaceKey). An unknown hub ("") counts as the
// hopspace's own.
func SharedHopspace(hopspacePath, hubPath string) bool {
	return hubPath != "" && !state.SamePath(hopspacePath, hubPath)
}

// Entry returns the hopspace's record of the worktree at worktreePath, on
// branch, of the hub at hubPath.
func (h *Hopspace) Entry(hubPath, branch, worktreePath string) (config.HopspaceBranch, bool) {
	if h == nil || h.Config == nil {
		return config.HopspaceBranch{}, false
	}
	_, e, ok := findEntry(h.Config, h.Path, hubPath, branch, worktreePath)
	return e, ok
}

// findEntry returns the key and entry of cfg recording the worktree at
// worktreePath, on branch, of the hub at hubPath. In a shared hopspace
// that is the entry under the worktree's key, else one recording the
// same directory under another key: another spelling of the path, or a
// branch key an earlier release wrote.
func findEntry(cfg *config.HopspaceConfig, hopspacePath, hubPath, branch, worktreePath string) (string, config.HopspaceBranch, bool) {
	if !SharedHopspace(hopspacePath, hubPath) {
		e, ok := cfg.Branches[branch]
		return branch, e, ok
	}
	if worktreePath == "" {
		return "", config.HopspaceBranch{}, false
	}
	key := state.WorktreeKey(worktreePath)
	if e, ok := cfg.Branches[key]; ok {
		return key, e, true
	}
	for _, k := range sortedEntryKeys(cfg.Branches) {
		e := cfg.Branches[k]
		if filepath.IsAbs(e.Path) && state.SamePath(e.Path, worktreePath) {
			return k, e, true
		}
	}
	return "", config.HopspaceBranch{}, false
}

func sortedEntryKeys(m map[string]config.HopspaceBranch) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isBranchKey reports whether key names a branch rather than a worktree
// path. git refuses branch names starting with a slash, so an absolute
// path is never one.
func isBranchKey(key string) bool {
	return !filepath.IsAbs(key)
}

// migrateSharedEntries moves the worktree records cfg keys by branch, as
// an earlier release wrote a shared hopspace, to their worktree's path,
// naming the branch and the hub in each. owner returns the known hub a
// worktree path belongs to, "" when none: a record of no known hub's
// worktree is dropped. An entry's packageManagers, which configure the
// branch rather than one worktree, stay under the branch key. A branch
// key without a path already is such settings and is left as it is, so a
// second run changes nothing. Returns the keys moved, old to new, and
// whether cfg changed.
func migrateSharedEntries(cfg *config.HopspaceConfig, owner func(path string) string) (map[string]string, bool) {
	renames := make(map[string]string)
	changed := false
	for _, k := range sortedEntryKeys(cfg.Branches) {
		e := cfg.Branches[k]
		if !isBranchKey(k) || e.Path == "" {
			continue
		}
		changed = true
		delete(cfg.Branches, k)
		if len(e.PackageManagers) > 0 {
			cfg.Branches[k] = config.HopspaceBranch{PackageManagers: e.PackageManagers}
		}
		hub := ""
		if filepath.IsAbs(e.Path) {
			hub = owner(e.Path)
		}
		key := state.WorktreeKey(e.Path)
		if _, taken := cfg.Branches[key]; hub == "" || taken {
			// No known hub's worktree, or a record under its path is
			// already there (written since).
			continue
		}
		e.Branch = k
		e.Hub = state.WorktreeKey(hub)
		e.PackageManagers = nil
		cfg.Branches[key] = e
		if len(cfg.Branches[k].PackageManagers) == 0 {
			renames[k] = key
		}
	}
	return renames, changed
}

// migrateLocked migrates cfg, the shared hopspace's hop.json as loaded
// under its lock for the hub at hubPath, and saves the result, backing
// the old file up next to it first. Nothing is written when there is
// nothing to migrate.
func (h *Hopspace) migrateLocked(cfg *config.HopspaceConfig, hubPath string) error {
	renames, changed := migrateSharedEntries(cfg, worktreeOwner(h.fs, hubPath))
	if !changed {
		return nil
	}
	path := filepath.Join(h.Path, "hop.json")
	data, err := afero.ReadFile(h.fs, path)
	if err != nil {
		return err
	}
	backup, err := backupHopJSON(h.fs, h.Path, data, time.Now())
	if err != nil {
		return fmt.Errorf("back up %s before migrating it: %w", path, err)
	}
	if err := config.NewWriter(h.fs).WriteHopspaceConfig(h.Path, cfg, config.RenamedBranches(renames)); err != nil {
		return err
	}
	output.Note("Migrated %s to per-worktree records (backup: %s)", path, backup)
	return nil
}

// worktreeOwner returns a function naming the hub a worktree path
// belongs to: the hub state records for that worktree, else the deepest
// hub (state's, or the one at currentHub) whose directory holds it.
func worktreeOwner(fs afero.Fs, currentHub string) func(string) string {
	hubs := []string{currentHub}
	st, err := state.LoadState(fs)
	if err != nil {
		st = state.NewState()
	}
	for _, repo := range st.Repositories {
		for _, hub := range repo.Hubs {
			if hub != nil && hub.Path != "" {
				hubs = append(hubs, hub.Path)
			}
		}
	}
	return func(path string) string {
		for _, repo := range st.Repositories {
			if _, wt, ok := repo.WorktreeAt(path); ok && wt.HubPath != "" {
				return wt.HubPath
			}
		}
		best := ""
		for _, hub := range hubs {
			if isStrictlyUnder(path, hub) && len(hub) > len(best) {
				best = hub
			}
		}
		return best
	}
}

// hopJSONBackupFormat is the UTC stamp in a hop.json backup's name, the
// format state and repair backups use.
const hopJSONBackupFormat = "20060102T150405Z"

// backupHopJSON writes data next to the hop.json in dir as
// hop.json.<UTC stamp>.bak and returns its path. The file is created
// exclusively: an existing backup is never overwritten, and a second one
// in the same second gets a suffix.
func backupHopJSON(fs afero.Fs, dir string, data []byte, now time.Time) (string, error) {
	base := "hop.json." + now.UTC().Format(hopJSONBackupFormat)
	for i := 0; i < 100; i++ {
		name := base + ".bak"
		if i > 0 {
			name = fmt.Sprintf("%s-%d.bak", base, i)
		}
		path := filepath.Join(dir, name)
		f, err := fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil {
			return "", werr
		}
		return path, cerr
	}
	return "", fmt.Errorf("no free backup name for %s in %s", base, dir)
}
