package hop

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/state"
)

// twoHubsHopJSON is a shared hopspace recording the worktrees of hubs
// /g1/app and /g2/app under their paths, plus branch-level settings.
const twoHubsHopJSON = `{
  "repo": {"uri": "u", "org": "org", "repo": "repo", "defaultBranch": "main"},
  "branches": {
    "/g1/app/hops/main": {"exists": true, "path": "/g1/app/hops/main", "branch": "main", "hub": "/g1/app"},
    "/g1/app/hops/feat/y": {"exists": true, "path": "/g1/app/hops/feat/y", "branch": "feat/y", "hub": "/g1/app"},
    "/g2/app/hops/main": {"exists": true, "path": "/g2/app/hops/main", "branch": "main", "hub": "/g2/app"},
    "/g2/app/hops/feat/y": {"exists": true, "path": "/g2/app/hops/feat/y", "branch": "feat/y", "hub": "/g2/app"},
    "feat/y": {"exists": false, "path": "", "packageManagers": {"npm": {"installCmd": ["npm", "ci"]}}}
  }
}`

func loadFixture(t *testing.T, fs afero.Fs, data string) *Hopspace {
	t.Helper()
	if err := afero.WriteFile(fs, filepath.Join(sharedSpace, "hop.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(fs, state.NewState()); err != nil {
		t.Fatal(err)
	}
	hs, err := LoadHopspace(fs, sharedSpace)
	if err != nil {
		t.Fatal(err)
	}
	return hs
}

func branchKeys(t *testing.T, fs afero.Fs) []string {
	t.Helper()
	hs, err := LoadHopspace(fs, sharedSpace)
	if err != nil {
		t.Fatal(err)
	}
	return sortedEntryKeys(hs.Config.Branches)
}

// Dropping a hub's records takes only that hub's worktrees; the other
// hub's records and the branch-level settings stay. A second drop finds
// nothing and writes nothing.
func TestDropHubRecords_OnlyThatHub(t *testing.T) {
	fs := afero.NewMemMapFs()
	hs := loadFixture(t, fs, twoHubsHopJSON)

	if got, want := hs.HubRecords("/g1/app"), []string{"/g1/app/hops/feat/y", "/g1/app/hops/main"}; !reflect.DeepEqual(got, want) {
		t.Errorf("HubRecords = %v, want %v", got, want)
	}
	dropped, err := hs.DropHubRecords("/g1/app")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/g1/app/hops/feat/y", "/g1/app/hops/main"}; !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
	if got, want := branchKeys(t, fs), []string{"/g2/app/hops/feat/y", "/g2/app/hops/main", "feat/y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left = %v, want %v", got, want)
	}

	before, err := fs.Stat(filepath.Join(sharedSpace, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	dropped, err = hs.DropHubRecords("/g1/app")
	if err != nil || len(dropped) != 0 {
		t.Errorf("second drop = %v, %v; want nothing", dropped, err)
	}
	after, err := fs.Stat(filepath.Join(sharedSpace, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("a drop with nothing to drop rewrote hop.json")
	}
}

// A hub's own hopspace keys its records by branch and goes with the hub:
// there is nothing to report or drop.
func TestDropHubRecords_OwnHopspace(t *testing.T) {
	fs := afero.NewMemMapFs()
	hs := loadFixture(t, fs, twoHubsHopJSON)

	if got := hs.HubRecords(sharedSpace); got != nil {
		t.Errorf("HubRecords = %v, want none", got)
	}
	dropped, err := hs.DropHubRecords(sharedSpace)
	if err != nil || dropped != nil {
		t.Errorf("DropHubRecords = %v, %v; want nothing", dropped, err)
	}
	if got := branchKeys(t, fs); len(got) != 5 {
		t.Errorf("entries = %v, want all five", got)
	}
}

// Records an earlier release keyed by branch count as the hub's once
// migrated: the preview reports them without writing, and the drop,
// which migrates first, removes them. Branch-level settings stay.
func TestHubRecords_LegacyBranchKeys(t *testing.T) {
	fs := writeSharedFixture(t)
	hs, err := LoadHopspace(fs, sharedSpace)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"/g1/app/hops/feat/pm", "/g1/app/hops/main"}
	if got := hs.HubRecords("/g1/app"); !reflect.DeepEqual(got, want) {
		t.Errorf("HubRecords = %v, want %v", got, want)
	}
	if data, _ := afero.ReadFile(fs, filepath.Join(sharedSpace, "hop.json")); string(data) != legacySharedHopJSON {
		t.Error("the preview wrote hop.json")
	}

	dropped, err := hs.DropHubRecords("/g1/app")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
	got := branchKeys(t, fs)
	for _, k := range []string{"/g2/app/hops/feat/y", "feat/pm", "settings-only"} {
		if !slices.Contains(got, k) {
			t.Errorf("entries %v lack %s", got, k)
		}
	}
	for _, k := range want {
		if slices.Contains(got, k) {
			t.Errorf("entries %v still hold %s", got, k)
		}
	}
}
