package cmd

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// remoteOnlyStatusHub is a hub whose only branch, feat, is checked out in
// dir (see remoteOnlyDefaultRepo) with the given recorded base.
func remoteOnlyStatusHub(dir, def string, base *string) *hop.Hub {
	return &hop.Hub{
		Path: dir,
		Config: &config.HubConfig{
			Repo: config.RepoConfig{DefaultBranch: def},
			Branches: map[string]config.HubBranch{
				"feat": {Path: dir, Base: base},
			},
		},
	}
}

// A default branch with no local branch but an origin tracking ref is
// measured as origin/<default>: the bare name resolves to nothing and
// every worktree read "unknown". The Base column keeps the bare name.
func TestStatus_DefaultOnlyOnOrigin(t *testing.T) {
	dir := remoteOnlyDefaultRepo(t)
	g := git.New()
	fs := afero.NewOsFs()

	// feat was rebase-merged into origin/next.
	r := hubBranchStatusRecord(fs, g, remoteOnlyStatusHub(dir, "next", nil), "feat")
	assert.Equal(t, "next", r.Base)
	assert.Equal(t, "merged (2 behind)", r.Status)

	// other never landed: one commit of its own, two it lacks.
	assert.Equal(t, "diverged (1 ahead, 2 behind)",
		getBranchSyncStatus(g, dir, "other", "next"))
	// main sits at the fork point, behind origin/next.
	assert.Equal(t, "behind (2)", getBranchSyncStatus(g, dir, "main", "next"))
}

// A recorded base kept only as a remote-tracking ref is measured the same
// way.
func TestStatus_RecordedBaseOnlyOnOrigin(t *testing.T) {
	dir := remoteOnlyDefaultRepo(t)
	base := "next"
	r := hubBranchStatusRecord(afero.NewOsFs(), git.New(),
		remoteOnlyStatusHub(dir, "main", &base), "feat")
	assert.Equal(t, "next", r.Base)
	assert.Equal(t, "merged (2 behind)", r.Status)
}

// Control: a local default branch is used as it stands, and a name known
// nowhere stays "unknown".
func TestStatus_LocalDefaultAndUnknown(t *testing.T) {
	dir := remoteOnlyDefaultRepo(t)
	g := git.New()

	assert.Equal(t, "1 ahead", getBranchSyncStatus(g, dir, "feat", "main"))
	assert.Equal(t, "unknown", getBranchSyncStatus(g, dir, "feat", "gone"))
}
