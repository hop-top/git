package services_test

import (
	"fmt"
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

// TestEnsureDeps_PopulatesSharedCache verifies that after EnsureDeps runs,
// the shared cache directory (the symlink target) actually contains the
// installed dependencies. Before the fix, the install command was always run
// in the worktree (writing to worktree/<DepsDir>), and ensurePMDeps then
// trashed that real directory and replaced it with a symlink to an empty
// cache dir. Result: every cache entry was 0 bytes and the dependency-sharing
// feature provided no benefit. See https://github.com/hop-top/git/issues/11.
func TestEnsureDeps_PopulatesSharedCache(t *testing.T) {
	tmpDir := setupDepsTestDir(t)
	osFs := afero.NewOsFs()

	worktreeDir := filepath.Join(tmpDir, "worktrees", "feature")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 6\n"), 0644))

	// Use a fake "vendoring" install command that mimics how `go mod vendor`,
	// `npm ci`, `pnpm install`, etc. behave: it writes into ./<DepsDir>
	// relative to the cwd (which the runner sets to worktreePath). The cache
	// argument is invisible to the install command.
	pm := services.PackageManager{
		Name:        "pnpm",
		DetectFiles: []string{"pnpm-lock.yaml"},
		LockFiles:   []string{"pnpm-lock.yaml"},
		DepsDir:     "node_modules",
		InstallCmd:  []string{"sh", "-c", "mkdir -p node_modules && echo populated > node_modules/marker"},
	}

	registry := &services.DepsRegistry{Entries: make(map[string]services.DepsEntry)}
	dm := services.NewDepsManagerFromParts(osFs, tmpDir, registry, []services.PackageManager{pm}, nil)

	require.NoError(t, dm.EnsureDeps(worktreeDir, "feature"))

	// The worktree path must be a symlink (the sharing contract).
	symlinkPath := filepath.Join(worktreeDir, "node_modules")
	target, err := os.Readlink(symlinkPath)
	require.NoError(t, err, "worktree/node_modules should be a symlink after EnsureDeps")

	// The cache target must be populated with the install command's output.
	// This is the regression: the marker file lives in the worktree dir the
	// install command wrote to, NOT in the cache target the symlink points to.
	markerInCache := filepath.Join(target, "marker")
	contents, err := os.ReadFile(markerInCache)
	require.NoError(t, err, "shared cache at %s must contain the install output (currently empty due to bug)", target)
	assert.Equal(t, "populated\n", string(contents))
}

// TestEnsureDeps_StaleSymlinkClearedBeforeInstall verifies that when the
// lockfile hash changes (so the worktree's existing symlink points to an OLD
// cache dir), EnsureDeps clears the stale symlink BEFORE running the install
// command. Otherwise the install writes through the symlink into the old
// cache — corrupting shared state used by other branches — and the
// post-install relocation would then move the symlink (not a real dir) into
// the new cache path, producing a symlink-to-symlink pointing nowhere useful.
// See https://github.com/hop-top/git/pull/13 review (ordering bug).
func TestEnsureDeps_StaleSymlinkClearedBeforeInstall(t *testing.T) {
	tmpDir := setupDepsTestDir(t)
	osFs := afero.NewOsFs()

	// Pre-populate an OLD cache dir and a symlink pointing at it.
	oldDepsKey := "node_modules.oldhsh"
	oldDepsPath := filepath.Join(tmpDir, "deps", oldDepsKey)
	require.NoError(t, os.MkdirAll(oldDepsPath, 0755))
	oldMarker := filepath.Join(oldDepsPath, "old-marker")
	require.NoError(t, os.WriteFile(oldMarker, []byte("old"), 0644))

	worktreeDir := filepath.Join(tmpDir, "worktrees", "feature")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	// New lockfile content → new hash → new cache key.
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 6\nnew: true\n"), 0644))

	// Stale symlink from a prior EnsureDeps with the old lockfile.
	symlinkPath := filepath.Join(worktreeDir, "node_modules")
	require.NoError(t, os.Symlink(oldDepsPath, symlinkPath))

	pm := services.PackageManager{
		Name:        "pnpm",
		DetectFiles: []string{"pnpm-lock.yaml"},
		LockFiles:   []string{"pnpm-lock.yaml"},
		DepsDir:     "node_modules",
		// Install command that would corrupt the old cache if the stale
		// symlink survived into it: writes a "new-marker" via the worktree
		// dep dir path.
		InstallCmd: []string{"sh", "-c", "mkdir -p node_modules && echo new > node_modules/new-marker"},
	}

	registry := &services.DepsRegistry{Entries: make(map[string]services.DepsEntry)}
	dm := services.NewDepsManagerFromParts(osFs, tmpDir, registry, []services.PackageManager{pm}, nil)

	require.NoError(t, dm.EnsureDeps(worktreeDir, "feature"))

	// The old cache must NOT have been mutated by the install command.
	// If the stale symlink survived into install, new-marker would have
	// ended up inside oldDepsPath.
	_, err := os.Stat(filepath.Join(oldDepsPath, "new-marker"))
	assert.True(t, os.IsNotExist(err), "old shared cache must not be corrupted by install writing through a stale symlink")
	// The original old-marker should still be there.
	contents, err := os.ReadFile(oldMarker)
	require.NoError(t, err, "old cache contents must be preserved")
	assert.Equal(t, "old", string(contents))

	// The new symlink target must be a real dir containing new-marker.
	target, err := os.Readlink(symlinkPath)
	require.NoError(t, err)
	info, err := os.Lstat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir() && info.Mode()&os.ModeSymlink == 0, "new cache target must be a real directory, not a symlink")
	newContents, err := os.ReadFile(filepath.Join(target, "new-marker"))
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(newContents))
}

