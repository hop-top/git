package hop_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

func strPtr(s string) *string { return &s }

// A rename rekeys the hub entry; everything recorded about the branch other
// than its location must carry over, and hopspaceBranch must follow the git
// branch it names.
func TestHubRenameBranch_KeepsEntryFields(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	writer := config.NewWriter(fs)
	_ = writer.WriteHubConfig(hubPath, &config.HubConfig{
		Repo: config.RepoConfig{DefaultBranch: "main"},
		Branches: map[string]config.HubBranch{
			"feat/x": {Path: "/hub/hops/feat/x", HopspaceBranch: "feat/x", Base: strPtr("develop")},
		},
	})
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := hub.RenameBranch("feat/x", "feat/y", "/hub/hops/feat/y"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}

	reloaded, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Config.Branches["feat/x"]; ok {
		t.Error("old key feat/x still present")
	}
	got, ok := reloaded.Config.Branches["feat/y"]
	if !ok {
		t.Fatal("new key feat/y missing")
	}
	if got.Path != "/hub/hops/feat/y" {
		t.Errorf("Path = %q, want /hub/hops/feat/y", got.Path)
	}
	if got.HopspaceBranch != "feat/y" {
		t.Errorf("HopspaceBranch = %q, want feat/y (the renamed git branch)", got.HopspaceBranch)
	}
	if got.Base == nil || *got.Base != "develop" {
		t.Errorf("Base = %v, want develop", got.Base)
	}
}

// A fork entry's hopspaceBranch names the branch in the fork's hopspace, not
// the hub key; renaming the hub key must leave it, and the fork, alone.
func TestHubRenameBranch_ForkEntryKeepsHopspaceBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	writer := config.NewWriter(fs)
	_ = writer.WriteHubConfig(hubPath, &config.HubConfig{
		Repo: config.RepoConfig{DefaultBranch: "main"},
		Branches: map[string]config.HubBranch{
			"alice-fix": {Path: "/hub/hops/alice-fix", HopspaceBranch: "fix", Fork: strPtr("alice")},
		},
	})
	hub, _ := hop.LoadHub(fs, hubPath)

	if err := hub.RenameBranch("alice-fix", "alice-fix2", "/hub/hops/alice-fix2"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}

	got := hub.Config.Branches["alice-fix2"]
	if got.HopspaceBranch != "fix" {
		t.Errorf("HopspaceBranch = %q, want fix (fork-side branch is not renamed)", got.HopspaceBranch)
	}
	if got.Fork == nil || *got.Fork != "alice" {
		t.Errorf("Fork = %v, want alice", got.Fork)
	}
}

func TestHopspaceRenameBranch_KeepsEntryFields(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/github.com/org/repo"
	sync := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pm := map[string]config.PackageManagerOverride{"npm": {InstallCmd: []string{"npm", "ci"}}}
	writer := config.NewWriter(fs)
	_ = writer.WriteHopspaceConfig(path, &config.HopspaceConfig{
		Repo: config.RepoConfig{DefaultBranch: "main"},
		Branches: map[string]config.HopspaceBranch{
			"feat/x": {Exists: true, Path: path + "/feat/x", LastSync: sync, PackageManagers: pm},
		},
		Forks: map[string]config.HopspaceFork{},
	})
	hs, err := hop.LoadHopspace(fs, path)
	if err != nil {
		t.Fatal(err)
	}

	if err := hs.RenameBranch(hs.Path, "feat/x", "feat/y", path+"/feat/x", path+"/feat/y"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}

	reloaded, _ := hop.LoadHopspace(fs, path)
	got, ok := reloaded.Config.Branches["feat/y"]
	if !ok {
		t.Fatal("new key feat/y missing")
	}
	if got.Path != path+"/feat/y" || !got.Exists || !got.LastSync.Equal(sync) {
		t.Errorf("entry = %+v, want path/exists/lastSync carried over", got)
	}
	if cmd := got.PackageManagers["npm"].InstallCmd; len(cmd) != 2 || cmd[1] != "ci" {
		t.Errorf("PackageManagers = %v, want npm override kept", got.PackageManagers)
	}
}

// When the hopspace lives in the hub (one hop.json for both), the two
// rename writes land in the same file; the final entry must hold both
// halves: hub fields (base, hopspaceBranch) and hopspace fields.
func TestMoveWorktree_SharedHopJSONKeepsBranchEntry(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	oldPath := "/hub/hops/feat/x"
	_ = fs.MkdirAll(oldPath, 0755)
	_ = fs.MkdirAll(hubPath+"/hops/main", 0755)

	writer := config.NewWriter(fs)
	_ = writer.WriteHopspaceConfig(hubPath, &config.HopspaceConfig{
		Repo: config.RepoConfig{DefaultBranch: "main"},
		Branches: map[string]config.HopspaceBranch{
			"feat/x": {Exists: true, Path: oldPath},
		},
		Forks: map[string]config.HopspaceFork{},
	})
	hs, _ := hop.LoadHopspace(fs, hubPath)
	_ = writer.WriteHubConfig(hubPath, &config.HubConfig{
		Repo: config.RepoConfig{DefaultBranch: "main"},
		Branches: map[string]config.HubBranch{
			"main":   {Path: hubPath + "/hops/main", HopspaceBranch: "main"},
			"feat/x": {Path: oldPath, HopspaceBranch: "feat/x", Base: strPtr("develop")},
		},
	})
	hub, _ := hop.LoadHub(fs, hubPath)

	wm := hop.NewWorktreeManager(fs, mocks.NewMockGit())
	if _, _, err := wm.MoveWorktree(hs, hub, "feat/x", "feat/y", "{hubPath}/hops/{branch}", "org", "repo"); err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}

	raw, err := afero.ReadFile(fs, hubPath+"/hop.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Branches map[string]map[string]any `json:"branches"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Branches["feat/x"]; ok {
		t.Error("old key feat/x still present")
	}
	entry := doc.Branches["feat/y"]
	want := map[string]any{
		"path":           "/hub/hops/feat/y",
		"hopspaceBranch": "feat/y",
		"base":           "develop",
		"exists":         true,
	}
	for k, v := range want {
		if entry[k] != v {
			t.Errorf("branches[feat/y].%s = %v, want %v", k, entry[k], v)
		}
	}
}

