package hop_test

import (
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

func TestHubSetBranchTask(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupMoveTestHub(fs, "/hub", "main", "feat/x", "/hub/hops/feat/x")

	if err := hub.SetBranchTask("feat/x", "T-1"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := hop.LoadHub(fs, "/hub")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Config.Branches["feat/x"].Task; got != "T-1" {
		t.Errorf("persisted task = %q, want T-1", got)
	}

	if err := hub.SetBranchTask("nope", "T-1"); err == nil {
		t.Error("SetBranchTask on an unregistered branch succeeded, want error")
	}
}

// A rename keeps the task: it describes the work, not the branch name.
func TestHubRenameBranch_KeepsTask(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := setupMoveTestHub(fs, "/hub", "main", "feat/old", "/hub/hops/feat/old")
	if err := hub.SetBranchTask("feat/old", "T-1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.RenameBranch("feat/old", "feat/new", "/hub/hops/feat/new"); err != nil {
		t.Fatal(err)
	}
	if got := hub.Config.Branches["feat/new"].Task; got != "T-1" {
		t.Errorf("task after rename = %q, want T-1", got)
	}
}
