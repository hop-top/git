package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// A function handed an afero.Fs acts on that filesystem and no other: an
// in-memory one in tests, a read-only or copy-on-write view in a dry run.

// Fork-attach creates the fork hopspace on the filesystem it was given.
// It used to create it on the real disk while reading it back from the
// injected one, which then had no such directory, so the fresh hopspace
// was taken for an existing one and loading its config failed.
func TestForkAttach_CreatesForkHopspaceOnInjectedFs(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)
	fs := afero.NewMemMapFs()
	hubPath := "/hub"

	hub, err := hop.CreateHub(fs, hubPath, "https://github.com/org/repo.git", "org", "repo", "main")
	require.NoError(t, err)
	require.NoError(t, hub.AddBranch("main", "main", "hops/main"))
	require.NoError(t, fs.MkdirAll(filepath.Join(hubPath, "hops", "main"), 0o755))

	g := mocks.NewMockGit()
	_, err = hop.ForkAttach(fs, g, "https://github.com/forker/repo.git", "feat", hubPath)
	require.NoError(t, err)

	forkHopspace := hop.GetHopspacePath(dataHome, hop.RepoRef{Org: "forker", Repo: "repo"})
	ok, err := afero.Exists(fs, filepath.Join(forkHopspace, "hop.json"))
	require.NoError(t, err)
	assert.True(t, ok, "fork hopspace config must be written to the injected fs")

	_, err = os.Stat(forkHopspace)
	assert.True(t, os.IsNotExist(err), "fork hopspace must not be created on the real disk: %v", err)
}

// IsWorktree reads .git from the filesystem it was given.
func TestIsWorktree_ReadsInjectedFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/wt/.git", []byte("gitdir: /hub/.git/worktrees/wt\n"), 0o644))

	assert.True(t, hop.IsWorktree(fs, "/wt"))
}

// git follows a symlinked .git and judges the target: a gitfile naming a
// git dir is a linked worktree, a directory is an ordinary repository, and
// anything else is not a repository at all. A symlink alone does not make
// a worktree. The same holds for a symlink to the worktree itself.
func TestIsWorktree_SymlinkIsJudgedByTarget(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()

	gitfile := filepath.Join(root, "gitfile")
	require.NoError(t, os.WriteFile(gitfile, []byte("gitdir: /hub/.git/worktrees/wt\n"), 0o644))
	junk := filepath.Join(root, "junk")
	require.NoError(t, os.WriteFile(junk, []byte("not a gitfile\n"), 0o644))
	gitdir := filepath.Join(root, "gitdir")
	require.NoError(t, os.Mkdir(gitdir, 0o755))

	for _, tc := range []struct {
		name   string
		target string
		want   bool
	}{
		{"to-gitfile", gitfile, true},
		{"to-directory", gitdir, false},
		{"to-other-file", junk, false},
		{"dangling", filepath.Join(root, "missing"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(root, tc.name)
			require.NoError(t, os.Mkdir(dir, 0o755))
			require.NoError(t, os.Symlink(tc.target, filepath.Join(dir, ".git")))

			assert.Equal(t, tc.want, hop.IsWorktree(fs, dir))
		})
	}

	t.Run("symlinked-worktree-dir", func(t *testing.T) {
		wt := filepath.Join(root, "wt")
		require.NoError(t, os.Mkdir(wt, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /hub/.git/worktrees/wt\n"), 0o644))
		link := filepath.Join(root, "wt-link")
		require.NoError(t, os.Symlink(wt, link))

		assert.True(t, hop.IsWorktree(fs, link))
	})
}

// The hops registry is read from the filesystem it is later saved to.
func TestLoadRegistry_ReadsInjectedFs(t *testing.T) {
	withHumanMode(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fs := afero.NewMemMapFs()
	raw := `{"hops": {"acme/widget:main": {"repo": "acme/widget", "branch": "main", "path": "/hub/hops/main"}}}`
	require.NoError(t, afero.WriteFile(fs, hop.GetHopsRegistryPath(), []byte(raw), 0o644))

	r := hop.LoadRegistry(fs)

	e, ok := r.Config.Hops["acme/widget:main"]
	assert.True(t, ok, "registry entry must be read from the injected fs")
	assert.Equal(t, "/hub/hops/main", e.Path)
}

// currentHub makes <root>/hub/hops/main on the real disk.
func currentHub(t *testing.T) (root, hub, worktree string) {
	t.Helper()
	root = t.TempDir()
	hub = filepath.Join(root, "hub")
	worktree = filepath.Join(hub, "hops", "main")
	require.NoError(t, os.MkdirAll(worktree, 0o755))
	return root, hub, worktree
}

// A read-only filesystem refuses the current symlink; the link must not
// appear on disk behind its back.
func TestUpdateCurrentSymlink_WritesThroughInjectedFs(t *testing.T) {
	_, hub, worktree := currentHub(t)
	fs := afero.NewReadOnlyFs(afero.NewOsFs())

	assert.Error(t, hop.UpdateCurrentSymlink(fs, hub, worktree))

	_, err := os.Lstat(filepath.Join(hub, "current"))
	assert.True(t, os.IsNotExist(err), "current symlink written past a read-only fs: %v", err)
}

// A read-only filesystem refuses to remove the current symlink; the link
// must survive on disk.
func TestRemoveCurrentSymlink_RemovesThroughInjectedFs(t *testing.T) {
	_, hub, _ := currentHub(t)
	link := filepath.Join(hub, "current")
	require.NoError(t, os.Symlink(filepath.Join("hops", "main"), link))
	fs := afero.NewReadOnlyFs(afero.NewOsFs())

	assert.Error(t, hop.RemoveCurrentSymlink(fs, hub))

	_, err := os.Lstat(link)
	assert.NoError(t, err, "current symlink removed past a read-only fs")
}

// The current symlink is read at the hub path as the injected fs names it.
func TestGetCurrentSymlink_ReadsInjectedFs(t *testing.T) {
	root, hub, _ := currentHub(t)
	require.NoError(t, os.Symlink(filepath.Join("hops", "main"), filepath.Join(hub, "current")))
	fs := afero.NewBasePathFs(afero.NewOsFs(), root)

	target, err := hop.GetCurrentSymlink(fs, "/hub")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("hops", "main"), target)
}
