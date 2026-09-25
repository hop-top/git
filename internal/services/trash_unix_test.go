//go:build unix

package services

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Something that is not a file, directory or symlink (a FIFO, which an
// open would block on) is refused, and the original is left in place.
func TestTrashMove_CopyRefusesSpecialFiles(t *testing.T) {
	tr, _ := crossDeviceTrash(t)
	src := filepath.Join(t.TempDir(), "deps")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644))

	_, err := tr.Move(src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file, directory or symlink")
	_, err = os.Lstat(filepath.Join(src, "pipe"))
	assert.NoError(t, err, "the original must be left in place")
	folders, err := os.ReadDir(filepath.Join(os.Getenv("GIT_HOP_DATA_HOME"), "backups"))
	require.NoError(t, err)
	assert.Empty(t, folders, "the partial copy must leave the trash")
}
