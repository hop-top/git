package cmd

import (
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// stateWorktree is a worktree of a hub to record in state: the branch
// checked out there, its absolute path and its state type ("linked"
// when empty; hop.WorktreeTypeBare for a bare hub's default-branch
// worktree).
type stateWorktree struct {
	Branch string
	Path   string
	Type   string
}

// recordAddedWorktree records the worktree `git hop add` just created in
// state, with its hub (recordHubWorktrees).
func recordAddedWorktree(fs afero.Fs, hub *hop.Hub, repoID, hubPath, branch, worktreePath string) {
	recordHubWorktrees(fs, hub, repoID, hubPath, stateWorktree{Branch: branch, Path: worktreePath})
}

// recordHubWorktrees records worktrees of the hub at hubPath in state,
// with the hub. The hub is recorded whether or not state already knows
// the repository: a second hub of a tracked repository used to get its
// worktrees recorded and never itself, so prune --all never visited it.
// Entries of the repository's other hubs are left alone; worktrees are
// keyed by path, so another hub's worktree of the same branch stays.
//
// A state file that cannot be read is not replaced; the worktrees are
// then left unrecorded, with an error. The entries are added to
// state.json as it is when they are saved (state.Update), so what
// another run recorded meanwhile stays.
func recordHubWorktrees(fs afero.Fs, hub *hop.Hub, repoID, hubPath string, worktrees ...stateWorktree) {
	now := time.Now()
	mode := state.HubModeLocal
	if hub.Config.Repo.Mode == config.RepoModeGlobal {
		mode = state.HubModeGlobal
	}
	err := state.Update(fs, func(st *state.State) error {
		if st.Repositories[repoID] == nil {
			st.AddRepository(repoID, &state.RepositoryState{
				URI:           hub.Config.Repo.URI,
				Org:           hub.Config.Repo.Org,
				Repo:          hub.Config.Repo.Repo,
				DefaultBranch: hub.Config.Repo.DefaultBranch,
				Worktrees:     make(map[string]*state.WorktreeState),
				Hubs:          []*state.HubState{},
			})
		}
		if err := st.AddHub(repoID, &state.HubState{
			Path:         hubPath,
			Mode:         mode,
			CreatedAt:    now,
			LastAccessed: now,
		}); err != nil {
			return err
		}
		for _, wt := range worktrees {
			typ := wt.Type
			if typ == "" {
				typ = "linked"
			}
			if err := st.PutWorktree(repoID, &state.WorktreeState{
				Path:         wt.Path,
				Branch:       wt.Branch,
				Type:         typ,
				HubPath:      hubPath,
				CreatedAt:    now,
				LastAccessed: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		output.Error("Failed to update state: %v", err)
	}
}
