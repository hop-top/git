package cmd

import (
	"testing"
	"time"

	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
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

// TestShowRepositoryWorktrees_SyncColumn verifies that the list view
// surfaces each worktree's sync position relative to the repo's
// default branch — the same column the hub-view of `status` added.
// Missing dirs render as "-"; present dirs run rev-list against the
// default branch via the git interface.
func TestShowRepositoryWorktrees_SyncColumn(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/wt/main", 0o755))
	require.NoError(t, fs.MkdirAll("/wt/feature", 0o755))

	m := mocks.NewMockGit()
	m.Runner.Responses["/wt/feature:git rev-list --left-right --count feature...main"] = "2\t0"

	repo := &state.RepositoryState{
		URI:           "git@github.com:test/repo.git",
		Org:           "test",
		Repo:          "repo",
		DefaultBranch: "main",
		Worktrees: map[string]*state.WorktreeState{
			"main":    {Path: "/wt/main", Type: "bare"},
			"feature": {Path: "/wt/feature", Type: "linked"},
			"gone":    {Path: "/wt/gone", Type: "linked"},
		},
	}

	prevMode := output.CurrentMode
	output.CurrentMode = output.ModePorcelain
	t.Cleanup(func() { output.CurrentMode = prevMode })

	out := captureStdout(t, func() {
		showRepositoryWorktrees(fs, m, "github.com/test/repo", repo)
	})

	// default branch row → "default"
	assert.Contains(t, out, "default", "main row should report 'default' sync status")
	// feature has /wt/feature on disk + mocked rev-list → "2 ahead"
	assert.Contains(t, out, "2 ahead", "feature row should report mocked sync status")
	// missing dir → "-" placeholder; "missing" state
	assert.Contains(t, out, "missing", "gone row should report missing state")
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
