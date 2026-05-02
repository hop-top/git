package hop

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func TestRestore_ByteIdentical(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)

	// Capture pre-state.
	preHop, _ := afero.ReadFile(fs, filepath.Join(hub, "hop.json"))
	preGitdir, _ := afero.ReadFile(fs, filepath.Join(hub, ".git", "worktrees", "feat", "gitdir"))
	prePointer, _ := afero.ReadFile(fs, filepath.Join(hub, "hops", "feat", ".git"))

	plan := &Plan{
		HubPath: hub,
		Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: "/hub/hops/feat"}},
	}
	b := NewRepairBackup(fs, hub)
	manifest, err := b.Snapshot(plan)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Mutate the live state to simulate a botched repair.
	if err := afero.WriteFile(fs, filepath.Join(hub, "hop.json"), []byte("CORRUPTED"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, filepath.Join(hub, ".git", "worktrees", "feat", "gitdir"), []byte("bad\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, filepath.Join(hub, "hops", "feat", ".git"), []byte("gitdir: /wrong\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Restore.
	got, err := b.Restore(manifest.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got.ID != manifest.ID {
		t.Errorf("expected restored manifest ID %q, got %q", manifest.ID, got.ID)
	}

	// Verify byte-identity post-restore.
	postHop, _ := afero.ReadFile(fs, filepath.Join(hub, "hop.json"))
	postGitdir, _ := afero.ReadFile(fs, filepath.Join(hub, ".git", "worktrees", "feat", "gitdir"))
	postPointer, _ := afero.ReadFile(fs, filepath.Join(hub, "hops", "feat", ".git"))

	if string(postHop) != string(preHop) {
		t.Errorf("hop.json not byte-identical after restore")
	}
	if string(postGitdir) != string(preGitdir) {
		t.Errorf(".git/worktrees/feat/gitdir not byte-identical after restore")
	}
	if string(postPointer) != string(prePointer) {
		t.Errorf("worktree .git pointer not byte-identical after restore")
	}
}

func TestRestore_LatestWhenIDEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: "/hub/hops/feat"}}}

	b := NewRepairBackup(fs, hub)
	m, err := b.Snapshot(plan)
	if err != nil {
		t.Fatal(err)
	}

	got, err := b.Restore("")
	if err != nil {
		t.Fatalf("Restore(\"\"): %v", err)
	}
	if got.ID != m.ID {
		t.Errorf("expected latest ID %q, got %q", m.ID, got.ID)
	}
}

func TestRestore_NoBackupsAvailable(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)

	b := NewRepairBackup(fs, hub)
	if _, err := b.Restore(""); err == nil {
		t.Error("expected error when no backups available")
	}
}

func TestRestore_UnknownID(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)

	b := NewRepairBackup(fs, hub)
	if _, err := b.Restore("repair-19700101T000000Z"); err == nil {
		t.Error("expected error for unknown backup id")
	}
}

func TestRestore_ChecksumMismatchAborts(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupHubForBackup(t, fs)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: "/hub/hops/feat"}}}

	b := NewRepairBackup(fs, hub)
	m, err := b.Snapshot(plan)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt the snapshot copy of hop.json so the checksum diverges.
	corrupt := filepath.Join(b.Path(m.ID), "hop.json")
	if err := afero.WriteFile(fs, corrupt, []byte("TAMPERED"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := b.Restore(m.ID); err == nil {
		t.Error("expected checksum mismatch to abort restore")
	}
}
