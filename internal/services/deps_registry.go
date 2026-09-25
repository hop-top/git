package services

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

// DepsRegistry manages the dependency registry for a repository
type DepsRegistry struct {
	Entries map[string]DepsEntry `json:"entries"`
}

// DepsEntry represents a single dependency installation
type DepsEntry struct {
	LockfileHash string    `json:"lockfileHash"`
	LockfilePath string    `json:"lockfilePath"`
	UsedBy       []string  `json:"usedBy"`
	LastUsed     time.Time `json:"lastUsed"`
	InstalledAt  time.Time `json:"installedAt"`
}

// LoadRegistry loads the dependency registry for a repository
func LoadRegistry(fs afero.Fs, repoPath string) (*DepsRegistry, error) {
	registryPath := getRegistryPath(repoPath)

	// Return empty registry if file doesn't exist
	exists, err := afero.Exists(fs, registryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check registry existence: %w", err)
	}
	if !exists {
		return &DepsRegistry{
			Entries: make(map[string]DepsEntry),
		}, nil
	}

	// Read and parse registry
	content, err := afero.ReadFile(fs, registryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry: %w", err)
	}

	var registry DepsRegistry
	if err := json.Unmarshal(content, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse registry: %w", err)
	}

	if registry.Entries == nil {
		registry.Entries = make(map[string]DepsEntry)
	}

	return &registry, nil
}

