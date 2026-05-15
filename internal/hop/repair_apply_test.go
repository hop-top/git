package hop

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

func TestApplier_NoOp(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	a := NewApplier(fs, g)

	plan := &Plan{HubPath: "/hub", Actions: []Action{{Kind: ActionNoOp, WorktreePath: "/hub/hops/main"}}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 0 {
		t.Errorf("expected 0 mutations, got %d", mut)
	}
	if len(g.WorktreeRepairCalls) != 0 {
		t.Errorf("NoOp must not invoke git repair")
	}
}

func TestApplier_RewriteGitdir_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hub := "/hub"
	wt := "/hub/hops/feat"
	gitdir := "/hub/.git/worktrees/feat"
	if err := fs.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.MkdirAll(gitdir, 0755); err != nil {
		t.Fatal(err)
	}
	// .git pointer references an existing gitdir AFTER repair completes.
	// We pre-write the healthy state because MockGit's WorktreeRepair is
	// a no-op — the test verifies the applier accepts a healthy post-state.
	if err := afero.WriteFile(fs, filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: wt}}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 1 {
		t.Errorf("expected 1 mutation, got %d", mut)
	}
	if len(g.WorktreeRepairCalls) != 1 || g.WorktreeRepairCalls[0] != hub {
		t.Errorf("expected one repair call on hub, got %v", g.WorktreeRepairCalls)
	}
}

func TestApplier_RewriteGitdir_FailsWhenStillBroken(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hub := "/hub"
	wt := "/hub/hops/feat"
	if err := fs.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	// .git points at a path that does NOT exist; mock repair is a no-op.
	if err := afero.WriteFile(fs, filepath.Join(wt, ".git"), []byte("gitdir: /nope\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRewriteGitdir, WorktreePath: wt}}}
	if _, err := a.Apply(plan); err == nil {
		t.Errorf("expected error when post-state still stale, got nil")
	}
}

func TestApplier_RegisterWithGit_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hub := "/hub"
	wt := "/hub/hops/feat"
	writeHub(t, fs, hub, map[string]config.HubBranch{
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	if err := fs.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	// Mock returns the path as registered after the add call.
	g.WorktreeListOut = "worktree " + wt + "\nHEAD abc\nbranch refs/heads/feat\n"

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRegisterWithGit, WorktreePath: wt}}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 1 {
		t.Errorf("expected 1 mutation, got %d", mut)
	}
	want := []string{hub, "feat", wt}
	if len(g.WorktreeAddCalls) != 3 {
		t.Fatalf("expected one WorktreeAdd flattened to 3 entries, got %v", g.WorktreeAddCalls)
	}
	for i, w := range want {
		if g.WorktreeAddCalls[i] != w {
			t.Errorf("WorktreeAdd arg %d: got %q want %q", i, g.WorktreeAddCalls[i], w)
		}
	}
}

func TestApplier_RegisterWithGit_FailsWhenAddErrors(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	g.WorktreeAddErr = errBoom("permission denied")

	hub := "/hub"
	wt := "/hub/hops/feat"
	writeHub(t, fs, hub, map[string]config.HubBranch{
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})
	_ = fs.MkdirAll(wt, 0755)

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionRegisterWithGit, WorktreePath: wt}}}
	if _, err := a.Apply(plan); err == nil {
		t.Errorf("expected propagated error from git add")
	}
}

func TestApplier_UnregisterFromGit_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	// After prune, registry no longer contains the path.
	g.WorktreeListOut = ""

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: "/hub", Actions: []Action{{Kind: ActionUnregisterFromGit, WorktreePath: "/hub/hops/ghost"}}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 1 {
		t.Errorf("expected 1 mutation, got %d", mut)
	}
}

func TestApplier_UnregisterFromGit_FailsWhenStillRegistered(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	// Mock prune is a no-op — list still contains path → verify fails.
	g.WorktreeListOut = "worktree /hub/hops/ghost\nHEAD abc\nbranch refs/heads/ghost\n"

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: "/hub", Actions: []Action{{Kind: ActionUnregisterFromGit, WorktreePath: "/hub/hops/ghost"}}}
	if _, err := a.Apply(plan); err == nil {
		t.Errorf("expected error when post-state still registered")
	}
}

