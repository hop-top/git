package state

import (
	"fmt"
	"sort"
	"time"
)

// A repository's worktrees are keyed by path (WorktreeKey): git itself
// identifies a worktree by its path, and two hubs of one repository can
// each have a worktree of the same branch. These methods are how callers
// find, add and remove entries; they compare paths with SamePath.

// WorktreeAt returns the entry recorded for the worktree at path, and its
// key.
func (r *RepositoryState) WorktreeAt(path string) (string, *WorktreeState, bool) {
	if r == nil || path == "" {
		return "", nil, false
	}
	if wt, ok := r.Worktrees[WorktreeKey(path)]; ok && wt != nil {
		return WorktreeKey(path), wt, true
	}
	want := ResolvePath(path)
	for key, wt := range r.Worktrees {
		if wt != nil && wt.Path != "" && ResolvePath(wt.Path) == want {
			return key, wt, true
		}
	}
	return "", nil, false
}

// Worktree returns the entry for the worktree of branch in the hub at
// hubPath.
func (r *RepositoryState) Worktree(hubPath, branch string) (*WorktreeState, bool) {
	if r == nil {
		return nil, false
	}
	for _, key := range r.SortedWorktreeKeys() {
		wt := r.Worktrees[key]
		if wt.Branch == branch && SamePath(wt.HubPath, hubPath) {
			return wt, true
		}
	}
	return nil, false
}

// HubWorktrees returns the entries whose hub is the one at hubPath, in
// SortedWorktreeKeys order.
func (r *RepositoryState) HubWorktrees(hubPath string) []*WorktreeState {
	var out []*WorktreeState
	if r == nil {
		return out
	}
	for _, key := range r.SortedWorktreeKeys() {
		if wt := r.Worktrees[key]; wt.HubPath != "" && SamePath(wt.HubPath, hubPath) {
			out = append(out, wt)
		}
	}
	return out
}

// SortedWorktreeKeys returns the keys of the repository's entries ordered
// by branch, then hub, then path, so every listing is stable. Nil entries
// are skipped.
func (r *RepositoryState) SortedWorktreeKeys() []string {
	if r == nil {
		return nil
	}
	keys := make([]string, 0, len(r.Worktrees))
	for key, wt := range r.Worktrees {
		if wt != nil {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := r.Worktrees[keys[i]], r.Worktrees[keys[j]]
		if a.Branch != b.Branch {
			return a.Branch < b.Branch
		}
		if a.HubPath != b.HubPath {
			return a.HubPath < b.HubPath
		}
		return keys[i] < keys[j]
	})
	return keys
}

// SortedWorktrees returns the repository's entries in SortedWorktreeKeys
// order.
func (r *RepositoryState) SortedWorktrees() []*WorktreeState {
	keys := r.SortedWorktreeKeys()
	out := make([]*WorktreeState, len(keys))
	for i, key := range keys {
		out[i] = r.Worktrees[key]
	}
	return out
}

// PutWorktree records wt under its path, replacing only an entry for the
// same worktree (the same path, however spelled). wt.Branch must be set.
func (s *State) PutWorktree(repoID string, wt *WorktreeState) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}
	if wt == nil || wt.Path == "" {
		return fmt.Errorf("worktree has no path")
	}
	if wt.Branch == "" {
		return fmt.Errorf("worktree %s has no branch", wt.Path)
	}
	if repo.Worktrees == nil {
		repo.Worktrees = make(map[string]*WorktreeState)
	}
	if key, _, ok := repo.WorktreeAt(wt.Path); ok {
		delete(repo.Worktrees, key)
	}
	repo.Worktrees[WorktreeKey(wt.Path)] = wt
	s.LastUpdated = time.Now()
	return nil
}

// AddWorktree records wt as the worktree of branch: PutWorktree with
// wt.Branch set to branch.
func (s *State) AddWorktree(repoID, branch string, wt *WorktreeState) error {
	if wt != nil {
		wt.Branch = branch
	}
	return s.PutWorktree(repoID, wt)
}

// RemoveWorktreeAt removes the entry for the worktree at path, if any.
func (s *State) RemoveWorktreeAt(repoID, path string) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}
	if key, _, ok := repo.WorktreeAt(path); ok {
		delete(repo.Worktrees, key)
		s.LastUpdated = time.Now()
	}
	return nil
}

// RemoveHub removes the hub at hubPath and the worktrees recorded for it.
// The repository goes too once it has no hub and no worktree left; its
// other hubs and their worktrees are kept.
func (s *State) RemoveHub(repoID, hubPath string) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}
	hubs := repo.Hubs[:0]
	for _, h := range repo.Hubs {
		if h != nil && !SamePath(h.Path, hubPath) {
			hubs = append(hubs, h)
		}
	}
	repo.Hubs = hubs
	for key, wt := range repo.Worktrees {
		if wt == nil || (wt.HubPath != "" && SamePath(wt.HubPath, hubPath)) {
			delete(repo.Worktrees, key)
		}
	}
	if len(repo.Hubs) == 0 && len(repo.Worktrees) == 0 {
		delete(s.Repositories, repoID)
	}
	s.LastUpdated = time.Now()
	return nil
}
