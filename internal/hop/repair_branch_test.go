package hop

import (
	"strings"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

// A worktree git lists but hop.json lacks is recorded under the branch
// git says it has checked out, not under its directory name: the two
// differ whenever a worktree was added by hand with its own directory.
func TestPlanner_UnlistedWorktreeCarriesGitBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	hubPath := "/hub"
	wtPath := "/hub/hops/daemon-leak-fix"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{})
	if err := fs.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}
	g.WorktreeListOut = "worktree /hub\nbare\n\n" +
		"worktree " + wtPath + "\nHEAD abc\nbranch refs/heads/fix/daemon-test-leak\n"

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected one action, got %+v", plan.Actions)
	}
	a := plan.Actions[0]
	if a.Kind != ActionUpdateHopJSON || a.NewValue != "fix/daemon-test-leak" {
		t.Errorf("action = %+v, want update-hopjson recording branch fix/daemon-test-leak", a)
	}
}

// A worktree on a detached HEAD has no branch to record it under. The
// planner leaves it out of hop.json and says why, instead of inventing
// a branch from the directory name.
func TestPlanner_UnlistedDetachedWorktreeIsAWarning(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	hubPath := "/hub"
	wtPath := "/hub/hops/scratch"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{})
	if err := fs.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}
	g.WorktreeListOut = "worktree /hub\nbare\n\n" +
		"worktree " + wtPath + "\nHEAD abc\ndetached\n"

	plan, err := NewPlanner(fs, g).Build(hubPath, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Errorf("expected no action for a detached worktree, got %+v", plan.Actions)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], wtPath) ||
		!strings.Contains(plan.Warnings[0], "detached HEAD") {
		t.Errorf("warnings = %q, want one naming %s and its detached HEAD", plan.Warnings, wtPath)
	}
}

// The applier records the branch the plan carries: as the hop.json key
// and hopspaceBranch, with the absolute path add and clone write.
func TestApplier_UpdateHopJSON_RecordsGitBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	hubPath := "/hub"
	wtPath := "/hub/hops/daemon-leak-fix"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{})
	if err := fs.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}

	plan := &Plan{HubPath: hubPath, Actions: []Action{{
		Kind: ActionUpdateHopJSON, WorktreePath: wtPath, NewValue: "fix/daemon-test-leak",
	}}}
	if _, err := NewApplier(fs, g).Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	hub, err := LoadHub(fs, hubPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hub.Config.Branches["daemon-leak-fix"]; ok {
		t.Errorf("recorded under the directory name: %+v", hub.Config.Branches)
	}
	b, ok := hub.Config.Branches["fix/daemon-test-leak"]
	if !ok {
		t.Fatalf("branch fix/daemon-test-leak not recorded: %+v", hub.Config.Branches)
	}
	if b.HopspaceBranch != "fix/daemon-test-leak" || b.Path != wtPath {
		t.Errorf("entry = %+v, want hopspaceBranch fix/daemon-test-leak at %s", b, wtPath)
	}
}

// Without a branch the applier records nothing: a guessed key is what
// made status show the worktree as unknown.
func TestApplier_UpdateHopJSON_NoBranchNoEntry(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	hubPath := "/hub"
	wtPath := "/hub/hops/extra"
	writeHub(t, fs, hubPath, map[string]config.HubBranch{})
	if err := fs.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}

	plan := &Plan{HubPath: hubPath, Actions: []Action{{Kind: ActionUpdateHopJSON, WorktreePath: wtPath}}}
	if _, err := NewApplier(fs, g).Apply(plan); err == nil {
		t.Error("expected an error for an entry with no branch")
	}
	hub, _ := LoadHub(fs, hubPath)
	if len(hub.Config.Branches) != 0 {
		t.Errorf("hop.json changed: %+v", hub.Config.Branches)
	}
}
