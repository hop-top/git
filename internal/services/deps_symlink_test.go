package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/services"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pnpmPM returns a PackageManager for pnpm without requiring pnpm on PATH.
// The InstallCmd is intentionally set to "true" (always succeeds) so that
// DetectPackageManagers and lockfile hashing still work in unit tests.
func pnpmPM() services.PackageManager {
	return services.PackageManager{
		Name:        "pnpm",
		DetectFiles: []string{"pnpm-lock.yaml"},
		LockFiles:   []string{"pnpm-lock.yaml"},
		DepsDir:     "node_modules",
		InstallCmd:  []string{"true"},
	}
}

// setupDepsTestDir creates a temporary directory structure for deps tests and
// configures GIT_HOP_DATA_HOME so that getDepsBasePath computes the correct
// canonical path for symlink targets. Returns the tmpDir.
func setupDepsTestDir(t *testing.T) string {
	t.Helper()
	// Use os.MkdirTemp so we control the parent directory.
	parent, err := os.MkdirTemp("", "git-hop-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(parent) })

	// With GIT_HOP_DATA_HOME=parent and repoPath=tmpDir (a child of parent),
	// getDepsBasePath correctly resolves to tmpDir/deps because repoPath starts
	// with dataHome and the relative segment is the tmpDir's base name.
	t.Setenv("GIT_HOP_DATA_HOME", parent)

	tmpDir, err := os.MkdirTemp(parent, "repo-")
	require.NoError(t, err)

	return tmpDir
}

// TestRebuildFromWorktrees_StaleSymlinkPreservesUsage verifies that when a
// lockfile changes (new hash) but EnsureDeps hasn't run yet, the old shared
// deps directory is NOT treated as orphaned because a live symlink still
// points to it. Before the fix, RebuildFromWorktrees compared the symlink
// target with the *current* lockfile hash, causing the old entry to lose all
// usedBy references and get garbage-collected.
func TestRebuildFromWorktrees_StaleSymlinkPreservesUsage(t *testing.T) {
	tmpDir := setupDepsTestDir(t)

	// Layout:
	//   tmpDir/deps/node_modules.old123/   ← shared deps dir
	//   tmpDir/worktrees/feature/pnpm-lock.yaml  ← lockfile (will be changed)
	//   tmpDir/worktrees/feature/node_modules →  tmpDir/deps/node_modules.old123

	oldDepsKey := "node_modules.old123"
	depsDir := filepath.Join(tmpDir, "deps")
	oldDepsPath := filepath.Join(depsDir, oldDepsKey)
	require.NoError(t, os.MkdirAll(oldDepsPath, 0755))

	worktreeDir := filepath.Join(tmpDir, "worktrees", "feature")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 6\n"), 0644))

	// Create a symlink pointing to the old deps
	symlinkPath := filepath.Join(worktreeDir, "node_modules")
	require.NoError(t, os.Symlink(oldDepsPath, symlinkPath))

	// Registry knows about the old entry with usage
	registry := &services.DepsRegistry{
		Entries: map[string]services.DepsEntry{
			oldDepsKey: {LockfileHash: "old123", UsedBy: []string{"feature"}},
		},
	}

	// Simulate the lockfile being updated (different content → different hash)
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 6\nupdated: true\n"), 0644))

	pms := []services.PackageManager{pnpmPM()}
	err := registry.RebuildFromWorktrees(afero.NewOsFs(), map[string]string{"feature": worktreeDir}, pms, tmpDir)
	require.NoError(t, err)

	// The old entry must still show usage because the symlink still points to it.
	entry, exists := registry.Entries[oldDepsKey]
	require.True(t, exists, "old registry entry should still exist")
	assert.Contains(t, entry.UsedBy, "feature", "symlink still points to old deps: entry must not be orphaned")

	// GC must not list the old key as orphaned.
	for _, key := range registry.GetOrphaned() {
		assert.NotEqual(t, oldDepsKey, key, "active-symlink entry must not be garbage-collected")
	}
}

// TestRebuildFromWorktrees_DanglingSymlinkPreservesUsage verifies that a
// dangling symlink (target deleted externally) still keeps its registry entry
// alive so that a subsequent EnsureDeps can reinstall and re-link.
func TestRebuildFromWorktrees_DanglingSymlinkPreservesUsage(t *testing.T) {
	tmpDir := setupDepsTestDir(t)

	depsKey := "node_modules.abc123"
	depsDir := filepath.Join(tmpDir, "deps")
	depsPath := filepath.Join(depsDir, depsKey)
	require.NoError(t, os.MkdirAll(depsPath, 0755))

	worktreeDir := filepath.Join(tmpDir, "worktrees", "main")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 6\n"), 0644))

	symlinkPath := filepath.Join(worktreeDir, "node_modules")
	require.NoError(t, os.Symlink(depsPath, symlinkPath))

	// Simulate external deletion of the deps directory (dangling symlink)
	require.NoError(t, os.RemoveAll(depsPath))

	registry := &services.DepsRegistry{
		Entries: map[string]services.DepsEntry{
			depsKey: {LockfileHash: "abc123", UsedBy: []string{"main"}},
		},
	}

	pms := []services.PackageManager{pnpmPM()}
	err := registry.RebuildFromWorktrees(afero.NewOsFs(), map[string]string{"main": worktreeDir}, pms, tmpDir)
	require.NoError(t, err)

	entry, exists := registry.Entries[depsKey]
	require.True(t, exists, "registry entry should not be removed when symlink is dangling")
	assert.Contains(t, entry.UsedBy, "main", "dangling symlink should still register usage to prevent GC")

	for _, key := range registry.GetOrphaned() {
		assert.NotEqual(t, depsKey, key, "dangling-symlink entry must not be garbage-collected")
	}
}

// TestRebuildFromWorktrees_UnmanagedSymlinkIgnored ensures that symlinks
// pointing outside the managed deps directory do not create spurious entries.
func TestRebuildFromWorktrees_UnmanagedSymlinkIgnored(t *testing.T) {
	tmpDir := setupDepsTestDir(t)

	// A directory outside the managed deps tree
	externalDir := filepath.Join(tmpDir, "external", "node_modules")
	require.NoError(t, os.MkdirAll(externalDir, 0755))

	worktreeDir := filepath.Join(tmpDir, "worktrees", "main")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 6\n"), 0644))

	symlinkPath := filepath.Join(worktreeDir, "node_modules")
	require.NoError(t, os.Symlink(externalDir, symlinkPath))

	registry := &services.DepsRegistry{
		Entries: make(map[string]services.DepsEntry),
	}

	pms := []services.PackageManager{pnpmPM()}
	err := registry.RebuildFromWorktrees(afero.NewOsFs(), map[string]string{"main": worktreeDir}, pms, tmpDir)
	require.NoError(t, err)

	// External symlinks should not create new registry entries
	assert.Empty(t, registry.Entries, "unmanaged symlink target must not create a registry entry")
}

// TestAudit_DetectsBrokenSymlinkWhenTargetDeleted verifies that Audit reports
// IssueBrokenSymlink when the symlink points to the expected (correct-hash)
// path but the target directory has been deleted. Before the fix, Audit only
// checked for broken symlinks when currentTarget != expectedDepsPath, so a
// GC-ed target that matched the current hash went undetected.
func TestAudit_DetectsBrokenSymlinkWhenTargetDeleted(t *testing.T) {
	tmpDir := setupDepsTestDir(t)

	// Use the real OS FS so that symlinks work.
	osFs := afero.NewOsFs()

	depsDir := filepath.Join(tmpDir, "deps")
	require.NoError(t, os.MkdirAll(depsDir, 0755))

	worktreeDir := filepath.Join(tmpDir, "worktrees", "main")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))

	lockfileContent := []byte("lockfileVersion: 6\n")
	lockfilePath := filepath.Join(worktreeDir, "pnpm-lock.yaml")
	require.NoError(t, os.WriteFile(lockfilePath, lockfileContent, 0644))

	// Compute the hash that DepsManager would use
	pm := pnpmPM()
	hash, err := pm.HashLockfile(osFs, lockfilePath)
	require.NoError(t, err)
	depsKey := pm.GetDepsKey(hash)

	// Create the shared deps dir and the symlink
	depsPath := filepath.Join(depsDir, depsKey)
	require.NoError(t, os.MkdirAll(depsPath, 0755))
	symlinkPath := filepath.Join(worktreeDir, "node_modules")
	require.NoError(t, os.Symlink(depsPath, symlinkPath))

	// Now delete the target directory (simulate GC removing it while symlink persists)
	require.NoError(t, os.RemoveAll(depsPath))

	// Registry still has the entry with the branch listed
	registry := &services.DepsRegistry{
		Entries: map[string]services.DepsEntry{
			depsKey: {LockfileHash: hash, UsedBy: []string{"main"}},
		},
	}
	require.NoError(t, registry.Save(osFs, tmpDir))

	// Create a DepsManager using the OS filesystem
	depsManager := services.NewDepsManagerFromParts(osFs, tmpDir, registry, []services.PackageManager{pm}, nil)

	issues, err := depsManager.Audit(map[string]string{"main": worktreeDir})
	require.NoError(t, err)

	require.NotEmpty(t, issues, "Audit should detect the broken symlink")
	assert.Equal(t, services.IssueBrokenSymlink, issues[0].Type,
		"issue type should be IssueBrokenSymlink when target was GC'd")
	assert.Equal(t, depsKey, issues[0].DepsKey)
}

