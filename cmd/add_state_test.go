package cmd

import (
	"testing"
	"time"

	"hop.top/git/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddWorktreeToState(t *testing.T) {
	// Create initial state with a repository
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
				},
				Hubs: []*state.HubState{
					{
						Path:         "/path/to/repo",
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

	// Add a new worktree
	worktree := &state.WorktreeState{
		Path:         "/path/to/repo/hops/feature-x",
		Type:         "linked",
		HubPath:      "/path/to/repo",
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
	}

	err := st.AddWorktree("github.com/test/repo", "feature-x", worktree)

	require.NoError(t, err)
	assert.Len(t, st.Repositories["github.com/test/repo"].Worktrees, 2)
	assert.Contains(t, st.Repositories["github.com/test/repo"].Worktrees, "feature-x")

	// Verify worktree details
	addedWorktree := st.Repositories["github.com/test/repo"].Worktrees["feature-x"]
	assert.Equal(t, "/path/to/repo/hops/feature-x", addedWorktree.Path)
	assert.Equal(t, "linked", addedWorktree.Type)
	assert.Equal(t, "/path/to/repo", addedWorktree.HubPath)
}

func TestAddWorktreeToState_RepositoryNotFound(t *testing.T) {
	st := state.NewState()

	worktree := &state.WorktreeState{
		Path:    "/path/to/worktree",
		Type:    "linked",
		HubPath: "/path/to/hub",
	}

	err := st.AddWorktree("github.com/nonexistent/repo", "branch", worktree)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository not found")
}