// branchEntry reads branches[branch] from the hop.json at dir as raw JSON,
// so members no Go type models are visible.
func branchEntry(t *testing.T, fs afero.Fs, dir, branch string) (map[string]any, bool) {
	t.Helper()
	raw, err := afero.ReadFile(fs, dir+"/hop.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Branches map[string]map[string]any `json:"branches"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	e, ok := doc.Branches[branch]
	return e, ok
}

// Members of a branch entry that neither HubBranch nor HopspaceBranch
// models (another tool's, or a newer git-hop's) belong to the branch, not
// to its name: a rename must carry them to the new key.
func TestHubRenameBranch_KeepsUnmodeledMembers(t *testing.T) {
	fs := afero.NewMemMapFs()
	_ = afero.WriteFile(fs, "/hub/hop.json", []byte(`{
  "repo": {"defaultBranch": "main"},
  "branches": {
    "feat/x": {"path": "/hub/hops/feat/x", "hopspaceBranch": "feat/x", "ticket": "ABC-1", "meta": {"owner": "kim"}}
  }
}`), 0644)
	hub, err := hop.LoadHub(fs, "/hub")
	if err != nil {
		t.Fatal(err)
	}

	if err := hub.RenameBranch("feat/x", "feat/y", "/hub/hops/feat/y"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}

	if _, ok := branchEntry(t, fs, "/hub", "feat/x"); ok {
		t.Error("old key feat/x still present")
	}
	got, ok := branchEntry(t, fs, "/hub", "feat/y")
	if !ok {
		t.Fatal("new key feat/y missing")
	}
	if got["ticket"] != "ABC-1" {
		t.Errorf("branches[feat/y].ticket = %v, want ABC-1", got["ticket"])
	}
	if meta, _ := got["meta"].(map[string]any); meta["owner"] != "kim" {
		t.Errorf("branches[feat/y].meta = %v, want owner kim", got["meta"])
	}
	if got["path"] != "/hub/hops/feat/y" {
		t.Errorf("branches[feat/y].path = %v, want the new path", got["path"])
	}
}

func TestHopspaceRenameBranch_KeepsUnmodeledMembers(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/github.com/org/repo"
	_ = afero.WriteFile(fs, path+"/hop.json", []byte(`{
  "repo": {"defaultBranch": "main"},
  "branches": {
    "feat/x": {"exists": true, "path": "`+path+`/feat/x", "lastSync": "2026-01-02T03:04:05Z", "ticket": "ABC-1"}
  },
  "forks": {}
}`), 0644)
	hs, err := hop.LoadHopspace(fs, path)
	if err != nil {
		t.Fatal(err)
	}

	if err := hs.RenameBranch(hs.Path, "feat/x", "feat/y", path+"/feat/x", path+"/feat/y"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}

	if _, ok := branchEntry(t, fs, path, "feat/x"); ok {
		t.Error("old key feat/x still present")
	}
	got, ok := branchEntry(t, fs, path, "feat/y")
	if !ok {
		t.Fatal("new key feat/y missing")
	}
	if got["ticket"] != "ABC-1" {
		t.Errorf("branches[feat/y].ticket = %v, want ABC-1", got["ticket"])
	}
}

// Shared hop.json: the hub write goes first and must carry the members it
// does not model (hopspace's and third-party ones) to the new key, so the
// file never holds an entry missing them.
func TestMoveWorktree_SharedHopJSONKeepsUnmodeledMembers(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	oldPath := "/hub/hops/feat/x"
	_ = fs.MkdirAll(oldPath, 0755)
	_ = fs.MkdirAll(hubPath+"/hops/main", 0755)
	_ = afero.WriteFile(fs, hubPath+"/hop.json", []byte(`{
  "repo": {"defaultBranch": "main"},
  "branches": {
    "main": {"path": "/hub/hops/main", "hopspaceBranch": "main", "exists": true},
    "feat/x": {"path": "`+oldPath+`", "hopspaceBranch": "feat/x", "exists": true, "ticket": "ABC-1"}
  },
  "forks": {}
}`), 0644)
	hs, _ := hop.LoadHopspace(fs, hubPath)
	hub, _ := hop.LoadHub(fs, hubPath)

	wm := hop.NewWorktreeManager(fs, mocks.NewMockGit())
	if _, _, err := wm.MoveWorktree(hs, hub, "feat/x", "feat/y", "{hubPath}/hops/{branch}", "org", "repo"); err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}

	got, ok := branchEntry(t, fs, hubPath, "feat/y")
	if !ok {
		t.Fatal("new key feat/y missing")
	}
	if got["ticket"] != "ABC-1" {
		t.Errorf("branches[feat/y].ticket = %v, want ABC-1", got["ticket"])
	}
	if got["exists"] != true || got["hopspaceBranch"] != "feat/y" {
		t.Errorf("branches[feat/y] = %v, want hub and hopspace halves", got)
	}
}
