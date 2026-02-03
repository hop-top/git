package integration_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
)

// TestCloneWorktreeCreatesHopspace verifies that the clone workflow
// creates both hub and hopspace configurations
func TestCloneWorktreeCreatesHopspace(t *testing.T) {
	// This is a regression test for the issue where early adopters
	// would get "no such file or directory" errors when running
	// `git hop <branch>` after cloning a repository

	fs := afero.NewMemMapFs()

	// Simulate the clone workflow
	projectRoot := "/projects/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"
	dataHome := "/data/git-hop"

	// Step 1: Create hub config (simulating what clone_worktree.go does)
	hubConfig := map[string]interface{}{
		"repo": map[string]interface{}{
			"uri":           uri,
			"org":           org,
			"repo":          repo,
			"defaultBranch": defaultBranch,
			"structure":     "bare-worktree",
			"isBare":        true,
		},
		"branches": map[string]interface{}{
			defaultBranch: map[string]interface{}{
				"path":   config.MakeWorktreePath(defaultBranch),
				"exists": true,
			},
		},
		"settings": map[string]interface{}{
			"autoEnvStart": true,
		},
	}

	// Create hub directory and config
	if err := fs.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatalf("Failed to create project root: %v", err)
	}

	hubConfigPath := filepath.Join(projectRoot, "hop.json")
	data, _ := json.MarshalIndent(hubConfig, "", "  ")
	if err := afero.WriteFile(fs, hubConfigPath, data, 0644); err != nil {
		t.Fatalf("Failed to write hub config: %v", err)
	}

	// Step 2: Initialize hopspace (this is what was missing before the fix)
	hopspacePath := hop.GetHopspacePath(dataHome, org, repo)
	mainWorktreePath := filepath.Join(projectRoot, "main")

	hopspace, err := hop.InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("Failed to initialize hopspace: %v", err)
	}

	// Register the main branch
	if err := hopspace.RegisterBranch(defaultBranch, mainWorktreePath); err != nil {
		t.Fatalf("Failed to register default branch: %v", err)
	}

	// Step 3: Verify hopspace was created correctly
	hopspaceConfigPath := filepath.Join(hopspacePath, "hop.json")
	exists, err := afero.Exists(fs, hopspaceConfigPath)
	if err != nil {
		t.Fatalf("Failed to check hopspace config: %v", err)
	}
	if !exists {
		t.Fatal("Hopspace config was not created - this is the bug we're preventing!")
	}

	// Step 4: Verify we can load the hopspace (this is what `git hop <branch>` does)
	loadedHopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		t.Fatalf("Failed to load hopspace - this would cause the 'no such file or directory' error: %v", err)
	}

	// Step 5: Verify hopspace has correct configuration
	if loadedHopspace.Config.Repo.URI != uri {
		t.Errorf("Hopspace URI = %v, want %v", loadedHopspace.Config.Repo.URI, uri)
	}
	if loadedHopspace.Config.Repo.Org != org {
		t.Errorf("Hopspace Org = %v, want %v", loadedHopspace.Config.Repo.Org, org)
	}
	if loadedHopspace.Config.Repo.Repo != repo {
		t.Errorf("Hopspace Repo = %v, want %v", loadedHopspace.Config.Repo.Repo, repo)
	}

	// Step 6: Verify main branch is registered
	branch, ok := loadedHopspace.Config.Branches[defaultBranch]
	if !ok {
		t.Fatalf("Main branch not registered in hopspace")
	}
	if branch.Path != mainWorktreePath {
		t.Errorf("Main branch path = %v, want %v", branch.Path, mainWorktreePath)
	}

	// Step 7: Simulate adding a new branch (what `git hop <branch>` does)
	newBranch := "feature-x"
	newWorktreePath := filepath.Join(dataHome, org, repo, newBranch)

	// This should now work because hopspace exists
	if err := loadedHopspace.RegisterBranch(newBranch, newWorktreePath); err != nil {
		t.Fatalf("Failed to register new branch: %v", err)
	}

	// Reload to verify persistence
	loadedHopspace2, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		t.Fatalf("Failed to reload hopspace: %v", err)
	}

	if _, ok := loadedHopspace2.Config.Branches[newBranch]; !ok {
		t.Errorf("New branch was not persisted")
	}
}

// TestDoctorFixesMissingHopspace verifies that doctor --fix can create
// missing hopspace from existing hub configuration
func TestDoctorFixesMissingHopspace(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Simulate a hub that exists without hopspace (the bug scenario)
	hubPath := "/projects/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"
	dataHome := "/data/git-hop"

	// Create hub
	hub, err := hop.CreateHub(fs, hubPath, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("Failed to create hub: %v", err)
	}

	// Get hopspace path
	hopspacePath := hop.GetHopspacePath(dataHome, org, repo)

	// Register branch in hub config directly (skip symlink for unit test)
	hub.Config.Branches[defaultBranch] = config.HubBranch{
		Path:           defaultBranch,
		HopspaceBranch: defaultBranch,
	}
	if err := hub.Save(); err != nil {
		t.Fatalf("Failed to save hub config: %v", err)
	}

	// Verify hopspace does NOT exist yet (the bug state)
	hopspaceConfigPath := filepath.Join(hopspacePath, "hop.json")
	exists, _ := afero.Exists(fs, hopspaceConfigPath)
	if exists {
		t.Fatal("Hopspace should not exist yet")
	}

	// Simulate what doctor --fix does
	hopspace, err := hop.InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("Failed to initialize hopspace: %v", err)
	}

	// Register all branches from hub
	for branchName, branch := range hub.Config.Branches {
		branchWorktreePath := filepath.Join(hubPath, branch.Path)
		if err := hopspace.RegisterBranch(branchName, branchWorktreePath); err != nil {
			t.Fatalf("Failed to register branch %s: %v", branchName, err)
		}
	}

	// Verify hopspace now exists and is correct
	loadedHopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		t.Fatalf("Failed to load hopspace after doctor fix: %v", err)
	}

	// Verify all hub branches are in hopspace
	for branchName := range hub.Config.Branches {
		if _, ok := loadedHopspace.Config.Branches[branchName]; !ok {
			t.Errorf("Branch %s from hub not found in hopspace", branchName)
		}
	}
}

