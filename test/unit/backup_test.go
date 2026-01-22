package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
)

func TestNewBackupManager(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	bm, err := hop.NewBackupManager(fs, g, "test-org", "test-repo")
	if err != nil {
		t.Fatalf("NewBackupManager failed: %v", err)
	}

	if bm == nil {
		t.Fatal("BackupManager is nil")
	}
}

func TestNewBackupManagerEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	_, err := hop.NewBackupManager(fs, g, "", "test-repo")
	if err == nil {
		t.Error("Expected error for empty org")
	}

	_, err = hop.NewBackupManager(fs, g, "test-org", "")
	if err == nil {
		t.Error("Expected error for empty repo")
	}
}

func TestGetCacheBackupPath(t *testing.T) {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(os.Getenv("HOME"), ".cache")
	}

	expected := filepath.Join(cacheHome, "git-hop", "test-org-test-repo")
	actual := hop.GetCacheBackupPath("test-org", "test-repo")

	if actual != expected {
		t.Errorf("GetCacheBackupPath() = %v, want %v", actual, expected)
	}
}

func TestListBackups(t *testing.T) {
	fs := afero.NewMemMapFs()

	backups, err := hop.ListBackups(fs, "test-org", "test-repo")
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(backups))
	}

	backupDir := filepath.Join(hop.GetCacheBackupPath("test-org", "test-repo"), "2026-01-21_10-00-00")
	fs.MkdirAll(backupDir, 0755)

	backups, err = hop.ListBackups(fs, "test-org", "test-repo")
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 1 {
		t.Errorf("Expected 1 backup, got %d", len(backups))
	}
}
