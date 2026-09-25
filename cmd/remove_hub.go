package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
)

// removeHub removes the hub at hubPath: its worktrees, the hub directory
// and its state entry, then the repository's data-home hopspace.
func removeHub(fs afero.Fs, hubPath string) {
	output.Info("Removing hub at %s...", hubPath)

	// Load hub to get repo info
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

	// Remove all worktrees
	for branchName, branchConfig := range hub.Config.Branches {
		worktreePath := config.ResolveWorktreePath(branchConfig.Path, hubPath)
		output.Info("Removing worktree for branch %s...", branchName)

		if err := fs.RemoveAll(worktreePath); err != nil {
			output.Warn("Failed to remove worktree %s: %v", branchName, err)
		}
	}

	// Remove hub directory
	output.Info("Removing hub directory...")
	if err := fs.RemoveAll(hubPath); err != nil {
		output.Fatal("Failed to remove hub directory: %v", err)
	}

	// Remove from global state: this hub and its worktrees. The
	// repository's other hubs, and their worktrees, stay.
	st, stErr := state.LoadState(fs)
	if stErr == nil {
		if err := st.RemoveHub(repoID, hubPath); err != nil {
			output.Warn("Failed to update state: %v", err)
		} else {
			if err := state.SaveState(fs, st); err != nil {
				output.Warn("Failed to save state: %v", err)
			}
		}
	}

	// A default hub's hopspace is the hub directory removed above; a
	// --global hub's is the repository's data-home hopspace, which other
	// hubs of the repository may share.
	d := dataHomeHopspaceFor(fs, st, stErr, hub, hubPath)
	switch {
	case !d.exists:
	case d.remove():
		output.Info("Cleaning up hopspace data...")
		if err := fs.RemoveAll(d.path); err != nil {
			output.Warn("Failed to remove hopspace data: %v", err)
		}
	default:
		output.Info("Keeping hopspace data at %s: %s", d.path, d.reason())
		if err := services.DropHubEnvEntries(fs, d.path, hubPath); err != nil {
			output.Warn("Failed to update ports and volumes: %v", err)
		}
		output.Hint("it is removed along with the last hub that uses it")
	}

	output.Success("Successfully removed hub: %s", hubPath)
}

// dataHomeHopspace is what removing a hub does to its repository's
// data-home hopspace ($GIT_HOP_DATA_HOME/<org>/<repo>, where clone
// --global keeps it).
type dataHomeHopspace struct {
	path   string
	exists bool
	// usedBy lists the other hubs state records that still use it.
	usedBy []string
	// stateErr is set when state could not be read: which hubs use the
	// hopspace is then unknown, and it is kept.
	stateErr error
}

// remove reports whether the hopspace goes with the hub: it is there and
// no other hub is known to use it.
func (d dataHomeHopspace) remove() bool {
	return d.exists && d.stateErr == nil && len(d.usedBy) == 0
}

// reason says why the hopspace is kept, or why it goes.
func (d dataHomeHopspace) reason() string {
	switch {
	case d.stateErr != nil:
		return fmt.Sprintf("cannot tell whether another hub uses it (%v)", d.stateErr)
	case len(d.usedBy) > 0:
		return "still used by " + strings.Join(d.usedBy, ", ")
	default:
		return "no other hub uses it"
	}
}

// dataHomeHopspaceFor decides the fate of the data-home hopspace of
// hub's repository when the hub at hubPath is removed. st is the state
// (stErr the error reading it); hubPath's own entry is ignored.
func dataHomeHopspaceFor(fs afero.Fs, st *state.State, stErr error, hub *hop.Hub, hubPath string) dataHomeHopspace {
	path := hop.GetHopspacePath(hop.GetGitHopDataHome(), hop.RepoRefFor(hub.Config.Repo))
	d := dataHomeHopspace{path: path, stateErr: stErr}
	d.exists, _ = afero.DirExists(fs, path)
	if !d.exists || stErr != nil {
		return d
	}
	for _, repo := range st.Repositories {
		for _, h := range repo.Hubs {
			if h == nil || state.SamePath(h.Path, hubPath) {
				continue
			}
			if hubUsesHopspace(fs, repo, h, path) {
				d.usedBy = append(d.usedBy, h.Path)
			}
		}
	}
	sort.Strings(d.usedBy)
	return d
}

// hubUsesHopspace reports whether the recorded hub h uses the hopspace
// at path. Its own marker decides, as hop.ResolveHopspacePath does. A
// hub whose directory is gone uses nothing. One whose hop.json cannot be
// read falls back on the mode state recorded for it, so a hub that may
// still be repaired keeps its hopspace.
func hubUsesHopspace(fs afero.Fs, repo *state.RepositoryState, h *state.HubState, path string) bool {
	if exists, _ := afero.DirExists(fs, h.Path); !exists {
		return false
	}
	if other, err := hop.LoadHub(fs, h.Path); err == nil {
		return state.SamePath(hop.ResolveHopspacePath(h.Path, other.Config.Repo), path)
	}
	return h.Mode == state.HubModeGlobal &&
		state.SamePath(hop.GetHopspacePath(hop.GetGitHopDataHome(), hop.NewRepoRef(repo.URI, repo.Org, repo.Repo)), path)
}
