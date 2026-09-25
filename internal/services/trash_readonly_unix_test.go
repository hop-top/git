//go:build unix

package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipAsRoot skips a test that relies on permission checks, which root
// bypasses.
func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// makeWritableOnCleanup restores u+w on every directory under the given
// roots, so t.TempDir can remove read-only trees.
func makeWritableOnCleanup(t *testing.T, roots ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, root := range roots {
			_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
				if err == nil && info.IsDir() {
					_ = os.Chmod(p, info.Mode().Perm()|0o700)
				}
				return nil
			})
		}
	})
}

// readOnlyTree builds a read-only nested tree like a Go module cache:
// src/mod/pkg/file.go and src/top.txt, every directory 0555.
func readOnlyTree(t *testing.T, root string) (string, map[string]string) {
	t.Helper()
	src := filepath.Join(root, "work", "pkg-mod")
	files := map[string]string{
		filepath.Join("mod", "pkg", "file.go"): "package pkg\n",
		filepath.Join("mod", "go.mod"):         "module mod\n",
		"top.txt":                              "top\n",
	}
	for rel, body := range files {
		p := filepath.Join(src, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o444))
	}
	for _, dir := range []string{filepath.Join(src, "mod", "pkg"), filepath.Join(src, "mod"), src} {
		require.NoError(t, os.Chmod(dir, 0o555))
	}
	return src, files
}

func assertTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err, "%s must be in %s", rel, root)
		assert.Equal(t, body, string(data))
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm(), "mode of %s", path)
}

// A read-only tree (a Go module cache) that cannot be renamed into the
// trash is copied there and then removed: its directories are made
// writable first, only inside the tree being trashed. A read-only
// directory a symlink in it points at is not touched.
func TestTrashMove_CopyRemovesReadOnlyTree(t *testing.T) {
	skipAsRoot(t)
	tr, renames := crossDeviceTrash(t)
	root := t.TempDir()
	src, files := readOnlyTree(t, root)
	outside := filepath.Join(root, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), []byte("keep"), 0o644))
	require.NoError(t, os.Chmod(outside, 0o555))
	require.NoError(t, os.Chmod(src, 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(src, "outside-link")))
	require.NoError(t, os.Chmod(src, 0o555))
	makeWritableOnCleanup(t, root, os.Getenv("GIT_HOP_DATA_HOME"))

	dest, err := tr.Move(src)
	require.NoError(t, err)
	assert.Equal(t, 1, *renames, "the move must have tried to rename first")

	_, err = os.Lstat(src)
	assert.True(t, os.IsNotExist(err), "the original must be gone")
	assertTree(t, dest, files)
	assertPerm(t, dest, 0o555)
	assertPerm(t, filepath.Join(dest, "mod", "pkg"), 0o555)
	assertSymlink(t, filepath.Join(dest, "outside-link"), outside)
	assertPerm(t, outside, 0o555)
	assertTree(t, outside, map[string]string{"keep": "keep"})
}

// partialRemoveFs is the OS filesystem whose RemoveAll of path removes
// one entry of it and then fails, as a removal stopped halfway does.
type partialRemoveFs struct {
	afero.OsFs
	path, first string
}

func (fs partialRemoveFs) RemoveAll(p string) error {
	if p != fs.path {
		return fs.OsFs.RemoveAll(p)
	}
	if err := os.RemoveAll(filepath.Join(p, fs.first)); err != nil {
		return err
	}
	return errors.New("simulated removal failure")
}

