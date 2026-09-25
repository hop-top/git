package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/state"
)

// recordRepairedWorktrees records in state the worktrees repair added to
// hop.json (update-hopjson actions for a worktree git lists and hop.json
// lacked), the way add records the worktree it creates: keyed by path,
// under the branch git has checked out there, with the hub. Without it
// doctor reports the hub as registered in state without them. A --global
// hub's shared hopspace records them too, or doctor reports them as in
// the hub but not in the hopspace.
//
// The rows repair dropped (a worktree gone from git and from disk) lose
// their state entry and, in a --global hub, their hopspace record.
//
// Only what hop.json now shows counts (repairedRows): an action that
// changed nothing records and drops nothing.
func recordRepairedWorktrees(fs afero.Fs, hubPath string, plan *hop.Plan) {
	hasRows := false
	for _, a := range plan.Actions {
		hasRows = hasRows || a.Kind == hop.ActionUpdateHopJSON
	}
	if !hasRows {
		return
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Warn("state not updated: %v", err)
		return
	}
	added, gone := repairedRows(fs, hub, plan)
	repoID := repoid.For(hubPath, hub.Config.Repo)
	if len(added) > 0 {
		recordHubWorktrees(fs, hub, repoID, hubPath, added...)
	}
	dropStateWorktrees(fs, repoID, hubPath, gone)
	if hub.Config.Repo.Mode == config.RepoModeGlobal {
		syncSharedHopspace(fs, hub, added, gone)
	}
}

// repairedRows returns the worktrees repair added to hop.json, those it
// lists now at the action's path under the branch checked out there, and
// the paths of the rows it dropped, those hop.json no longer lists and
// whose directory is gone.
func repairedRows(fs afero.Fs, hub *hop.Hub, plan *hop.Plan) ([]stateWorktree, []string) {
	var added []stateWorktree
	var gone []string
	for _, a := range plan.Actions {
		if a.Kind != hop.ActionUpdateHopJSON {
			continue
		}
		if a.NewValue == "" {
			if exists, _ := afero.DirExists(fs, a.WorktreePath); !exists && !hubListsPath(hub, a.WorktreePath) {
				gone = append(gone, filepath.Clean(a.WorktreePath))
			}
			continue
		}
		if _, listed := hub.Config.Branches[a.NewValue]; !listed ||
			!state.SamePath(hub.BranchPath(a.NewValue), a.WorktreePath) {
			continue
		}
		wt := stateWorktree{Branch: a.NewValue, Path: filepath.Clean(a.WorktreePath)}
		if a.NewValue == hub.Config.Repo.DefaultBranch {
			wt.Type = hop.WorktreeTypeBare
		}
		added = append(added, wt)
	}
	return added, gone
}
