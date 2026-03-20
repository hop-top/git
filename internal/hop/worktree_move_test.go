package hop_test

import (
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
	"github.com/spf13/afero"
)

func setupMoveTestHopspace(fs afero.Fs, hubPath, branch, worktreePath string) *hop.Hopspace {
	cfg := &config.HopspaceConfig{
		Repo: config.RepoConfig{
			URI:           "git@github.com:org/repo.git",
			Org:           "org",
			Repo:          "repo",
			DefaultBranch: "main",
		},
		Branches: map[string]config.HopspaceBranch{
			branch: {Exists: true, Path: worktreePath},
		},
		Forks: make(map[string]config.HopspaceFork),
	}
	writer := config.NewWriter(fs)
	_ = writer.WriteHopspaceConfig(hubPath, cfg)
	hs, _ := hop.LoadHopspace(fs, hubPath)
	return hs
}

func setupMoveTestHub(fs afero.Fs, hubPath, defaultBranch, branch, worktreePath string) *hop.Hub {
	cfg := &config.HubConfig{
		Repo: config.RepoConfig{DefaultBranch: defaultBranch},
		Branches: map[string]config.HubBranch{
			defaultBranch: {Path: hubPath + "/hops/" + defaultBranch},
			branch:        {Path: worktreePath},
		},
		Settings: config.HubSettings{},
	}
	writer := config.NewWriter(fs)
	_ = writer.WriteHubConfig(hubPath, cfg)
	hub, _ := hop.LoadHub(fs, hubPath)
	return hub
}

func TestMoveWorktree_RenamesAll(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	oldBranch := "feature/old"
	newBranch := "feature/new"
	oldPath := "/hub/hops/feature-old"
	fs.MkdirAll(oldPath, 0755)
	fs.MkdirAll(hubPath+"/hops/main", 0755)

	hopspace := setupMoveTestHopspace(fs, hubPath, oldBranch, oldPath)
	hub := setupMoveTestHub(fs, hubPath, "main", oldBranch, oldPath)

	mockGit := mocks.NewMockGit()
	wm := hop.NewWorktreeManager(fs, mockGit)

	oldOut, newOut, err := wm.MoveWorktree(hopspace, hub, oldBranch, newBranch, "{hubPath}/hops/{branch}", "org", "repo")
	if err != nil {
		t.Fatalf("MoveWorktree failed: %v", err)
	}
	if oldOut != oldPath {
		t.Errorf("expected oldPath %s, got %s", oldPath, oldOut)
	}
	_ = newOut

	// git branch -m called
	if len(mockGit.RenamedBranches) < 2 || mockGit.RenamedBranches[0] != oldBranch {
		t.Errorf("expected RenameBranch(%s, ...) to be called", oldBranch)
	}

	// git worktree move called
	if len(mockGit.MovedWorktrees) < 2 || mockGit.MovedWorktrees[0] != oldPath {
		t.Errorf("expected WorktreeMove(%s, ...) to be called", oldPath)
	}
}

// TestMoveWorktree_BranchAlreadyRenamed verifies that MoveWorktree succeeds when git hop add
// already created the branch under newBranch (e.g. "feat/foo") but the worktree path still
// uses oldBranch (e.g. "track/foo"). In that case LocalBranchExists(newBranch)=true so
// RenameBranch must be skipped — otherwise git branch -m fails with "no branch named <old>".
func TestMoveWorktree_BranchAlreadyRenamed(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	oldBranch := "track/foo"
	newBranch := "feat/foo"
	oldPath := "/hub/hops/track/foo"
	fs.MkdirAll(oldPath, 0755)
	fs.MkdirAll(hubPath+"/hops/main", 0755)

	hopspace := setupMoveTestHopspace(fs, hubPath, oldBranch, oldPath)
	hub := setupMoveTestHub(fs, hubPath, "main", oldBranch, oldPath)

	mockGit := mocks.NewMockGit()
	// Simulate: git already has newBranch (old was renamed externally by git hop add)
	mockGit.LocalBranches = []string{newBranch}

	wm := hop.NewWorktreeManager(fs, mockGit)
	_, _, err := wm.MoveWorktree(hopspace, hub, oldBranch, newBranch, "{hubPath}/hops/{branch}", "org", "repo")
	if err != nil {
		t.Fatalf("MoveWorktree should succeed when branch was already renamed: %v", err)
	}

	// RenameBranch must NOT have been called
	if len(mockGit.RenamedBranches) > 0 {
		t.Errorf("expected RenameBranch to be skipped, but it was called with %v", mockGit.RenamedBranches)
	}

	// WorktreeMove must still have been called
	if len(mockGit.MovedWorktrees) < 2 || mockGit.MovedWorktrees[0] != oldPath {
		t.Errorf("expected WorktreeMove(%s, ...) to be called", oldPath)
	}
}

func TestMoveWorktree_DefaultBranchBlocked(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	fs.MkdirAll(hubPath, 0755)

	hopspace := setupMoveTestHopspace(fs, hubPath, "main", hubPath+"/hops/main")
	hub := setupMoveTestHub(fs, hubPath, "main", "main", hubPath+"/hops/main")

	wm := hop.NewWorktreeManager(fs, mocks.NewMockGit())
	_, _, err := wm.MoveWorktree(hopspace, hub, "main", "other", "{hubPath}/hops/{branch}", "org", "repo")
	if err == nil {
		t.Fatal("expected error when moving default branch, got nil")
	}
}

func TestMoveWorktree_NewBranchAlreadyExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	fs.MkdirAll(hubPath+"/hops/feature-a", 0755)
	fs.MkdirAll(hubPath+"/hops/feature-b", 0755)

	cfg := &config.HopspaceConfig{
		Repo:     config.RepoConfig{DefaultBranch: "main"},
		Branches: map[string]config.HopspaceBranch{
			"feature/a": {Exists: true, Path: hubPath + "/hops/feature-a"},
			"feature/b": {Exists: true, Path: hubPath + "/hops/feature-b"},
		},
		Forks: make(map[string]config.HopspaceFork),
	}
	writer := config.NewWriter(fs)
	_ = writer.WriteHopspaceConfig(hubPath, cfg)
	hopspace, _ := hop.LoadHopspace(fs, hubPath)

	hubCfg := &config.HubConfig{
		Repo:     config.RepoConfig{DefaultBranch: "main"},
		Branches: map[string]config.HubBranch{
			"feature/a": {Path: hubPath + "/hops/feature-a"},
			"feature/b": {Path: hubPath + "/hops/feature-b"},
		},
		Settings: config.HubSettings{},
	}
	_ = writer.WriteHubConfig(hubPath, hubCfg)
	hub, _ := hop.LoadHub(fs, hubPath)

	wm := hop.NewWorktreeManager(fs, mocks.NewMockGit())
	_, _, err := wm.MoveWorktree(hopspace, hub, "feature/a", "feature/b", "{hubPath}/hops/{branch}", "org", "repo")
	if err == nil {
		t.Fatal("expected error when new branch already exists, got nil")
	}
}