// A removal that fails halfway is rolled back: what it removed is put
// back from the copy, with its modes, and the copy leaves the trash.
func TestTrashMove_RemovalFailureRollsBack(t *testing.T) {
	skipAsRoot(t)
	root := t.TempDir()
	src, files := readOnlyTree(t, root)
	t.Setenv("GIT_HOP_DATA_HOME", t.TempDir())
	makeWritableOnCleanup(t, root, os.Getenv("GIT_HOP_DATA_HOME"))
	tr := NewTrash(partialRemoveFs{path: src, first: "mod"})

	_, err := tr.Move(src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated removal failure")

	assertTree(t, src, files)
	assertPerm(t, src, 0o555)
	assertPerm(t, filepath.Join(src, "mod"), 0o555)
	assertPerm(t, filepath.Join(src, "mod", "pkg"), 0o555)
	assertPerm(t, filepath.Join(src, "mod", "pkg", "file.go"), 0o444)

	backups, err := tr.List()
	require.NoError(t, err)
	assert.Empty(t, backups, "the copy must leave the trash")
	folders, err := os.ReadDir(filepath.Join(os.Getenv("GIT_HOP_DATA_HOME"), "backups"))
	require.NoError(t, err)
	assert.Empty(t, folders, "nor leave an empty folder there")
}

// chmodFailFs is the OS filesystem whose Chmod fails for paths under
// dir, as it does for a directory another user owns.
type chmodFailFs struct {
	afero.OsFs
	dir string
}

func (fs chmodFailFs) Chmod(p string, mode os.FileMode) error {
	if p == fs.dir || strings.HasPrefix(p, fs.dir+string(filepath.Separator)) {
		return &os.PathError{Op: "chmod", Path: p, Err: os.ErrPermission}
	}
	return fs.OsFs.Chmod(p, mode)
}

// A tree whose directories cannot be made writable is not removed at
// all: the move fails before anything is deleted, and the copy leaves
// the trash.
func TestTrashMove_UnwritableTreeLeftWhole(t *testing.T) {
	skipAsRoot(t)
	root := t.TempDir()
	src, files := readOnlyTree(t, root)
	t.Setenv("GIT_HOP_DATA_HOME", t.TempDir())
	makeWritableOnCleanup(t, root, os.Getenv("GIT_HOP_DATA_HOME"))
	tr := NewTrash(chmodFailFs{dir: filepath.Join(src, "mod")})

	_, err := tr.Move(src)
	require.Error(t, err)
	assertTree(t, src, files)
	assertPerm(t, src, 0o555)
	assertPerm(t, filepath.Join(src, "mod"), 0o555)

	backups, err := tr.List()
	require.NoError(t, err)
	assert.Empty(t, backups, "the copy must leave the trash")
}

// Clean removes backups holding read-only trees too.
func TestTrashClean_RemovesReadOnlyBackups(t *testing.T) {
	skipAsRoot(t)
	tr, _ := crossDeviceTrash(t)
	root := t.TempDir()
	src, _ := readOnlyTree(t, root)
	makeWritableOnCleanup(t, root, os.Getenv("GIT_HOP_DATA_HOME"))

	_, err := tr.Move(src)
	require.NoError(t, err)

	n, _, err := tr.Clean(-48 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	backups, err := tr.List()
	require.NoError(t, err)
	assert.Empty(t, backups)
}

// Restore takes the same fallback, and removes a read-only backup once
// it is copied back.
func TestTrashRestore_CopyOfReadOnlyTree(t *testing.T) {
	skipAsRoot(t)
	tr, _ := crossDeviceTrash(t)
	root := t.TempDir()
	src, files := readOnlyTree(t, root)
	makeWritableOnCleanup(t, root, os.Getenv("GIT_HOP_DATA_HOME"))

	dest, err := tr.Move(src)
	require.NoError(t, err)
	require.NoError(t, tr.Restore(dest, src))

	assertTree(t, src, files)
	assertPerm(t, filepath.Join(src, "mod", "pkg"), 0o555)
	_, err = os.Lstat(dest)
	assert.True(t, os.IsNotExist(err), "the backup must be gone")
}

// Trashing a symlink to a read-only directory leaves the directory's
// mode alone: only the link is trashed.
func TestTrashMove_CopyOfSymlinkToReadOnlyDir(t *testing.T) {
	skipAsRoot(t)
	tr, _ := crossDeviceTrash(t)
	root := t.TempDir()
	target, files := readOnlyTree(t, root)
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(target, link))
	makeWritableOnCleanup(t, root, os.Getenv("GIT_HOP_DATA_HOME"))

	dest, err := tr.Move(link)
	require.NoError(t, err)
	assertSymlink(t, dest, target)
	assertTree(t, target, files)
	assertPerm(t, target, 0o555)
	assertPerm(t, filepath.Join(target, "mod"), 0o555)
}
