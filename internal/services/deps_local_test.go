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

// Package managers link file:, link: and workspace dependencies with
// relative links out of node_modules. Moved into the store, such a link
// resolves from the store, not the worktree, so an install with one stays
// in the worktree that made it, marked with the lockfile hash it was made
// for (services.LocalInstallMarker).

// linkingPM installs a package and runs link, a shell command run in
// ./node_modules, logging each install to log.
func linkingPM(log, link string) services.PackageManager {
	return writerPM("node_modules",
		"echo install >> '"+log+"' && mkdir -p node_modules/a && echo a > node_modules/a/index.js"+
			" && (cd node_modules && "+link+")")
}

// workspaceLink links node_modules/util to the worktree's packages/util,
// as npm does for a workspace package.
const workspaceLink = "ln -s ../packages/util util"

type localFixture struct {
	hopspace, log string
	pm            services.PackageManager
	dm            *services.DepsManager
}

func newLocalFixture(t *testing.T, link string) localFixture {
	t.Helper()
	hopspace := setupDepsTestDir(t)
	log := filepath.Join(t.TempDir(), "install.log")
	pm := linkingPM(log, link)
	return localFixture{hopspace: hopspace, log: log, pm: pm, dm: newStoreManager(t, hopspace, pm)}
}

// worktree creates a worktree with packages/util and the given lockfile.
func (f localFixture) worktree(t *testing.T, branch, lock string) (string, string) {
	t.Helper()
	wt := filepath.Join(f.hopspace, "hops", branch)
	require.NoError(t, os.MkdirAll(filepath.Join(wt, "packages", "util"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wt, "packages", "util", "index.js"), []byte(branch), 0o644))
	wt, _ = newWorktree(t, wt, lock)
	hash, err := f.pm.HashLockfile(afero.NewOsFs(), filepath.Join(wt, "pnpm-lock.yaml"))
	require.NoError(t, err)
	return wt, hash
}

func assertLocalInstall(t *testing.T, wt, hash string) {
	t.Helper()
	nm := filepath.Join(wt, "node_modules")
	info, err := os.Lstat(nm)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "node_modules must be a real directory in the worktree")
	marker, err := os.ReadFile(filepath.Join(nm, services.LocalInstallMarker))
	require.NoError(t, err, "a local install is marked")
	assert.Equal(t, hash, strings.TrimSpace(string(marker)))
	assert.FileExists(t, filepath.Join(nm, "a", "index.js"))
}

func assertNotInStore(t *testing.T, hopspace, hash string) {
	t.Helper()
	assert.NoDirExists(t, filepath.Join(services.DepsStorePath(hopspace), hash), "nothing in the store for a local install")
}

func TestDepsLocal_OutwardLinkKeepsInstallInWorktree(t *testing.T) {
	f := newLocalFixture(t, workspaceLink)
	main, hash := f.worktree(t, "main", "lockfileVersion: 6\n")
	feat, _ := f.worktree(t, "feat", "lockfileVersion: 6\n")

	require.NoError(t, f.dm.EnsureDeps(main, "main"))
	require.NoError(t, f.dm.EnsureDeps(feat, "feat"))

	assertLocalInstall(t, main, hash)
	assertLocalInstall(t, feat, hash)
	assertNotInStore(t, f.hopspace, hash)
	assert.Empty(t, f.dm.Registry.Entries, "a local install is not a store install")
	assert.Equal(t, 2, installCount(t, f.log), "each worktree installs its own")
	for wt, branch := range map[string]string{main: "main", feat: "feat"} {
		data, err := os.ReadFile(filepath.Join(wt, "node_modules", "util", "index.js"))
		require.NoError(t, err)
		assert.Equal(t, branch, string(data), "the workspace link resolves in its own worktree")
	}
}

