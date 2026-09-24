package hop_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// ForkAttach looks for a main-repo worktree to run the ancestry check in.
// `git hop init` records branch paths relative to the hub ("hops/main");
// the lookup used them as given, so from any cwd but the hub it found no
// worktree and fork-attach failed before fetching anything.
func TestForkAttach_FindsMainRepoFromHubRelativePath(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", t.TempDir())
	fs := afero.NewMemMapFs()
	hubPath := "/hub"

	hub, err := hop.CreateHub(fs, hubPath, "https://github.com/org/repo.git", "org", "repo", "main")
	require.NoError(t, err)
	require.NoError(t, hub.AddBranch("main", "main", "hops/main"))
	require.NoError(t, fs.MkdirAll(filepath.Join(hubPath, "hops", "main"), 0o755))

	g := mocks.NewMockGit()
	uri := "https://github.com/forker/repo.git"
	err = hop.ForkAttach(fs, g, uri, "feat", hubPath)
	if err != nil {
		assert.NotContains(t, err.Error(), "could not find main repository")
	}

	assert.True(t, g.Runner.CalledWith(filepath.Join(hubPath, "hops", "main")+":git fetch "+uri+" feat"),
		"fetch must run in the hub-relative main worktree; calls: %v", g.Runner.Calls)
}