func TestApplier_UpdateHopJSON_RemovesMissingPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hub := "/hub"
	writeHub(t, fs, hub, map[string]config.HubBranch{
		"gone": {Path: "hops/gone", HopspaceBranch: "gone"},
	})
	// Path does NOT exist on disk.

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionUpdateHopJSON, WorktreePath: "/hub/hops/gone"}}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 1 {
		t.Errorf("expected 1 mutation, got %d", mut)
	}
	reloaded, err := LoadHub(fs, hub)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Config.Branches["gone"]; ok {
		t.Errorf("expected branch 'gone' to be removed")
	}
}

func TestApplier_UpdateHopJSON_AddsExistingPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hub := "/hub"
	wt := "/hub/hops/extra"
	writeHub(t, fs, hub, map[string]config.HubBranch{})
	if err := fs.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{{Kind: ActionUpdateHopJSON, WorktreePath: wt}}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 1 {
		t.Errorf("expected 1 mutation, got %d", mut)
	}
	reloaded, _ := LoadHub(fs, hub)
	if _, ok := reloaded.Config.Branches["extra"]; !ok {
		t.Errorf("expected branch 'extra' to be added: got %+v", reloaded.Config.Branches)
	}
}

func TestApplier_StopsAtFirstError(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()
	g.WorktreeRepairErr = errBoom("disk full")

	hub := "/hub"
	wt1 := "/hub/hops/a"
	wt2 := "/hub/hops/b"
	if err := fs.MkdirAll(wt1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.MkdirAll(wt2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, filepath.Join(wt1, ".git"), []byte("gitdir: /nope\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{
		{Kind: ActionRewriteGitdir, WorktreePath: wt1},
		{Kind: ActionRewriteGitdir, WorktreePath: wt2},
	}}
	mut, err := a.Apply(plan)
	if err == nil {
		t.Fatal("expected error from first action")
	}
	if mut != 0 {
		t.Errorf("expected 0 mutations counted before failure, got %d", mut)
	}
	if len(g.WorktreeRepairCalls) != 1 {
		t.Errorf("second action must not run; got repair calls=%v", g.WorktreeRepairCalls)
	}
}

// errBoom is a tiny error helper to avoid repeated fmt.Errorf imports
// in test cases that just need an opaque error value.
type boomErr string

func (e boomErr) Error() string { return string(e) }
func errBoom(s string) error    { return boomErr(s) }

// TestApplier_RecordBase covers the happy path: an ActionRecordBase for
// a branch with Base=nil writes the inferred base into hop.json and the
// post-action reload verifies it.
func TestApplier_RecordBase(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hub := "/hub"
	wt := "/hub/hops/feat"
	writeHub(t, fs, hub, map[string]config.HubBranch{
		"main": {Path: "hops/main", HopspaceBranch: "main"},
		"feat": {Path: "hops/feat", HopspaceBranch: "feat"},
	})

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{
		{Kind: ActionRecordBase, WorktreePath: wt, NewValue: "develop",
			Reason: "branch.feat.merge=refs/heads/develop"},
	}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 1 {
		t.Errorf("expected 1 mutation, got %d", mut)
	}
	reloaded, _ := LoadHub(fs, hub)
	b := reloaded.Config.Branches["feat"]
	if b.Base == nil || *b.Base != "develop" {
		t.Errorf("expected Base=develop, got %v", b.Base)
	}
}

// TestApplier_RecordBase_SkipsWhenAlreadySet confirms idempotence: if
// another run beats this one to it (or the user set Base manually), the
// apply is a no-op rather than overwriting their choice.
func TestApplier_RecordBase_SkipsWhenAlreadySet(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := mocks.NewMockGit()

	hub := "/hub"
	wt := "/hub/hops/feat"
	existing := "release/2026-05"
	writeHub(t, fs, hub, map[string]config.HubBranch{
		"main": {Path: "hops/main", HopspaceBranch: "main"},
		"feat": {Path: "hops/feat", HopspaceBranch: "feat", Base: &existing},
	})

	a := NewApplier(fs, g)
	plan := &Plan{HubPath: hub, Actions: []Action{
		{Kind: ActionRecordBase, WorktreePath: wt, NewValue: "develop"},
	}}
	mut, err := a.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mut != 0 {
		t.Errorf("expected 0 mutations (idempotent skip), got %d", mut)
	}
	reloaded, _ := LoadHub(fs, hub)
	b := reloaded.Config.Branches["feat"]
	if b.Base == nil || *b.Base != existing {
		t.Errorf("expected Base preserved as %q, got %v", existing, b.Base)
	}
}