// Which links make an install local: those resolving out of node_modules
// by a relative path, or into the worktree by any path.
func TestDepsLocal_LinkKinds(t *testing.T) {
	outside := t.TempDir()
	for name, tc := range map[string]struct {
		link  string
		local bool
	}{
		"workspace":             {link: workspaceLink, local: true},
		"file: sibling":         {link: "ln -s ../../../../sibling sib", local: true},
		"scoped":                {link: "mkdir @s && ln -s ../../packages/util @s/util", local: true},
		"nested":                {link: "mkdir -p a/node_modules && ln -s ../../../packages/util a/node_modules/util", local: true},
		"dangling outward":      {link: "ln -s ../missing gone", local: true},
		"inside (.bin)":         {link: "mkdir .bin && ln -s ../a/index.js .bin/a", local: false},
		"inside (pnpm)":         {link: "mkdir -p .pnpm/a && ln -s .pnpm/a b", local: false},
		"absolute, outside":     {link: "ln -s '" + outside + "' ext", local: false},
		"absolute, in worktree": {link: "ln -s \"$(cd .. && pwd)/packages/util\" util", local: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLocalFixture(t, tc.link)
			wt, hash := f.worktree(t, "main", "lockfileVersion: 6\n")

			require.NoError(t, f.dm.EnsureDeps(wt, "main"))

			if tc.local {
				assertLocalInstall(t, wt, hash)
				assertNotInStore(t, f.hopspace, hash)
				return
			}
			assertEntryLinked(t, wt, filepath.Join(services.DepsStorePath(f.hopspace), hash, "node_modules"))
			assert.NoFileExists(t, filepath.Join(wt, "node_modules", services.LocalInstallMarker))
		})
	}
}

// A local install marked for the current lockfile is set up: nothing
// reinstalls it, and doctor has nothing to say about it.
func TestDepsLocal_CurrentLocalInstallLeftAlone(t *testing.T) {
	f := newLocalFixture(t, workspaceLink)
	wt, hash := f.worktree(t, "main", "lockfileVersion: 6\n")
	require.NoError(t, f.dm.EnsureDeps(wt, "main"))

	require.NoError(t, f.dm.EnsureDeps(wt, "main"))
	issues, err := f.dm.Audit(map[string]string{"main": wt})
	require.NoError(t, err)
	require.NoError(t, f.dm.Fix(issues, false))

	assert.Empty(t, issues)
	assert.Equal(t, 1, installCount(t, f.log))
	assertLocalInstall(t, wt, hash)
	assert.NoDirExists(t, filepath.Join(os.Getenv("GIT_HOP_DATA_HOME"), "backups"), "nothing trashed")
}

// After a lockfile change a local install is refreshed in place, where
// the package manager updates it, not trashed.
func TestDepsLocal_StaleLocalInstallRefreshedInPlace(t *testing.T) {
	f := newLocalFixture(t, workspaceLink)
	wt, _ := f.worktree(t, "main", "lockfileVersion: 6\n")
	require.NoError(t, f.dm.EnsureDeps(wt, "main"))
	require.NoError(t, os.WriteFile(filepath.Join(wt, "node_modules", ".cache"), []byte("kept"), 0o644))
	lock := filepath.Join(wt, "pnpm-lock.yaml")
	require.NoError(t, os.WriteFile(lock, []byte("lockfileVersion: 7\n"), 0o644))
	hash, err := f.pm.HashLockfile(afero.NewOsFs(), lock)
	require.NoError(t, err)

	issues, err := f.dm.Audit(map[string]string{"main": wt})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueStaleLocal, issues[0].Type)
	assert.Equal(t, services.SeverityWarning, issues[0].Type.Severity())

	require.NoError(t, f.dm.EnsureDeps(wt, "main"))

	assertLocalInstall(t, wt, hash)
	assert.FileExists(t, filepath.Join(wt, "node_modules", ".cache"), "installed over, not trashed")
	assert.Equal(t, 2, installCount(t, f.log))
}

// An install in the worktree git-hop did not mark (npm ci removes the
// marker with everything else) is a warning when it could not be shared
// anyway, and the usual error when it could.
func TestDepsLocal_UnmarkedLocalFolder(t *testing.T) {
	for name, tc := range map[string]struct {
		link string
		want services.IssueType
	}{
		"has outward links": {link: workspaceLink, want: services.IssueStaleLocal},
		"shareable":         {link: "true", want: services.IssueLocalFolder},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLocalFixture(t, tc.link)
			wt, _ := f.worktree(t, "main", "lockfileVersion: 6\n")
			nm := filepath.Join(wt, "node_modules")
			require.NoError(t, os.MkdirAll(filepath.Join(nm, "a"), 0o755))
			if tc.link != "true" {
				require.NoError(t, os.Symlink("../packages/util", filepath.Join(nm, "util")))
			}

			issues, err := f.dm.Audit(map[string]string{"main": wt})
			require.NoError(t, err)

			require.Len(t, issues, 1)
			assert.Equal(t, tc.want, issues[0].Type)
		})
	}
}

