package cmd

import (
	"testing"
	"time"

	"github.com/jadb/git-hop/internal/state"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListWorktrees_FromState(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create test state
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
						Path:         "/path/to/repo",
						Type:         "bare",
						HubPath:      "/path/to/repo",
						CreatedAt:    time.Now(),
						LastAccessed: time.Now(),
					},
					"feature-x": {
						Path:         "/path/to/repo/hops/feature-x",
						Type:         "linked",
						HubPath:      "/path/to/repo",
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

	// Save state
	require.NoError(t, state.SaveState(fs, st))

	// Load and verify we can list worktrees
	loaded, err := state.LoadState(fs)
	require.NoError(t, err)
	assert.Len(t, loaded.Repositories, 1)
	assert.Len(t, loaded.Repositories["github.com/test/repo"].Worktrees, 2)
}

func TestListWorktrees_EmptyState(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create empty state
	st := state.NewState()
	require.NoError(t, state.SaveState(fs, st))

	// Load and verify
	loaded, err := state.LoadState(fs)
	require.NoError(t, err)
	assert.Empty(t, loaded.Repositories)
}
