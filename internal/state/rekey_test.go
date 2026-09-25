package state

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Repo IDs carry the host of the origin. State written before keyed every
// repository github.com/<org>/<repo>; LoadState moves each to the key its
// hubs' origin gives, and the next save backs the old file up.

const (
	gitlabOrigin = "git@gitlab.example.com:acme/widgets.git"
	githubOrigin = "https://github.com/acme/widgets.git"
	oldKey       = "github.com/acme/widgets"
	gitlabKey    = "gitlab.example.com/acme/widgets"
)

// isolateGitDomain points --global git config at a fresh file, holding
// hop.gitDomain when domain is not empty.
func isolateGitDomain(t *testing.T, domain string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", file)
	if domain != "" {
		out, err := exec.Command("git", "config", "--global", "hop.gitDomain", domain).CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

// writeHub writes a hub hop.json at path whose repo.uri is uri.
func writeHub(t *testing.T, fs afero.Fs, path, uri string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"repo": map[string]any{"uri": uri, "org": "acme", "repo": "widgets", "defaultBranch": "main"},
	})
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(path, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(path, "hop.json"), data, 0o644))
}

// repoWithHubs is a repository entry with one main worktree per hub.
func repoWithHubs(uri string, hubs ...string) *RepositoryState {
	repo := &RepositoryState{URI: uri, Org: "acme", Repo: "widgets", DefaultBranch: "main", Worktrees: map[string]*WorktreeState{}}
	for _, h := range hubs {
		repo.Hubs = append(repo.Hubs, &HubState{Path: h, Mode: HubModeLocal})
		wt := filepath.Join(h, "hops", "main")
		repo.Worktrees[WorktreeKey(wt)] = &WorktreeState{Path: wt, Branch: "main", Type: "bare", HubPath: h}
	}
	return repo
}

// seedState writes st as state.json the way a release before the change
// would have, and returns the bytes written.
func seedState(t *testing.T, fs afero.Fs, st *State) []byte {
	t.Helper()
	st.Version = Version
	data, err := json.MarshalIndent(st, "", "  ")
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(GetStateHome(), 0o755))
	require.NoError(t, afero.WriteFile(fs, statePath(), data, 0o644))
	return data
}

func repoKeys(st *State) []string {
	keys := make([]string, 0, len(st.Repositories))
	for k := range st.Repositories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLoadState_RekeysNonGitHubOrigin(t *testing.T) {
	isolateGitDomain(t, "")
	fs := afero.NewMemMapFs()
	writeHub(t, fs, "/hubs/widgets", gitlabOrigin)
	old := NewState()
	old.AddRepository(oldKey, repoWithHubs(gitlabOrigin, "/hubs/widgets"))
	seedState(t, fs, old)

	st, err := LoadState(fs)
	require.NoError(t, err)
	require.Equal(t, []string{gitlabKey}, repoKeys(st))
	repo := st.Repositories[gitlabKey]
	require.Len(t, repo.Hubs, 1)
	assert.Equal(t, "/hubs/widgets", repo.Hubs[0].Path)
	assert.Len(t, repo.Worktrees, 1, "worktrees move with the repository")
	assert.Empty(t, RepoIDCollisions(fs, st))
}

func TestLoadState_GitHubOriginKeepsKey(t *testing.T) {
	isolateGitDomain(t, "git.corp.example")
	fs := afero.NewMemMapFs()
	writeHub(t, fs, "/hubs/widgets", githubOrigin)
	old := NewState()
	old.AddRepository(oldKey, repoWithHubs(githubOrigin, "/hubs/widgets"))
	seedState(t, fs, old)

	st, err := LoadState(fs)
	require.NoError(t, err)
	assert.Equal(t, []string{oldKey}, repoKeys(st))
}

// A hub whose origin has no host (a local path, file://, none) is keyed
// by hop.gitDomain, github.com by default.
func TestLoadState_LocalOriginUsesGitDomain(t *testing.T) {
	for _, origin := range []string{"/srv/git/acme/widgets.git", "file:///srv/git/acme/widgets.git", ""} {
		t.Run("default/"+origin, func(t *testing.T) {
			isolateGitDomain(t, "")
			fs := afero.NewMemMapFs()
			writeHub(t, fs, "/hubs/widgets", origin)
			old := NewState()
			old.AddRepository(oldKey, repoWithHubs(origin, "/hubs/widgets"))
			seedState(t, fs, old)

			st, err := LoadState(fs)
			require.NoError(t, err)
			assert.Equal(t, []string{oldKey}, repoKeys(st))
		})
		t.Run("gitDomain/"+origin, func(t *testing.T) {
			isolateGitDomain(t, "git.corp.example")
			fs := afero.NewMemMapFs()
			writeHub(t, fs, "/hubs/widgets", origin)
			old := NewState()
			old.AddRepository(oldKey, repoWithHubs(origin, "/hubs/widgets"))
			seedState(t, fs, old)

			st, err := LoadState(fs)
			require.NoError(t, err)
			assert.Equal(t, []string{"git.corp.example/acme/widgets"}, repoKeys(st))
		})
	}
}

// Without a readable hop.json there is no origin to go by.
func TestLoadState_UnreadableHubKeepsKey(t *testing.T) {
	isolateGitDomain(t, "")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/hubs/broken", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/hubs/broken/hop.json", []byte("{not json"), 0o644))
	old := NewState()
	old.AddRepository(oldKey, repoWithHubs(gitlabOrigin, "/hubs/gone", "/hubs/broken"))
	seedState(t, fs, old)

	st, err := LoadState(fs)
	require.NoError(t, err)
	assert.Equal(t, []string{oldKey}, repoKeys(st))
	assert.Len(t, st.Repositories[oldKey].Hubs, 2)
}

