package hop_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

// A hub keeps its hopspace in its own hop.json unless it was cloned with
// --global, which marks the hub (repo.mode = "global") and puts the
// hopspace in <data>/<org>/<repo>. Only the marker decides: a data-home
// hop.json next to an unmarked hub is a stale copy, never the hopspace.
func TestResolveHopspacePath(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", "/data")
	hubPath := "/work/hub"
	global := filepath.Join("/data", "org", "repo")

	t.Run("unmarked hub is its own hopspace", func(t *testing.T) {
		repo := config.RepoConfig{Org: "org", Repo: "repo"}
		assert.Equal(t, hubPath, hop.ResolveHopspacePath(hubPath, repo))
	})

	t.Run("hub marked global resolves to the data home", func(t *testing.T) {
		repo := config.RepoConfig{Org: "org", Repo: "repo", Mode: config.RepoModeGlobal}
		assert.Equal(t, global, hop.ResolveHopspacePath(hubPath, repo))
	})
}
