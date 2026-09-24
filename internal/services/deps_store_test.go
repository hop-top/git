package services_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// The deps store of a hopspace is <hopspace>/deps, whether the hopspace is
// a hub (default clone) or a data-home hopspace (--global clone). Earlier
// releases derived it by cutting the hopspace path at the data home's
// length without checking the hopspace was inside the data home, so a hub
// path longer than the data home landed in <data>/<tail of hub path>/deps.

// storeFixture lays out a data home and a hopspace under a fresh temp dir.
// dataHome and hopspace are relative to that dir.
type storeFixture struct {
	dataHome string
	hopspace string
}

func (f storeFixture) setup(t *testing.T) (dataHome, hopspace string) {
	t.Helper()
	root := t.TempDir()
	dataHome = filepath.Join(root, f.dataHome)
	hopspace = filepath.Join(root, f.hopspace)
	require.NoError(t, os.MkdirAll(dataHome, 0o755))
	require.NoError(t, os.MkdirAll(hopspace, 0o755))
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)
	return dataHome, hopspace
}

// installLoggingPM is a pnpm-like manager whose install writes a marker into
// ./node_modules (as npm/pnpm do) and appends a line to log, so a test can
// tell whether an install ran.
func installLoggingPM(log string) services.PackageManager {
	return services.PackageManager{
		Name:        "pnpm",
		DetectFiles: []string{"pnpm-lock.yaml"},
		LockFiles:   []string{"pnpm-lock.yaml"},
		DepsDir:     "node_modules",
		InstallCmd: []string{"sh", "-c",
			"echo install >> '" + log + "' && mkdir -p node_modules && echo fresh > node_modules/marker"},
	}
}

// newWorktree creates a worktree dir holding a pnpm lockfile with content
// lock and returns its path and deps key.
func newWorktree(t *testing.T, dir, lock string) (string, string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	lockfile := filepath.Join(dir, "pnpm-lock.yaml")
	require.NoError(t, os.WriteFile(lockfile, []byte(lock), 0o644))
	pm := pnpmPM()
	hash, err := pm.HashLockfile(afero.NewOsFs(), lockfile)
	require.NoError(t, err)
	return dir, pm.GetDepsKey(hash)
}

func newStoreManager(t *testing.T, hopspace string, pm services.PackageManager) *services.DepsManager {
	t.Helper()
	fs := afero.NewOsFs()
	registry, err := services.LoadRegistry(fs, hopspace)
	require.NoError(t, err)
	return services.NewDepsManagerFromParts(fs, hopspace, registry, []services.PackageManager{pm}, nil)
}

func assertStoreAt(t *testing.T, hopspace, worktree, key, wantStore string) {
	t.Helper()
	target, err := os.Readlink(filepath.Join(worktree, "node_modules"))
	require.NoError(t, err, "worktree node_modules must be a symlink")
	assert.Equal(t, filepath.Join(wantStore, key), target, "symlink target")
	assert.FileExists(t, filepath.Join(wantStore, key, "marker"), "install output in the store")
	assert.FileExists(t, filepath.Join(wantStore, ".registry.json"), "registry beside the installs")
	assert.Equal(t, wantStore, services.DepsStorePath(hopspace))
}

// A hub path longer than the data home, outside it. The regression guard:
// the old slicing put this store at <data>/<tail>/deps.
func TestDepsStore_LongHubOutsideDataHome(t *testing.T) {
	dataHome, hub := storeFixture{dataHome: "d", hopspace: "a-much-longer-hub-directory"}.setup(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hub, installLoggingPM(log))
	wt, key := newWorktree(t, filepath.Join(hub, "hops", "main"), "lockfileVersion: 6\n")

	require.NoError(t, dm.EnsureDeps(wt, "main"))

	assertStoreAt(t, hub, wt, key, filepath.Join(hub, "deps"))
	entries, err := os.ReadDir(dataHome)
	require.NoError(t, err)
	assert.Empty(t, entries, "a hub-local hopspace writes nothing under the data home")
}

// A hub path no longer than the data home. Inverse guard: the old code
// already fell back to <hub>/deps here.
func TestDepsStore_ShortHubOutsideDataHome(t *testing.T) {
	_, hub := storeFixture{dataHome: "a-long-data-home-directory", hopspace: "h"}.setup(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hub, installLoggingPM(log))
	wt, key := newWorktree(t, filepath.Join(hub, "hops", "main"), "lockfileVersion: 6\n")

	require.NoError(t, dm.EnsureDeps(wt, "main"))

	assertStoreAt(t, hub, wt, key, filepath.Join(hub, "deps"))
}

