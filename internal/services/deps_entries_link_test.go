package services_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// relinkFixture is worktree main linked entry by entry into the install
// for its lockfile ("old"), with a second install in the store ("new")
// for the lockfile it changes to.
type relinkFixture struct {
	entryLinksFixture
	newKey, newInstall string
	oldBefore          map[string]string
}

func newRelinkFixture(t *testing.T) relinkFixture {
	t.Helper()
	f := newEntryLinksFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.install, ".yarn-integrity"), []byte("old\n"), 0o644))
	require.NoError(t, f.dm.LayEntryLinksForTest(f.install, f.nm))
	_, newKey := newWorktree(t, filepath.Join(f.hopspace, "hops", "scratch"), "lockfileVersion: 9\n")
	newInstall := writeInstall(t, f.hopspace, newKey)
	for _, rel := range []string{"c/index.js", "@s/z/index.js"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(newInstall, rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(newInstall, rel), []byte("new\n"), 0o644))
	}
	return relinkFixture{entryLinksFixture: f, newKey: newKey, newInstall: newInstall, oldBefore: snapshotTree(t, f.install)}
}

func (f relinkFixture) changeLockfile(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(f.wt, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644))
}

// A lockfile change whose install the store has relinks the worktree in
// place: store links and copied files replaced, the worktree's own
// entries kept, no install written.
func TestDepsEntriesLink_RelinkInPlaceKeepsWorktreeEntries(t *testing.T) {
	f := newRelinkFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.nm, ".vite", "deps"), 0o755))
	elsewhere := t.TempDir()
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(f.nm, "linked-dev")))
	newBefore := snapshotTree(t, f.newInstall)
	f.changeLockfile(t)

	require.NoError(t, f.dm.EnsureDeps(f.wt, "main"))

	assertEntryLinked(t, f.wt, f.newInstall)
	for _, rel := range []string{"@s/x", "@s/z", ".bin/cli"} {
		target, err := os.Readlink(filepath.Join(f.nm, rel))
		require.NoError(t, err, rel)
		assert.Equal(t, filepath.Join(f.newInstall, rel), target)
	}
	assert.DirExists(t, filepath.Join(f.nm, ".vite", "deps"), "a tool cache stays")
	target, err := os.Readlink(filepath.Join(f.nm, "linked-dev"))
	require.NoError(t, err)
	assert.Equal(t, elsewhere, target, "a link outside the store stays")
	assert.NoFileExists(t, filepath.Join(f.nm, ".yarn-integrity"), "a file copied from the old install goes")
	assert.Equal(t, f.oldBefore, snapshotTree(t, f.install))
	assert.Equal(t, newBefore, snapshotTree(t, f.newInstall))
	assert.Equal(t, []string{"main"}, f.dm.Registry.Entries[f.newKey].UsedBy)
}

// A complete layout is left as it is: nothing relinked, no copied file
// rewritten over the package manager's own.
func TestDepsEntriesLink_CompleteLayoutLeftAlone(t *testing.T) {
	f := newEntryLinksFixture(t)
	lock := filepath.Join(f.nm, ".package-lock.json")
	require.NoError(t, os.WriteFile(lock, []byte("npm wrote this\n"), 0o644))

	require.NoError(t, f.dm.EnsureDeps(f.wt, "main"))

	data, err := os.ReadFile(lock)
	require.NoError(t, err)
	assert.Equal(t, "npm wrote this\n", string(data))
	assert.Equal(t, []string{"main"}, f.dm.Registry.Entries[f.key].UsedBy)
}

// doctor --fix lays missing and dangling links again, without reinstalling.
func TestDepsEntriesLink_FixRelaysMissingLinks(t *testing.T) {
	f := newEntryLinksFixture(t)
	before := snapshotTree(t, f.install)
	require.NoError(t, os.Remove(filepath.Join(f.nm, "a")))
	require.NoError(t, os.RemoveAll(filepath.Join(f.nm, "@s")))
	require.NoError(t, os.Remove(filepath.Join(f.nm, "b")))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "gone"), filepath.Join(f.nm, "b")))

	issues := f.audit(t)
	require.Len(t, issues, 1)
	require.NoError(t, f.dm.Fix(issues, false))

	assertEntryLinked(t, f.wt, f.install)
	assert.Empty(t, f.audit(t))
	assert.Equal(t, before, snapshotTree(t, f.install))
}

// npm install in the worktree replaced links with package directories:
// --fix trashes the folder and relinks, and the trash holds no links into
// the store (a copy to the trash would follow them).
func TestDepsEntriesLink_FixRelinksLocalFolder(t *testing.T) {
	f := newEntryLinksFixture(t)
	before := snapshotTree(t, f.install)
	require.NoError(t, os.Remove(filepath.Join(f.nm, "a")))
	require.NoError(t, os.MkdirAll(filepath.Join(f.nm, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.nm, "a", "index.js"), []byte("local\n"), 0o644))

	issues := f.audit(t)
	require.Len(t, issues, 1)
	require.Equal(t, services.IssueLocalFolder, issues[0].Type)
	require.NoError(t, f.dm.Fix(issues, false))

	assertEntryLinked(t, f.wt, f.install)
	assert.Empty(t, f.audit(t))
	assert.Equal(t, before, snapshotTree(t, f.install))
	trashed := trashedPaths(t)
	assert.Contains(t, strings.Join(trashed, "\n"), filepath.Join("node_modules", "a", "index.js"))
	for _, path := range trashed {
		if target, err := os.Readlink(path); err == nil {
			assert.NotContains(t, target, services.DepsStorePath(f.hopspace), "trash holds a store link: %s", path)
		}
	}
}

// trashedPaths lists everything in git-hop's trash.
func trashedPaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	root := filepath.Join(os.Getenv("GIT_HOP_DATA_HOME"), "backups")
	_ = filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err == nil {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}

// A lockfile change whose install fails puts the links back into the
// install the worktree used.
func TestDepsEntriesLink_FailedInstallRestoresLinks(t *testing.T) {
	for name, cmd := range map[string][]string{
		"install fails":  {"sh", "-c", "mkdir -p node_modules/partial && exit 3"},
		"binary missing": {"git-hop-test-no-such-installer"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newEntryLinksFixture(t)
			before := snapshotTree(t, f.install)
			pm := pnpmPM()
			pm.InstallCmd = cmd
			dm := newStoreManager(t, f.hopspace, pm)
			require.NoError(t, os.WriteFile(filepath.Join(f.wt, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644))

			err := dm.EnsureDeps(f.wt, "main")

			if name == "binary missing" {
				require.NoError(t, err, "a missing package manager is skipped")
			} else {
				require.Error(t, err)
			}
			assertEntryLinked(t, f.wt, f.install)
			assert.NoDirExists(t, filepath.Join(f.nm, "partial"))
			assert.Equal(t, before, snapshotTree(t, f.install))
		})
	}
}