// One entry that held hubs of two repositories (same org/repo, two
// hosts) is split: each hub and its worktrees go to its own key; a hub
// that cannot be read stays with the old key.
func TestLoadState_SplitsHubsOfDifferentHosts(t *testing.T) {
	isolateGitDomain(t, "")
	fs := afero.NewMemMapFs()
	writeHub(t, fs, "/hubs/gh", githubOrigin)
	writeHub(t, fs, "/hubs/gl", gitlabOrigin)
	old := NewState()
	repo := repoWithHubs(githubOrigin, "/hubs/gh", "/hubs/gl", "/hubs/gone")
	repo.Worktrees["/elsewhere/x"] = &WorktreeState{Path: "/elsewhere/x", Branch: "x"}
	old.AddRepository(oldKey, repo)
	seedState(t, fs, old)

	st, err := LoadState(fs)
	require.NoError(t, err)
	require.Equal(t, []string{oldKey, gitlabKey}, repoKeys(st))

	gh := st.Repositories[oldKey]
	assert.ElementsMatch(t, []string{"/hubs/gh", "/hubs/gone"}, hubPaths(gh.Hubs))
	assert.Len(t, gh.Worktrees, 3, "gh main, gone main, and the hubless worktree stay")
	assert.Contains(t, gh.Worktrees, "/elsewhere/x")

	gl := st.Repositories[gitlabKey]
	assert.Equal(t, []string{"/hubs/gl"}, hubPaths(gl.Hubs))
	require.Len(t, gl.Worktrees, 1)
	for _, wt := range gl.Worktrees {
		assert.Equal(t, "/hubs/gl", wt.HubPath)
	}
	assert.Equal(t, gitlabOrigin, gl.URI)
	assert.Equal(t, "main", gl.DefaultBranch)
}

