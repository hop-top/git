package hop

import (
	"testing"
	"time"

	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRepositoryID_FromURL(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		expected  string
	}{
		{
			name:      "GitHub SSH URL",
			remoteURL: "git@github.com:test/repo.git",
			expected:  "github.com/test/repo",
		},
		{
			name:      "GitHub HTTPS URL",
			remoteURL: "https://github.com/test/repo.git",
			expected:  "github.com/test/repo",
		},
		{
			name:      "GitLab URL",
			remoteURL: "git@gitlab.com:org/project.git",
			expected:  "gitlab.com/org/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseRepositoryIDFromURL(tt.remoteURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetRepositoryID_FromPath(t *testing.T) {
	result := GetRepositoryIDFromPath("/path/to/test/repo", "github.com")
	assert.Equal(t, "github.com/test/repo", result)
}

func TestUpdateLastAccessed(t *testing.T) {
	oldTime := time.Now().Add(-1 * time.Hour)
	st := &state.State{
		Version:     "1.0.0",
		LastUpdated: oldTime,
		Repositories: map[string]*state.RepositoryState{
			"github.com/test/repo": {
				URI:           "git@github.com:test/repo.git",
				Org:           "test",
				Repo:          "repo",
				DefaultBranch: "main",
				Worktrees: map[string]*state.WorktreeState{
					"feature-x": {
						Path:         "/path/to/worktree",
						Type:         "linked",
						HubPath:      "/path/to/hub",
						CreatedAt:    oldTime,
						LastAccessed: oldTime,
					},
				},
				Hubs: []*state.HubState{
					{
						Path:         "/path/to/hub",
						Mode:         "local",
						CreatedAt:    oldTime,
						LastAccessed: oldTime,
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

	err := st.UpdateLastAccessed("github.com/test/repo", "feature-x", "/path/to/hub")

	require.NoError(t, err)
	assert.True(t, st.Repositories["github.com/test/repo"].Worktrees["feature-x"].LastAccessed.After(oldTime))
	assert.True(t, st.Repositories["github.com/test/repo"].Hubs[0].LastAccessed.After(oldTime))
}

func TestWorktreeExists(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create test directories
	existingPath := "/path/to/existing"
	require.NoError(t, fs.MkdirAll(existingPath, 0755))

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "Path exists",
			path:     existingPath,
			expected: true,
		},
		{
			name:     "Path does not exist",
			path:     "/path/to/nonexistent",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := worktreeExists(fs, tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
