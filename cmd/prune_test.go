package cmd

import (
	"testing"
	"time"

	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneOrphanedWorktrees(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create test state with one valid and one orphaned worktree
	st := &state.State{
		Version:     "1.0.0",
		LastUpdated: time.Now(),
		Repositories: map[string]*state.RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees: map[string]*state.WorktreeState{
					"main": {
						Path:         "/path/to/existing",
						Type:         "bare",
						HubPath:      "/path/to/existing",
						CreatedAt:    time.Now(),
						LastAccessed: time.Now(),
					},
					"orphaned": {
						Path:         "/path/to/orphaned",
						Type:         "linked",
						HubPath:      "/path/to/existing",
						CreatedAt:    time.Now(),
						LastAccessed: time.Now(),
					},
				},
				Hubs: []*state.HubState{},
				GlobalHopspace: &state.GlobalHopspaceState{
					Enabled: false,
					Path:    nil,
				},
			},
		},
		Orphaned: []*state.OrphanedEntry{},
	}

	// Create only the "existing" directory
	require.NoError(t, fs.MkdirAll("/path/to/existing", 0755))

	// Prune orphaned entries
	pruned := pruneOrphanedWorktrees(fs, st)

	assert.Equal(t, 1, pruned)
	assert.Len(t, st.Repositories["github.com/test/repo"].Worktrees, 1)
	assert.Contains(t, st.Repositories["github.com/test/repo"].Worktrees, "main")
	assert.NotContains(t, st.Repositories["github.com/test/repo"].Worktrees, "orphaned")
}

func TestPruneOrphanedHubs(t *testing.T) {
	fs := afero.NewMemMapFs()

	st := &state.State{
		Version:     "1.0.0",
		LastUpdated: time.Now(),
		Repositories: map[string]*state.RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees:     map[string]*state.WorktreeState{},
				Hubs: []*state.HubState{
					{
						Path:         "/path/to/existing/hub",
						Mode:         "local",
						CreatedAt:    time.Now(),
						LastAccessed: time.Now(),
					},
					{
						Path:         "/path/to/orphaned/hub",
						Mode:         "local",
						CreatedAt:    time.Now(),
						LastAccessed: time.Now(),
					},
				},
				GlobalHopspace: &state.GlobalHopspaceState{
					Enabled: false,
					Path:    nil,
				},
			},
		},
		Orphaned: []*state.OrphanedEntry{},
	}

	// Create only one hub directory
	require.NoError(t, fs.MkdirAll("/path/to/existing/hub", 0755))

	// Prune orphaned hubs
	pruned := pruneOrphanedHubs(fs, st)

	assert.Equal(t, 1, pruned)
	assert.Len(t, st.Repositories["github.com/test/repo"].Hubs, 1)
	assert.Equal(t, "/path/to/existing/hub", st.Repositories["github.com/test/repo"].Hubs[0].Path)
}
