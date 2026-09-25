package services

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crossDeviceTrash is a trash on the OS filesystem whose rename always
// fails as a move across filesystems does, so every move takes the copy
// fallback. renames counts the attempts.
func crossDeviceTrash(t *testing.T) (*Trash, *int) {
	t.Helper()
	t.Setenv("GIT_HOP_DATA_HOME", t.TempDir())
	tr := NewTrash(afero.NewOsFs())
	require.NotNil(t, tr.rename, "the OS filesystem renames first")
	renames := 0
	tr.rename = func(src, dst string) error {
		renames++
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	}
	return tr, &renames
}

// sharedStore is a directory of many files, like a shared deps install,
// and returns its path and the files in it.
func sharedStore(t *testing.T, root string) (string, []string) {
	t.Helper()
	store := filepath.Join(root, "store")
	var files []string
	for i := 0; i < 50; i++ {
		f := filepath.Join(store, fmt.Sprintf("pkg%02d", i), "index.js")
		require.NoError(t, os.MkdirAll(filepath.Dir(f), 0o755))
		require.NoError(t, os.WriteFile(f, []byte(fmt.Sprintf("module %d", i)), 0o644))
		files = append(files, f)
	}
	return store, files
}

func assertStoreIntact(t *testing.T, files []string) {
	t.Helper()
	for i, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err, "store file %s must survive the trash", f)
		assert.Equal(t, fmt.Sprintf("module %d", i), string(data))
	}
}

func assertSymlink(t *testing.T, path, target string) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode()&os.ModeSymlink != 0, "%s must be a symlink, is %v", path, info.Mode())
	got, err := os.Readlink(path)
	require.NoError(t, err)
	assert.Equal(t, target, got)
}

