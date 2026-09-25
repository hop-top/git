package hop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/test/mocks"
)

// A failed bare clone is cleaned up on the filesystem it was set up on,
// never on whatever the real disk holds at that path.
func TestCloneBareRepo_CleansUpOnInjectedFs(t *testing.T) {
	projectRoot := t.TempDir()
	sentinel := filepath.Join(projectRoot, "keep")
	require.NoError(t, os.WriteFile(sentinel, []byte("real disk\n"), 0o644))

	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	mainPath := filepath.Join(projectRoot, "hops", "main")
	g.Runner.Errors["git -C "+projectRoot+" worktree add "+mainPath+" main"] = errors.New("worktree add failed")

	err := cloneBareRepo(fs, g, "https://github.com/org/repo.git", projectRoot, "main")
	require.Error(t, err)

	ok, err := afero.Exists(fs, projectRoot)
	require.NoError(t, err)
	assert.False(t, ok, "failed clone must be removed from the injected fs")

	_, err = os.Stat(sentinel)
	assert.NoError(t, err, "cleanup reached past the injected fs onto the real disk")
}
