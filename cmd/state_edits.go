package cmd

import (
	"github.com/spf13/afero"

	"hop.top/git/internal/state"
)

// stateEdits records the changes a pass makes to the state it loaded,
// so they can be replayed on state.json as it is when saving. prune and
// doctor --fix decide what to change from a slow scan (git commands,
// prompts), long after loading and without the state lock; saving the
// state they loaded would undo whatever another run saved meanwhile
// (state.Update).
//
// Each edit is applied to the loaded state at once, so the passes that
// follow see it, and again to the fresh state by save.
type stateEdits []func(*state.State)

// apply makes edit to st and records it. A nil e records nothing.
func (e *stateEdits) apply(st *state.State, edit func(*state.State)) {
	edit(st)
	if e != nil {
		*e = append(*e, edit)
	}
}

// dropWorktree removes repoID's worktree entry under key.
func (e *stateEdits) dropWorktree(st *state.State, repoID, key string) {
	e.apply(st, func(s *state.State) {
		if repo := s.Repositories[repoID]; repo != nil {
			delete(repo.Worktrees, key)
		}
	})
}

// relocateWorktree moves repoID's worktree entry under key to newPath.
// A copy of wt is recorded each time, so the states never share it.
func (e *stateEdits) relocateWorktree(st *state.State, repoID, key string, wt state.WorktreeState, newPath string) {
	wt.Path = newPath
	e.apply(st, func(s *state.State) {
		repo := s.Repositories[repoID]
		if repo == nil {
			return
		}
		delete(repo.Worktrees, key)
		moved := wt
		_ = s.PutWorktree(repoID, &moved)
	})
}

// dropHub removes repoID's hub entry recorded at path. Only the hub
// goes; its worktrees are pruned on their own.
func (e *stateEdits) dropHub(st *state.State, repoID, path string) {
	e.apply(st, func(s *state.State) {
		repo := s.Repositories[repoID]
		if repo == nil {
			return
		}
		var kept []*state.HubState
		for _, hub := range repo.Hubs {
			if hub == nil || hub.Path != path {
				kept = append(kept, hub)
			}
		}
		repo.Hubs = kept
	})
}

// save replays the edits on state.json under the state lock. Without
// edits nothing is written.
func (e stateEdits) save(fs afero.Fs) error {
	if len(e) == 0 {
		return nil
	}
	return state.Update(fs, func(st *state.State) error {
		for _, edit := range e {
			edit(st)
		}
		return nil
	})
}
