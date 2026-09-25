package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// A worktree's DepsDir is a link into the shared store, so anything that
// empties it through the link (npm ci removes each entry of node_modules
// before installing; rm -rf node_modules/*) empties the install every other
// worktree links to. git-hop records the entries of each install it puts in
// the store, sees one missing, and reinstalls.

// npmLikePM installs two package directories and a hidden lockfile copy into
// ./node_modules, as npm does, logging each run to log.
func npmLikePM(log string) services.PackageManager {
	return writerPM("node_modules",
		"echo install >> '"+log+"' && mkdir -p node_modules/a node_modules/b && echo a > node_modules/a/index.js"+
			" && echo lock > node_modules/.package-lock.json")
}

type sharedInstallFixture struct {
	hopspace, main, feat, install, log string
	dm                                 *services.DepsManager
}

// newSharedInstallFixture links two worktrees, main and feat, to one install.
func newSharedInstallFixture(t *testing.T) sharedInstallFixture {
	t.Helper()
	hopspace := setupDepsTestDir(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hopspace, npmLikePM(log))
	main, key := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "lockfileVersion: 6\n")
	feat, _ := newWorktree(t, filepath.Join(hopspace, "hops", "feat"), "lockfileVersion: 6\n")
	require.NoError(t, dm.EnsureDeps(main, "main"))
	require.NoError(t, dm.EnsureDeps(feat, "feat"))
	require.Equal(t, 1, installCount(t, log), "feat reuses main's install")
	install := filepath.Join(services.DepsStorePath(hopspace), key)
	return sharedInstallFixture{hopspace: hopspace, main: main, feat: feat, install: install, log: log, dm: dm}
}

func (f sharedInstallFixture) worktrees() map[string]string {
	return map[string]string{"main": f.main, "feat": f.feat}
}

// emptyThroughLink removes the entries of the worktree's DepsDir through
// its link, as npm ci does, keeping those for which keep is true.
func emptyThroughLink(t *testing.T, worktree string, keep func(name string) bool) {
	t.Helper()
	nm := filepath.Join(worktree, "node_modules")
	entries, err := os.ReadDir(nm)
	require.NoError(t, err)
	for _, e := range entries {
		if keep != nil && keep(e.Name()) {
			continue
		}
		require.NoError(t, os.RemoveAll(filepath.Join(nm, e.Name())))
	}
}

func hidden(name string) bool { return name[0] == '.' }

func TestDepsIntegrity_EnsureDepsReinstallsEmptiedInstall(t *testing.T) {
	for name, keep := range map[string]func(string) bool{
		"npm ci":                nil,
		"rm -rf node_modules/*": hidden,
		"one package removed":   func(name string) bool { return name != "a" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newSharedInstallFixture(t)
			emptyThroughLink(t, f.feat, keep)

			require.NoError(t, f.dm.EnsureDeps(f.main, "main"))

			assert.Equal(t, 2, installCount(t, f.log), "the damaged install is reinstalled")
			assertLinkedTo(t, f.main, f.install)
			assertLinkedTo(t, f.feat, f.install)
			assert.FileExists(t, filepath.Join(f.feat, "node_modules", "a", "index.js"),
				"every worktree linked to the install gets it back")
		})
	}
}

// Entries added to an install after it was made (a postinstall step, a
// tool's cache) do not make it look damaged.
func TestDepsIntegrity_AddedEntriesKeepInstallIntact(t *testing.T) {
	f := newSharedInstallFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.install, ".cache"), 0o755))

	require.NoError(t, f.dm.EnsureDeps(f.main, "main"))
	issues, err := f.dm.Audit(f.worktrees())
	require.NoError(t, err)

	assert.Equal(t, 1, installCount(t, f.log))
	assert.Empty(t, issues)
}

func TestDepsIntegrity_DoctorReportsAndFixesDamagedInstall(t *testing.T) {
	f := newSharedInstallFixture(t)
	emptyThroughLink(t, f.feat, hidden)

	issues, err := f.dm.Audit(f.worktrees())
	require.NoError(t, err)
	require.Len(t, issues, 2, "each worktree linked to the damaged install")
	for _, issue := range issues {
		assert.Equal(t, services.IssueDamagedInstall, issue.Type, issue.Branch)
		assert.Equal(t, services.SeverityError, issue.Type.Severity())
		assert.Equal(t, f.install, issue.SymlinkTarget)
	}

	require.NoError(t, f.dm.Fix(issues, false))

	assert.Equal(t, 2, installCount(t, f.log), "one reinstall repairs both worktrees")
	assert.FileExists(t, filepath.Join(f.main, "node_modules", "a", "index.js"))
	issues, err = f.dm.Audit(f.worktrees())
	require.NoError(t, err)
	assert.Empty(t, issues)
}

// An install made before git-hop recorded entries has no record: one with
// only hidden entries left is taken as damaged, one with packages as intact.
func TestDepsIntegrity_UnrecordedInstall(t *testing.T) {
	for name, tc := range map[string]struct {
		keep     func(string) bool
		installs int
	}{
		"packages left": {keep: func(string) bool { return true }, installs: 1},
		"only hidden":   {keep: hidden, installs: 2},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSharedInstallFixture(t)
			require.NoError(t, os.Remove(services.InstallManifestPath(f.install)))
			emptyThroughLink(t, f.feat, tc.keep)

			require.NoError(t, f.dm.EnsureDeps(f.main, "main"))

			assert.Equal(t, tc.installs, installCount(t, f.log))
		})
	}
}

// gc takes an install's entry record with it.
func TestDepsIntegrity_GCRemovesManifest(t *testing.T) {
	f := newSharedInstallFixture(t)
	manifest := services.InstallManifestPath(f.install)
	require.FileExists(t, manifest)
	require.NoError(t, os.Remove(filepath.Join(f.main, "node_modules")))
	require.NoError(t, os.Remove(filepath.Join(f.feat, "node_modules")))

	_, _, err := f.dm.GarbageCollect(f.worktrees(), false)
	require.NoError(t, err)

	assert.NoFileExists(t, manifest)
	assert.NoDirExists(t, filepath.Dir(f.install))
}
