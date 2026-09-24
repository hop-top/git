package cmd

import (
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// recordAddedWorktree records the worktree `git hop add` just created in
// state, with its hub. The hub is recorded whether or not state already
// knows the repository: a second hub of a tracked repository used to get
// its worktrees recorded and never itself, so prune --all never visited
// it. Entries of the repository's other hubs are left alone; worktrees
// are keyed by path, so another hub's worktree of the same branch stays.
//
// A state file that cannot be read is not replaced; the worktree is then
// left unrecorded, with a warning.
func recordAddedWorktree(fs afero.Fs, hub *hop.Hub, repoID, hubPath, branch, worktreePath string) {
	st, err := state.LoadState(fs)
	if err != nil {
		output.Error("Failed to update state: %v", err)
		return
	}

	now := time.Now()
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
	mode := state.HubModeLocal
	if hub.Config.Repo.Mode == config.RepoModeGlobal {
		mode = state.HubModeGlobal
	}
	if err := st.AddHub(repoID, &state.HubState{
		Path:         hubPath,
		Mode:         mode,
		CreatedAt:    now,
		LastAccessed: now,
	}); err != nil {
		output.Error("Failed to update state: %v", err)
		return
	}

	if err := st.PutWorktree(repoID, &state.WorktreeState{
		Path:         worktreePath,
		Branch:       branch,
		Type:         "linked",
		HubPath:      hubPath,
		CreatedAt:    now,
		LastAccessed: now,
	}); err != nil {
		output.Error("Failed to update state: %v", err)
		return
	}
	if err := state.SaveState(fs, st); err != nil {
		output.Error("Failed to save state: %v", err)
	}
}
