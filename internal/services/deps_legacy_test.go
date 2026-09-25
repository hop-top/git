package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// legacyFixture lays out a data home and two hubs whose paths share their
// tail at the data home's length, so the old slicing gave both one store.
type legacyFixture struct {
	dataHome, hubA, hubB, shared string
}

func newLegacyFixture(t *testing.T) legacyFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := legacyFixture{
		dataHome: filepath.Join(root, "d"),
		hubA:     filepath.Join(root, "x", "same-tail-hub"),
		hubB:     filepath.Join(root, "y", "same-tail-hub"),
	}
	t.Setenv("GIT_HOP_DATA_HOME", f.dataHome)
	f.shared = legacyStore(f.dataHome, f.hubA)
	require.Equal(t, f.shared, legacyStore(f.dataHome, f.hubB), "fixture hubs must share one legacy store")
	for _, dir := range []string{f.hubA, f.hubB, filepath.Join(f.shared, "node_modules.aaa111")} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(f.shared, "node_modules.aaa111", "f"), []byte("12345"), 0o644))
	return f
}

func (f legacyFixture) worktree(t *testing.T, hub, branch string) string {
	t.Helper()
	wt := filepath.Join(hub, "hops", branch)
	require.NoError(t, os.MkdirAll(wt, 0o755))
	return wt
}

func findLegacy(t *testing.T, hopspaces, worktrees []string) []services.LegacyDepsStore {
	t.Helper()
	stores, err := services.FindLegacyDepsStores(afero.NewOsFs(), hopspaces, worktrees)
	require.NoError(t, err)
	return stores
}

// A store one hub still links into is linked, whichever hub asks: the
// scan covers every hub's worktrees, not the caller's.
func TestFindLegacyDepsStores_SharedStoreLinkedFromOtherHub(t *testing.T) {
	f := newLegacyFixture(t)
	wtA := f.worktree(t, f.hubA, "main")
	wtB := f.worktree(t, f.hubB, "main")
	link := filepath.Join(wtA, "web", "node_modules")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(f.shared, "node_modules.aaa111"), link))

	stores := findLegacy(t, []string{f.hubA, f.hubB}, []string{wtA, wtB})

	require.Len(t, stores, 1, "two hubs, one shared store")
	assert.Equal(t, f.shared, stores[0].Path)
	assert.True(t, stores[0].Linked())
	assert.Equal(t, []string{link}, stores[0].Links, "a link nested below the worktree root is found")
	assert.Equal(t, int64(5), stores[0].Size)
}

// A relative link is resolved against its own directory.
func TestFindLegacyDepsStores_RelativeLink(t *testing.T) {
	f := newLegacyFixture(t)
	wt := f.worktree(t, f.hubB, "main")
	link := filepath.Join(wt, "node_modules")
	rel, err := filepath.Rel(wt, filepath.Join(f.shared, "node_modules.aaa111"))
	require.NoError(t, err)
	require.NoError(t, os.Symlink(rel, link))

	stores := findLegacy(t, []string{f.hubA, f.hubB}, []string{wt})

	require.Len(t, stores, 1)
	assert.Equal(t, []string{link}, stores[0].Links)
}

// Nothing links in: the store is unlinked, and removing it also removes
// the directories above it it leaves empty, never the data home.
func TestFindLegacyDepsStores_OrphanRemoved(t *testing.T) {
	f := newLegacyFixture(t)
	wt := f.worktree(t, f.hubA, "main")
	gone := filepath.Join(f.hubA, "hops", "gone")

	stores := findLegacy(t, []string{f.hubA}, []string{wt, gone})

	require.Len(t, stores, 1)
	assert.False(t, stores[0].Linked())
	require.NoError(t, services.RemoveLegacyDepsStore(afero.NewOsFs(), stores[0]))
	assert.NoDirExists(t, f.shared)
	assert.NoDirExists(t, filepath.Dir(f.shared), "empty parent left by the store is removed")
	assert.DirExists(t, f.dataHome)
}

// A linked store is never removed, even when asked.
func TestRemoveLegacyDepsStore_RefusesLinked(t *testing.T) {
	f := newLegacyFixture(t)
	err := services.RemoveLegacyDepsStore(afero.NewOsFs(),
		services.LegacyDepsStore{Path: f.shared, Links: []string{"/somewhere/node_modules"}})
	require.Error(t, err)
	assert.DirExists(t, f.shared)
}

// Where the old slicing names a directory that is some hopspace's own
// store, it is not a legacy store.
func TestFindLegacyDepsStores_SkipsCurrentStores(t *testing.T) {
	f := newLegacyFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(f.shared), "hop.json"), []byte("{}"), 0o644))

	assert.Empty(t, findLegacy(t, []string{f.hubA}, nil), "a hopspace (hop.json beside it) owns that store")

	require.NoError(t, os.Remove(filepath.Join(filepath.Dir(f.shared), "hop.json")))
	assert.Empty(t, findLegacy(t, []string{f.hubA, filepath.Dir(f.shared)}, nil),
		"a hopspace in scope whose store it is")
}

// A worktree that cannot be walked completely makes the answer unknown:
// an error, never "unlinked".
func TestFindLegacyDepsStores_UnreadableWorktreeIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	f := newLegacyFixture(t)
	wt := f.worktree(t, f.hubA, "main")
	locked := filepath.Join(wt, "locked")
	require.NoError(t, os.MkdirAll(locked, 0o755))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := services.FindLegacyDepsStores(afero.NewOsFs(), []string{f.hubA}, []string{wt})
	require.Error(t, err)
}
