package hop

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
)

const sharedSpace = "/data/org/repo"

// legacySharedHopJSON is a shared hopspace's hop.json as an earlier
// release wrote it: records keyed by branch, whichever hub wrote last.
const legacySharedHopJSON = `{
  "repo": {"uri": "u", "org": "org", "repo": "repo", "defaultBranch": "main"},
  "branches": {
    "main": {"exists": true, "path": "/g1/app/hops/main", "lastSync": "2026-01-01T00:00:00Z"},
    "feat/y": {"exists": true, "path": "/g2/app/hops/feat/y", "lastSync": "2026-01-01T00:00:00Z", "note": "kept"},
    "feat/gone": {"exists": true, "path": "/elsewhere/hops/feat/gone", "lastSync": "2026-01-01T00:00:00Z"},
    "feat/pm": {"exists": true, "path": "/g1/app/hops/feat/pm", "lastSync": "2026-01-01T00:00:00Z",
      "packageManagers": {"npm": {"installCmd": ["npm", "ci"]}}},
    "settings-only": {"exists": false, "path": "", "lastSync": "0001-01-01T00:00:00Z",
      "packageManagers": {"npm": {"installCmd": ["npm", "i"]}}}
  },
  "forks": {},
  "custom": {"a": 1}
}`

// writeSharedFixture writes the legacy hopspace and a state that knows
// hub /g2/app (its feat/y worktree too); /g1/app is the hub running.
func writeSharedFixture(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, filepath.Join(sharedSpace, "hop.json"), []byte(legacySharedHopJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	st := state.NewState()
	st.AddRepository("github.com/org/repo", &state.RepositoryState{
		Worktrees: map[string]*state.WorktreeState{
			"/g2/app/hops/feat/y": {Path: "/g2/app/hops/feat/y", Branch: "feat/y", HubPath: "/g2/app"},
		},
		Hubs: []*state.HubState{{Path: "/g2/app", Mode: state.HubModeGlobal}},
	})
	if err := state.SaveState(fs, st); err != nil {
		t.Fatal(err)
	}
	return fs
}

func hopJSONBackups(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	got, err := afero.Glob(fs, filepath.Join(dir, "hop.json.*.bak"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The first write for a --global hub moves every record an earlier
// release keyed by branch to its worktree's path, after backing the file
// up; a second write finds nothing to migrate.
func TestSharedHopspace_MigratesBranchKeys(t *testing.T) {
	fs := writeSharedFixture(t)
	hs, err := LoadHopspace(fs, sharedSpace)
	if err != nil {
		t.Fatal(err)
	}
	if err := hs.RegisterBranch("/g1/app", "feat/q", "/g1/app/hops/feat/q"); err != nil {
		t.Fatal(err)
	}

	backups := hopJSONBackups(t, fs, sharedSpace)
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want one", backups)
	}
	if data, _ := afero.ReadFile(fs, backups[0]); string(data) != legacySharedHopJSON {
		t.Errorf("backup holds %q, want the file as it was", data)
	}

	doc := readHopJSONDoc(t, fs, sharedSpace)
	branches, _ := doc["branches"].(map[string]any)
	entry := func(key string) map[string]any {
		e, _ := branches[key].(map[string]any)
		return e
	}
	for key, want := range map[string][2]string{
		"/g1/app/hops/main":    {"main", "/g1/app"},
		"/g2/app/hops/feat/y":  {"feat/y", "/g2/app"},
		"/g1/app/hops/feat/pm": {"feat/pm", "/g1/app"},
		"/g1/app/hops/feat/q":  {"feat/q", "/g1/app"},
	} {
		e := entry(key)
		if e == nil || e["branch"] != want[0] || e["hub"] != want[1] || e["exists"] != true {
			t.Errorf("branches[%s] = %v, want branch %s of hub %s", key, e, want[0], want[1])
		}
	}
	for _, key := range []string{"main", "feat/y", "feat/gone", "/elsewhere/hops/feat/gone"} {
		if _, ok := branches[key]; ok {
			t.Errorf("branches[%s] is still there", key)
		}
	}
	if e := entry("/g2/app/hops/feat/y"); e["note"] != "kept" {
		t.Errorf("feat/y's unmodeled member = %v, want it carried to the path key", e["note"])
	}
	if e := entry("/g1/app/hops/feat/pm"); e["packageManagers"] != nil {
		t.Errorf("feat/pm's record keeps packageManagers %v; they configure the branch", e["packageManagers"])
	}
	for key, cmd := range map[string]string{"feat/pm": "ci", "settings-only": "i"} {
		pm, _ := entry(key)["packageManagers"].(map[string]any)
		npm, _ := pm["npm"].(map[string]any)
		args, _ := npm["installCmd"].([]any)
		if len(args) != 2 || args[1] != cmd {
			t.Errorf("branches[%s].packageManagers = %v, want the npm %s override kept", key, pm, cmd)
		}
	}
	if custom, _ := doc["custom"].(map[string]any); custom["a"] != float64(1) {
		t.Errorf("custom = %v, want the unmodeled member kept", doc["custom"])
	}

	// Idempotent: nothing left to migrate, so no new backup and no change.
	before, _ := afero.ReadFile(fs, filepath.Join(sharedSpace, "hop.json"))
	if err := hs.Update("/g1/app", func(*config.HopspaceConfig) error { return errUnchanged }); err != nil {
		t.Fatal(err)
	}
	after, _ := afero.ReadFile(fs, filepath.Join(sharedSpace, "hop.json"))
	if !bytes.Equal(before, after) {
		t.Errorf("second run rewrote hop.json:\n%s\nwas\n%s", after, before)
	}
	if got := hopJSONBackups(t, fs, sharedSpace); len(got) != 1 {
		t.Errorf("backups after a second run = %v, want still one", got)
	}
}

// A hub's own hop.json keeps its branch keys: hub and hopspace share
// them there.
func TestHubOwnHopspace_KeepsBranchKeys(t *testing.T) {
	fs := afero.NewMemMapFs()
	if _, err := CreateHub(fs, "/hub", "u", "org", "repo", "main"); err != nil {
		t.Fatal(err)
	}
	hub, _ := LoadHub(fs, "/hub")
	if err := hub.AddBranch("feat", "feat", "/hub/hops/feat"); err != nil {
		t.Fatal(err)
	}
	hs, err := LoadHopspace(fs, "/hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := hs.RegisterBranch("/hub", "feat", "/hub/hops/feat"); err != nil {
		t.Fatal(err)
	}
	branches, _ := readHopJSONDoc(t, fs, "/hub")["branches"].(map[string]any)
	e, _ := branches["feat"].(map[string]any)
	if e == nil || e["exists"] != true || e["hopspaceBranch"] != "feat" {
		t.Fatalf("branches[feat] = %v, want hub and hopspace members under the branch", e)
	}
	if _, ok := e["branch"]; ok {
		t.Errorf("branches[feat] = %v, want no branch member in a hub's own hop.json", e)
	}
	if _, ok := e["hub"]; ok {
		t.Errorf("branches[feat] = %v, want no hub member in a hub's own hop.json", e)
	}
	if got := hopJSONBackups(t, fs, "/hub"); len(got) != 0 {
		t.Errorf("backups = %v, want none", got)
	}
}

// Two --global hubs with a worktree of the same branch each keep their
// record: removing or moving one leaves the other.
func TestSharedHopspace_SameBranchInTwoHubs(t *testing.T) {
	fs := afero.NewMemMapFs()
	hs, err := InitHopspace(fs, sharedSpace, "u", "org", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, hub := range []string{"/g1/app", "/g2/app"} {
		if err := hs.RegisterBranch(hub, "feat/y", hub+"/hops/feat/y"); err != nil {
			t.Fatal(err)
		}
	}
	if err := hs.RenameBranch("/g1/app", "feat/y", "feat/w", "/g1/app/hops/feat/y", "/g1/app/hops/feat/w"); err != nil {
		t.Fatal(err)
	}
	if e, ok := hs.Entry("/g2/app", "feat/y", "/g2/app/hops/feat/y"); !ok || e.Branch != "feat/y" {
		t.Fatalf("g2's feat/y after g1's move = %+v, %v", e, ok)
	}
	if e, ok := hs.Entry("/g1/app", "feat/w", "/g1/app/hops/feat/w"); !ok || e.Branch != "feat/w" || e.Path != "/g1/app/hops/feat/w" {
		t.Fatalf("g1's feat/w = %+v, %v", e, ok)
	}
	if err := hs.UnregisterBranch("/g2/app", "feat/y", "/g2/app/hops/feat/y"); err != nil {
		t.Fatal(err)
	}
	if _, ok := hs.Entry("/g2/app", "feat/y", "/g2/app/hops/feat/y"); ok {
		t.Error("g2's feat/y is still recorded")
	}
	if _, ok := hs.Entry("/g1/app", "feat/w", "/g1/app/hops/feat/w"); !ok {
		t.Error("g2's remove dropped g1's record")
	}
}
