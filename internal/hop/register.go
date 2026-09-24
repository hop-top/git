package hop

import (
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// Worktree types recorded in state for a hub's initial worktree.
const (
	// WorktreeTypeBare is the initial worktree of a bare hub (hops/<branch>).
	WorktreeTypeBare = "bare"
	// WorktreeTypeMain is a regular repository's own working tree, which a
	// regular conversion keeps as the default branch's worktree.
	WorktreeTypeMain = "main"
)

// NewHub describes a hub that clone or init just created, for
// RegisterNewHub.
type NewHub struct {
	URI           string
	Org           string
	Repo          string
	DefaultBranch string
	HubPath       string
	// WorktreePath is the default branch's worktree, absolute.
	WorktreePath string
	// WorktreeType is WorktreeTypeBare or WorktreeTypeMain.
	WorktreeType string
	// Global marks a hub whose hopspace lives in the data home.
	Global bool
}

// RegisterNewHub records a new hub everywhere git-hop looks for one: the
// data home exists, the hops registry lists the default branch, and state
// holds the repository, the hub and its initial worktree, which is what
// list, status --all and prune read. Failures warn; the hub itself is
// already on disk and usable.
func RegisterNewHub(fs afero.Fs, h NewHub) {
	// The data home is part of a working install (doctor checks it) even
	// when this hub keeps its hopspace locally and writes nothing there.
	if err := fs.MkdirAll(GetGitHopDataHome(), 0o755); err != nil {
		output.Warn("failed to create data directory: %v", err)
	}

	if err := registerProject(fs, h.Org, h.Repo, h.DefaultBranch, h.WorktreePath); err != nil {
		output.Warn("failed to register in global registry: %v", err)
	}

	st, err := state.LoadState(fs)
	if err != nil {
		st = state.NewState()
	}

	repoID := repoIDFor(h.Org, h.Repo)
	if st.Repositories[repoID] == nil {
		st.AddRepository(repoID, &state.RepositoryState{
			URI:           h.URI,
			Org:           h.Org,
			Repo:          h.Repo,
			DefaultBranch: h.DefaultBranch,
			Worktrees:     make(map[string]*state.WorktreeState),
			Hubs:          []*state.HubState{},
		})
	}

	mode := state.HubModeLocal
	if h.Global {
		mode = state.HubModeGlobal
	}
	now := time.Now()
	if err := st.AddHub(repoID, &state.HubState{
		Path:         h.HubPath,
		Mode:         mode,
		CreatedAt:    now,
		LastAccessed: now,
	}); err != nil {
		output.Warn("failed to add hub to state: %v", err)
	}

	if err := st.AddWorktree(repoID, h.DefaultBranch, &state.WorktreeState{
		Path:         h.WorktreePath,
		Type:         h.WorktreeType,
		HubPath:      h.HubPath,
		CreatedAt:    now,
		LastAccessed: now,
	}); err != nil {
		output.Warn("failed to add worktree to state: %v", err)
		return
	}
	if err := state.SaveState(fs, st); err != nil {
		output.Warn("failed to save state: %v", err)
	}
}