// Rekeying is persisted by the next save, after a backup of the file as
// it was; saving and loading again changes nothing and backs up nothing.
func TestSaveState_RekeyBacksUpOnceAndIsIdempotent(t *testing.T) {
	isolateGitDomain(t, "")
	fs := afero.NewMemMapFs()
	writeHub(t, fs, "/hubs/widgets", gitlabOrigin)
	old := NewState()
	old.AddRepository(oldKey, repoWithHubs(gitlabOrigin, "/hubs/widgets"))
	original := seedState(t, fs, old)

	first := loadAndSave(t, fs)
	got := backups(t, fs)
	require.Len(t, got, 1, "one backup of the pre-rekey file")
	for name, data := range got {
		assert.True(t, IsBackupName(name), name)
		assert.Equal(t, string(original), string(data))
	}

	saved, err := parseState(first)
	require.NoError(t, err)
	assert.Equal(t, []string{gitlabKey}, repoKeys(saved), "the rekey is on disk")

	second := loadAndSave(t, fs)
	assert.Equal(t, normalizeSaved(first), normalizeSaved(second))
	assert.Len(t, backups(t, fs), 1, "a rekeyed file is not backed up again")
	assert.False(t, needsRekey(fs, saved))
}

// A repository whose origin gives a key state already holds is not
// merged into it: both entries stay, the file is not rewritten for it,
// and RepoIDCollisions (what doctor reports) names it, on every load.
func TestLoadState_CollisionKeepsBoth(t *testing.T) {
	isolateGitDomain(t, "")
	fs := afero.NewMemMapFs()
	writeHub(t, fs, "/hubs/old", gitlabOrigin)
	writeHub(t, fs, "/hubs/new", gitlabOrigin)
	old := NewState()
	old.AddRepository(oldKey, repoWithHubs(gitlabOrigin, "/hubs/old"))
	old.AddRepository(gitlabKey, repoWithHubs(gitlabOrigin, "/hubs/new"))
	seedState(t, fs, old)

	for run := 0; run < 2; run++ {
		st, err := LoadState(fs)
		require.NoError(t, err)
		require.Equal(t, []string{oldKey, gitlabKey}, repoKeys(st), "run %d", run)
		assert.Equal(t, []string{"/hubs/old"}, hubPaths(st.Repositories[oldKey].Hubs))
		assert.Equal(t, []string{"/hubs/new"}, hubPaths(st.Repositories[gitlabKey].Hubs))
		assert.Equal(t, []RepoIDCollision{{From: oldKey, To: gitlabKey, Hubs: []string{"/hubs/old"}}}, RepoIDCollisions(fs, st))
		require.NoError(t, SaveState(fs, st))
	}
	assert.Empty(t, backups(t, fs), "nothing was rekeyed, so nothing was backed up")
}

// Two old entries can't both claim one new key in a single pass.
func TestPlanRekey_OneTargetClaimedOnce(t *testing.T) {
	isolateGitDomain(t, "")
	fs := afero.NewMemMapFs()
	writeHub(t, fs, "/hubs/a", gitlabOrigin)
	st := NewState()
	st.AddRepository(oldKey, repoWithHubs(gitlabOrigin, "/hubs/a"))
	// A key of another shape is left alone.
	st.AddRepository("acme/widgets", repoWithHubs(gitlabOrigin, "/hubs/a"))

	changed, collisions := rekeyRepoIDs(fs, st)
	assert.True(t, changed)
	assert.Empty(t, collisions)
	assert.Equal(t, []string{"acme/widgets", gitlabKey}, repoKeys(st))
}

// hop.gitDomain is resolved in each hub: a hub's own value keys a
// repository whose origin has no host.
func TestLoadState_LocalOriginUsesHubGitDomain(t *testing.T) {
	isolateGitDomain(t, "global.example")
	hub := t.TempDir()
	out, err := exec.Command("git", "init", "-q", "--bare", hub).CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("git", "-C", hub, "config", "hop.gitDomain", "hub.example").CombinedOutput()
	require.NoError(t, err, string(out))

	fs := afero.NewMemMapFs()
	const origin = "/srv/git/acme/widgets.git"
	writeHub(t, fs, hub, origin)
	old := NewState()
	old.AddRepository(oldKey, repoWithHubs(origin, hub))
	seedState(t, fs, old)

	st, err := LoadState(fs)
	require.NoError(t, err)
	assert.Equal(t, []string{"hub.example/acme/widgets"}, repoKeys(st))
}