// A store install made before installs with outward links were kept local
// breaks every worktree linked to it: doctor reports it, and --fix gives
// the worktree its own install, leaving the store install to gc.
func TestDepsLocal_StoreInstallWithOutwardLinks(t *testing.T) {
	f := newLocalFixture(t, workspaceLink)
	wt, hash := f.worktree(t, "main", "lockfileVersion: 6\n")
	install := filepath.Join(services.DepsStorePath(f.hopspace), hash, "node_modules")
	require.NoError(t, os.MkdirAll(filepath.Join(install, "a"), 0o755))
	require.NoError(t, os.Symlink("../../packages/util", filepath.Join(install, "util")))
	require.NoError(t, os.Symlink(install, filepath.Join(wt, "node_modules")))
	worktrees := map[string]string{"main": wt}

	issues, err := f.dm.Audit(worktrees)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueNeedsLocal, issues[0].Type)
	assert.Equal(t, services.SeverityError, issues[0].Type.Severity())
	assert.Equal(t, services.LocalReasonLinks, issues[0].LocalReason)

	require.NoError(t, f.dm.Fix(issues, false))

	assertLocalInstall(t, wt, hash)
	assert.DirExists(t, filepath.Join(install, "a"), "the store install is left to gc")
	issues, err = f.dm.Audit(worktrees)
	require.NoError(t, err)
	assert.Empty(t, issues)
	orphaned, _, err := f.dm.GarbageCollect(worktrees, false)
	require.NoError(t, err)
	assert.Equal(t, []string{hash + "/node_modules"}, orphaned)
}

// A worktree linked to a store install with outward links is not linked
// again by env start or add: it gets its own install.
func TestDepsLocal_EnsureDepsDoesNotReuseStoreInstallWithOutwardLinks(t *testing.T) {
	f := newLocalFixture(t, workspaceLink)
	wt, hash := f.worktree(t, "main", "lockfileVersion: 6\n")
	install := filepath.Join(services.DepsStorePath(f.hopspace), hash, "node_modules")
	require.NoError(t, os.MkdirAll(filepath.Join(install, "a"), 0o755))
	require.NoError(t, os.Symlink("../../packages/util", filepath.Join(install, "util")))

	require.NoError(t, f.dm.EnsureDeps(wt, "main"))

	assertLocalInstall(t, wt, hash)
	assert.Equal(t, 1, installCount(t, f.log))
}

// A local install whose refresh no longer has outward links (the lockfile
// dropped its workspace) moves to the store like any other, unmarked.
func TestDepsLocal_LocalInstallBecomesShared(t *testing.T) {
	f := newLocalFixture(t, "if grep -q ws ../pnpm-lock.yaml; then "+workspaceLink+"; else rm -f util; fi")
	wt, _ := f.worktree(t, "main", "lockfileVersion: 6\nws: true\n")
	require.NoError(t, f.dm.EnsureDeps(wt, "main"))
	require.FileExists(t, filepath.Join(wt, "node_modules", services.LocalInstallMarker))
	lock := filepath.Join(wt, "pnpm-lock.yaml")
	require.NoError(t, os.WriteFile(lock, []byte("lockfileVersion: 6\n"), 0o644))
	hash, err := f.pm.HashLockfile(afero.NewOsFs(), lock)
	require.NoError(t, err)

	require.NoError(t, f.dm.EnsureDeps(wt, "main"))

	install := filepath.Join(services.DepsStorePath(f.hopspace), hash, "node_modules")
	assertEntryLinked(t, wt, install)
	assert.NoFileExists(t, filepath.Join(install, services.LocalInstallMarker), "the marker stays out of the store")
	assert.FileExists(t, services.InstallManifestPath(install))
}
