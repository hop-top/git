package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"hop.top/git/internal/task"
)

// fakeResolver serves tasks from a map and counts lookups.
type fakeResolver struct {
	tasks map[string]task.Task
	err   error
	calls int
}

func (f *fakeResolver) Resolve(_ context.Context, id string) (task.Task, error) {
	f.calls++
	if f.err != nil {
		return task.Task{}, f.err
	}
	t, ok := f.tasks[id]
	if !ok {
		return task.Task{}, fmt.Errorf("task not found: %s", id)
	}
	t.ID = id
	return t, nil
}

func TestResolveAddTarget_ExplicitBranchSkipsLookup(t *testing.T) {
	r := &fakeResolver{}
	branch, id, err := resolveAddTarget(context.Background(), r, []string{"feat/mine"}, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feat/mine" || id != "T-1" {
		t.Errorf("got (%q, %q), want (feat/mine, T-1)", branch, id)
	}
	if r.calls != 0 {
		t.Errorf("resolver called %d times, want 0 for an explicit branch", r.calls)
	}
}

func TestResolveAddTarget_NoTask(t *testing.T) {
	branch, id, err := resolveAddTarget(context.Background(), &fakeResolver{}, []string{"feat/mine"}, "")
	if err != nil || branch != "feat/mine" || id != "" {
		t.Errorf("got (%q, %q, %v), want (feat/mine, \"\", nil)", branch, id, err)
	}
}

func TestResolveAddTarget_DerivesBranchWithoutID(t *testing.T) {
	r := &fakeResolver{tasks: map[string]task.Task{
		"T-2": {Title: "Login redirect loops", Tags: []string{"type:bug"}},
	}}
	branch, id, err := resolveAddTarget(context.Background(), r, nil, "T-2")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "fix/login-redirect-loops" || id != "T-2" {
		t.Errorf("got (%q, %q), want (fix/login-redirect-loops, T-2)", branch, id)
	}
}

func TestResolveAddTarget_LookupFailureAsksForBranch(t *testing.T) {
	for name, r := range map[string]*fakeResolver{
		"tlc missing":   {err: task.ErrUnavailable},
		"lookup failed": {tasks: map[string]task.Task{}},
		"no usable title": {tasks: map[string]task.Task{
			"T-3": {Title: "???"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := resolveAddTarget(context.Background(), r, nil, "T-3")
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), "git hop add <branch> --task T-3") {
				t.Errorf("err = %q, want a hint naming the explicit form", err)
			}
		})
	}
}

func TestResolveAddTarget_InvalidID(t *testing.T) {
	r := &fakeResolver{}
	for _, id := range []string{"-rf", "T 1"} {
		if _, _, err := resolveAddTarget(context.Background(), r, []string{"feat/x"}, id); err == nil {
			t.Errorf("resolveAddTarget(--task %q) succeeded, want error", id)
		}
	}
	if r.calls != 0 {
		t.Errorf("resolver called for an invalid id")
	}
}

func TestAddArgs(t *testing.T) {
	cmd := addCmd
	t.Cleanup(func() { _ = cmd.Flags().Set("task", ""); cmd.Flags().Lookup("task").Changed = false })

	if err := addArgs(cmd, []string{"feat/x"}); err != nil {
		t.Errorf("one branch: %v", err)
	}
	if err := addArgs(cmd, []string{"a", "b"}); err == nil {
		t.Error("two args accepted")
	}
	err := addArgs(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--task") {
		t.Errorf("no args, no --task: err = %v, want one mentioning --task", err)
	}
	if err := cmd.Flags().Set("task", "T-1"); err != nil {
		t.Fatal(err)
	}
	if err := addArgs(cmd, nil); err != nil {
		t.Errorf("no args with --task: %v", err)
	}
}

func TestAddTaskEnv(t *testing.T) {
	env := map[string]string{"GIT_HOP_BRANCH_TYPE": "feature"}
	addTaskEnv(env, "")
	if _, ok := env["GIT_HOP_TASK"]; ok {
		t.Error("GIT_HOP_TASK set with no task")
	}
	addTaskEnv(env, "T-1")
	if env["GIT_HOP_TASK"] != "T-1" {
		t.Errorf("GIT_HOP_TASK = %q, want T-1", env["GIT_HOP_TASK"])
	}
}
