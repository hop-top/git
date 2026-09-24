package hop

import (
	"errors"
	"fmt"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
)

// ErrNotInHub reports that a directory belongs to no hub git-hop knows:
// no hop.json above it and no hub recorded in state around it.
var ErrNotInHub = errors.New("not inside a git-hop repository")

// ResolvedHub is the hub a directory belongs to.
type ResolvedHub struct {
	*Hub
	// RepoID is the hub's repository key in state.
	RepoID string
	// Recorded marks a hub known only from state: one without hop.json,
	// such as a repository registered as-is. Its Hub is a read-only view
	// built from state; writing it fails.
	Recorded bool
}

// ResolveHub finds the hub dir belongs to, for the commands that act on
// "the current repository". hop.json discovery (FindHub) comes first.
// Failing that, a hub recorded in st whose path is dir or one of its
// ancestors is used, the deepest one if several nest: that is how a
// repository registered as-is, which has no hop.json, is recognised.
//
// It returns ErrNotInHub when neither finds a hub, and the load error
// when hop.json exists but cannot be read. st may be nil.
func ResolveHub(fs afero.Fs, st *state.State, dir string) (*ResolvedHub, error) {
	if hubPath, err := FindHub(fs, dir); err == nil {
		hub, err := LoadHub(fs, hubPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read hub config at %s: %w", hubPath, err)
		}
		return &ResolvedHub{
			Hub:    hub,
			RepoID: repoIDFor(hub.Config.Repo.Org, hub.Config.Repo.Repo),
		}, nil
	}

	if repoID, hubPath, ok := recordedHubAround(st, dir); ok {
		return &ResolvedHub{
			Hub:      recordedHubView(hubPath, st.Repositories[repoID]),
			RepoID:   repoID,
			Recorded: true,
		}, nil
	}
	return nil, ErrNotInHub
}

// recordedHubAround returns the hub recorded in st that contains dir,
// the deepest one when several do.
func recordedHubAround(st *state.State, dir string) (repoID, hubPath string, ok bool) {
	if st == nil {
		return "", "", false
	}
	for id, repo := range st.Repositories {
		if repo == nil {
			continue
		}
		for _, h := range repo.Hubs {
			if h == nil || h.Path == "" {
				continue
			}
			if !pathWithin(dir, h.Path) {
				continue
			}
			if !ok || len(h.Path) > len(hubPath) {
				repoID, hubPath, ok = id, h.Path, true
			}
		}
	}
	return repoID, hubPath, ok
}

// recordedHubView presents a state-recorded hub as a Hub: the repository
// identity plus the worktrees state lists under this hub. It is backed by
// a read-only filesystem so nothing can write a hop.json the hub never had.
func recordedHubView(hubPath string, repo *state.RepositoryState) *Hub {
	cfg := &config.HubConfig{
		Repo: config.RepoConfig{
			URI:           repo.URI,
			Org:           repo.Org,
			Repo:          repo.Repo,
			DefaultBranch: repo.DefaultBranch,
		},
		Branches: map[string]config.HubBranch{},
	}
	for _, wt := range repo.HubWorktrees(hubPath) {
		cfg.Branches[wt.Branch] = config.HubBranch{Path: wt.Path, HopspaceBranch: wt.Branch}
	}
	return &Hub{
		Path:   hubPath,
		Config: cfg,
		fs:     afero.NewReadOnlyFs(afero.NewMemMapFs()),
	}
}