// A --global hopspace under the data home. Inverse guard: slicing at the
// data home's length is exact here, so the old code got it right.
func TestDepsStore_GlobalHopspace(t *testing.T) {
	dataHome, hopspace := storeFixture{dataHome: "data", hopspace: filepath.Join("data", "org", "repo")}.setup(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hopspace, installLoggingPM(log))
	wt, key := newWorktree(t, filepath.Join(t.TempDir(), "hub", "hops", "main"), "lockfileVersion: 6\n")

	require.NoError(t, dm.EnsureDeps(wt, "main"))

	assertStoreAt(t, hopspace, wt, key, filepath.Join(dataHome, "org", "repo", "deps"))
}

// legacyStore reproduces where earlier releases put a hopspace's deps store.
func legacyStore(dataHome, hopspace string) string {
	tail := strings.TrimPrefix(hopspace[len(dataHome):], string(filepath.Separator))
	return filepath.Join(dataHome, tail, "deps")
}

// Deps installed at the old location keep resolving: live links are left
// alone, a new worktree with the same lockfile reuses the old install
// instead of reinstalling, audit finds nothing to repair, and gc never
// touches the old store. Only an install the old store cannot satisfy
// goes to the new one.
func TestDepsStore_LegacyLocationKeepsResolving(t *testing.T) {
	dataHome, hub := storeFixture{dataHome: "d", hopspace: "a-much-longer-hub-directory"}.setup(t)
	legacy := legacyStore(dataHome, hub)
	require.NotEqual(t, services.DepsStorePath(hub), legacy, "fixture must hit the old slicing bug")

	lock := "lockfileVersion: 6\n"
	wtA, key := newWorktree(t, filepath.Join(hub, "hops", "a"), lock)
	legacyInstall := filepath.Join(legacy, key)
	require.NoError(t, os.MkdirAll(legacyInstall, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyInstall, "marker"), []byte("legacy\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, ".registry.json"),
		[]byte(`{"entries":{"`+key+`":{"lockfileHash":"x","usedBy":["a"]}}}`), 0o644))
	require.NoError(t, os.Symlink(legacyInstall, filepath.Join(wtA, "node_modules")))

	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hub, installLoggingPM(log))

	// Existing worktree: link untouched, no reinstall.
	require.NoError(t, dm.EnsureDeps(wtA, "a"))
	target, err := os.Readlink(filepath.Join(wtA, "node_modules"))
	require.NoError(t, err)
	assert.Equal(t, legacyInstall, target, "live link into the old store is kept")
	assert.NoFileExists(t, log, "no reinstall for deps the old store holds")

	// New worktree, same lockfile: reuses the old install.
	wtB, _ := newWorktree(t, filepath.Join(hub, "hops", "b"), lock)
	require.NoError(t, dm.EnsureDeps(wtB, "b"))
	target, err = os.Readlink(filepath.Join(wtB, "node_modules"))
	require.NoError(t, err)
	assert.Equal(t, legacyInstall, target, "same lockfile links the old install")
	assert.NoFileExists(t, log, "no reinstall for deps the old store holds")

	// Audit: links into the old store are healthy.
	issues, err := dm.Audit(map[string]string{"a": wtA, "b": wtB})
	require.NoError(t, err)
	assert.Empty(t, issues, "links into the old store are not issues")

	// gc never deletes from the old store.
	orphaned, _, err := dm.GarbageCollect(map[string]string{"a": wtA, "b": wtB}, false)
	require.NoError(t, err)
	assert.Empty(t, orphaned, "old-store installs are not this store's orphans")
	assert.FileExists(t, filepath.Join(wtA, "node_modules", "marker"))
	assert.FileExists(t, filepath.Join(wtB, "node_modules", "marker"))
	assert.FileExists(t, filepath.Join(legacy, ".registry.json"), "old registry left alone")

	// A lockfile the old store has no install for goes to the new store.
	wtC, keyC := newWorktree(t, filepath.Join(hub, "hops", "c"), "lockfileVersion: 9\n")
	require.NoError(t, dm.EnsureDeps(wtC, "c"))
	assertStoreAt(t, hub, wtC, keyC, filepath.Join(hub, "deps"))
	assert.FileExists(t, log, "a new lockfile installs")
}

// A link into the old store whose install is gone is broken; doctor --fix
// reinstalls into the new store.
func TestDepsStore_LegacyLinkBrokenRepairsIntoNewStore(t *testing.T) {
	dataHome, hub := storeFixture{dataHome: "d", hopspace: "a-much-longer-hub-directory"}.setup(t)
	legacy := legacyStore(dataHome, hub)

	wt, key := newWorktree(t, filepath.Join(hub, "hops", "a"), "lockfileVersion: 6\n")
	require.NoError(t, os.MkdirAll(legacy, 0o755))
	require.NoError(t, os.Symlink(filepath.Join(legacy, key), filepath.Join(wt, "node_modules")))

	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hub, installLoggingPM(log))

	issues, err := dm.Audit(map[string]string{"a": wt})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueBrokenSymlink, issues[0].Type)

	require.NoError(t, dm.Fix(issues, false))
	assertStoreAt(t, hub, wt, key, filepath.Join(hub, "deps"))
}