// TestHubHopspaceConsistency verifies hub and hopspace remain consistent
func TestHubHopspaceConsistency(t *testing.T) {
	fs := afero.NewMemMapFs()

	hubPath := "/projects/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"
	dataHome := "/data/git-hop"

	// Create hub
	hub, err := hop.CreateHub(fs, hubPath, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("Failed to create hub: %v", err)
	}

	// Create hopspace
	hopspacePath := hop.GetHopspacePath(dataHome, org, repo)
	hopspace, err := hop.InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("Failed to initialize hopspace: %v", err)
	}

	// Add branches to both hub and hopspace
	branches := []string{"main", "feature-a", "feature-b"}
	for _, branch := range branches {
		worktreePath := filepath.Join(hopspacePath, branch)

		// Add to hub config directly (skip symlink for unit test)
		hub.Config.Branches[branch] = config.HubBranch{
			Path:           branch,
			HopspaceBranch: branch,
		}

		// Add to hopspace
		if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
			t.Fatalf("Failed to register branch %s in hopspace: %v", branch, err)
		}
	}

	// Save hub config
	if err := hub.Save(); err != nil {
		t.Fatalf("Failed to save hub config: %v", err)
	}

	// Reload both
	hubReloaded, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		t.Fatalf("Failed to reload hub: %v", err)
	}

	hopspaceReloaded, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		t.Fatalf("Failed to reload hopspace: %v", err)
	}

	// Verify consistency
	if len(hubReloaded.Config.Branches) != len(hopspaceReloaded.Config.Branches) {
		t.Errorf("Hub has %d branches, hopspace has %d branches - inconsistent!",
			len(hubReloaded.Config.Branches), len(hopspaceReloaded.Config.Branches))
	}

	// Verify all hub branches exist in hopspace
	for branchName := range hubReloaded.Config.Branches {
		if _, ok := hopspaceReloaded.Config.Branches[branchName]; !ok {
			t.Errorf("Branch %s in hub but not in hopspace", branchName)
		}
	}

	// Verify all hopspace branches exist in hub
	for branchName := range hopspaceReloaded.Config.Branches {
		if _, ok := hubReloaded.Config.Branches[branchName]; !ok {
			t.Errorf("Branch %s in hopspace but not in hub", branchName)
		}
	}
}

// TestLoadHubConfig verifies hub config can be loaded correctly
func TestLoadHubConfig(t *testing.T) {
	fs := afero.NewMemMapFs()

	hubPath := "/projects/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Create hub
	hub, err := hop.CreateHub(fs, hubPath, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("Failed to create hub: %v", err)
	}

	// Verify IsHub detects it
	if !hop.IsHub(fs, hubPath) {
		t.Error("IsHub returned false for valid hub")
	}

	// Load hub
	loadedHub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		t.Fatalf("Failed to load hub: %v", err)
	}

	// Verify config
	if loadedHub.Config.Repo.URI != uri {
		t.Errorf("Hub URI = %v, want %v", loadedHub.Config.Repo.URI, uri)
	}
	if loadedHub.Config.Repo.Org != org {
		t.Errorf("Hub Org = %v, want %v", loadedHub.Config.Repo.Org, org)
	}
	if loadedHub.Config.Repo.Repo != repo {
		t.Errorf("Hub Repo = %v, want %v", loadedHub.Config.Repo.Repo, repo)
	}

	// Verify it matches original
	if loadedHub.Path != hub.Path {
		t.Errorf("Loaded hub path = %v, want %v", loadedHub.Path, hub.Path)
	}
}

// TestHopspaceConfigPersistence verifies config changes are persisted
func TestHopspaceConfigPersistence(t *testing.T) {
	fs := afero.NewMemMapFs()

	path := "/data/test-org/test-repo"
	uri := "https://github.com/test-org/test-repo.git"
	org := "test-org"
	repo := "test-repo"
	defaultBranch := "main"

	// Create hopspace
	hopspace, err := hop.InitHopspace(fs, path, uri, org, repo, defaultBranch)
	if err != nil {
		t.Fatalf("InitHopspace failed: %v", err)
	}

	// Add multiple branches
	branches := map[string]string{
		"feature-1": "/work/feature-1",
		"feature-2": "/work/feature-2",
		"bugfix-1":  "/work/bugfix-1",
	}

	for name, wpath := range branches {
		if err := hopspace.RegisterBranch(name, wpath); err != nil {
			t.Fatalf("RegisterBranch(%s) failed: %v", name, err)
		}
	}

	// Load config file directly and verify
	configPath := filepath.Join(path, "hop.json")
	data, err := afero.ReadFile(fs, configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	var cfg config.HopspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Verify all branches are in the file
	if len(cfg.Branches) != len(branches) {
		t.Errorf("Config file has %d branches, expected %d", len(cfg.Branches), len(branches))
	}

	for name, expectedPath := range branches {
		branch, ok := cfg.Branches[name]
		if !ok {
			t.Errorf("Branch %s not found in config file", name)
			continue
		}
		if branch.Path != expectedPath {
			t.Errorf("Branch %s path in file = %v, want %v", name, branch.Path, expectedPath)
		}
	}
}
