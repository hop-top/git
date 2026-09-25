package detector

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// recordingGit answers git-flow config reads for a repo with a feature/
// type and records every git-flow command it is asked to run, with the
// directory it runs in. worktrees is the `git worktree list --porcelain`
// it reports; detached is set by `git checkout --detach`.
type recordingGit struct {
	calls     []string
	dirs      []string
	config    map[string]string
	worktrees string
	finishErr error
	runs      []string
}

func (g *recordingGit) GetConfig(_, key string) (string, error) {
	switch key {
	case "gitflow.initialized":
		return "true", nil
	case "gitflow.branch.feature.parent":
		return "develop", nil
	}
	if v, ok := g.config[key]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}

func (g *recordingGit) GetConfigRegex(_, _ string) (map[string]string, error) {
	return map[string]string{"gitflow.branch.feature.prefix": "feature/"}, nil
}

func (g *recordingGit) RunGitFlowStart(dir, branchType, name, base string) error {
	g.calls = append(g.calls, strings.TrimSpace(branchType+" start "+name+" "+base))
	g.dirs = append(g.dirs, dir)
	return nil
}

func (g *recordingGit) RunGitFlowFinish(dir, branchType, name string) error {
	g.calls = append(g.calls, branchType+" finish "+name)
	g.dirs = append(g.dirs, dir)
	return g.finishErr
}

func (g *recordingGit) RunInDir(dir, cmd string, args ...string) (string, error) {
	line := dir + ": " + cmd + " " + strings.Join(args, " ")
	g.runs = append(g.runs, line)
	switch strings.Join(args, " ") {
	case "worktree list --porcelain":
		return g.worktrees, nil
	case "branch --show-current":
		if g.detached() {
			return "\n", nil
		}
		return "feature/x\n", nil
	}
	return "", nil
}

// detached reports whether a detach ran with no checkout after it.
func (g *recordingGit) detached() bool {
	d := false
	for _, r := range g.runs {
		switch {
		case strings.HasSuffix(r, "checkout --quiet --detach"):
			d = true
		case strings.Contains(r, "checkout --quiet feature/x"):
			d = false
		}
	}
	return d
}

func newGitFlowManager(g GitInterface, opts ...GitFlowOption) *Manager {
	m := NewManager(nil, g)
	m.Register(NewGitFlowNextDetector(g, opts...))
	m.Register(NewGenericDetector(DefaultGenericConfig()))
	return m
}

// addAndRemove detects branch, runs its add action in wt, then its
// pre-remove action.
func addAndRemove(t *testing.T, m *Manager, branch, wt string) *BranchTypeInfo {
	t.Helper()
	info, err := m.DetectBranch(branch, "/repo")
	if err != nil {
		t.Fatalf("DetectBranch: %v", err)
	}
	if err := m.ExecuteAdd(context.Background(), info, "/repo", wt); err != nil {
		t.Fatalf("ExecuteAdd: %v", err)
	}
	if _, err := m.ExecutePreRemove(context.Background(), branch, "/repo", wt); err != nil {
		t.Fatalf("ExecutePreRemove: %v", err)
	}
	return info
}

func TestGitFlowNextDetector_ActionsOffByDefault(t *testing.T) {
	g := &recordingGit{}
	var skipped []string
	m := newGitFlowManager(g, WithSkippedAction(func(info *BranchTypeInfo, action string) {
		skipped = append(skipped, info.Type+" "+action+" "+info.Name)
	}))

	info := addAndRemove(t, m, "feature/x", t.TempDir())
	// Detection is read-only and must keep working with actions off: it is
	// what fills the GIT_HOP_BRANCH_* hook variables.
	if info == nil || info.Source != "gitflow-next" || info.Type != "feature" || info.Name != "x" {
		t.Fatalf("detection = %+v, want gitflow-next feature x", info)
	}

	if len(g.calls) != 0 || len(g.runs) != 0 {
		t.Errorf("git ran with actions off: %v %v", g.calls, g.runs)
	}
	want := []string{"feature start x", "feature finish x"}
	if !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped actions = %v, want %v", skipped, want)
	}
}

// Both actions run in the branch's worktree, never in the repository
// path: a bare hub is no work tree for git-flow to run in.
func TestGitFlowNextDetector_ActionsRunInWorktree(t *testing.T) {
	wt := t.TempDir()
	g := &recordingGit{worktrees: "worktree /repo/hops/develop\nHEAD abc\nbranch refs/heads/develop\n\n" +
		"worktree " + wt + "\nHEAD def\nbranch refs/heads/feature/x\n"}
	notified := false
	m := newGitFlowManager(g,
		WithGitFlowActions(true),
		WithSkippedAction(func(*BranchTypeInfo, string) { notified = true }),
	)

	addAndRemove(t, m, "feature/x", wt)

	if want := []string{"feature start x", "feature finish x"}; !reflect.DeepEqual(g.calls, want) {
		t.Errorf("git-flow calls = %v, want %v", g.calls, want)
	}
	if want := []string{wt, wt}; !reflect.DeepEqual(g.dirs, want) {
		t.Errorf("git-flow ran in %v, want %v", g.dirs, want)
	}
	if g.detached() {
		t.Error("worktree detached although the finish target is checked out")
	}
	if notified {
		t.Error("skip notice fired although actions ran")
	}
}

func TestGitFlowNextDetector_StartBase(t *testing.T) {
	g := &recordingGit{}
	d := NewGitFlowNextDetector(g, WithGitFlowActions(true), WithStartBase("release/1"))
	if err := d.OnAdd(context.Background(), &BranchTypeInfo{Type: "feature", Name: "x"}, "/wt", "/repo"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"feature start x release/1"}; !reflect.DeepEqual(g.calls, want) {
		t.Errorf("git-flow calls = %v, want %v", g.calls, want)
	}
}

