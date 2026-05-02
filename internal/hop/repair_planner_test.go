package hop

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

// writeHub serialises a minimal HubConfig at hubPath/hop.json so LoadHub
// succeeds in tests without depending on the writer package.
func writeHub(t *testing.T, fs afero.Fs, hubPath string, branches map[string]config.HubBranch) {
	t.Helper()
	cfg := config.HubConfig{
		Repo:     config.RepoConfig{Org: "test", Repo: "repo", DefaultBranch: "main"},
		Branches: branches,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal hub: %v", err)
	}
	if err := fs.MkdirAll(hubPath, 0755); err != nil {
		t.Fatalf("mkdir hub: %v", err)
	}
	if err := afero.WriteFile(fs, filepath.Join(hubPath, "hop.json"), data, 0644); err != nil {
		t.Fatalf("write hub config: %v", err)
	}
}

func TestPlanner_AllHealthy(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	mainPath := "/hub/hops/main"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main": {Path: "hops/main", HopspaceBranch: "main"},
	})
	if err := fs.MkdirAll(mainPath, 0755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	// Healthy .git pointer pointing at an existing gitdir target.
	gitdir := "/hub/.git/worktrees/main"
	if err := fs.MkdirAll(gitdir, 0755); err != nil {
		t.Fatalf("mkdir gitdir: %v", err)
	}
	if err := afero.WriteFile(fs, filepath.Join(mainPath, ".git"), []byte("gitdir: "+gitdir+"\n"), 0644); err != nil {
		t.Fatalf("write .git pointer: %v", err)
	}
	g.WorktreeListOut = "worktree " + mainPath + "\nHEAD abc\nbranch refs/heads/main\n"

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.HasMutations() {
		t.Errorf("expected no mutations, got %+v", plan.Actions)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionNoOp {
		t.Errorf("expected one NoOp action, got %+v", plan.Actions)
	}
}

func TestPlanner_OrphanInGit(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{})
	// Git knows about a worktree whose directory does not exist.
	g.WorktreeListOut = "worktree /hub/hops/ghost\nHEAD abc\nbranch refs/heads/ghost\n"

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.Actions[0].Kind != ActionUnregisterFromGit {
		t.Errorf("expected ActionUnregisterFromGit, got %s", plan.Actions[0].Kind)
	}
	if plan.Actions[0].WorktreePath != "/hub/hops/ghost" {
		t.Errorf("unexpected path: %s", plan.Actions[0].WorktreePath)
	}
}

func TestPlanner_OrphanOnDisk(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/feat"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	if err := fs.MkdirAll(wtPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Git registry is empty.
	g.WorktreeListOut = ""

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %+v", plan.Actions)
	}
	if plan.Actions[0].Kind != ActionRegisterWithGit {
		t.Errorf("expected ActionRegisterWithGit, got %s", plan.Actions[0].Kind)
	}
}

func TestPlanner_HopJSONReferencesMissingPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"gone": {Path: "hops/gone", HopspaceBranch: "gone"},
	})
	g.WorktreeListOut = ""

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionUpdateHopJSON {
		t.Errorf("expected ActionUpdateHopJSON, got %+v", plan.Actions)
	}
}

func TestPlanner_StaleGitdir(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/feat"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	if err := fs.MkdirAll(wtPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// .git points at a path that does not exist.
	if err := afero.WriteFile(fs, filepath.Join(wtPath, ".git"), []byte("gitdir: /nope/ghost\n"), 0644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	g.WorktreeListOut = "worktree " + wtPath + "\nHEAD abc\nbranch refs/heads/feat\n"

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionRewriteGitdir {
		t.Errorf("expected ActionRewriteGitdir, got %+v", plan.Actions)
	}
	if plan.Actions[0].OldValue != "/nope/ghost" {
		t.Errorf("expected stale path /nope/ghost, got %q", plan.Actions[0].OldValue)
	}
}

func TestPlanner_PathspecLimitsByBranchKey(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main": {Path: "hops/main", HopspaceBranch: "main"},
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	g.WorktreeListOut = ""

	plan, err := NewPlanner(fs, g).Build(hubPath, []string{"feat"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected exactly 1 action, got %+v", plan.Actions)
	}
	if plan.Actions[0].WorktreePath != "/hub/hops/feat" {
		t.Errorf("expected feat path, got %s", plan.Actions[0].WorktreePath)
	}
}

func TestPlanner_PathspecLimitsByAbsPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main": {Path: "hops/main", HopspaceBranch: "main"},
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	g.WorktreeListOut = ""

	plan, err := NewPlanner(fs, g).Build(hubPath, []string{"hops/main"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].WorktreePath != "/hub/hops/main" {
		t.Errorf("expected only main, got %+v", plan.Actions)
	}
}

func TestPlanner_RegisteredButMissingFromHopJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/extra"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{})
	if err := fs.MkdirAll(wtPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	g.WorktreeListOut = "worktree " + wtPath + "\nHEAD abc\nbranch refs/heads/extra\n"

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionUpdateHopJSON {
		t.Errorf("expected ActionUpdateHopJSON, got %+v", plan.Actions)
	}
}

func TestParsePorcelainWorktrees(t *testing.T) {
	in := "worktree /a\nHEAD abc\nbranch refs/heads/main\n\nworktree /b\nHEAD def\nbranch refs/heads/feat\n"
	got := parsePorcelainWorktrees(in)
	want := []string{"/a", "/b"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}
