package hop_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/jadb/git-hop/internal/services"
	"github.com/spf13/afero"
)

func TestLoadRegistry_EmptyRegistry(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/test/repo"

	// Load registry when it doesn't exist
	registry, err := services.LoadRegistry(fs, repoPath)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	if registry == nil {
		t.Fatal("LoadRegistry() returned nil registry")
	}

	if registry.Entries == nil {
		t.Fatal("LoadRegistry() registry.Entries is nil")
	}

	if len(registry.Entries) != 0 {
		t.Errorf("LoadRegistry() empty registry has %d entries, want 0", len(registry.Entries))
	}
}

func TestLoadRegistry_ExistingRegistry(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/test/repo"

	// Create registry directory
	registryDir := filepath.Join(repoPath, "deps")
	if err := fs.MkdirAll(registryDir, 0755); err != nil {
		t.Fatalf("failed to create registry dir: %v", err)
	}

	// Create registry file with test data
	now := time.Now()
	testRegistry := services.DepsRegistry{
		Entries: map[string]services.DepsEntry{
			"node_modules.abc123": {
				LockfileHash: "abc123",
				LockfilePath: "/test/worktree/package-lock.json",
				UsedBy:       []string{"main", "develop"},
				LastUsed:     now,
				InstalledAt:  now,
			},
		},
	}

	data, err := json.Marshal(testRegistry)
	if err != nil {
		t.Fatalf("failed to marshal test registry: %v", err)
	}

	registryPath := filepath.Join(registryDir, ".registry.json")
	if err := afero.WriteFile(fs, registryPath, data, 0644); err != nil {
		t.Fatalf("failed to write registry file: %v", err)
	}

	// Load registry
	registry, err := services.LoadRegistry(fs, repoPath)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	if len(registry.Entries) != 1 {
		t.Fatalf("LoadRegistry() has %d entries, want 1", len(registry.Entries))
	}

	entry, exists := registry.Entries["node_modules.abc123"]
	if !exists {
		t.Fatal("LoadRegistry() missing expected entry 'node_modules.abc123'")
	}

	if entry.LockfileHash != "abc123" {
		t.Errorf("entry.LockfileHash = %v, want abc123", entry.LockfileHash)
	}

	if len(entry.UsedBy) != 2 {
		t.Errorf("entry.UsedBy has %d branches, want 2", len(entry.UsedBy))
	}
}

