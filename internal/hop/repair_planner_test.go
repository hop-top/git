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

// healthyWorktreeOnDisk writes the minimal layout the planner expects to
// classify a worktree as "no structural action needed" — the .git pointer
// resolves to an existing gitdir. Used by inference tests so the
// structural NoOp doesn't crowd out the ActionRecordBase under inspection.
func healthyWorktreeOnDisk(t *testing.T, fs afero.Fs, hubPath, wtPath, branch string) {
	t.Helper()
	if err := fs.MkdirAll(wtPath, 0755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitdir := filepath.Join(hubPath, ".git", "worktrees", branch)
	if err := fs.MkdirAll(gitdir, 0755); err != nil {
		t.Fatalf("mkdir gitdir: %v", err)
	}
	if err := afero.WriteFile(fs, filepath.Join(wtPath, ".git"),
		[]byte("gitdir: "+gitdir+"\n"), 0644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
}

// TestPlanner_BaseInferenceDisabled confirms the default behavior: even
// when HubBranch.Base is nil for a non-default branch, no record-base
// action is emitted unless WithBaseInference(true) was set. This is the
// guard against surprising existing `git hop repair` users.
func TestPlanner_BaseInferenceDisabled(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/feat"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main": {Path: "hops/main", HopspaceBranch: "main"},
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/main", "main")
	healthyWorktreeOnDisk(t, fs, hubPath, wtPath, "feat")
	g.WorktreeListOut = "worktree /hub/hops/main\nHEAD a\nbranch refs/heads/main\n\nworktree " + wtPath + "\nHEAD b\nbranch refs/heads/feat\n"

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Kind == ActionRecordBase {
			t.Errorf("unexpected record-base action without --base: %+v", a)
		}
	}
}

// TestPlanner_BaseInference_FromTrackingConfig covers signal 1:
// branch.<name>.merge points at a known hub branch → use that.
// This is the deterministic path that should dominate when present,
// since it reflects what the user told git via push/pull/branch
// --set-upstream-to.
func TestPlanner_BaseInference_FromTrackingConfig(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/feat"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main":    {Path: "hops/main", HopspaceBranch: "main"},
		"develop": {Path: "hops/develop", HopspaceBranch: "develop"},
		"feat":    {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/main", "main")
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/develop", "develop")
	healthyWorktreeOnDisk(t, fs, hubPath, wtPath, "feat")
	g.WorktreeListOut = "worktree /hub/hops/main\nHEAD a\nbranch refs/heads/main\n\n" +
		"worktree /hub/hops/develop\nHEAD b\nbranch refs/heads/develop\n\n" +
		"worktree " + wtPath + "\nHEAD c\nbranch refs/heads/feat\n"

	// Tracking config says feat tracks develop.
	g.Runner.Responses[wtPath+":git config --get branch.feat.merge"] = "refs/heads/develop"

	plan, err := NewPlanner(fs, g).WithBaseInference(true).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var got *Action
	for i, a := range plan.Actions {
		if a.Kind == ActionRecordBase && a.WorktreePath == wtPath {
			got = &plan.Actions[i]
		}
	}
	if got == nil {
		t.Fatalf("expected record-base action for feat, got actions=%+v", plan.Actions)
	}
	if got.NewValue != "develop" {
		t.Errorf("base: got %q, want %q", got.NewValue, "develop")
	}
	if !contains(got.Reason, "branch.feat.merge") {
		t.Errorf("reason should mention tracking config, got %q", got.Reason)
	}
}

// TestPlanner_BaseInference_FromMergeBase covers signal 2 (fallback):
// no tracking config, but merge-bases against other branches resolve
// to commits with timestamps — pick the most recent. Here `develop`'s
// merge-base is newer than `main`'s, so develop wins.
func TestPlanner_BaseInference_FromMergeBase(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/feat"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main":    {Path: "hops/main", HopspaceBranch: "main"},
		"develop": {Path: "hops/develop", HopspaceBranch: "develop"},
		"feat":    {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/main", "main")
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/develop", "develop")
	healthyWorktreeOnDisk(t, fs, hubPath, wtPath, "feat")
	g.WorktreeListOut = "worktree /hub/hops/main\nHEAD a\nbranch refs/heads/main\n\n" +
		"worktree /hub/hops/develop\nHEAD b\nbranch refs/heads/develop\n\n" +
		"worktree " + wtPath + "\nHEAD c\nbranch refs/heads/feat\n"

	// No tracking config response → fall through to merge-base.
	g.MergeBaseResponses = map[string]string{
		wtPath + ":feat:main":    "shamain",
		wtPath + ":feat:develop": "shadev",
	}
	g.Runner.Responses[wtPath+":git log -1 --format=%ct shamain"] = "1000\n"
	g.Runner.Responses[wtPath+":git log -1 --format=%ct shadev"] = "2000\n"

	plan, err := NewPlanner(fs, g).WithBaseInference(true).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var got *Action
	for i, a := range plan.Actions {
		if a.Kind == ActionRecordBase && a.WorktreePath == wtPath {
			got = &plan.Actions[i]
		}
	}
	if got == nil {
		t.Fatalf("expected record-base action for feat, got actions=%+v", plan.Actions)
	}
	if got.NewValue != "develop" {
		t.Errorf("base: got %q, want %q (newer merge-base should win)", got.NewValue, "develop")
	}
}

// TestPlanner_BaseInference_AmbiguousTie covers the safety case: two
// candidates share the most-recent merge-base timestamp. We refuse to
// guess and emit a Warning instead of recording the wrong base.
func TestPlanner_BaseInference_AmbiguousTie(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/feat"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main":    {Path: "hops/main", HopspaceBranch: "main"},
		"develop": {Path: "hops/develop", HopspaceBranch: "develop"},
		"feat":    {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/main", "main")
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/develop", "develop")
	healthyWorktreeOnDisk(t, fs, hubPath, wtPath, "feat")
	g.WorktreeListOut = "worktree /hub/hops/main\nHEAD a\nbranch refs/heads/main\n\n" +
		"worktree /hub/hops/develop\nHEAD b\nbranch refs/heads/develop\n\n" +
		"worktree " + wtPath + "\nHEAD c\nbranch refs/heads/feat\n"

	g.MergeBaseResponses = map[string]string{
		wtPath + ":feat:main":    "shaA",
		wtPath + ":feat:develop": "shaB",
	}
	// Same timestamp — ambiguous.
	g.Runner.Responses[wtPath+":git log -1 --format=%ct shaA"] = "1500\n"
	g.Runner.Responses[wtPath+":git log -1 --format=%ct shaB"] = "1500\n"

	plan, err := NewPlanner(fs, g).WithBaseInference(true).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Kind == ActionRecordBase {
			t.Errorf("expected no record-base action on tie, got %+v", a)
		}
	}
	if len(plan.Warnings) == 0 {
		t.Errorf("expected an ambiguity warning, got none")
	}
}

// TestPlanner_BaseInference_SkipsAlreadyRecorded verifies idempotence:
// a branch whose Base is already set is not re-proposed, even with
// inference enabled.
func TestPlanner_BaseInference_SkipsAlreadyRecorded(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hubPath := "/hub"
	wtPath := "/hub/hops/feat"
	develop := "develop"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{
		"main":    {Path: "hops/main", HopspaceBranch: "main"},
		"develop": {Path: "hops/develop", HopspaceBranch: "develop"},
		"feat":    {Path: "hops/feat", HopspaceBranch: "feat", Base: &develop},
	})
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/main", "main")
	healthyWorktreeOnDisk(t, fs, hubPath, "/hub/hops/develop", "develop")
	healthyWorktreeOnDisk(t, fs, hubPath, wtPath, "feat")
	g.WorktreeListOut = "worktree /hub/hops/main\nHEAD a\nbranch refs/heads/main\n\n" +
		"worktree /hub/hops/develop\nHEAD b\nbranch refs/heads/develop\n\n" +
		"worktree " + wtPath + "\nHEAD c\nbranch refs/heads/feat\n"

	plan, err := NewPlanner(fs, g).WithBaseInference(true).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Kind == ActionRecordBase {
			t.Errorf("expected no record-base for branch with Base already set, got %+v", a)
		}
	}
}

// contains is a tiny helper so the test file doesn't need strings.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(substr) > 0 && indexOf(s, substr) >= 0))
}
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
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
