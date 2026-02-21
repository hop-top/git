package hop_test

import (
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
)

func TestNewStashManager(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	sm := hop.NewStashManager(g, fs)
	if sm == nil {
		t.Fatal("StashManager is nil")
	}
}

func TestExportStashes(t *testing.T) {
	t.Skip("Skipping test that requires real git repository")

	fs := afero.NewMemMapFs()
	g := git.New()

	sm := hop.NewStashManager(g, fs)

	stashes, err := sm.ExportStashes("/nonexistent")
	if err != nil {
		t.Fatalf("ExportStashes failed: %v", err)
	}

	if stashes == nil {
		t.Fatal("ExportStashes returned nil")
	}

	if len(stashes) != 0 {
		t.Errorf("Expected 0 stashes, got %d", len(stashes))
	}
}