func TestRegistry_SaveAndLoad(t *testing.T) {
	fs := afero.NewMemMapFs()
	repoPath := "/test/repo"

	// Create a new registry
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	now := time.Now()
	registry.Entries["vendor.def456"] = services.DepsEntry{
		LockfileHash: "def456",
		LockfilePath: "/test/worktree/go.sum",
		UsedBy:       []string{"feature-branch"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// Save registry
	if err := registry.Save(fs, repoPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load it back
	loadedRegistry, err := services.LoadRegistry(fs, repoPath)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	if len(loadedRegistry.Entries) != 1 {
		t.Fatalf("Loaded registry has %d entries, want 1", len(loadedRegistry.Entries))
	}

	entry, exists := loadedRegistry.Entries["vendor.def456"]
	if !exists {
		t.Fatal("Loaded registry missing expected entry 'vendor.def456'")
	}

	if entry.LockfileHash != "def456" {
		t.Errorf("entry.LockfileHash = %v, want def456", entry.LockfileHash)
	}
}

func TestRegistry_AddUsage(t *testing.T) {
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	depsKey := "node_modules.abc123"
	branch := "main"

	// Add usage to new entry
	registry.AddUsage(depsKey, branch)

	entry, exists := registry.Entries[depsKey]
	if !exists {
		t.Fatal("AddUsage() did not create entry")
	}

	if len(entry.UsedBy) != 1 || entry.UsedBy[0] != branch {
		t.Errorf("entry.UsedBy = %v, want [%s]", entry.UsedBy, branch)
	}

	// Add another branch
	registry.AddUsage(depsKey, "develop")
	entry = registry.Entries[depsKey]

	if len(entry.UsedBy) != 2 {
		t.Errorf("entry.UsedBy has %d branches, want 2", len(entry.UsedBy))
	}

	// Add duplicate branch (should not duplicate)
	registry.AddUsage(depsKey, "main")
	entry = registry.Entries[depsKey]

	if len(entry.UsedBy) != 2 {
		t.Errorf("entry.UsedBy has %d branches after duplicate add, want 2", len(entry.UsedBy))
	}
}

func TestRegistry_RemoveUsage(t *testing.T) {
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	depsKey := "node_modules.abc123"

	// Set up entry with multiple branches
	registry.Entries[depsKey] = services.DepsEntry{
		UsedBy: []string{"main", "develop", "feature"},
	}

	// Remove one branch
	registry.RemoveUsage(depsKey, "develop")

	entry := registry.Entries[depsKey]
	if len(entry.UsedBy) != 2 {
		t.Errorf("entry.UsedBy has %d branches, want 2", len(entry.UsedBy))
	}

	// Verify develop was removed
	for _, branch := range entry.UsedBy {
		if branch == "develop" {
			t.Error("RemoveUsage() did not remove 'develop' branch")
		}
	}

	// Remove non-existent branch (should be no-op)
	registry.RemoveUsage(depsKey, "nonexistent")
	entry = registry.Entries[depsKey]

	if len(entry.UsedBy) != 2 {
		t.Errorf("entry.UsedBy has %d branches after removing nonexistent, want 2", len(entry.UsedBy))
	}

	// Remove from non-existent entry (should be no-op)
	registry.RemoveUsage("nonexistent.key", "main")
}

func TestRegistry_GetOrphaned(t *testing.T) {
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	// Entry with branches (not orphaned)
	registry.Entries["node_modules.abc123"] = services.DepsEntry{
		UsedBy: []string{"main"},
	}

	// Entry with no branches (orphaned)
	registry.Entries["vendor.def456"] = services.DepsEntry{
		UsedBy: []string{},
	}

	// Another orphaned entry
	registry.Entries["venv.ghi789"] = services.DepsEntry{
		UsedBy: []string{},
	}

	orphaned := registry.GetOrphaned()

	if len(orphaned) != 2 {
		t.Errorf("GetOrphaned() returned %d entries, want 2", len(orphaned))
	}

	// Verify orphaned entries
	orphanedMap := make(map[string]bool)
	for _, key := range orphaned {
		orphanedMap[key] = true
	}

	if !orphanedMap["vendor.def456"] {
		t.Error("GetOrphaned() should include 'vendor.def456'")
	}
	if !orphanedMap["venv.ghi789"] {
		t.Error("GetOrphaned() should include 'venv.ghi789'")
	}
	if orphanedMap["node_modules.abc123"] {
		t.Error("GetOrphaned() should not include 'node_modules.abc123'")
	}
}

func TestRegistry_UpdateEntryMetadata(t *testing.T) {
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	depsKey := "node_modules.abc123"
	lockfileHash := "abc123"
	lockfilePath := "/test/package-lock.json"

	// Update metadata for new entry
	registry.UpdateEntryMetadata(depsKey, lockfileHash, lockfilePath)

	entry, exists := registry.Entries[depsKey]
	if !exists {
		t.Fatal("UpdateEntryMetadata() did not create entry")
	}

	if entry.LockfileHash != lockfileHash {
		t.Errorf("entry.LockfileHash = %v, want %v", entry.LockfileHash, lockfileHash)
	}
	if entry.LockfilePath != lockfilePath {
		t.Errorf("entry.LockfilePath = %v, want %v", entry.LockfilePath, lockfilePath)
	}

	// Update existing entry
	newHash := "xyz789"
	newPath := "/test/new-lock.json"
	registry.UpdateEntryMetadata(depsKey, newHash, newPath)

	entry = registry.Entries[depsKey]
	if entry.LockfileHash != newHash {
		t.Errorf("entry.LockfileHash = %v, want %v", entry.LockfileHash, newHash)
	}
	if entry.LockfilePath != newPath {
		t.Errorf("entry.LockfilePath = %v, want %v", entry.LockfilePath, newPath)
	}
}

func TestRegistry_DeleteEntry(t *testing.T) {
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	registry.Entries["node_modules.abc123"] = services.DepsEntry{
		UsedBy: []string{"main"},
	}

	registry.Entries["vendor.def456"] = services.DepsEntry{
		UsedBy: []string{"develop"},
	}

	// Delete one entry
	registry.DeleteEntry("node_modules.abc123")

	if len(registry.Entries) != 1 {
		t.Errorf("registry has %d entries after delete, want 1", len(registry.Entries))
	}

	if _, exists := registry.Entries["node_modules.abc123"]; exists {
		t.Error("DeleteEntry() did not remove entry")
	}

	if _, exists := registry.Entries["vendor.def456"]; !exists {
		t.Error("DeleteEntry() removed wrong entry")
	}

	// Delete non-existent entry (should be no-op)
	registry.DeleteEntry("nonexistent.key")

	if len(registry.Entries) != 1 {
		t.Errorf("registry has %d entries after deleting nonexistent, want 1", len(registry.Entries))
	}
}

func TestRegistry_MultiPMScenario(t *testing.T) {
	// Test registry with multiple package managers
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	now := time.Now()

	// npm deps
	registry.Entries["node_modules.abc123"] = services.DepsEntry{
		LockfileHash: "abc123",
		LockfilePath: "/test/package-lock.json",
		UsedBy:       []string{"main", "develop"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// go deps
	registry.Entries["vendor.def456"] = services.DepsEntry{
		LockfileHash: "def456",
		LockfilePath: "/test/go.sum",
		UsedBy:       []string{"main"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// pip deps
	registry.Entries["venv.ghi789"] = services.DepsEntry{
		LockfileHash: "ghi789",
		LockfilePath: "/test/requirements.txt",
		UsedBy:       []string{"feature-branch"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// Verify all entries exist
	if len(registry.Entries) != 3 {
		t.Errorf("registry has %d entries, want 3", len(registry.Entries))
	}

	// Add usage to npm from new branch
	registry.AddUsage("node_modules.abc123", "feature-branch")
	npmEntry := registry.Entries["node_modules.abc123"]
	if len(npmEntry.UsedBy) != 3 {
		t.Errorf("npm entry has %d branches, want 3", len(npmEntry.UsedBy))
	}

	// Remove usage from go
	registry.RemoveUsage("vendor.def456", "main")
	goEntry := registry.Entries["vendor.def456"]
	if len(goEntry.UsedBy) != 0 {
		t.Errorf("go entry has %d branches after removal, want 0", len(goEntry.UsedBy))
	}

	// Check orphaned - should only be go
	orphaned := registry.GetOrphaned()
	if len(orphaned) != 1 {
		t.Errorf("GetOrphaned() returned %d entries, want 1", len(orphaned))
	}
	if len(orphaned) > 0 && orphaned[0] != "vendor.def456" {
		t.Errorf("GetOrphaned()[0] = %v, want vendor.def456", orphaned[0])
	}
}

func TestRegistry_SamePMMultipleHashes(t *testing.T) {
	// Test same package manager with different lockfile hashes
	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	now := time.Now()

	// npm with hash abc123 (used by main)
	registry.Entries["node_modules.abc123"] = services.DepsEntry{
		LockfileHash: "abc123",
		LockfilePath: "/test/main/package-lock.json",
		UsedBy:       []string{"main"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// npm with hash def456 (used by develop)
	registry.Entries["node_modules.def456"] = services.DepsEntry{
		LockfileHash: "def456",
		LockfilePath: "/test/develop/package-lock.json",
		UsedBy:       []string{"develop"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// npm with hash ghi789 (used by feature-1 and feature-2)
	registry.Entries["node_modules.ghi789"] = services.DepsEntry{
		LockfileHash: "ghi789",
		LockfilePath: "/test/feature-1/package-lock.json",
		UsedBy:       []string{"feature-1", "feature-2"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// Verify all three versions exist
	if len(registry.Entries) != 3 {
		t.Errorf("registry has %d entries, want 3", len(registry.Entries))
	}

	// Add feature-3 using same hash as feature-1
	registry.AddUsage("node_modules.ghi789", "feature-3")
	entry := registry.Entries["node_modules.ghi789"]
	if len(entry.UsedBy) != 3 {
		t.Errorf("shared entry has %d branches, want 3", len(entry.UsedBy))
	}

	// Remove all users of abc123
	registry.RemoveUsage("node_modules.abc123", "main")

	// Only abc123 should be orphaned
	orphaned := registry.GetOrphaned()
	if len(orphaned) != 1 {
		t.Errorf("GetOrphaned() returned %d entries, want 1", len(orphaned))
	}
}

func TestRegistry_ComplexMultiPMMultiBranch(t *testing.T) {
	// Simulate complex real-world scenario:
	// - Monorepo with frontend (npm) and backend (go)
	// - Multiple branches with different dependency versions
	fs := afero.NewMemMapFs()
	repoPath := "/test/monorepo"

	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	now := time.Now()

	// Frontend deps - main branch
	registry.Entries["node_modules.aaa111"] = services.DepsEntry{
		LockfileHash: "aaa111",
		LockfilePath: "/test/monorepo/main/package-lock.json",
		UsedBy:       []string{"main"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// Backend deps - main branch
	registry.Entries["vendor.bbb222"] = services.DepsEntry{
		LockfileHash: "bbb222",
		LockfilePath: "/test/monorepo/main/go.sum",
		UsedBy:       []string{"main"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// Frontend deps - develop branch (same as main)
	registry.AddUsage("node_modules.aaa111", "develop")

	// Backend deps - develop branch (different from main)
	registry.Entries["vendor.ccc333"] = services.DepsEntry{
		LockfileHash: "ccc333",
		LockfilePath: "/test/monorepo/develop/go.sum",
		UsedBy:       []string{"develop"},
		LastUsed:     now,
		InstalledAt:  now,
	}

	// Feature branch - frontend updated, backend same as develop
	registry.Entries["node_modules.ddd444"] = services.DepsEntry{
		LockfileHash: "ddd444",
		LockfilePath: "/test/monorepo/feature/package-lock.json",
		UsedBy:       []string{"feature-auth"},
		LastUsed:     now,
		InstalledAt:  now,
	}
	registry.AddUsage("vendor.ccc333", "feature-auth")

	// Save and reload
	if err := registry.Save(fs, repoPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := services.LoadRegistry(fs, repoPath)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	// Verify structure
	if len(loaded.Entries) != 4 {
		t.Errorf("loaded registry has %d entries, want 4", len(loaded.Entries))
	}

	// Verify shared deps
	npmMain := loaded.Entries["node_modules.aaa111"]
	if len(npmMain.UsedBy) != 2 {
		t.Errorf("node_modules.aaa111 used by %d branches, want 2", len(npmMain.UsedBy))
	}

	goShared := loaded.Entries["vendor.ccc333"]
	if len(goShared.UsedBy) != 2 {
		t.Errorf("vendor.ccc333 used by %d branches, want 2", len(goShared.UsedBy))
	}

	// Simulate branch deletion - remove feature-auth
	loaded.RemoveUsage("node_modules.ddd444", "feature-auth")
	loaded.RemoveUsage("vendor.ccc333", "feature-auth")

	// Check orphaned
	orphaned := loaded.GetOrphaned()

	// node_modules.ddd444 should be orphaned (only used by feature-auth)
	orphanedMap := make(map[string]bool)
	for _, key := range orphaned {
		orphanedMap[key] = true
	}

	if !orphanedMap["node_modules.ddd444"] {
		t.Error("node_modules.ddd444 should be orphaned after removing feature-auth")
	}

	// vendor.ccc333 should NOT be orphaned (still used by develop)
	if orphanedMap["vendor.ccc333"] {
		t.Error("vendor.ccc333 should not be orphaned (still used by develop)")
	}
}
