package hop_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/git"
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

	assert.True(t, g.Runner.CalledWith(filepath.Join(hubPath, "hops", "main")+forkFetch(uri, "feat")),
		"fetch must run in the hub-relative main worktree; calls: %v", g.Runner.Calls)
}

// A regular file at a non-fork branch's worktree path is not that
// worktree. The main-repo probe used to accept any path that stat'ed,
// so a file sorted ahead of the real worktree was picked as the repo to
// fetch into, and the fetch failed on it.
func TestForkAttach_SkipsFileAtWorktreePath(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", t.TempDir())
	fs := afero.NewMemMapFs()
	hubPath := "/hub"

	hub, err := hop.CreateHub(fs, hubPath, "https://github.com/org/repo.git", "org", "repo", "main")
	require.NoError(t, err)
	require.NoError(t, hub.AddBranch("a-file", "a-file", "hops/a-file"))
	require.NoError(t, hub.AddBranch("main", "main", "hops/main"))
	occupied := filepath.Join(hubPath, "hops", "a-file")
	require.NoError(t, afero.WriteFile(fs, occupied, []byte("not a worktree"), 0o644))
	mainPath := filepath.Join(hubPath, "hops", "main")
	require.NoError(t, fs.MkdirAll(mainPath, 0o755))

	g := mocks.NewMockGit()
	uri := "https://github.com/forker/repo.git"
	_ = hop.ForkAttach(fs, g, uri, "feat", hubPath)

	fetch := forkFetch(uri, "feat")
	assert.False(t, g.Runner.CalledWith(occupied+fetch),
		"nothing may run in the file at %s; calls: %v", occupied, g.Runner.Calls)
	assert.True(t, g.Runner.CalledWith(mainPath+fetch),
		"fetch must run in the main worktree directory; calls: %v", g.Runner.Calls)
}

// With only a file where the worktree should be, there is no main repo:
// fork-attach says so rather than running git in the file.
func TestForkAttach_FileAtOnlyWorktreePath_NoMainRepo(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", t.TempDir())
	fs := afero.NewMemMapFs()
	hubPath := "/hub"

	hub, err := hop.CreateHub(fs, hubPath, "https://github.com/org/repo.git", "org", "repo", "main")
	require.NoError(t, err)
	require.NoError(t, hub.AddBranch("main", "main", "hops/main"))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(hubPath, "hops", "main"), []byte("not a worktree"), 0o644))

	g := mocks.NewMockGit()
	err = hop.ForkAttach(fs, g, "https://github.com/forker/repo.git", "feat", hubPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not find main repository")
	assert.Empty(t, g.Runner.Calls, "no git command may run")
}

// forkFetch is the mock-runner key suffix of fork-attach's fetch.
func forkFetch(uri, branch string) string {
	return ":git " + strings.Join(git.FetchArgs(uri, branch), " ")
}
