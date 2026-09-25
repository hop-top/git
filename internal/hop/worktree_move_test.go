package hop_test

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
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
	mockGit.CurrentBranches = map[string]string{oldPath: newBranch}

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

// A local branch that already holds newBranch's name but is not what the
// worktree has checked out is someone else's branch: adopting it would
// relabel hop.json while the worktree stays on oldBranch, and git branch -m
// would refuse the rename anyway. The move must refuse before touching git.
func TestMoveWorktree_RefusesUnrelatedExistingBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hub"
	oldPath := "/hub/hops/feat/x"
	fs.MkdirAll(oldPath, 0755)
	fs.MkdirAll(hubPath+"/hops/main", 0755)

	hopspace := setupMoveTestHopspace(fs, hubPath, "feat/x", oldPath)
	hub := setupMoveTestHub(fs, hubPath, "main", "feat/x", oldPath)

	mockGit := mocks.NewMockGit()
	mockGit.LocalBranches = []string{"feat/x", "feat/y"}
	mockGit.CurrentBranches = map[string]string{oldPath: "feat/x"}

	wm := hop.NewWorktreeManager(fs, mockGit)
	_, _, err := wm.MoveWorktree(hopspace, hub, "feat/x", "feat/y", "{hubPath}/hops/{branch}", "org", "repo")
	if err == nil || !strings.Contains(err.Error(), "feat/y") || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("MoveWorktree error = %v, want refusal naming existing branch feat/y", err)
	}
	if len(mockGit.RenamedBranches) > 0 || len(mockGit.MovedWorktrees) > 0 {
		t.Errorf("git mutated on refused move: renamed=%v moved=%v", mockGit.RenamedBranches, mockGit.MovedWorktrees)
	}
	if _, ok := hub.Config.Branches["feat/x"]; !ok {
		t.Error("hub entry feat/x rekeyed on refused move")
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
		Repo: config.RepoConfig{DefaultBranch: "main"},
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
		Repo: config.RepoConfig{DefaultBranch: "main"},
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

// A file or a non-empty directory at the destination is refused before
// anything changes: `git worktree move` would nest the worktree inside
// the directory while hop.json recorded the directory itself. An empty
// directory is taken, and removed first so git moves the worktree to
// the path itself.
func TestMoveWorktree_Destination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(fs afero.Fs, dest string)
		refuse bool
	}{
		{"non-empty directory", func(fs afero.Fs, dest string) {
			_ = fs.MkdirAll(dest, 0755)
			_ = afero.WriteFile(fs, dest+"/keep.txt", []byte("work"), 0644)
		}, true},
		{"file", func(fs afero.Fs, dest string) {
			_ = afero.WriteFile(fs, dest, []byte("work"), 0644)
		}, true},
		{"empty directory", func(fs afero.Fs, dest string) { _ = fs.MkdirAll(dest, 0755) }, false},
		{"nothing", func(afero.Fs, string) {}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			hubPath, oldPath, dest := "/hub", "/hub/hops/feature-old", "/hub/hops/feature-new"
			fs.MkdirAll(oldPath, 0755)
			fs.MkdirAll(hubPath+"/hops/main", 0755)
			hopspace := setupMoveTestHopspace(fs, hubPath, "feature-old", oldPath)
			hub := setupMoveTestHub(fs, hubPath, "main", "feature-old", oldPath)
			tc.setup(fs, dest)
			g := mocks.NewMockGit()

			checkErr := hop.CheckMove(fs, hub, g, "feature-old", "feature-new", dest)
			_, _, err := hop.NewWorktreeManager(fs, g).MoveWorktree(hopspace, hub, "feature-old", "feature-new", "{hubPath}/hops/{branch}", "org", "repo")

			if !tc.refuse {
				if checkErr != nil || err != nil {
					t.Fatalf("move refused: check %v, move %v", checkErr, err)
				}
				if exists, _ := afero.Exists(fs, dest); exists {
					t.Errorf("%s must be gone before git moves the worktree there", dest)
				}
				return
			}
			for _, e := range []error{checkErr, err} {
				if e == nil || !strings.Contains(e.Error(), "already exists and is not an empty directory") {
					t.Errorf("want refusal, got %v", e)
				}
			}
			if len(g.RenamedBranches) > 0 || len(g.MovedWorktrees) > 0 {
				t.Errorf("a refused move must not touch git: renamed %v, moved %v", g.RenamedBranches, g.MovedWorktrees)
			}
			if _, ok := hub.Config.Branches["feature-old"]; !ok {
				t.Error("a refused move must leave hop.json as it was")
			}
			info, statErr := fs.Stat(dest)
			if statErr != nil {
				t.Fatalf("%s must survive: %v", dest, statErr)
			}
			keep := dest
			if info.IsDir() {
				keep = dest + "/keep.txt"
			}
			if got, _ := afero.ReadFile(fs, keep); string(got) != "work" {
				t.Errorf("%s must survive unchanged, got %q", keep, got)
			}
		})
	}
}
