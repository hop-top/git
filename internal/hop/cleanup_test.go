package hop

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/git"
)

func TestRemoveEmptyDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	cleanup := NewCleanupManager(fs, git.New())

	empty := "/path/to/empty"
	require.NoError(t, fs.MkdirAll(empty, 0755))
	require.NoError(t, cleanup.RemoveEmptyDirectory(empty))
	exists, err := afero.Exists(fs, empty)
	require.NoError(t, err)
	assert.False(t, exists, "an empty directory is removed")

	assert.NoError(t, cleanup.RemoveEmptyDirectory("/path/to/nonexistent"), "one already gone is not an error")
}

// A directory with anything in it, however deep, and a file are refused
// and left exactly as they were.
func TestRemoveEmptyDirectory_RefusesAnythingElse(t *testing.T) {
	fs := afero.NewMemMapFs()
	cleanup := NewCleanupManager(fs, git.New())

	files := []string{"/o/top/keep.txt", "/o/deep/a/b/keep.txt", "/o/dot/.keep", "/o/file"}
	for _, f := range files {
		require.NoError(t, fs.MkdirAll(filepath.Dir(f), 0755))
		require.NoError(t, afero.WriteFile(fs, f, []byte("work"), 0644))
	}
	require.NoError(t, fs.MkdirAll("/o/nested/empty", 0755))

	for _, p := range []string{"/o/top", "/o/deep", "/o/dot", "/o/file", "/o/nested"} {
		err := cleanup.RemoveEmptyDirectory(p)
		assert.ErrorContains(t, err, "is not an empty directory", p)
		exists, _ := afero.Exists(fs, p)
		assert.True(t, exists, "%s must survive", p)
	}
	for _, f := range files {
		got, err := afero.ReadFile(fs, f)
		require.NoError(t, err, "%s must survive", f)
		assert.Equal(t, "work", string(got))
	}
	exists, _ := afero.DirExists(fs, "/o/nested/empty")
	assert.True(t, exists, "an empty directory inside one that is refused survives too")
}

// MockCommandRunner is a mock implementation of CommandRunner for testing
type MockCommandRunner struct {
	RunInDirFunc func(dir string, cmd string, args ...string) (string, error)
}

func (m *MockCommandRunner) Run(cmd string, args ...string) (string, error) {
	return m.RunInDir("", cmd, args...)
}

func (m *MockCommandRunner) RunInDir(dir string, cmd string, args ...string) (string, error) {
	if m.RunInDirFunc != nil {
		return m.RunInDirFunc(dir, cmd, args...)
	}
	return "", nil
}

// TestPruneWorktrees: git worktree prune runs in the hub, the one
// repository the hub owns, never in a worktree a shared --global
// hopspace records (another hub's repository, as like as not).
func TestPruneWorktrees(t *testing.T) {
	fs := afero.NewMemMapFs()
	var dirs []string
	var args [][]string
	g := &git.Git{Runner: &MockCommandRunner{
		RunInDirFunc: func(dir string, cmd string, a ...string) (string, error) {
			dirs = append(dirs, dir)
			args = append(args, append([]string{cmd}, a...))
			return "", nil
		},
	}}
	require.NoError(t, fs.MkdirAll("/g1/app/hops/main", 0o755))

	require.NoError(t, NewCleanupManager(fs, g).PruneWorktrees("/g1/app"))

	assert.Equal(t, []string{"/g1/app"}, dirs)
	assert.Equal(t, [][]string{{"git", "worktree", "prune"}}, args)
}

// TestPruneWorktrees_MissingHub: a hub that is not on disk has nothing
// to prune, and git is never run.
func TestPruneWorktrees_MissingHub(t *testing.T) {
	g := &git.Git{Runner: &MockCommandRunner{
		RunInDirFunc: func(dir string, cmd string, args ...string) (string, error) {
			t.Errorf("git ran in %s", dir)
			return "", nil
		},
	}}
	assert.NoError(t, NewCleanupManager(afero.NewMemMapFs(), g).PruneWorktrees("/gone"))
}

// TestRemoveEmptyParent tests that the parent directory is removed when empty.
func TestRemoveEmptyParent(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	cleanup := NewCleanupManager(fs, g)

	hubPath := "/hub"
	worktreePath := "/hub/fix/my-branch"
	parentDir := "/hub/fix"

	// Create hub dir and parent dir (worktree already removed, parent is now empty).
	require.NoError(t, fs.MkdirAll(hubPath, 0755))
	require.NoError(t, fs.MkdirAll(parentDir, 0755))

	err := cleanup.RemoveEmptyParent(worktreePath, hubPath)
	assert.NoError(t, err)

	exists, _ := afero.DirExists(fs, parentDir)
	assert.False(t, exists, "empty parent dir should be removed")
}

// TestRemoveEmptyParent_Siblings tests that the parent is kept when siblings exist.
func TestRemoveEmptyParent_Siblings(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	cleanup := NewCleanupManager(fs, g)

	hubPath := "/hub"
	worktreePath := "/hub/fix/my-branch"
	parentDir := "/hub/fix"
	siblingDir := "/hub/fix/other-branch"

	require.NoError(t, fs.MkdirAll(hubPath, 0755))
	require.NoError(t, fs.MkdirAll(parentDir, 0755))
	require.NoError(t, fs.MkdirAll(siblingDir, 0755))

	err := cleanup.RemoveEmptyParent(worktreePath, hubPath)
	assert.NoError(t, err)

	exists, _ := afero.DirExists(fs, parentDir)
	assert.True(t, exists, "parent with siblings should not be removed")
}

// TestRemoveEmptyParent_HubRoot tests that the hub root is never removed.
func TestRemoveEmptyParent_HubRoot(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	cleanup := NewCleanupManager(fs, g)

	hubPath := "/hub"
	// worktree is a direct child of hub (no prefix subdir)
	worktreePath := "/hub/main"

	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	err := cleanup.RemoveEmptyParent(worktreePath, hubPath)
	assert.NoError(t, err)

	exists, _ := afero.DirExists(fs, hubPath)
	assert.True(t, exists, "hub root must not be removed")
}

// TestRemoveEmptyParent_NonExistentParent treats missing parent as no-op.
func TestRemoveEmptyParent_NonExistentParent(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()
	cleanup := NewCleanupManager(fs, g)

	hubPath := "/hub"
	worktreePath := "/hub/feat/gone-branch"

	require.NoError(t, fs.MkdirAll(hubPath, 0755))
	// parent /hub/feat does NOT exist

	err := cleanup.RemoveEmptyParent(worktreePath, hubPath)
	assert.NoError(t, err)
}
