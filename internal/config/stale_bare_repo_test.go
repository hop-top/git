package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
)

// The bareRepo setting was removed: hubs are always bare
// (docs/stories/015-hopspace-shape-contract.md). Configs written by older
// releases still carry it in git config hop.bareRepo and the legacy
// global.json. Both must keep loading, and git-hop must neither read the
// stale key nor write it back.

const staleBareRepoKey = "hop.bareRepo"

func TestLoad_StaleBareRepoGitConfigIsIgnored(t *testing.T) {
	// A non-boolean value proves the key is not parsed at all.
	for _, v := range []string{"false", "true", "not-a-bool"} {
		t.Run(v, func(t *testing.T) {
			store := map[string]string{
				staleBareRepoKey: v,
				"hop.gitDomain":  "gitlab.com",
			}
			loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))

			cfg := loader.Load()
			if cfg.Defaults.GitDomain != "gitlab.com" {
				t.Errorf("GitDomain = %q, want gitlab.com", cfg.Defaults.GitDomain)
			}

			if err := loader.WriteShellIntegration(cfg.ShellIntegration); err != nil {
				t.Fatalf("WriteShellIntegration() error = %v", err)
			}
			if got := store[staleBareRepoKey]; got != v {
				t.Errorf("WriteShellIntegration() touched user's %s: got %q, want %q left as-is",
					staleBareRepoKey, got, v)
			}
		})
	}
}

func TestMigration_LegacyJSONWithBareRepo(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	configDir := filepath.Join(tmpDir, ".config", "git-hop")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"defaults": {"bareRepo": true, "gitDomain": "gitlab.com"}}`
	jsonPath := filepath.Join(configDir, "global.json")
	if err := os.WriteFile(jsonPath, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	store := map[string]string{}
	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))

	cfg := loader.Load()
	if store["hop.migrated"] != "true" {
		t.Fatalf("legacy global.json was not migrated; store = %v", store)
	}
	if cfg.Defaults.GitDomain != "gitlab.com" {
		t.Errorf("migrated GitDomain = %q, want gitlab.com", cfg.Defaults.GitDomain)
	}
	if v, ok := store[staleBareRepoKey]; ok {
		t.Errorf("migration wrote %s = %q; the setting no longer exists", staleBareRepoKey, v)
	}
}

// hop.json never modeled bareRepo, but a hand-edited or older file may carry
// it. It must load, and a rewrite must keep repo.isBare (the recorded layout)
// and must not fail on the stale member.
func TestHopJSON_StaleBareRepoLoadsAndRewrites(t *testing.T) {
	const hopJSON = `{
  "bareRepo": false,
  "defaults": {"bareRepo": false},
  "repo": {
    "uri": "file:///origin",
    "org": "acme",
    "repo": "widget",
    "defaultBranch": "main",
    "isBare": true,
    "structure": "bare-worktree"
  },
  "branches": {"main": {"path": "/hub/hops/main", "hopspaceBranch": "main"}}
}`
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/hub/hop.json", []byte(hopJSON), 0644); err != nil {
		t.Fatal(err)
	}

	hub, err := config.NewLoader(fs).LoadHubConfig("/hub")
	if err != nil {
		t.Fatalf("LoadHubConfig() error = %v", err)
	}
	if hub.Repo.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want main", hub.Repo.DefaultBranch)
	}
	if _, err := config.NewLoader(fs).LoadHopspaceConfig("/hub"); err != nil {
		t.Fatalf("LoadHopspaceConfig() error = %v", err)
	}

	if err := config.NewWriter(fs).WriteHubConfig("/hub", hub); err != nil {
		t.Fatalf("WriteHubConfig() error = %v", err)
	}
	data, err := afero.ReadFile(fs, "/hub/hop.json")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Repo struct {
			IsBare *bool `json:"isBare"`
		} `json:"repo"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("hop.json: %v\n%s", err, data)
	}
	if out.Repo.IsBare == nil || !*out.Repo.IsBare {
		t.Errorf("repo.isBare lost on rewrite\n%s", data)
	}
}
