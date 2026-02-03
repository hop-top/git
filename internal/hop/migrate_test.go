package hop

import (
	"testing"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/state"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateRegistry_EmptyRegistry(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create empty old registry
	oldRegistry := &Registry{
		Config: &config.HopsConfig{
			Hops: make(map[string]config.HopEntry),
		},
		fs: fs,
	}

	newState := state.NewState()

	err := MigrateRegistry(fs, oldRegistry, newState)

	require.NoError(t, err)
	assert.Empty(t, newState.Repositories)
}

func TestMigrateRegistry_WithHops(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create old registry with entries
	oldRegistry := &Registry{
		Config: &config.HopsConfig{
			Hops: map[string]config.HopEntry{
				"test/repo:main": {
					Repo:         "test/repo",
					Branch:       "main",
					Path:         "/path/to/repo",
					ProjectRoot:  "/path/to/repo",
					AddedAt:      time.Now(),
					LastSeen:     time.Now(),
					EnvState:     "none",
					HasDockerEnv: false,
				},
				"test/repo:feature-x": {
					Repo:         "test/repo",
					Branch:       "feature-x",
					Path:         "/path/to/repo/hops/feature-x",
					ProjectRoot:  "/path/to/repo",
					AddedAt:      time.Now(),
					LastSeen:     time.Now(),
					EnvState:     "none",
					HasDockerEnv: false,
				},
			},
		},
		fs: fs,
	}

	newState := state.NewState()

	err := MigrateRegistry(fs, oldRegistry, newState)

	require.NoError(t, err)

	// Should have one repository with two worktrees
	assert.Len(t, newState.Repositories, 1)
	assert.Contains(t, newState.Repositories, "github.com/test/repo")

	repo := newState.Repositories["github.com/test/repo"]
	assert.Equal(t, "test", repo.Org)
	assert.Equal(t, "repo", repo.Repo)
	assert.Len(t, repo.Worktrees, 2)
	assert.Contains(t, repo.Worktrees, "main")
	assert.Contains(t, repo.Worktrees, "feature-x")
}

func TestMigrateRegistry_MultipleRepos(t *testing.T) {
	fs := afero.NewMemMapFs()

	oldRegistry := &Registry{
		Config: &config.HopsConfig{
			Hops: map[string]config.HopEntry{
				"org1/repo1:main": {
					Repo:        "org1/repo1",
					Branch:      "main",
					Path:        "/path/to/repo1",
					ProjectRoot: "/path/to/repo1",
				},
				"org2/repo2:main": {
					Repo:        "org2/repo2",
					Branch:      "main",
					Path:        "/path/to/repo2",
					ProjectRoot: "/path/to/repo2",
				},
			},
		},
		fs: fs,
	}

	newState := state.NewState()

	err := MigrateRegistry(fs, oldRegistry, newState)

	require.NoError(t, err)
	assert.Len(t, newState.Repositories, 2)
	assert.Contains(t, newState.Repositories, "github.com/org1/repo1")
	assert.Contains(t, newState.Repositories, "github.com/org2/repo2")
}

func TestExtractOrgRepo(t *testing.T) {
	tests := []struct {
		name         string
		repoString   string
		expectedOrg  string
		expectedRepo string
	}{
		{
			name:         "Simple org/repo",
			repoString:   "test/repo",
			expectedOrg:  "test",
			expectedRepo: "repo",
		},
		{
			name:         "Nested path",
			repoString:   "organization/project",
			expectedOrg:  "organization",
			expectedRepo: "project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, repo := extractOrgRepo(tt.repoString)
			assert.Equal(t, tt.expectedOrg, org)
			assert.Equal(t, tt.expectedRepo, repo)
		})
	}
}

func TestDetermineWorktreeType(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		projectRoot  string
		expectedType string
	}{
		{
			name:         "Bare repo (path equals project root)",
			path:         "/path/to/repo",
			projectRoot:  "/path/to/repo",
			expectedType: "bare",
		},
		{
			name:         "Linked worktree",
			path:         "/path/to/repo/hops/feature-x",
			projectRoot:  "/path/to/repo",
			expectedType: "linked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineWorktreeType(tt.path, tt.projectRoot)
			assert.Equal(t, tt.expectedType, result)
		})
	}
}

func TestGroupHopsByRepo(t *testing.T) {
	hops := map[string]config.HopEntry{
		"test/repo:main": {
			Repo:   "test/repo",
			Branch: "main",
		},
		"test/repo:feature": {
			Repo:   "test/repo",
			Branch: "feature",
		},
		"other/repo:main": {
			Repo:   "other/repo",
			Branch: "main",
		},
	}

	grouped := groupHopsByRepo(hops)

	assert.Len(t, grouped, 2)
	assert.Len(t, grouped["test/repo"], 2)
	assert.Len(t, grouped["other/repo"], 1)
}
