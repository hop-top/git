package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

// chdir moves the process into dir for the duration of the test. The switch
// path's resolution must not depend on where the process happens to sit, so
// every case below runs from a cwd that is NOT the hub.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

// newHubWithRelativePath builds a real on-disk hub whose hop.json stores a
// RELATIVE worktree path ("hops/main"), the shape `git hop add` writes.
func newHubWithRelativePath(t *testing.T) (fs afero.Fs, hubPath, worktreeAbs string, hub *hop.Hub) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	hubPath = filepath.Join(root, "hub")
	worktreeAbs = filepath.Join(hubPath, "hops", "main")
	require.NoError(t, os.MkdirAll(worktreeAbs, 0o755))

	hub = &hop.Hub{
		Path: hubPath,
		Config: &config.HubConfig{
			Repo: config.RepoConfig{Org: "acme", Repo: "widget"},
			Branches: map[string]config.HubBranch{
				"main": {Path: filepath.Join("hops", "main")},
			},
		},
	}

	return afero.NewOsFs(), hubPath, worktreeAbs, hub
}

// TestSwitchWorktreePathAnchoredOnHub pins the resolution used by the branch
// switch path: a relative hop.json path must resolve against the hub, never
// against the process cwd. Running from an unrelated directory is the exact
// scenario `git hop <branch>` hits when invoked from a subdirectory.
func TestSwitchWorktreePathAnchoredOnHub(t *testing.T) {
	_, hubPath, worktreeAbs, hub := newHubWithRelativePath(t)

	elsewhere, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	chdir(t, elsewhere)

	branch := hub.Config.Branches["main"]

	resolved := resolveSwitchWorktreePath(branch, hubPath)

	assert.Equal(t, worktreeAbs, resolved,
		"relative worktree path must anchor on the hub")
	assert.True(t, filepath.IsAbs(resolved),
		"resolved worktree path must be absolute so Chdir/symlink/hook env agree")
	assert.NotEqual(t, filepath.Join(elsewhere, branch.Path), resolved,
		"resolved worktree path must not anchor on the process cwd")
}

// TestSwitchWorktreePathAbsoluteIsNoOp verifies the documented no-op contract
// the switch path relies on: an absolute hop.json path passes through
// untouched, so hubs storing absolute paths keep working.
func TestSwitchWorktreePathAbsoluteIsNoOp(t *testing.T) {
	abs := filepath.Join(string(filepath.Separator), "srv", "worktrees", "main")
	branch := config.HubBranch{Path: abs}

	assert.Equal(t, abs, resolveSwitchWorktreePath(branch, "/some/other/hub"))
}

// TestResolveSwitchFromStateMatchesRelativeBranchPath guards the from-state
// comparison after the switch path was hub-anchored. The `current` symlink
// target and the registered branch path must be resolved the same way, or the
// reverse lookup silently stops finding the branch when cwd is not the hub.
func TestResolveSwitchFromStateMatchesRelativeBranchPath(t *testing.T) {
	fs, hubPath, worktreeAbs, hub := newHubWithRelativePath(t)

	require.NoError(t, hop.UpdateCurrentSymlink(fs, hubPath, worktreeAbs))

	elsewhere, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	chdir(t, elsewhere)

	fromBranch, fromWorktreePath := resolveSwitchFromState(fs, hubPath, hub)

	assert.Equal(t, "main", fromBranch,
		"current symlink must reverse-map to the registered branch from any cwd")
	assert.Equal(t, worktreeAbs, fromWorktreePath)
}

// TestResolveSwitchFromStateNoSymlink keeps the documented empty-from-state
// behavior: a hub with no `current` symlink is normal, not an error.
func TestResolveSwitchFromStateNoSymlink(t *testing.T) {
	fs, hubPath, _, hub := newHubWithRelativePath(t)

	fromBranch, fromWorktreePath := resolveSwitchFromState(fs, hubPath, hub)

	assert.Empty(t, fromBranch)
	assert.Empty(t, fromWorktreePath)
}
