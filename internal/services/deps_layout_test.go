package services_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// Node finds a package's dependencies by walking up from the package's
// real path (symlinks resolved) to each directory named node_modules. A
// shared install whose own directory is named node_modules.<hash> is
// never one of those, so a package inside it cannot import its siblings.
// The store therefore keeps each install in a directory named like the
// DepsDir it stands in for: <store>/<hash>/<DepsDir>.

// writerPM is a manager detected by lockfile pnpm-lock.yaml whose install
// runs script with cwd = the worktree, as npm and pnpm do.
func writerPM(depsDir, script string) services.PackageManager {
	return services.PackageManager{
		Name:        "pnpm",
		DetectFiles: []string{"pnpm-lock.yaml"},
		LockFiles:   []string{"pnpm-lock.yaml"},
		DepsDir:     depsDir,
		InstallCmd:  []string{"sh", "-c", script},
	}
}

func TestDepsLayout_LinkTargetNamedLikeDepsDir(t *testing.T) {
	for _, depsDir := range []string{"node_modules", "vendor/bundle"} {
		t.Run(depsDir, func(t *testing.T) {
			hopspace := setupDepsTestDir(t)
			pm := writerPM(depsDir, "mkdir -p "+depsDir+" && echo fresh > "+depsDir+"/marker")
			dm := newStoreManager(t, hopspace, pm)
			wt, _ := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "lockfileVersion: 6\n")
			hash, err := pm.HashLockfile(afero.NewOsFs(), filepath.Join(wt, "pnpm-lock.yaml"))
			require.NoError(t, err)
			key := pm.GetDepsKey(hash)

			require.NoError(t, dm.EnsureDeps(wt, "main"))

			link, err := os.Readlink(filepath.Join(wt, depsDir, "marker"))
			require.NoError(t, err, "worktree %s/marker must be a link", depsDir)
			install := filepath.Dir(link)
			assert.Equal(t, filepath.Base(depsDir), filepath.Base(install),
				"the install the links point into is named like the worktree's directory")
			assert.Equal(t, filepath.Join(services.DepsStorePath(hopspace), hash, depsDir), install)
			assert.FileExists(t, link)
			assert.Contains(t, dm.Registry.Entries, key)
			assert.Equal(t, []string{"main"}, dm.Registry.Entries[key].UsedBy)
		})
	}
}

// esmInstall installs two ESM packages, a importing b, the way npm lays
// out a flat tree: both at the top of ./node_modules.
const esmInstall = `set -e
mkdir -p node_modules/a node_modules/b
printf '{"name":"a","version":"1.0.0","type":"module","exports":"./index.js"}' > node_modules/a/package.json
printf 'import { b } from "b";\nexport const a = () => "a+" + b();\n' > node_modules/a/index.js
printf '{"name":"b","version":"1.0.0","type":"module","exports":"./index.js"}' > node_modules/b/package.json
printf 'export const b = () => "b";\n' > node_modules/b/index.js
`

