package cmd

import (
	"testing"
	"time"

	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyStateConsistency(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create state with valid and invalid worktrees
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
					"missing": {
						Path:         "/path/to/missing",
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

	// Create only the existing directory
	require.NoError(t, fs.MkdirAll("/path/to/existing", 0755))

	// Check consistency
	issues := checkStateConsistency(fs, st)

	assert.Len(t, issues, 1)
	assert.Contains(t, issues[0], "missing")
	assert.Contains(t, issues[0], "/path/to/missing")
}

func TestVerifyStateConsistency_AllValid(t *testing.T) {
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
				Worktrees: map[string]*state.WorktreeState{
					"main": {
						Path:         "/path/to/existing",
						Type:         "bare",
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

	// Create all directories
	require.NoError(t, fs.MkdirAll("/path/to/existing", 0755))

	// Check consistency
	issues := checkStateConsistency(fs, st)

	assert.Empty(t, issues)
}

func TestCheckStateInDoctorCommand(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create state with one valid and one missing worktree
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
					"feature": {
						Path:         "/path/to/missing",
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

	// Save the state
	require.NoError(t, state.SaveState(fs, st))

	// Check state consistency
	issues := checkStateConsistency(fs, st)

	// Should find one issue for the missing worktree
	assert.Len(t, issues, 1)
	assert.Contains(t, issues[0], "feature")
	assert.Contains(t, issues[0], "/path/to/missing")
}
