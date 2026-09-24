package hop

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
)

func recordedState(repoID, org, repo, hubPath string) *state.State {
	st := state.NewState()
	st.AddRepository(repoID, &state.RepositoryState{
		Org: org, Repo: repo, DefaultBranch: "main",
		Worktrees: map[string]*state.WorktreeState{
			hubPath: {Path: hubPath, Branch: "main", Type: WorktreeTypeMain, HubPath: hubPath},
		},
		Hubs: []*state.HubState{{Path: hubPath}},
	})
	return st
}

// A hub state records, with no hop.json, is recognised from its root
// and from below it, as a read-only view of its state record.
func TestResolveHub_RecordedHub(t *testing.T) {
	fs := afero.NewMemMapFs()
	st := recordedState("github.com/local/plain", "local", "plain", "/w/local/plain")

	for _, dir := range []string{"/w/local/plain", "/w/local/plain/sub/deeper"} {
		ref, err := ResolveHub(fs, st, dir)
		require.NoError(t, err, dir)
		assert.True(t, ref.Recorded)
		assert.Equal(t, "github.com/local/plain", ref.RepoID)
		assert.Equal(t, "/w/local/plain", ref.Path)
		assert.Equal(t, "main", ref.Config.Repo.DefaultBranch)
		require.Contains(t, ref.Config.Branches, "main")
		assert.Equal(t, "/w/local/plain", ref.Config.Branches["main"].Path)
	}

	ref, err := ResolveHub(fs, st, "/w/local/plain")
	require.NoError(t, err)
	assert.Error(t, ref.Save(), "a recorded hub's view must not write hop.json")
	exists, _ := afero.Exists(fs, "/w/local/plain/hop.json")
	assert.False(t, exists)
}

// hop.json discovery comes first, even when state records a hub around
// the same directory.
func TestResolveHub_HopJSONFirst(t *testing.T) {
	fs := afero.NewMemMapFs()
	st := recordedState("github.com/local/outer", "local", "outer", "/w")
	require.NoError(t, fs.MkdirAll("/w/acme/widget", 0o755))
	require.NoError(t, config.NewWriter(fs).WriteHubConfig("/w/acme/widget", &config.HubConfig{
		Repo: config.RepoConfig{Org: "acme", Repo: "widget", DefaultBranch: "main"},
	}))

	ref, err := ResolveHub(fs, st, filepath.Join("/w/acme/widget", "hops", "main"))
	require.NoError(t, err)
	assert.False(t, ref.Recorded)
	assert.Equal(t, "github.com/acme/widget", ref.RepoID)
	assert.Equal(t, "/w/acme/widget", ref.Path)
}

// Nested recorded hubs resolve to the deepest one containing the directory.
func TestResolveHub_DeepestRecordedHub(t *testing.T) {
	fs := afero.NewMemMapFs()
	st := recordedState("github.com/local/outer", "local", "outer", "/w")
	inner := recordedState("github.com/local/inner", "local", "inner", "/w/inner")
	st.Repositories["github.com/local/inner"] = inner.Repositories["github.com/local/inner"]

	ref, err := ResolveHub(fs, st, "/w/inner/src")
	require.NoError(t, err)
	assert.Equal(t, "github.com/local/inner", ref.RepoID)

	ref, err = ResolveHub(fs, st, "/w/other")
	require.NoError(t, err)
	assert.Equal(t, "github.com/local/outer", ref.RepoID)
}

// A directory neither hop.json nor state knows is not in a hub, including
// a sibling whose name merely starts with a recorded hub's.
func TestResolveHub_NotInHub(t *testing.T) {
	fs := afero.NewMemMapFs()
	st := recordedState("github.com/local/plain", "local", "plain", "/w/local/plain")

	for _, dir := range []string{"/w/local/plainer", "/w/local", "/elsewhere"} {
		_, err := ResolveHub(fs, st, dir)
		assert.True(t, errors.Is(err, ErrNotInHub), "%s: %v", dir, err)
	}
	_, err := ResolveHub(fs, nil, "/w/local/plain")
	assert.True(t, errors.Is(err, ErrNotInHub), "nil state: %v", err)
}
