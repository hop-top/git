package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// Restore replays a backup back into the hub. id may be empty to restore
// the most recent backup. Returns the restored manifest on success.
//
// Restore is a pure file operation:
//   1. Replace <hub>/.git/worktrees with the snapshot copy.
//   2. Replace <hub>/hop.json with the snapshot copy.
//   3. Replace each affected worktree's .git pointer with its snapshot copy.
//
// Each restored file is checksum-validated against manifest.Files
// before being written; a mismatch aborts the restore (refuse to write
// known-corrupt data).
func (b *RepairBackup) Restore(id string) (*RepairManifest, error) {
	if id == "" {
		latest, err := b.Latest()
		if err != nil {
			return nil, fmt.Errorf("locate latest: %w", err)
		}
		if latest == nil {
			return nil, fmt.Errorf("no backups available")
		}
		id = latest.ID
	}
	dir := b.Path(id)
	if exists, _ := afero.DirExists(b.fs, dir); !exists {
		return nil, fmt.Errorf("backup not found: %s", id)
	}
	manifest, err := readManifest(b.fs, dir)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	if err := b.restoreGitWorktrees(dir, manifest); err != nil {
		return nil, fmt.Errorf("restore .git/worktrees: %w", err)
	}
	if err := b.restoreHopJSON(dir, manifest); err != nil {
		return nil, fmt.Errorf("restore hop.json: %w", err)
	}
	if err := b.restorePointers(dir, manifest); err != nil {
		return nil, fmt.Errorf("restore pointers: %w", err)
	}
	return manifest, nil
}

func (b *RepairBackup) restoreGitWorktrees(backupDir string, m *RepairManifest) error {
	src := filepath.Join(backupDir, ".git_worktrees")
	if exists, _ := afero.DirExists(b.fs, src); !exists {
		return nil
	}
	dst := filepath.Join(b.hubPath, ".git", "worktrees")
	if err := b.fs.RemoveAll(dst); err != nil {
		return err
	}
	sum, err := copyTreeWithSum(b.fs, src, dst)
	if err != nil {
		return err
	}
	if want := m.Files[".git/worktrees"]; want != "" && sum != want {
		return fmt.Errorf(".git/worktrees sha256 mismatch: got %s want %s", sum, want)
	}
	return nil
}

func (b *RepairBackup) restoreHopJSON(backupDir string, m *RepairManifest) error {
	src := filepath.Join(backupDir, "hop.json")
	data, err := afero.ReadFile(b.fs, src)
	if err != nil {
		return nil
	}
	if want := m.Files["hop.json"]; want != "" && sha256Hex(data) != want {
		return fmt.Errorf("hop.json sha256 mismatch")
	}
	dst := filepath.Join(b.hubPath, "hop.json")
	return afero.WriteFile(b.fs, dst, data, 0644)
}

func (b *RepairBackup) restorePointers(backupDir string, m *RepairManifest) error {
	pointersDir := filepath.Join(backupDir, "pointers")
	exists, _ := afero.DirExists(b.fs, pointersDir)
	if !exists {
		return nil
	}
	for key, want := range m.Files {
		const prefix = "worktree:"
		const suffix = "/.git"
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
			continue
		}
		wtPath := strings.TrimSuffix(strings.TrimPrefix(key, prefix), suffix)
		src := filepath.Join(pointersDir, pointerKey(wtPath))
		data, err := afero.ReadFile(b.fs, src)
		if err != nil {
			return fmt.Errorf("read snapshot pointer %s: %w", wtPath, err)
		}
		if want != "" && sha256Hex(data) != want {
			return fmt.Errorf("pointer %s sha256 mismatch", wtPath)
		}
		dst := filepath.Join(wtPath, ".git")
		if err := afero.WriteFile(b.fs, dst, data, 0644); err != nil {
			return fmt.Errorf("write pointer %s: %w", wtPath, err)
		}
	}
	return nil
}
