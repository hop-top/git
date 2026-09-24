package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/task"
)

// addTaskFlag holds the --task CLI flag value.
var addTaskFlag string

// taskResolver looks a task id up. The production resolver shells out to
// tlc; tests substitute a fake.
type taskResolver interface {
	Resolve(ctx context.Context, id string) (task.Task, error)
}

var addTaskResolver taskResolver = task.NewTLC()

// addArgs accepts one branch, or none when --task is given to derive it.
func addArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
		return err
	}
	if len(args) == 0 && !cmd.Flags().Changed("task") {
		return errors.New("requires a branch name, or --task <id> to derive one")
	}
	return nil
}

// resolveAddTarget returns the branch `git hop add` operates on and the
// task id to record against it. An explicit branch is used verbatim and
// the resolver is never consulted; without one, the branch is derived
// from the task (see task.BranchName). The id is never part of the name.
func resolveAddTarget(ctx context.Context, r taskResolver, args []string, taskID string) (branch, id string, err error) {
	if taskID != "" {
		if err := task.ValidateID(taskID); err != nil {
			return "", "", err
		}
	}
	if len(args) > 0 {
		return args[0], taskID, nil
	}

	t, err := r.Resolve(ctx, taskID)
	if err == nil {
		branch, err = task.BranchName(t)
	}
	if err != nil {
		return "", "", fmt.Errorf("cannot derive a branch name for task %s: %w\n"+
			"hint: name the branch explicitly: git hop add <branch> --task %s", taskID, err, taskID)
	}
	return branch, taskID, nil
}

// mustResolveAddTarget is resolveAddTarget for the add command: a
// failure is fatal.
func mustResolveAddTarget(args []string) (branch, taskID string) {
	branch, taskID, err := resolveAddTarget(context.Background(), addTaskResolver, args, addTaskFlag)
	if err != nil {
		output.Fatal("%v", err)
	}
	return branch, taskID
}

// addTaskEnv exports taskID to the add hooks as GIT_HOP_TASK.
func addTaskEnv(env map[string]string, taskID string) {
	if taskID != "" {
		env["GIT_HOP_TASK"] = taskID
	}
}

// recordAddTask stores taskID as branches.<branch>.task in hop.json. The
// worktree already exists by now, so a failed write only warns.
func recordAddTask(hub *hop.Hub, branch, taskID string) {
	if taskID == "" {
		return
	}
	if err := hub.SetBranchTask(branch, taskID); err != nil {
		output.Warn("Failed to record task %s: %v", taskID, err)
	}
}
