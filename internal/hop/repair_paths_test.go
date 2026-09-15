package hop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// withStateHome points XDG_STATE_HOME at dir for the duration of the test.
// Not parallel-safe: the xdg resolver reads the ambient process env.
func withStateHome(t *testing.T, dir string) {
	t.Helper()
	orig, had := os.LookupEnv("XDG_STATE_HOME")
	os.Setenv("XDG_STATE_HOME", dir)
	t.Cleanup(func() {
		if had {
			os.Setenv("XDG_STATE_HOME", orig)
		} else {
			os.Unsetenv("XDG_STATE_HOME")
		}
	})
}

func TestRepairStateDir_OutsideHubAndKeyedByHub(t *testing.T) {
	withStateHome(t, "/xdg-state")

	a := RepairStateDir("/hubs/alpha")
	b := RepairStateDir("/hubs/beta")
	again := RepairStateDir("/hubs/alpha/")

	if !strings.HasPrefix(a, filepath.Join("/xdg-state", "git-hop")+string(filepath.Separator)) {
		t.Fatalf("state dir must live under $XDG_STATE_HOME/git-hop, got %s", a)
	}
	if strings.HasPrefix(a, "/hubs/") {
		t.Fatalf("state dir must not live inside the hub, got %s", a)
	}
	if a == b {
		t.Fatalf("different hubs must not share a state dir: %s", a)
	}
	if a != again {
		t.Fatalf("state dir must be stable across path spelling: %s vs %s", a, again)
	}
	if !strings.Contains(filepath.Base(a), "alpha") {
		t.Errorf("state dir should carry the hub basename for operators, got %s", a)
	}
	if RepairLockPath("/hubs/alpha") != filepath.Join(a, "repair.lock") {
		t.Errorf("lock must sit in the state dir, got %s", RepairLockPath("/hubs/alpha"))
	}
	if LegacyRepairDir("/hubs/alpha") != "/hubs/alpha/.hop" {
		t.Errorf("legacy dir = %s", LegacyRepairDir("/hubs/alpha"))
	}
}

// TestRepairBackup_SnapshotLeavesNoHubDotHop is the regression for repair
// creating <hub>/.hop/ in hubs that never had one. Other tools treat the
// mere presence of .hop/ as a config root, so a snapshot must not conjure
// it.
func TestRepairBackup_SnapshotLeavesNoHubDotHop(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: "/hub/hops/feat"}}}

	b := NewRepairBackup(fs, hub)
	m, err := b.Snapshot(plan)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if exists, _ := afero.Exists(fs, filepath.Join(hub, ".hop")); exists {
		t.Fatalf("Snapshot must not create %s/.hop", hub)
	}
	dir := b.Path(m.ID)
	if !strings.HasPrefix(dir, RepairStateDir(hub)) {
		t.Fatalf("backup dir %s must live under %s", dir, RepairStateDir(hub))
	}
	if _, err := fs.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatalf("manifest missing at %s: %v", dir, err)
	}
	if m.HubPath != hub {
		t.Errorf("manifest must record the hub path, got %q", m.HubPath)
	}
}

// writeLegacyBackup plants a pre-relocation snapshot at
// <hub>/.hop/backups/<id> with a hop.json payload and a manifest.
func writeLegacyBackup(t *testing.T, fs afero.Fs, hub, id, hopJSON string) string {
	t.Helper()
	dir := filepath.Join(hub, ".hop", "backups", id)
	if err := fs.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, filepath.Join(dir, "hop.json"), []byte(hopJSON), 0644); err != nil {
		t.Fatal(err)
	}
	m := &RepairManifest{
		Version: repairBackupVersion,
		ID:      id,
		HubPath: hub,
		Files:   map[string]string{"hop.json": sha256Hex([]byte(hopJSON))},
	}
	if err := writeManifest(fs, dir, m); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRepairBackup_ListSeesLegacyHubBackups: snapshots taken before the
// relocation still show up in --list-backups.
func TestRepairBackup_ListSeesLegacyHubBackups(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)
	writeLegacyBackup(t, fs, hub, "repair-20200101T000000Z", `{"branches":{"legacy":{}}}`)

	b := NewRepairBackup(fs, hub)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: "/hub/hops/feat"}}}
	if _, err := b.Snapshot(plan); err != nil {
		t.Fatal(err)
	}

	list, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected legacy + new backup, got %d: %+v", len(list), list)
	}
	if list[len(list)-1].ID != "repair-20200101T000000Z" {
		t.Errorf("legacy backup should sort oldest, got %+v", list)
	}
}

// TestRepairBackup_RestoreLegacyHubBackup: --undo <legacy-id> still works.
func TestRepairBackup_RestoreLegacyHubBackup(t *testing.T) {
	withStateHome(t, "/xdg-state")
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)
	want := `{"branches":{"legacy":{}}}`
	writeLegacyBackup(t, fs, hub, "repair-20200101T000000Z", want)

	b := NewRepairBackup(fs, hub)
	if got := b.Path("repair-20200101T000000Z"); got != filepath.Join(hub, ".hop", "backups", "repair-20200101T000000Z") {
		t.Fatalf("Path must resolve a legacy id to the hub location, got %s", got)
	}
	if _, err := b.Restore("repair-20200101T000000Z"); err != nil {
		t.Fatalf("Restore legacy: %v", err)
	}
	got, _ := afero.ReadFile(fs, filepath.Join(hub, "hop.json"))
	if string(got) != want {
		t.Errorf("hop.json after legacy restore = %q, want %q", got, want)
	}
}
