package services_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// Yarn Plug'n'Play (yarn 2+ by default) writes .pnp.cjs in the worktree and
// no node_modules: there is nothing to share, and nothing missing.

// pnpPM installs the way yarn's PnP linker does, logging each run.
func pnpPM(log string) services.PackageManager {
	return writerPM("node_modules", "echo install >> '"+log+"' && echo pnp > .pnp.cjs")
}

func TestDepsPnP_NoDepsDirIsNotAnError(t *testing.T) {
	hopspace := setupDepsTestDir(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hopspace, pnpPM(log))
	wt, key := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "__metadata:\n  version: 8\n")

	require.NoError(t, dm.EnsureDeps(wt, "main"))

	assert.Equal(t, 1, installCount(t, log))
	assert.FileExists(t, filepath.Join(wt, ".pnp.cjs"))
	assert.NoFileExists(t, filepath.Join(wt, "node_modules"))
	assert.NoDirExists(t, filepath.Dir(filepath.Join(services.DepsStorePath(hopspace), key)), "nothing in the store")
	assert.Empty(t, dm.Registry.Entries)

	issues, err := dm.Audit(map[string]string{"main": wt})
	require.NoError(t, err)
	assert.Empty(t, issues, "a PnP worktree has no node_modules to miss")
}

// A PnP worktree whose install has not run yet (no .pnp.cjs) is missing
// its deps as before, and --fix installs them.
func TestDepsPnP_MissingUntilInstalled(t *testing.T) {
	hopspace := setupDepsTestDir(t)
	log := filepath.Join(t.TempDir(), "install.log")
	dm := newStoreManager(t, hopspace, pnpPM(log))
	wt, _ := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "__metadata:\n  version: 8\n")
	worktrees := map[string]string{"main": wt}

	issues, err := dm.Audit(worktrees)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueMissingDeps, issues[0].Type)

	require.NoError(t, dm.Fix(issues, false))
	issues, err = dm.Audit(worktrees)
	require.NoError(t, err)
	assert.Empty(t, issues)
	assert.FileExists(t, filepath.Join(wt, ".pnp.cjs"))
}
