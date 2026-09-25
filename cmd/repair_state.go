package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/state"
)

// recordRepairedWorktrees records in state the worktrees repair added to
// hop.json (update-hopjson actions for a worktree git lists and hop.json
// lacked), the way add records the worktree it creates: keyed by path,
// under the branch git has checked out there, with the hub. Without it
// doctor reports the hub as registered in state without them.
//
// Only a worktree hop.json now lists at that path is recorded; an
// action that dropped a hop.json row, or one that changed nothing, adds
// nothing.
func recordRepairedWorktrees(fs afero.Fs, hubPath string, plan *hop.Plan) {
	var candidates []hop.Action
	for _, a := range plan.Actions {
		if a.Kind == hop.ActionUpdateHopJSON && a.NewValue != "" {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		return
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Warn("state not updated: %v", err)
		return
	}
	var worktrees []stateWorktree
	for _, a := range candidates {
		if _, listed := hub.Config.Branches[a.NewValue]; !listed ||
			!state.SamePath(hub.BranchPath(a.NewValue), a.WorktreePath) {
			continue
		}
		wt := stateWorktree{Branch: a.NewValue, Path: filepath.Clean(a.WorktreePath)}
		if a.NewValue == hub.Config.Repo.DefaultBranch {
			wt.Type = hop.WorktreeTypeBare
		}
		worktrees = append(worktrees, wt)
	}
	if len(worktrees) == 0 {
		return
	}
	recordHubWorktrees(fs, hub, repoid.For(hubPath, hub.Config.Repo), hubPath, worktrees...)
}