// A move that cannot rename copies what it trashes. Symlinks in it are
// copied as symlinks, whatever they point at: one into a shared store
// must not pull the store into the trash, nor may removing the original
// reach through it. A dangling link and a relative one are kept as they
// are; files and directories keep their modes.
func TestTrashMove_CopyKeepsSymlinks(t *testing.T) {
	tr, renames := crossDeviceTrash(t)
	root := t.TempDir()
	store, storeFiles := sharedStore(t, root)
	sibling := filepath.Join(root, "sibling.txt")
	require.NoError(t, os.WriteFile(sibling, []byte("sibling"), 0o644))

	src := filepath.Join(root, "work", "node_modules")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "nested"), 0o755))
	require.NoError(t, os.Symlink(store, filepath.Join(src, "store-link")))
	require.NoError(t, os.Symlink(filepath.Join(root, "gone"), filepath.Join(src, "dangling")))
	require.NoError(t, os.Symlink("../../../sibling.txt", filepath.Join(src, "nested", "relative")))
	require.NoError(t, os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\n"), 0o775))
	require.NoError(t, os.Chmod(filepath.Join(src, "run.sh"), 0o775)) // past the umask
	private := filepath.Join(src, "private")
	require.NoError(t, os.MkdirAll(private, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(private, "f"), []byte("f"), 0o600))
	require.NoError(t, os.Chmod(private, 0o710))

	dest, err := tr.Move(src)
	require.NoError(t, err)
	assert.Equal(t, 1, *renames, "the move must have tried to rename first")

	_, err = os.Lstat(src)
	assert.True(t, os.IsNotExist(err), "the original must be gone")

	assertSymlink(t, filepath.Join(dest, "store-link"), store)
	assertSymlink(t, filepath.Join(dest, "dangling"), filepath.Join(root, "gone"))
	assertSymlink(t, filepath.Join(dest, "nested", "relative"), "../../../sibling.txt")

	info, err := os.Stat(filepath.Join(dest, "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o775), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(dest, "private"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o710), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(dest, "private", "f"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	data, err := os.ReadFile(filepath.Join(dest, "private", "f"))
	require.NoError(t, err)
	assert.Equal(t, "f", string(data))

	assertStoreIntact(t, storeFiles)
	data, err = os.ReadFile(sibling)
	require.NoError(t, err)
	assert.Equal(t, "sibling", string(data))
}

// Trashing a symlink itself trashes the link, not what it points at.
func TestTrashMove_CopyOfSymlinkToLargeDir(t *testing.T) {
	tr, renames := crossDeviceTrash(t)
	root := t.TempDir()
	store, storeFiles := sharedStore(t, root)
	link := filepath.Join(root, "work", "node_modules")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	require.NoError(t, os.Symlink(store, link))

	dest, err := tr.Move(link)
	require.NoError(t, err)
	assert.Equal(t, 1, *renames)

	assertSymlink(t, dest, store)
	_, err = os.Lstat(link)
	assert.True(t, os.IsNotExist(err), "the link must be gone")
	assertStoreIntact(t, storeFiles)
}

// Trashing a dangling symlink, or a relative one, keeps its target as is.
func TestTrashMove_CopyOfDanglingAndRelativeSymlinks(t *testing.T) {
	tr, _ := crossDeviceTrash(t)
	root := t.TempDir()
	dangling := filepath.Join(root, "dangling")
	require.NoError(t, os.Symlink(filepath.Join(root, "nowhere"), dangling))
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.txt"), []byte("t"), 0o644))
	relative := filepath.Join(root, "sub", "relative")
	require.NoError(t, os.MkdirAll(filepath.Dir(relative), 0o755))
	require.NoError(t, os.Symlink("../target.txt", relative))

	dest, err := tr.Move(dangling)
	require.NoError(t, err)
	assertSymlink(t, dest, filepath.Join(root, "nowhere"))

	dest, err = tr.Move(relative)
	require.NoError(t, err)
	assertSymlink(t, dest, "../target.txt")
	data, err := os.ReadFile(filepath.Join(root, "target.txt"))
	require.NoError(t, err)
	assert.Equal(t, "t", string(data))
}

// Restore takes the same fallback, and brings symlinks back as symlinks.
func TestTrashRestore_CopyKeepsSymlinks(t *testing.T) {
	tr, _ := crossDeviceTrash(t)
	root := t.TempDir()
	store, storeFiles := sharedStore(t, root)
	src := filepath.Join(root, "work", "deps")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.Symlink(store, filepath.Join(src, "store-link")))

	dest, err := tr.Move(src)
	require.NoError(t, err)
	require.NoError(t, tr.Restore(dest, src))

	assertSymlink(t, filepath.Join(src, "store-link"), store)
	_, err = os.Lstat(dest)
	assert.True(t, os.IsNotExist(err), "the backup must be gone")
	assertStoreIntact(t, storeFiles)
}

// Two moves of the same name in the same second go to different trash
// folders: the second neither merges into the first nor fails, and both
// are listed under that second and cleaned with it. Once on the OS
// filesystem (rename), once through the copy.
func TestTrashMove_SameNameSameSecond(t *testing.T) {
	for name, newTrash := range map[string]func(t *testing.T) *Trash{
		"rename": func(t *testing.T) *Trash {
			t.Setenv("GIT_HOP_DATA_HOME", t.TempDir())
			return NewTrash(afero.NewOsFs())
		},
		"copy": func(t *testing.T) *Trash {
			tr, _ := crossDeviceTrash(t)
			return tr
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTrash(t)
			at := time.Date(2026, 9, 25, 10, 30, 15, 0, time.Local)
			tr.now = func() time.Time { return at }

			root := t.TempDir()
			var dests []string
			for _, wt := range []string{"a", "b"} {
				src := filepath.Join(root, wt, "node_modules")
				require.NoError(t, os.MkdirAll(src, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(src, "from"), []byte(wt), 0o644))
				dest, err := tr.Move(src)
				require.NoError(t, err, "move of %s", src)
				dests = append(dests, dest)
			}

			require.NotEqual(t, dests[0], dests[1])
			for i, wt := range []string{"a", "b"} {
				assert.Equal(t, "node_modules", filepath.Base(dests[i]))
				entries, err := os.ReadDir(dests[i])
				require.NoError(t, err)
				require.Len(t, entries, 1, "%s holds only what was trashed from %s", dests[i], wt)
				data, err := os.ReadFile(filepath.Join(dests[i], "from"))
				require.NoError(t, err)
				assert.Equal(t, wt, string(data))
			}

			backups, err := tr.List()
			require.NoError(t, err)
			require.Len(t, backups, 2)
			for _, b := range backups {
				assert.Equal(t, "node_modules", b.Name)
				assert.True(t, b.Timestamp.Equal(at), "timestamp %v, want %v", b.Timestamp, at)
			}

			n, _, err := tr.Clean(time.Since(at) - time.Minute)
			require.NoError(t, err)
			assert.Equal(t, 2, n)
		})
	}
}
