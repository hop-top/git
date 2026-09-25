package cmd

import (
	"errors"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// hubListsPath reports whether hop.json lists a worktree at path.
func hubListsPath(hub *hop.Hub, path string) bool {
	for name := range hub.Config.Branches {
		if state.SamePath(hub.BranchPath(name), path) {
			return true
		}
	}
	return false
}

// syncSharedHopspace records in the --global hub's data-home hopspace the
// worktrees repair added to hop.json, and drops the records of the
// worktrees at gone, whose rows it dropped.
func syncSharedHopspace(fs afero.Fs, hub *hop.Hub, added []stateWorktree, gone []string) {
	if len(added) == 0 && len(gone) == 0 {
		return
	}
	hopspacePath := hop.ResolveHopspacePath(hub.Path, hub.Config.Repo)
	hopspace, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		output.Warn("hopspace not updated: %v", err)
		return
	}
	for _, wt := range added {
		if err := hopspace.RegisterBranch(hub.Path, wt.Branch, wt.Path); err != nil {
			output.Warn("failed to record %s in hopspace: %v", wt.Path, err)
		}
	}
	for _, path := range gone {
		if err := hopspace.UnregisterBranch(hub.Path, "", path); err != nil {
			output.Warn("failed to drop %s from hopspace: %v", path, err)
		}
	}
}

// dropStateWorktrees removes from state the entries of the hub's
// worktrees at paths. Another hub's entry at such a path is kept.
func dropStateWorktrees(fs afero.Fs, repoID, hubPath string, paths []string) {
	if len(paths) == 0 {
		return
	}
	err := state.Update(fs, func(st *state.State) error {
		repo := st.Repositories[repoID]
		changed := false
		for _, path := range paths {
			_, wt, ok := repo.WorktreeAt(path)
			if !ok || (wt.HubPath != "" && !state.SamePath(wt.HubPath, hubPath)) {
				continue
			}
			if err := st.RemoveWorktreeAt(repoID, path); err != nil {
				return err
			}
			changed = true
		}
		if !changed {
			return state.ErrSkipSave
		}
		return nil
	})
	if err != nil && !errors.Is(err, state.ErrSkipSave) {
		output.Warn("state not updated: %v", err)
	}
}
