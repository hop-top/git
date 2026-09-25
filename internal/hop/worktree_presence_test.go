package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
)

// A worktree is present only when a directory is at its path. Something
// else there (a file, a symlink to one, a dangling symlink) is not a
// worktree, and not nothing either: git cannot check a worktree out over
// it. Run on the real filesystem, since symlinks are the point.
func TestWorktreeAt(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	dir := filepath.Join(root, "dir")
	file := filepath.Join(root, "file")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	for name, target := range map[string]string{
		"link-to-dir":  dir,
		"link-to-file": file,
		"dangling":     filepath.Join(root, "nowhere"),
	} {
		require.NoError(t, os.Symlink(target, filepath.Join(root, name)))
	}

	fs := afero.NewOsFs()
	for _, tc := range []struct {
		name string
		want hop.WorktreePresence
	}{
		{"dir", hop.WorktreePresent},
		{"link-to-dir", hop.WorktreePresent},
		{"file", hop.WorktreeOccupied},
		{"link-to-file", hop.WorktreeOccupied},
		{"dangling", hop.WorktreeOccupied},
		{"absent", hop.WorktreeAbsent},
		{"file/below-a-file", hop.WorktreeAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, tc.name)
			assert.Equal(t, tc.want, hop.WorktreeAt(fs, path))
			assert.Equal(t, tc.want == hop.WorktreePresent, hop.WorktreeDirPresent(fs, path))
		})
	}
}
