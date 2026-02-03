package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadState_NewFile(t *testing.T) {
	fs := afero.NewMemMapFs()

	state, err := LoadState(fs)

	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Equal(t, "1.0.0", state.Version)
	assert.NotNil(t, state.Repositories)
	assert.Empty(t, state.Repositories)
	assert.NotNil(t, state.Orphaned)
	assert.Empty(t, state.Orphaned)
}

func TestLoadState_ExistingFile(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create a state file
	stateData := State{
		Version:     "1.0.0",
		LastUpdated: time.Now(),
		Repositories: map[string]*RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees: map[string]*WorktreeState{
					"main": {
						Path:         "/path/to/worktree",
						Type:         "bare",
						HubPath:      "/path/to/hub",
						CreatedAt:    time.Now(),
						LastAccessed: time.Now(),
					},
				},
				Hubs: []*HubState{
					{
						Path:         "/path/to/hub",
						Mode:         "local",
						CreatedAt:    time.Now(),
						LastAccessed: time.Now(),
					},
				},
				GlobalHopspace: &GlobalHopspaceState{
					Enabled: false,
					Path:    nil,
				},
			},
		},
		Orphaned: []*OrphanedEntry{},
	}

	statePath := filepath.Join(GetStateHome(), "state.json")
	require.NoError(t, fs.MkdirAll(filepath.Dir(statePath), 0755))

	data, err := json.MarshalIndent(stateData, "", "  ")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, statePath, data, 0644))

	// Load the state
	state, err := LoadState(fs)

	require.NoError(t, err)
	assert.Equal(t, "1.0.0", state.Version)
	assert.Len(t, state.Repositories, 1)
	assert.Contains(t, state.Repositories, "github.com/test/repo")
}

func TestSaveState(t *testing.T) {
	fs := afero.NewMemMapFs()

	state := &State{
		Version:     "1.0.0",
		LastUpdated: time.Now(),
		Repositories: map[string]*RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees:     map[string]*WorktreeState{},
				Hubs:          []*HubState{},
				GlobalHopspace: &GlobalHopspaceState{
					Enabled: false,
					Path:    nil,
				},
			},
		},
		Orphaned: []*OrphanedEntry{},
	}

	err := SaveState(fs, state)

	require.NoError(t, err)

	// Verify file was created
	statePath := filepath.Join(GetStateHome(), "state.json")
	exists, err := afero.Exists(fs, statePath)
	require.NoError(t, err)
	assert.True(t, exists)

	// Verify content
	data, err := afero.ReadFile(fs, statePath)
	require.NoError(t, err)

	var loaded State
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, state.Version, loaded.Version)
	assert.Len(t, loaded.Repositories, 1)
}

func TestGetStateHome(t *testing.T) {
	// Test with XDG_STATE_HOME set
	os.Setenv("XDG_STATE_HOME", "/custom/state")
	defer os.Unsetenv("XDG_STATE_HOME")

	statePath := GetStateHome()
	assert.Contains(t, statePath, "/custom/state")
	assert.Contains(t, statePath, "git-hop")
}

func TestAddRepository(t *testing.T) {
	state := &State{
		Version:      "1.0.0",
		LastUpdated:  time.Now(),
		Repositories: map[string]*RepositoryState{},
		Orphaned:     []*OrphanedEntry{},
	}

	repoState := &RepositoryState{
		URI:           "git@github.com:test/repo.git",
		Org:           "test",
		Repo:          "repo",
		DefaultBranch: "main",
		Worktrees:     map[string]*WorktreeState{},
		Hubs:          []*HubState{},
		GlobalHopspace: &GlobalHopspaceState{
			Enabled: false,
			Path:    nil,
		},
	}

	state.AddRepository("github.com/test/repo", repoState)

	assert.Len(t, state.Repositories, 1)
	assert.Contains(t, state.Repositories, "github.com/test/repo")
	assert.Equal(t, "test", state.Repositories["github.com/test/repo"].Org)
}

func TestAddWorktree(t *testing.T) {
	state := &State{
		Version:     "1.0.0",
		LastUpdated: time.Now(),
		Repositories: map[string]*RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees:     map[string]*WorktreeState{},
				Hubs:          []*HubState{},
				GlobalHopspace: &GlobalHopspaceState{
					Enabled: false,
					Path:    nil,
				},
			},
		},
		Orphaned: []*OrphanedEntry{},
	}

	worktree := &WorktreeState{
		Path:         "/path/to/worktree",
		Type:         "linked",
		HubPath:      "/path/to/hub",
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
	}

	err := state.AddWorktree("github.com/test/repo", "feature-x", worktree)

	require.NoError(t, err)
	assert.Len(t, state.Repositories["github.com/test/repo"].Worktrees, 1)
	assert.Contains(t, state.Repositories["github.com/test/repo"].Worktrees, "feature-x")
}

func TestRemoveWorktree(t *testing.T) {
	now := time.Now()
	state := &State{
		Version:     "1.0.0",
		LastUpdated: now,
		Repositories: map[string]*RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees: map[string]*WorktreeState{
					"feature-x": {
						Path:         "/path/to/worktree",
						Type:         "linked",
						HubPath:      "/path/to/hub",
						CreatedAt:    now,
						LastAccessed: now,
					},
				},
				Hubs: []*HubState{},
				GlobalHopspace: &GlobalHopspaceState{
					Enabled: false,
					Path:    nil,
				},
			},
		},
		Orphaned: []*OrphanedEntry{},
	}

	err := state.RemoveWorktree("github.com/test/repo", "feature-x")

	require.NoError(t, err)
	assert.Empty(t, state.Repositories["github.com/test/repo"].Worktrees)
}

func TestUpdateLastAccessed(t *testing.T) {
	oldTime := time.Now().Add(-1 * time.Hour)
	state := &State{
		Version:     "1.0.0",
		LastUpdated: oldTime,
		Repositories: map[string]*RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees: map[string]*WorktreeState{
					"main": {
						Path:         "/path/to/worktree",
						Type:         "bare",
						HubPath:      "/path/to/hub",
						CreatedAt:    oldTime,
						LastAccessed: oldTime,
					},
				},
				Hubs: []*HubState{
					{
						Path:         "/path/to/hub",
						Mode:         "local",
						CreatedAt:    oldTime,
						LastAccessed: oldTime,
					},
				},
				GlobalHopspace: &GlobalHopspaceState{
					Enabled: false,
					Path:    nil,
				},
			},
		},
		Orphaned: []*OrphanedEntry{},
	}

	err := state.UpdateLastAccessed("github.com/test/repo", "main", "/path/to/hub")

	require.NoError(t, err)
	assert.True(t, state.Repositories["github.com/test/repo"].Worktrees["main"].LastAccessed.After(oldTime))
	assert.True(t, state.Repositories["github.com/test/repo"].Hubs[0].LastAccessed.After(oldTime))
}