// A package in the shared install imports its sibling from a worktree
// linked to it. Fails with ERR_MODULE_NOT_FOUND when the install sits in
// a directory not named node_modules.
func TestDepsLayout_ESMResolvesFromSharedInstall(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	hopspace := setupDepsTestDir(t)
	dm := newStoreManager(t, hopspace, writerPM("node_modules", esmInstall))
	wt, _ := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "lockfileVersion: 6\n")
	require.NoError(t, os.WriteFile(filepath.Join(wt, "package.json"),
		[]byte(`{"name":"proj","private":true,"type":"module"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wt, "index.js"),
		[]byte(`import { a } from "a"; console.log(a());`), 0o644))

	require.NoError(t, dm.EnsureDeps(wt, "main"))

	cmd := exec.Command(node, "index.js")
	cmd.Dir = wt
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "node index.js:\n%s", out)
	assert.Equal(t, "a+b\n", string(out))
}

// flatKey returns the key earlier releases gave the install key names:
// "node_modules.<hash>" for "<hash>/node_modules".
func flatKey(key string) string {
	hash, _, _ := strings.Cut(key, "/")
	return "node_modules." + hash
}

// snapshotTree maps every path below root to its content ("<dir>" for a
// directory, "-> target" for a symlink), to show a tree was not written.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			snap[rel] = "-> " + target
			return err
		case info.IsDir():
			snap[rel] = "<dir>"
		default:
			data, err := os.ReadFile(path)
			snap[rel] = string(data)
			return err
		}
		return nil
	}))
	return snap
}

func installCount(t *testing.T, log string) int {
	t.Helper()
	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(data), "install\n")
}

func assertLinkedTo(t *testing.T, worktree, want string) {
	t.Helper()
	target, err := os.Readlink(filepath.Join(worktree, "node_modules"))
	require.NoError(t, err, "worktree node_modules must still be a symlink")
	assert.Equal(t, want, target)
}

// oldLayoutFixture is a worktree linked, as an earlier release left it, to
// a populated install in the old layout in the hopspace's own store.
type oldLayoutFixture struct {
	hopspace, worktree, key, oldInstall string
	before                              map[string]string
}

func newOldLayoutFixture(t *testing.T) oldLayoutFixture {
	t.Helper()
	hopspace := setupDepsTestDir(t)
	wt, key := newWorktree(t, filepath.Join(hopspace, "hops", "old"), "lockfileVersion: 6\n")
	old := filepath.Join(services.DepsStorePath(hopspace), flatKey(key))
	require.NoError(t, os.MkdirAll(filepath.Join(old, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(old, "pkg", "index.js"), []byte("old\n"), 0o644))
	require.NoError(t, os.Symlink(old, filepath.Join(wt, "node_modules")))
	return oldLayoutFixture{hopspace: hopspace, worktree: wt, key: key, oldInstall: old, before: snapshotTree(t, old)}
}

func (f oldLayoutFixture) assertOldInstallUntouched(t *testing.T) {
	t.Helper()
	assert.Equal(t, f.before, snapshotTree(t, f.oldInstall), "the old install is never written")
}

// The next install for a worktree linked to an old-layout install (env
// start) relinks it to an install in the new layout, once that is there.
func TestDepsLayout_OldLayoutLink_RelinkedAfterInstall(t *testing.T) {
	f := newOldLayoutFixture(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, f.hopspace, installLoggingPM(log))

	require.NoError(t, dm.EnsureDeps(f.worktree, "old"))

	assertStoreAt(t, f.hopspace, f.worktree, f.key, services.DepsStorePath(f.hopspace))
	assert.Equal(t, 1, installCount(t, log))
	assert.Equal(t, []string{"old"}, dm.Registry.Entries[f.key].UsedBy)
	f.assertOldInstallUntouched(t)
}

// A failed install leaves the worktree linked as it was: the old link is
// put back, and nothing the install left behind stays in its place.
func TestDepsLayout_OldLayoutLink_KeptWhenInstallFails(t *testing.T) {
	f := newOldLayoutFixture(t)
	failing := writerPM("node_modules", "mkdir -p node_modules && echo partial > node_modules/partial && exit 3")
	dm := newStoreManager(t, f.hopspace, failing)

	err := dm.EnsureDeps(f.worktree, "old")

	require.Error(t, err)
	assertLinkedTo(t, f.worktree, f.oldInstall)
	assert.NoDirExists(t, filepath.Join(services.DepsStorePath(f.hopspace), f.key), "no install in the new layout")
	f.assertOldInstallUntouched(t)
}

// A package manager missing from PATH is skipped silently, as before; the
// link it would have replaced stays.
func TestDepsLayout_OldLayoutLink_KeptWhenBinaryMissing(t *testing.T) {
	f := newOldLayoutFixture(t)
	missing := writerPM("node_modules", "")
	missing.InstallCmd = []string{"git-hop-test-no-such-installer"}
	dm := newStoreManager(t, f.hopspace, missing)

	require.NoError(t, dm.EnsureDeps(f.worktree, "old"))

	assertLinkedTo(t, f.worktree, f.oldInstall)
	f.assertOldInstallUntouched(t)
}

// doctor reports an old-layout link as a warning; --fix relinks it only
// once the new install succeeds.
func TestDepsLayout_DoctorFixRelinksOldLayout(t *testing.T) {
	f := newOldLayoutFixture(t)
	worktrees := map[string]string{"old": f.worktree}

	failing := newStoreManager(t, f.hopspace, writerPM("node_modules", "exit 3"))
	issues, err := failing.Audit(worktrees)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueOldLayout, issues[0].Type)
	assert.Equal(t, services.SeverityWarning, issues[0].Type.Severity())
	assert.Equal(t, f.oldInstall, issues[0].SymlinkTarget)

	require.Error(t, failing.Fix(issues, false))
	assertLinkedTo(t, f.worktree, f.oldInstall)

	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, f.hopspace, installLoggingPM(log))
	issues, err = dm.Audit(worktrees)
	require.NoError(t, err)
	require.NoError(t, dm.Fix(issues, false))
	assertStoreAt(t, f.hopspace, f.worktree, f.key, services.DepsStorePath(f.hopspace))

	issues, err = dm.Audit(worktrees)
	require.NoError(t, err)
	assert.Empty(t, issues)
	f.assertOldInstallUntouched(t)
}

// gc removes an old-layout install only once no worktree of any hub links
// into it, and never without knowing every hub's worktrees.
func TestDepsLayout_GCOldLayoutInstallOnlyOnceUnlinked(t *testing.T) {
	f := newOldLayoutFixture(t)
	// The link belongs to another hub's worktree: this hub's worktree
	// map does not have it.
	otherHub := f.worktree
	self, _ := newWorktree(t, filepath.Join(f.hopspace, "hops", "self"), "lockfileVersion: 9\n")
	worktrees := map[string]string{"self": self}
	old := flatKey(f.key)

	dm := newStoreManager(t, f.hopspace, pnpmPM())
	dm.Registry.AddUsage(old, "old")

	orphaned, _, err := dm.GarbageCollect(worktrees, false)
	require.NoError(t, err)
	assert.NotContains(t, orphaned, old, "not collected without every hub's worktrees")
	assert.DirExists(t, f.oldInstall)

	dm.SetLinkScope([]string{otherHub})
	orphaned, _, err = dm.GarbageCollect(worktrees, false)
	require.NoError(t, err)
	assert.NotContains(t, orphaned, old, "another hub still links it")
	f.assertOldInstallUntouched(t)

	require.NoError(t, os.Remove(filepath.Join(otherHub, "node_modules")))
	orphaned, _, err = dm.GarbageCollect(worktrees, false)
	require.NoError(t, err)
	assert.Contains(t, orphaned, old)
	assert.NoDirExists(t, f.oldInstall)
	assert.NotContains(t, dm.Registry.Entries, old)
}

// gc of an install in the new layout takes its hash directory with it.
func TestDepsLayout_GCRemovesHashDirectory(t *testing.T) {
	hopspace := setupDepsTestDir(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hopspace, installLoggingPM(log))
	wt, key := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "lockfileVersion: 6\n")
	require.NoError(t, dm.EnsureDeps(wt, "main"))
	require.NoError(t, os.RemoveAll(filepath.Join(wt, "node_modules")))

	orphaned, _, err := dm.GarbageCollect(map[string]string{"main": wt}, false)
	require.NoError(t, err)

	assert.Equal(t, []string{key}, orphaned)
	hash, _, _ := strings.Cut(key, "/")
	assert.NoDirExists(t, filepath.Join(services.DepsStorePath(hopspace), hash))
	assert.DirExists(t, services.DepsStorePath(hopspace))
}