// With the finish target checked out nowhere, the branch's worktree is
// detached before finish runs there, so git-flow checks the target out
// in it rather than falling back to the bare hub.
func TestGitFlowNextDetector_FinishDetachesWhenTargetCheckedOutNowhere(t *testing.T) {
	wt := t.TempDir()
	g := &recordingGit{worktrees: "worktree /repo/hops/main\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree " + wt + "\nHEAD def\nbranch refs/heads/feature/x\n"}
	d := NewGitFlowNextDetector(g, WithGitFlowActions(true))
	info := &BranchTypeInfo{Type: "feature", Name: "x", Prefix: "feature/", Parent: "develop", Source: "gitflow-next"}

	if err := d.OnRemove(context.Background(), info, wt, "/repo"); err != nil {
		t.Fatal(err)
	}
	if !g.detached() {
		t.Errorf("worktree not detached before finish: %v", g.runs)
	}
	if want := []string{wt}; !reflect.DeepEqual(g.dirs, want) {
		t.Errorf("finish ran in %v, want %v", g.dirs, want)
	}
}

// The recorded base, not the type's parent, is the finish target.
func TestGitFlowNextDetector_FinishTargetIsRecordedBase(t *testing.T) {
	wt := t.TempDir()
	g := &recordingGit{
		config:    map[string]string{"gitflow.branch.feature/x.base": "release/1"},
		worktrees: "worktree /repo/hops/develop\nHEAD abc\nbranch refs/heads/develop\n",
	}
	d := NewGitFlowNextDetector(g, WithGitFlowActions(true))
	info := &BranchTypeInfo{Type: "feature", Name: "x", Prefix: "feature/", Parent: "develop", Source: "gitflow-next"}

	if err := d.OnRemove(context.Background(), info, wt, "/repo"); err != nil {
		t.Fatal(err)
	}
	if !g.detached() {
		t.Errorf("release/1 is checked out nowhere, yet the worktree was not detached: %v", g.runs)
	}
}

// A failed finish that left the worktree detached checks the branch out
// again, so a retry finds it as it was.
func TestGitFlowNextDetector_FailedFinishReattaches(t *testing.T) {
	wt := t.TempDir()
	g := &recordingGit{finishErr: errors.New("boom")}
	d := NewGitFlowNextDetector(g, WithGitFlowActions(true))
	info := &BranchTypeInfo{Type: "feature", Name: "x", Prefix: "feature/", Parent: "develop", Source: "gitflow-next"}

	if err := d.OnRemove(context.Background(), info, wt, "/repo"); err == nil {
		t.Fatal("OnRemove swallowed the finish failure")
	}
	if g.detached() {
		t.Errorf("worktree left detached after a failed finish: %v", g.runs)
	}
}

// A branch whose worktree is gone finishes in the target's worktree.
func TestGitFlowNextDetector_FinishWithoutWorktree(t *testing.T) {
	g := &recordingGit{worktrees: "worktree /repo/hops/develop\nHEAD abc\nbranch refs/heads/develop\n"}
	d := NewGitFlowNextDetector(g, WithGitFlowActions(true))
	info := &BranchTypeInfo{Type: "feature", Name: "x", Prefix: "feature/", Parent: "develop", Source: "gitflow-next"}

	if err := d.OnRemove(context.Background(), info, "/nonexistent/wt", "/repo"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/repo/hops/develop"}; !reflect.DeepEqual(g.dirs, want) {
		t.Errorf("finish ran in %v, want %v", g.dirs, want)
	}

	g = &recordingGit{}
	d = NewGitFlowNextDetector(g, WithGitFlowActions(true))
	if err := d.OnRemove(context.Background(), info, "/nonexistent/wt", "/repo"); err == nil {
		t.Error("finish with no worktree to run in did not fail")
	}
	if len(g.calls) != 0 {
		t.Errorf("git-flow ran with no work tree: %v", g.calls)
	}
}

func TestGitFlowNextDetector_StartsBranch(t *testing.T) {
	info := &BranchTypeInfo{Type: "feature", Name: "x", Source: "gitflow-next"}
	if NewGitFlowNextDetector(nil).StartsBranch(info) {
		t.Error("starts a branch with actions off")
	}
	on := NewGitFlowNextDetector(nil, WithGitFlowActions(true))
	if !on.StartsBranch(info) {
		t.Error("does not start its own branch type with actions on")
	}
	if on.StartsBranch(&BranchTypeInfo{Source: "generic"}) || on.StartsBranch(nil) {
		t.Error("starts a branch another detector matched")
	}
}

func TestGitFlowNextDetector_ActionsEnabledReportsFailure(t *testing.T) {
	g := &mockGitForDetector{flowStartErr: errors.New("boom")}
	d := NewGitFlowNextDetector(g, WithGitFlowActions(true))
	err := d.OnAdd(context.Background(), &BranchTypeInfo{Type: "feature", Name: "x"}, "", "/repo")
	if err == nil {
		t.Fatal("OnAdd swallowed the git-flow failure")
	}
}

func TestGitFlowNextDetector_ActionsEnabledAccessor(t *testing.T) {
	if NewGitFlowNextDetector(nil).ActionsEnabled() {
		t.Error("actions enabled by default")
	}
	if !NewGitFlowNextDetector(nil, WithGitFlowActions(true)).ActionsEnabled() {
		t.Error("WithGitFlowActions(true) did not enable actions")
	}
}
