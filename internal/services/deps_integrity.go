package services

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

// A worktree's DepsDir is a link into the store, so anything that empties
// it through the link empties the install every other worktree links to:
// npm ci removes each entry of node_modules before it installs, and
// rm -rf node_modules/* does the same to the visible ones. Each install
// git-hop puts in the store is recorded with the entries it had
// (installManifest); one missing an entry is damaged, and is reinstalled
// by the next install (linkDeps) and reported by doctor
// (IssueDamagedInstall).

// installManifest is what the store records of an install it holds.
type installManifest struct {
	// Entries are the names directly in the install when it was made.
	Entries []string `json:"entries"`
}

// InstallManifestPath returns where the store records the install at
// depsPath: beside it, "<store>/<hash>/<DepsDir>.git-hop.json", so no
// change made through a worktree's link reaches it.
func InstallManifestPath(depsPath string) string {
	return depsPath + ".git-hop.json"
}

// writeInstallManifest records the entries the install at depsPath has now.
func (m *DepsManager) writeInstallManifest(depsPath string) error {
	names, err := entryNames(m.fs, depsPath)
	if err != nil {
		return fmt.Errorf("failed to list install %s: %w", depsPath, err)
	}
	content, err := json.MarshalIndent(installManifest{Entries: names}, "", "  ")
	if err != nil {
		return err
	}
	if err := afero.WriteFile(m.fs, InstallManifestPath(depsPath), content, 0o644); err != nil {
		return fmt.Errorf("failed to record install %s: %w", depsPath, err)
	}
	return nil
}

// installIntact reports whether the install at depsPath is there with
// every entry it was recorded with. Entries added since (a tool's cache, a
// postinstall step) do not matter. An install made before installs were
// recorded, or whose record is unreadable, counts as intact while it has
// an entry that is not hidden: emptying node_modules through a shell glob
// leaves npm's hidden .package-lock.json behind.
func (m *DepsManager) installIntact(depsPath string) (bool, error) {
	names, err := entryNames(m.fs, depsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if len(names) == 0 {
		return false, nil
	}
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}
	var manifest installManifest
	data, err := afero.ReadFile(m.fs, InstallManifestPath(depsPath))
	if err != nil || json.Unmarshal(data, &manifest) != nil || len(manifest.Entries) == 0 {
		for _, name := range names {
			if !strings.HasPrefix(name, ".") {
				return true, nil
			}
		}
		return false, nil
	}
	for _, name := range manifest.Entries {
		if !present[name] {
			return false, nil
		}
	}
	return true, nil
}

// damagedStoreInstall reports whether target, a worktree link's target,
// is one of pm's installs in this store in the current layout and is
// missing entries (installIntact). Installs in the old layout are never
// written again, so they are not checked.
func (m *DepsManager) damagedStoreInstall(pm PackageManager, target string) bool {
	key, ok := depsKeyOf(m.RepoPath, target, pm)
	if !ok || isFlatDepsKey(key) {
		return false
	}
	intact, err := m.installIntact(target)
	return err == nil && !intact
}

// entryNames returns the sorted names directly in dir.
func entryNames(fs afero.Fs, dir string) ([]string, error) {
	infos, err := afero.ReadDir(fs, dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name())
	}
	sort.Strings(names)
	return names, nil
}
