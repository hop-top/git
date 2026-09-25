package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// Earlier releases linked a worktree's node_modules to the store install
// as a whole. The next install, env start or doctor --fix converts such a
// link to the per-entry layout into the same install: no reinstall, and
// the install is only read.

type singleLinkFixture struct {
	entryLinksFixture
	log    string
	before map[string]string
}

// newSingleLinkFixture links worktree main's node_modules as a whole to
// the install for its lockfile, with a manager that logs installs.
func newSingleLinkFixture(t *testing.T) singleLinkFixture {
	t.Helper()
	hopspace := setupDepsTestDir(t)
	wt, key := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "lockfileVersion: 6\n")
	install := writeInstall(t, hopspace, key)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hopspace, installLoggingPM(log))
	nm := filepath.Join(wt, "node_modules")
	require.NoError(t, os.Symlink(install, nm))
	return singleLinkFixture{
		entryLinksFixture: entryLinksFixture{hopspace: hopspace, wt: wt, key: key, install: install, nm: nm, dm: dm},
		log:               log,
		before:            snapshotTree(t, install),
	}
}

func (f singleLinkFixture) assertConverted(t *testing.T) {
	t.Helper()
	assertEntryLinked(t, f.wt, f.install)
	assert.Equal(t, 0, installCount(t, f.log), "nothing reinstalled")
	assert.Equal(t, f.before, snapshotTree(t, f.install), "the install is only read")
	assert.Equal(t, []string{"main"}, f.dm.Registry.Entries[f.key].UsedBy)
}

func TestDepsEntriesMigrate_EnsureDepsConvertsSingleLink(t *testing.T) {
	f := newSingleLinkFixture(t)

	require.NoError(t, f.dm.EnsureDeps(f.wt, "main"))

	f.assertConverted(t)
	assert.Empty(t, f.audit(t))
}

// doctor warns about the single link; --fix converts it.
func TestDepsEntriesMigrate_DoctorWarnsFixConverts(t *testing.T) {
	f := newSingleLinkFixture(t)

	issues := f.audit(t)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueSingleLink, issues[0].Type)
	assert.Equal(t, services.SeverityWarning, issues[0].Type.Severity())
	assert.Equal(t, f.key, issues[0].TargetName())

	require.NoError(t, f.dm.Fix(issues, false))

	f.assertConverted(t)
	assert.Empty(t, f.audit(t))
}

// A conversion that fails puts the single link back.
func TestDepsEntriesMigrate_FailedConversionKeepsLink(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	f := newSingleLinkFixture(t)
	unreadable := filepath.Join(f.install, ".package-lock.json")
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	require.Error(t, f.dm.EnsureDeps(f.wt, "main"))

	require.NoError(t, os.Chmod(unreadable, 0o644))
	assertLinkedTo(t, f.wt, f.install)
	assert.Equal(t, f.before, snapshotTree(t, f.install))
	assert.Equal(t, 0, installCount(t, f.log))
}

// A single link to an install that lost entries (emptied through it) is
// reinstalled, and the worktree laid out entry by entry.
func TestDepsEntriesMigrate_DamagedInstallReinstalled(t *testing.T) {
	f := newSingleLinkFixture(t)
	require.NoError(t, os.WriteFile(services.InstallManifestPath(f.install), []byte(`{"entries":["a","b","gone"]}`), 0o644))

	require.NoError(t, f.dm.EnsureDeps(f.wt, "main"))

	assert.Equal(t, 1, installCount(t, f.log))
	assertEntryLinked(t, f.wt, f.install)
	assert.FileExists(t, filepath.Join(f.nm, "marker"))
}
