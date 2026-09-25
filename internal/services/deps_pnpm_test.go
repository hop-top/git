package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// pnpm refuses to install through a link to a directory outside the
// project (ERR_PNPM_UNSAFE_MODULES_DIR), so a pnpm install is never
// shared: it stays in the worktree like any other local install. pnpm is
// recognised by the .modules.yaml it writes into every node_modules.

// pnpmLikePM installs a package into ./node_modules the way pnpm lays it
// out, with its .modules.yaml.
func pnpmLikePM(log string) services.PackageManager {
	return linkingPM(log, "mkdir -p .pnpm/b/node_modules/b && ln -s .pnpm/b/node_modules/b b && echo layout > .modules.yaml")
}

func TestDepsPnpm_InstalledPerWorktree(t *testing.T) {
	hopspace := setupDepsTestDir(t)
	log := filepath.Join(t.TempDir(), "install.log")
	pm := pnpmLikePM(log)
	f := localFixture{hopspace: hopspace, log: log, pm: pm, dm: newStoreManager(t, hopspace, pm)}
	main, hash := f.worktree(t, "main", "lockfileVersion: 9\n")
	feat, _ := f.worktree(t, "feat", "lockfileVersion: 9\n")

	require.NoError(t, f.dm.EnsureDeps(main, "main"))
	require.NoError(t, f.dm.EnsureDeps(feat, "feat"))
	require.NoError(t, f.dm.EnsureDeps(feat, "feat"))

	assertLocalInstall(t, main, hash)
	assertLocalInstall(t, feat, hash)
	assertNotInStore(t, hopspace, hash)
	assert.Equal(t, 2, installCount(t, log))
	issues, err := f.dm.Audit(map[string]string{"main": main, "feat": feat})
	require.NoError(t, err)
	assert.Empty(t, issues)
}

// A worktree an earlier release linked to a shared pnpm install is an
// error: pnpm cannot install there. --fix gives it its own install.
func TestDepsPnpm_SharedInstallNeedsLocal(t *testing.T) {
	for name, recorded := range map[string]bool{"unrecorded": false, "recorded": true} {
		t.Run(name, func(t *testing.T) {
			hopspace := setupDepsTestDir(t)
			log := filepath.Join(t.TempDir(), "install.log")
			pm := pnpmLikePM(log)
			f := localFixture{hopspace: hopspace, log: log, pm: pm, dm: newStoreManager(t, hopspace, pm)}
			wt, hash := f.worktree(t, "main", "lockfileVersion: 9\n")
			install := filepath.Join(services.DepsStorePath(hopspace), hash, "node_modules")
			require.NoError(t, os.MkdirAll(filepath.Join(install, "a"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(install, ".modules.yaml"), []byte("layout\n"), 0o644))
			if recorded {
				require.NoError(t, os.WriteFile(services.InstallManifestPath(install), []byte(`{"entries":["a"]}`), 0o644))
			}
			require.NoError(t, os.Symlink(install, filepath.Join(wt, "node_modules")))
			worktrees := map[string]string{"main": wt}

			issues, err := f.dm.Audit(worktrees)
			require.NoError(t, err)
			require.Len(t, issues, 1)
			assert.Equal(t, services.IssueNeedsLocal, issues[0].Type)
			assert.Equal(t, services.LocalReasonPnpm, issues[0].LocalReason)

			require.NoError(t, f.dm.Fix(issues, false))
			assertLocalInstall(t, wt, hash)
		})
	}
}
