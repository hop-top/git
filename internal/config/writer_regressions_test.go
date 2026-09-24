package config

import (
	"encoding/json"
	"testing"

	"github.com/spf13/afero"
)

// cloneShapedHopJSON mirrors the merged hub+hopspace hop.json that
// `git hop <uri>` writes in local mode. repo.structure, repo.isBare,
// forks and branches.<b>.exists/lastSync are not modeled by HubConfig.
const cloneShapedHopJSON = `{
  "branches": {
    "main": {
      "exists": true,
      "hopspaceBranch": "main",
      "lastSync": "2026-09-23T21:22:04-04:00",
      "path": "/hub/hops/main"
    }
  },
  "forks": {},
  "repo": {
    "defaultBranch": "main",
    "isBare": true,
    "org": "acme",
    "repo": "widget",
    "structure": "bare-worktree",
    "uri": "file:///origin"
  },
  "settings": {
    "envPatterns": ["dev", "staging", "qa"]
  }
}`

func seedHopJSON(t *testing.T, content string) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/hub/hop.json", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return fs
}

func readHopJSON(t *testing.T, fs afero.Fs) map[string]any {
	t.Helper()
	data, err := afero.ReadFile(fs, "/hub/hop.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("hop.json not valid JSON: %v\n%s", err, data)
	}
	return m
}

func obj(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	cur := m
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			t.Fatalf("missing object at %v (have %v)", keys, cur)
		}
		cur = next
	}
	return cur
}

func TestWriteHubConfig_PreservesUnmodeledFields(t *testing.T) {
	fs := seedHopJSON(t, cloneShapedHopJSON)

	cfg, err := NewLoader(fs).LoadHubConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Branches["feat/x"] = HubBranch{Path: "/hub/hops/feat/x", HopspaceBranch: "feat/x"}
	if err := NewWriter(fs).WriteHubConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}

	got := readHopJSON(t, fs)
	if _, ok := got["forks"]; !ok {
		t.Error("forks dropped")
	}
	repo := obj(t, got, "repo")
	if repo["structure"] != "bare-worktree" {
		t.Errorf("repo.structure = %v, want bare-worktree", repo["structure"])
	}
	if repo["isBare"] != true {
		t.Errorf("repo.isBare = %v, want true", repo["isBare"])
	}
	main := obj(t, got, "branches", "main")
	if main["exists"] != true {
		t.Errorf("branches.main.exists = %v, want true", main["exists"])
	}
	if main["lastSync"] != "2026-09-23T21:22:04-04:00" {
		t.Errorf("branches.main.lastSync = %v", main["lastSync"])
	}
	if obj(t, got, "branches", "feat/x")["hopspaceBranch"] != "feat/x" {
		t.Error("new branch not written")
	}
}

func TestWriteHopspaceConfig_PreservesHubFieldsInSharedFile(t *testing.T) {
	fs := seedHopJSON(t, cloneShapedHopJSON)

	cfg, err := NewLoader(fs).LoadHopspaceConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := NewWriter(fs).WriteHopspaceConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}

	got := readHopJSON(t, fs)
	if _, ok := got["settings"]; !ok {
		t.Error("settings dropped")
	}
	if obj(t, got, "branches", "main")["hopspaceBranch"] != "main" {
		t.Error("branches.main.hopspaceBranch dropped")
	}
	if obj(t, got, "repo")["structure"] != "bare-worktree" {
		t.Error("repo.structure dropped")
	}
}

// Writes from a stale in-memory snapshot must not erase fields another
// view (hub vs hopspace sharing one file) persisted after the snapshot
// was loaded.
func TestWriteHubConfig_KeepsFieldsWrittenAfterLoad(t *testing.T) {
	fs := seedHopJSON(t, cloneShapedHopJSON)
	loader, writer := NewLoader(fs), NewWriter(fs)

	hub, err := loader.LoadHubConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	hs, err := loader.LoadHopspaceConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}

	hs.Branches["feat/x"] = HopspaceBranch{Exists: true, Path: "/hub/hops/feat/x"}
	if err := writer.WriteHopspaceConfig("/hub", hs); err != nil {
		t.Fatal(err)
	}
	hub.Branches["feat/x"] = HubBranch{Path: "/hub/hops/feat/x", HopspaceBranch: "feat/x"}
	if err := writer.WriteHubConfig("/hub", hub); err != nil {
		t.Fatal(err)
	}

	got := readHopJSON(t, fs)
	featX := obj(t, got, "branches", "feat/x")
	if featX["exists"] != true {
		t.Errorf("branches.feat/x.exists = %v, want true", featX["exists"])
	}
	if featX["hopspaceBranch"] != "feat/x" {
		t.Errorf("branches.feat/x.hopspaceBranch = %v", featX["hopspaceBranch"])
	}
}

// Inverse direction: modeled fields stay owned by the in-memory config.
func TestWriteHubConfig_ModeledDeletionsStick(t *testing.T) {
	fs := seedHopJSON(t, `{
  "repo": {"uri": "u", "org": "o", "repo": "r", "defaultBranch": "main"},
  "branches": {
    "main": {"path": "hops/main", "hopspaceBranch": "main", "base": "develop", "exists": true},
    "gone": {"path": "hops/gone", "hopspaceBranch": "gone", "exists": true}
  },
  "settings": {"envPatterns": [], "compareBranch": "develop"}
}`)

	cfg, err := NewLoader(fs).LoadHubConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	delete(cfg.Branches, "gone")
	b := cfg.Branches["main"]
	b.Base = nil
	cfg.Branches["main"] = b
	cfg.Settings.CompareBranch = nil
	if err := NewWriter(fs).WriteHubConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}

	got := readHopJSON(t, fs)
	if _, ok := obj(t, got, "branches")["gone"]; ok {
		t.Error("deleted branch resurrected from disk")
	}
	main := obj(t, got, "branches", "main")
	if _, ok := main["base"]; ok {
		t.Error("cleared branches.main.base resurrected from disk")
	}
	if main["exists"] != true {
		t.Error("unmodeled branches.main.exists dropped")
	}
	if _, ok := obj(t, got, "settings")["compareBranch"]; ok {
		t.Error("cleared settings.compareBranch resurrected from disk")
	}
}

func TestWriteHubConfig_NoExistingFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &HubConfig{Repo: RepoConfig{Org: "o"}, Branches: map[string]HubBranch{}}
	if err := NewWriter(fs).WriteHubConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}
	if obj(t, readHopJSON(t, fs), "repo")["org"] != "o" {
		t.Error("config not written")
	}
}