// Save writes the registry to disk
func (r *DepsRegistry) Save(fs afero.Fs, repoPath string) error {
	registryPath := getRegistryPath(repoPath)

	// Ensure parent directory exists
	dir := filepath.Dir(registryPath)
	if err := fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Marshal registry
	content, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	// Write to file
	if err := afero.WriteFile(fs, registryPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	return nil
}

// AddUsage adds a branch to the usedBy list for a deps entry
func (r *DepsRegistry) AddUsage(depsKey, branch string) {
	entry, exists := r.Entries[depsKey]
	if !exists {
		entry = DepsEntry{
			UsedBy:      []string{},
			InstalledAt: time.Now(),
		}
	}

	// Add branch if not already in list
	found := false
	for _, b := range entry.UsedBy {
		if b == branch {
			found = true
			break
		}
	}
	if !found {
		entry.UsedBy = append(entry.UsedBy, branch)
	}

	entry.LastUsed = time.Now()
	r.Entries[depsKey] = entry
}

// RemoveUsage removes a branch from the usedBy list for a deps entry
func (r *DepsRegistry) RemoveUsage(depsKey, branch string) {
	entry, exists := r.Entries[depsKey]
	if !exists {
		return
	}

	// Remove branch from usedBy list
	newUsedBy := []string{}
	for _, b := range entry.UsedBy {
		if b != branch {
			newUsedBy = append(newUsedBy, b)
		}
	}
	entry.UsedBy = newUsedBy
	r.Entries[depsKey] = entry
}

// RebuildFromWorktrees rebuilds the registry from the current state of worktrees.
// worktrees is a map of branchName → worktreePath.
func (r *DepsRegistry) RebuildFromWorktrees(fs afero.Fs, worktrees map[string]string, pms []PackageManager, repoPath string) error {
	// Clear all usedBy arrays
	for key, entry := range r.Entries {
		entry.UsedBy = []string{}
		r.Entries[key] = entry
	}

	// Scan each worktree
	for branch, worktreePath := range worktrees {
		// Detect package managers in this worktree
		detectedPMs, err := DetectPackageManagers(fs, worktreePath, pms)
		if err != nil {
			return fmt.Errorf("failed to detect package managers in %s: %w", worktreePath, err)
		}

		// For each detected PM, record usage based on what the symlink actually points to.
		// This is intentionally independent of the current lockfile hash: if a lockfile
		// changes between GC runs, the old shared deps directory must not be removed while
		// a live symlink still references it.
		for _, pm := range detectedPMs {
			symlinkPath := filepath.Join(worktreePath, pm.DepsDir)

			if linker, ok := fs.(afero.Symlinker); ok {
				target, err := linker.ReadlinkIfPossible(symlinkPath)
				if err != nil || target == "" {
					continue
				}
				if key, ok := depsKeyOf(repoPath, target, pm); ok {
					r.AddUsage(key, branch)
				}
			}
		}
	}

	return nil
}

// GetOrphaned returns a list of deps keys that have no branches using them
func (r *DepsRegistry) GetOrphaned() []string {
	orphaned := []string{}
	for key, entry := range r.Entries {
		if len(entry.UsedBy) == 0 {
			orphaned = append(orphaned, key)
		}
	}
	return orphaned
}

// UpdateEntryMetadata updates the metadata for a deps entry
func (r *DepsRegistry) UpdateEntryMetadata(depsKey, lockfileHash, lockfilePath string) {
	entry, exists := r.Entries[depsKey]
	if !exists {
		entry = DepsEntry{
			UsedBy:      []string{},
			InstalledAt: time.Now(),
		}
	}
	entry.LockfileHash = lockfileHash
	entry.LockfilePath = lockfilePath
	entry.LastUsed = time.Now()
	r.Entries[depsKey] = entry
}

// DeleteEntry removes an entry from the registry
func (r *DepsRegistry) DeleteEntry(depsKey string) {
	delete(r.Entries, depsKey)
}

// getRegistryPath returns the path to the registry file
func getRegistryPath(repoPath string) string {
	return filepath.Join(DepsStorePath(repoPath), ".registry.json")
}

// DepsStorePath returns the directory holding a hopspace's shared
// dependency installs and their registry: <hopspace>/deps. That is the
// hub for a default clone and <data>/<org>/<repo> for a --global one.
func DepsStorePath(hopspacePath string) string {
	return filepath.Join(hopspacePath, "deps")
}

// getDepsPath returns the full path to a specific deps installation: the
// key is its path under the store (see PackageManager.GetDepsKey).
func getDepsPath(repoPath, depsKey string) string {
	return filepath.Join(DepsStorePath(repoPath), filepath.FromSlash(depsKey))
}

// depsKeyOf returns the key of the install a symlink target names, in
// either layout, if the target is one of pm's installs in repoPath's
// store: "<hash>/<DepsDir>" (GetDepsKey) or a single "<name>.<hash>"
// element (flatDepsKey). The target must be written exactly as links are
// made (getDepsPath), so a link to anything else is never counted.
func depsKeyOf(repoPath, target string, pm PackageManager) (string, bool) {
	rel, err := filepath.Rel(DepsStorePath(repoPath), target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	key := filepath.ToSlash(rel)
	if !isFlatDepsKey(key) {
		hash, ok := strings.CutSuffix(key, "/"+filepath.ToSlash(pm.DepsDir))
		if !ok || hash == "" || strings.Contains(hash, "/") {
			return "", false
		}
	}
	if getDepsPath(repoPath, key) != target {
		return "", false
	}
	return key, true
}

// legacyDepsStorePath returns where earlier releases put a hopspace's deps
// store when that differs from DepsStorePath, else "". They cut the
// hopspace path at the data home's length without checking the hopspace
// was inside the data home, so a hub path longer than the data home got
// <data>/<tail of hub path>/deps. Worktrees still link there; the store is
// never written, its installs never reused (they are in the layout Node
// cannot resolve from, see DepsManager.linkDeps), and it is removed whole
// once nothing links into it (FindLegacyDepsStores). The
// slicing is reproduced verbatim on purpose: it is the only way to find
// those stores.
func legacyDepsStorePath(hopspacePath string) string {
	dataHome := hop.GetGitHopDataHome()
	if len(hopspacePath) <= len(dataHome) {
		return ""
	}
	tail := strings.TrimPrefix(hopspacePath[len(dataHome):], string(filepath.Separator))
	legacy := filepath.Join(dataHome, tail, "deps")
	if legacy == DepsStorePath(hopspacePath) {
		return ""
	}
	return legacy
}
