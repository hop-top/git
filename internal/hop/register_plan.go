package hop

import (
	"sort"
	"time"

	"hop.top/git/internal/state"
)

// HubRegistration is what recording a hub adds to state: the part of it
// state does not hold yet. What state already holds for the repository is
// kept as it is, so recording a hub is a merge, and recording it again
// adds nothing. State keys worktrees by path, so another hub's worktree of
// the same branch is a different entry and is left alone.
type HubRegistration struct {
	RepoID string
	// Repo: the repository has no entry yet.
	Repo bool
	// Hub: the repository's hubs do not include this one.
	Hub bool
	// Worktrees are the hub's worktrees state does not record, with their
	// new entries.
	Worktrees []*state.WorktreeState
}

// Empty reports whether there is nothing to record.
func (p HubRegistration) Empty() bool {
	return !p.Repo && !p.Hub && len(p.Worktrees) == 0
}

// Branches returns the branches of the worktrees recorded, sorted.
func (p HubRegistration) Branches() []string {
	branches := make([]string, 0, len(p.Worktrees))
	for _, wt := range p.Worktrees {
		branches = append(branches, wt.Branch)
	}
	sort.Strings(branches)
	return branches
}

// hubWorktrees returns the hub's worktrees: the default branch's initial
// worktree and the linked ones, sorted by branch.
func (h NewHub) hubWorktrees() []*state.WorktreeState {
	var worktrees []*state.WorktreeState
	for branch, path := range h.Linked {
		worktrees = append(worktrees, &state.WorktreeState{Path: path, Branch: branch, Type: "linked"})
	}
	if h.WorktreePath != "" {
		worktrees = append(worktrees, &state.WorktreeState{Path: h.WorktreePath, Branch: h.DefaultBranch, Type: h.WorktreeType})
	}
	sort.Slice(worktrees, func(i, j int) bool { return worktrees[i].Branch < worktrees[j].Branch })
	return worktrees
}

// PlanHubRegistration returns what recording h would add to st. It does
// not change st. A hub or worktree state records under another spelling
// of the same path counts as recorded (state.SamePath).
func PlanHubRegistration(st *state.State, h NewHub) HubRegistration {
	plan := HubRegistration{RepoID: repoIDFor(h.Org, h.Repo)}
	repo := st.Repositories[plan.RepoID]
	plan.Repo = repo == nil
	plan.Hub = true
	if repo != nil {
		for _, hub := range repo.Hubs {
			if hub != nil && state.SamePath(hub.Path, h.HubPath) {
				plan.Hub = false
				break
			}
		}
	}
	for _, wt := range h.hubWorktrees() {
		if _, _, recorded := repo.WorktreeAt(wt.Path); !recorded {
			plan.Worktrees = append(plan.Worktrees, wt)
		}
	}
	return plan
}

// apply adds the plan's entries to st, stamped now.
func (p HubRegistration) apply(st *state.State, h NewHub, now time.Time) {
	if p.Repo {
		st.AddRepository(p.RepoID, &state.RepositoryState{
			URI:           h.URI,
			Org:           h.Org,
			Repo:          h.Repo,
			DefaultBranch: h.DefaultBranch,
			Worktrees:     make(map[string]*state.WorktreeState),
			Hubs:          []*state.HubState{},
		})
	}
	if p.Hub {
		mode := state.HubModeLocal
		if h.Global {
			mode = state.HubModeGlobal
		}
		_ = st.AddHub(p.RepoID, &state.HubState{
			Path:         h.HubPath,
			Mode:         mode,
			CreatedAt:    now,
			LastAccessed: now,
		})
	}
	for _, wt := range p.Worktrees {
		entry := *wt
		entry.HubPath = h.HubPath
		entry.CreatedAt = now
		entry.LastAccessed = now
		_ = st.PutWorktree(p.RepoID, &entry)
	}
	st.LastUpdated = now
}