// TestEnsureDeps_InstallProducedNothingErrors verifies that when an install
// command succeeds but neither populates targetDir (pip-style) nor creates
// worktreePath/<DepsDir> (cwd-relative style), EnsureDeps reports an error
// rather than silently leaving an empty shared cache entry behind. The
// previous implementation treated "no worktree deps dir" as the pip path and
// returned nil regardless of PM, which hid install bugs.
func TestEnsureDeps_InstallProducedNothingErrors(t *testing.T) {
	tmpDir := setupDepsTestDir(t)
	osFs := afero.NewOsFs()

	worktreeDir := filepath.Join(tmpDir, "worktrees", "feature")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 6\n"), 0644))

	// Install command that succeeds but produces nothing anywhere.
	pm := services.PackageManager{
		Name:        "pnpm",
		DetectFiles: []string{"pnpm-lock.yaml"},
		LockFiles:   []string{"pnpm-lock.yaml"},
		DepsDir:     "node_modules",
		InstallCmd:  []string{"true"},
	}

	registry := &services.DepsRegistry{Entries: make(map[string]services.DepsEntry)}
	dm := services.NewDepsManagerFromParts(osFs, tmpDir, registry, []services.PackageManager{pm}, nil)

	err := dm.EnsureDeps(worktreeDir, "feature")
	require.Error(t, err, "install command producing no output must be an error, not silent success")
	assert.Contains(t, err.Error(), "produced", "error should explain the install produced no deps")
}

// TestRelocateDir_FallsBackToCopy verifies that the move-with-fallback
// helper used by installDeps falls back to a recursive copy when os.Rename
// cannot move src to dst. os.Rename fails with EXDEV across filesystems
// (worktree and data home on different volumes); we can't easily produce
// EXDEV in a unit test, so we inject a failing rename function via
// services.SetRelocateRenameForTest and verify the copy path runs and
// produces the expected tree.
func TestRelocateDir_FallsBackToCopy(t *testing.T) {
	tmpDir := t.TempDir()
	osFs := afero.NewOsFs()

	src := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "nested", "b.txt"), []byte("b"), 0644))

	dst := filepath.Join(tmpDir, "dst")

	// Inject a rename that always fails to force the fallback branch.
	calls := 0
	restore := services.SetRelocateRenameForTest(func(string, string) error {
		calls++
		return fmt.Errorf("injected EXDEV")
	})
	defer restore()

	err := services.RelocateDir(osFs, src, dst)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 1, "the injected rename must have been called (else the test didn't exercise the fallback path)")

	// src must be gone after the fallback copy+remove.
	_, err = os.Stat(src)
	assert.True(t, os.IsNotExist(err), "src must be removed after relocation")

	// dst must now be a directory with the full tree.
	info, err := os.Lstat(dst)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "dst must be a directory after fallback copy")

	a, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(a))
	b, err := os.ReadFile(filepath.Join(dst, "nested", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "b", string(b))
}

// TestRelocateDir_HappyPathRename verifies the non-fallback path: with no
// injection, a same-filesystem rename succeeds and the copy branch is not
// exercised. This complements the fallback test above to ensure both
// branches are covered.
func TestRelocateDir_HappyPathRename(t *testing.T) {
	tmpDir := t.TempDir()
	osFs := afero.NewOsFs()

	src := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(src, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0644))

	dst := filepath.Join(tmpDir, "dst")
	require.NoError(t, services.RelocateDir(osFs, src, dst))

	content, err := os.ReadFile(filepath.Join(dst, "f.txt"))
	require.NoError(t, err)
	assert.Equal(t, "x", string(content))
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

