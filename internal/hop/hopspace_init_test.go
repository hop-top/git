package hop_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
)

func TestInitHopspace(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	if hopspace == nil {
		t.Fatal("Hopspace is nil")
	}

	// Verify config file exists
	configPath := filepath.Join(path, "hop.json")
	exists, err := afero.Exists(fs, configPath)
	if err != nil {
		t.Fatalf("Failed to check config file: %v", err)
	}
	if !exists {
		t.Error("hop.json was not created")
	}

	// Verify config contents
	if hopspace.Config.Repo.URI != uri {
		t.Errorf("URI = %v, want %v", hopspace.Config.Repo.URI, uri)
	}
	if hopspace.Config.Repo.Org != org {
		t.Errorf("Org = %v, want %v", hopspace.Config.Repo.Org, org)
	}
	if hopspace.Config.Repo.Repo != repo {
		t.Errorf("Repo = %v, want %v", hopspace.Config.Repo.Repo, repo)
	}
	if hopspace.Config.Repo.DefaultBranch != defaultBranch {
		t.Errorf("DefaultBranch = %v, want %v", hopspace.Config.Repo.DefaultBranch, defaultBranch)
	}

	// Verify branches map is initialized
	if hopspace.Config.Branches == nil {
		t.Error("Branches map is nil")
	}

	// Verify forks map is initialized
	if hopspace.Config.Forks == nil {
		t.Error("Forks map is nil")
	}
}

func TestInitHopspaceAlreadyExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Initialize hopspace first time
	_, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	// Initialize again - should load existing
	hopspace2, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace second call failed: %v", err)
	}

	if hopspace2 == nil {
		t.Fatal("Second hopspace is nil")
	}

	// Should have loaded the existing config
	if hopspace2.Config.Repo.URI != uri {
		t.Error("Second init did not load existing config")
	}
}

func TestLoadHopspace(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Initialize hopspace
	_, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	// Load it
	hopspace, err := hop.LoadHopspace(fs, path)
	if err != nil {
		t.Fatalf("LoadHopspace failed: %v", err)
	}

	// Verify config was loaded
	if hopspace.Config.Repo.URI != uri {
		t.Errorf("URI = %v, want %v", hopspace.Config.Repo.URI, uri)
	}
}

func TestLoadHopspaceNotFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/nonexistent/repo"

	// Try to load non-existent hopspace
	_, err := hop.LoadHopspace(fs, path)
	if err == nil {
		t.Error("Expected error when loading non-existent hopspace")
	}
}

func TestHopspaceRegisterBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	// Register a branch
	branchName := "feature-x"
	worktreePath := "/hops/feature-x"

	err = hopspace.RegisterBranch(branchName, worktreePath)
	if err != nil {
		t.Fatalf("RegisterBranch failed: %v", err)
	}

	// Verify branch was registered
	branch, ok := hopspace.Config.Branches[branchName]
	if !ok {
		t.Fatalf("Branch %s was not registered", branchName)
	}

	if !branch.Exists {
		t.Error("Branch Exists flag is false")
	}

	if branch.Path != worktreePath {
		t.Errorf("Branch path = %v, want %v", branch.Path, worktreePath)
	}

	if branch.LastSync.IsZero() {
		t.Error("Branch LastSync is zero")
	}

	// Verify config was persisted
	hopspace2, err := hop.LoadHopspace(fs, path)
	if err != nil {
		t.Fatalf("LoadHopspace failed: %v", err)
	}

	if _, ok := hopspace2.Config.Branches[branchName]; !ok {
		t.Error("Branch was not persisted to config file")
	}
}

func TestHopspaceUnregisterBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	// Register a branch
	branchName := "feature-x"
	err = hopspace.RegisterBranch(branchName, "/hops/feature-x")
	if err != nil {
		t.Fatalf("RegisterBranch failed: %v", err)
	}

	// Unregister the branch
	err = hopspace.UnregisterBranch(branchName)
	if err != nil {
		t.Fatalf("UnregisterBranch failed: %v", err)
	}

	// Verify branch was removed
	if _, ok := hopspace.Config.Branches[branchName]; ok {
		t.Error("Branch was not unregistered")
	}

	// Verify config was persisted
	hopspace2, err := hop.LoadHopspace(fs, path)
	if err != nil {
		t.Fatalf("LoadHopspace failed: %v", err)
	}

	if _, ok := hopspace2.Config.Branches[branchName]; ok {
		t.Error("Branch removal was not persisted to config file")
	}
}

// TestHopspaceUnregisterBranch_NonExistent tests that unregistering
// a non-existent branch doesn't return an error (Bug 1 regression test)
func TestHopspaceUnregisterBranch_NonExistent(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	// Register a branch so we have something to verify wasn't removed
	testBranch := "test-branch"
	err = hopspace.RegisterBranch(testBranch, "/hops/test-branch")
	if err != nil {
		t.Fatalf("RegisterBranch failed: %v", err)
	}

	// Try to unregister a branch that doesn't exist
	err = hopspace.UnregisterBranch("non-existent-branch")

	// Should NOT return an error - this is expected behavior
	if err != nil {
		t.Errorf("UnregisterBranch returned error for non-existent branch: %v", err)
	}

	// Config should remain valid and unchanged
	hopspace2, err := hop.LoadHopspace(fs, path)
	if err != nil {
		t.Fatalf("LoadHopspace failed after unregistering non-existent branch: %v", err)
	}

	// Original branches should still be there
	if _, ok := hopspace2.Config.Branches[testBranch]; !ok {
		t.Error("Test branch was accidentally removed")
	}
}

func TestHopspaceRegisterMultipleBranches(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Initialize hopspace
	hopspace, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	// Register multiple branches
	branches := map[string]string{
		"main":      "/hops/main",
		"feature-a": "/hops/feature-a",
		"feature-b": "/hops/feature-b",
		"bugfix-1":  "/hops/bugfix-1",
	}

	for name, path := range branches {
		err = hopspace.RegisterBranch(name, path)
		if err != nil {
			t.Fatalf("RegisterBranch(%s) failed: %v", name, err)
		}
		// Small delay to ensure different timestamps
		time.Sleep(time.Millisecond)
	}

	// Verify all branches are registered
	if len(hopspace.Config.Branches) != len(branches) {
		t.Errorf("Expected %d branches, got %d", len(branches), len(hopspace.Config.Branches))
	}

	for name, expectedPath := range branches {
		branch, ok := hopspace.Config.Branches[name]
		if !ok {
			t.Errorf("Branch %s was not registered", name)
			continue
		}
		if branch.Path != expectedPath {
			t.Errorf("Branch %s path = %v, want %v", name, branch.Path, expectedPath)
		}
	}

	// Reload and verify persistence
	hopspace2, err := hop.LoadHopspace(fs, path)
	if err != nil {
		t.Fatalf("LoadHopspace failed: %v", err)
	}

	if len(hopspace2.Config.Branches) != len(branches) {
		t.Errorf("After reload: expected %d branches, got %d", len(branches), len(hopspace2.Config.Branches))
	}
}

