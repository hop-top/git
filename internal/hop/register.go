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
	// WorktreePath is the default branch's worktree, absolute. Empty
	// when the hub has none yet (a bare repository init adopted before
	// any worktree was added).
	WorktreePath string
	// WorktreeType is WorktreeTypeBare or WorktreeTypeMain.
	WorktreeType string
	// Linked maps branch to absolute path for the hub's other existing
	// worktrees, recorded as `git hop add` records one.
	Linked map[string]string
	// Global marks a hub whose hopspace lives in the data home.
	Global bool
}

// RegisterNewHub records a hub everywhere git-hop looks for one: the
// data home exists, the hops registry lists the default branch, and state
// holds the repository, the hub and its worktrees, which is what list,
// status --all and prune read. Failures warn; the hub itself is already
// on disk and usable.
//
// It merges with what is recorded and never overwrites it
// (PlanHubRegistration): recording a hub again changes nothing, and a
// repository state already knows from another hub keeps that hub and its
// worktrees, including its worktrees of the same branches. A state file
// that cannot be read is left alone.
//
// Returns what was added to state, and the error that kept it from
// being saved.
func RegisterNewHub(fs afero.Fs, h NewHub) (HubRegistration, error) {
	// The data home is part of a working install (doctor checks it) even
	// when this hub keeps its hopspace locally and writes nothing there.
	if err := fs.MkdirAll(GetGitHopDataHome(), 0o755); err != nil {
		output.Warn("failed to create data directory: %v", err)
	}

	if h.WorktreePath != "" {
		if err := registerProject(fs, h.Org, h.Repo, h.DefaultBranch, h.WorktreePath); err != nil {
			output.Warn("failed to register in global registry: %v", err)
		}
	}

	st, err := state.LoadState(fs)
	if err != nil {
		output.Warn("state not updated: %v", err)
		return HubRegistration{}, err
	}

	plan := PlanHubRegistration(st, h)
	if plan.Empty() {
		return plan, nil
	}
	plan.apply(st, h, time.Now())
	if err := state.SaveState(fs, st); err != nil {
		output.Warn("failed to save state: %v", err)
		return plan, err
	}
	return plan, nil
}
