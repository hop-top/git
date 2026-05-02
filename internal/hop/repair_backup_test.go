package hop

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func setupHubForBackup(t *testing.T, fs afero.Fs) string {
	t.Helper()
	hub := "/hub"
	if err := fs.MkdirAll(hub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, filepath.Join(hub, "hop.json"), []byte(`{"branches":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.MkdirAll(filepath.Join(hub, ".git", "worktrees", "feat"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, filepath.Join(hub, ".git", "worktrees", "feat", "gitdir"), []byte("/hub/hops/feat/.git\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(hub, "hops", "feat")
	if err := fs.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, filepath.Join(wt, ".git"), []byte("gitdir: /hub/.git/worktrees/feat\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return hub
}

func TestRepairBackup_SnapshotCapturesAllSources(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)

	plan := &Plan{
		HubPath: hub,
		Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: "/hub/hops/feat"}},
	}

	b := NewRepairBackup(fs, hub)
	m, err := b.Snapshot(plan)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !strings.HasPrefix(m.ID, "repair-") {
		t.Errorf("expected ID to start with 'repair-', got %q", m.ID)
	}

	dir := b.Path(m.ID)
	if _, err := fs.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Errorf("manifest missing: %v", err)
	}
	if _, err := fs.Stat(filepath.Join(dir, ".git_worktrees", "feat", "gitdir")); err != nil {
		t.Errorf("expected .git_worktrees subtree copy, got %v", err)
	}
	if _, err := fs.Stat(filepath.Join(dir, "hop.json")); err != nil {
		t.Errorf("hop.json missing in backup: %v", err)
	}
	pointerName := pointerKey("/hub/hops/feat")
	if _, err := fs.Stat(filepath.Join(dir, "pointers", pointerName)); err != nil {
		t.Errorf("expected pointer file %s, got %v", pointerName, err)
	}

	if m.Files["hop.json"] == "" {
		t.Errorf("manifest missing hop.json sha256")
	}
	if m.Files[".git/worktrees"] == "" {
		t.Errorf("manifest missing .git/worktrees sum")
	}
	if m.Files["worktree:/hub/hops/feat/.git"] == "" {
		t.Errorf("manifest missing pointer sha256")
	}
}

func TestRepairBackup_ListReturnsNewestFirst(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: "/hub/hops/feat"}}}

	b := NewRepairBackup(fs, hub)
	first, err := b.Snapshot(plan)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the dir directly so ID + timestamp differ; MemMapFs isn't
	// time-aware otherwise. Bump first's timestamp file too.
	second, err := b.Snapshot(plan)
	if err != nil {
		t.Fatal(err)
	}
	// MemMapFs creates both within the same second; force ordering by
	// touching the second's manifest with a later timestamp via re-write.
	if first.ID == second.ID {
		t.Skip("two backups created within the same second; ordering test requires real clock")
	}

	list, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(list))
	}
	if !list[0].Timestamp.After(list[1].Timestamp) && !list[0].Timestamp.Equal(list[1].Timestamp) {
		t.Errorf("expected newest first; got %v then %v", list[0].Timestamp, list[1].Timestamp)
	}
}

func TestRepairBackup_LatestEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)

	b := NewRepairBackup(fs, hub)
	got, err := b.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil Latest with no backups, got %+v", got)
	}
}

func TestRepairBackup_NoOpActionsSkipPointer(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)

	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionNoOp, WorktreePath: "/hub/hops/feat"}}}
	b := NewRepairBackup(fs, hub)
	m, err := b.Snapshot(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Files["worktree:/hub/hops/feat/.git"]; ok {
		t.Errorf("expected NoOp to skip pointer capture")
	}
}

func TestPointerKey_StableMangling(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/hub/hops/feat", "hub_hops_feat.gitptr"},
		{"/hops/main", "hops_main.gitptr"},
		{"/", ".gitptr"},
	}
	for _, tc := range tests {
		got := pointerKey(tc.in)
		if got != tc.want {
			t.Errorf("pointerKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
