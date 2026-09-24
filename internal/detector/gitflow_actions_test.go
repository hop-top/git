package detector

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// recordingGit answers git-flow config reads for a repo with a feature/
// type and records every git-flow command it is asked to run.
type recordingGit struct {
	calls []string
}

func (g *recordingGit) GetConfig(_, key string) (string, error) {
	switch key {
	case "gitflow.initialized":
		return "true", nil
	case "gitflow.branch.feature.parent":
		return "develop", nil
	}
	return "", errors.New("not found")
}

func (g *recordingGit) GetConfigRegex(_, _ string) (map[string]string, error) {
	return map[string]string{"gitflow.branch.feature.prefix": "feature/"}, nil
}

func (g *recordingGit) RunGitFlowStart(_, branchType, name string) error {
	g.calls = append(g.calls, branchType+" start "+name)
	return nil
}

func (g *recordingGit) RunGitFlowFinish(_, branchType, name string) error {
	g.calls = append(g.calls, branchType+" finish "+name)
	return nil
}

func newGitFlowManager(g GitInterface, opts ...GitFlowOption) *Manager {
	m := NewManager(nil, g)
	m.Register(NewGitFlowNextDetector(g, opts...))
	m.Register(NewGenericDetector(DefaultGenericConfig()))
	return m
}

func TestGitFlowNextDetector_ActionsOffByDefault(t *testing.T) {
	g := &recordingGit{}
	var skipped []string
	m := newGitFlowManager(g, WithSkippedAction(func(info *BranchTypeInfo, action string) {
		skipped = append(skipped, info.Type+" "+action+" "+info.Name)
	}))

	for _, run := range []func(context.Context, string, string, string) (*BranchTypeInfo, error){
		m.ExecutePreAdd, m.ExecutePreRemove,
	} {
		info, err := run(context.Background(), "feature/x", "/repo", "/repo/hops/feature/x")
		if err != nil {
			t.Fatalf("detector returned error: %v", err)
		}
		// Detection is read-only and must keep working with actions off:
		// it is what fills the GIT_HOP_BRANCH_* hook variables.
		if info == nil || info.Source != "gitflow-next" || info.Type != "feature" || info.Name != "x" {
			t.Fatalf("detection = %+v, want gitflow-next feature x", info)
		}
	}

	if len(g.calls) != 0 {
		t.Errorf("git-flow commands ran with actions off: %v", g.calls)
	}
	want := []string{"feature start x", "feature finish x"}
	if !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped actions = %v, want %v", skipped, want)
	}
}

func TestGitFlowNextDetector_ActionsEnabled(t *testing.T) {
	g := &recordingGit{}
	notified := false
	m := newGitFlowManager(g,
		WithGitFlowActions(true),
		WithSkippedAction(func(*BranchTypeInfo, string) { notified = true }),
	)

	if _, err := m.ExecutePreAdd(context.Background(), "feature/x", "/repo", "/wt"); err != nil {
		t.Fatalf("ExecutePreAdd: %v", err)
	}
	if _, err := m.ExecutePreRemove(context.Background(), "feature/x", "/repo", "/wt"); err != nil {
		t.Fatalf("ExecutePreRemove: %v", err)
	}

	want := []string{"feature start x", "feature finish x"}
	if !reflect.DeepEqual(g.calls, want) {
		t.Errorf("git-flow calls = %v, want %v", g.calls, want)
	}
	if notified {
		t.Error("skip notice fired although actions ran")
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
