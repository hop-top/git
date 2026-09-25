package state

import (
	"sort"
	"strings"
	"sync"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
)

// Releases before repo IDs carried the origin's host keyed every
// repository "github.com/<org>/<repo>", whatever its origin, so a
// gitlab.example.com/acme/widgets repository sat under
// github.com/acme/widgets, and two repositories with one org/repo on two
// hosts shared an entry. rekeyRepoIDs moves such repositories to the key
// their origin gives (repoid.New).
//
// Whether a repository needs it is read from its hubs, not from the
// file's version: a release that predates the change loads a 2.x file,
// adds github.com-keyed entries and saves it keeping "version": "2.0.0",
// so a version bump could not tell a migrated file from one written
// since. Rekeying a rekeyed state changes nothing, and neither does a
// second run over a collision, so no version bump is needed.
//
// The origin of a hub is the repo.uri of its hop.json, which every
// command builds the repo ID from, with hop.gitDomain resolved in the hub
// for an origin without a host. A hub whose hop.json cannot be read keeps
// its key.

// RepoIDCollision is a group of hubs recorded under From whose origin
// gives the key To, which state already holds for other hubs. Neither
// entry is merged into the other: both are kept, a warning is printed
// on load, and doctor reports it.
type RepoIDCollision struct {
	From string
	To   string
	Hubs []string
}

// rekeyMove moves hubs (and their worktrees) of the repository under
// from to the new key to. whole moves the entire entry: every hub of
// from goes to the same key.
type rekeyMove struct {
	from, to string
	uri      string
	hubs     []*HubState
	whole    bool
}

// planRekey works out which repositories of st to move and which moves
// collide with an existing key, in key order. It reads each hub's
// hop.json and changes nothing.
func planRekey(fs afero.Fs, st *State) ([]rekeyMove, []RepoIDCollision) {
	loader := config.NewLoader(fs)

	keys := make([]string, 0, len(st.Repositories))
	for k := range st.Repositories {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var moves []rekeyMove
	var collisions []RepoIDCollision
	claimed := map[string]bool{}
	for _, key := range keys {
		repo := st.Repositories[key]
		_, org, name, ok := repoid.Split(key)
		if repo == nil || !ok {
			continue
		}
		targets := map[string][]*HubState{}
		uris := map[string]string{}
		staying := 0
		for _, hub := range repo.Hubs {
			if hub == nil || hub.Path == "" {
				staying++
				continue
			}
			cfg, err := loader.LoadHubConfig(hub.Path)
			if err != nil {
				staying++
				continue
			}
			uri := cfg.Repo.URI
			to := repoid.NewIn(hub.Path, uri, org, name)
			if to == key {
				staying++
				continue
			}
			targets[to] = append(targets[to], hub)
			if uris[to] == "" {
				uris[to] = uri
			}
		}
		if len(targets) == 0 {
			continue
		}
		tos := make([]string, 0, len(targets))
		for to := range targets {
			tos = append(tos, to)
		}
		sort.Strings(tos)
		var planned []rekeyMove
		for _, to := range tos {
			hubs := targets[to]
			if _, exists := st.Repositories[to]; exists || claimed[to] {
				collisions = append(collisions, RepoIDCollision{From: key, To: to, Hubs: hubPaths(hubs)})
				staying += len(hubs)
				continue
			}
			claimed[to] = true
			planned = append(planned, rekeyMove{from: key, to: to, uri: uris[to], hubs: hubs})
		}
		if staying == 0 && len(planned) == 1 {
			planned[0].whole = true
		}
		moves = append(moves, planned...)
	}
	return moves, collisions
}

func hubPaths(hubs []*HubState) []string {
	paths := make([]string, 0, len(hubs))
	for _, h := range hubs {
		paths = append(paths, h.Path)
	}
	return paths
}

// rekeyRepoIDs applies planRekey to st. It reports whether st changed
// and the moves it left out because their key was taken.
func rekeyRepoIDs(fs afero.Fs, st *State) (bool, []RepoIDCollision) {
	moves, collisions := planRekey(fs, st)
	for _, m := range moves {
		applyRekey(st, m)
	}
	return len(moves) > 0, collisions
}

func applyRekey(st *State, m rekeyMove) {
	old := st.Repositories[m.from]
	if m.whole {
		st.Repositories[m.to] = old
		delete(st.Repositories, m.from)
		return
	}
	moved := &RepositoryState{
		URI:           m.uri,
		Org:           old.Org,
		Repo:          old.Repo,
		DefaultBranch: old.DefaultBranch,
		Worktrees:     map[string]*WorktreeState{},
		Hubs:          m.hubs,
	}
	isMoved := func(path string) bool {
		if path == "" {
			return false
		}
		for _, h := range m.hubs {
			if SamePath(h.Path, path) {
				return true
			}
		}
		return false
	}
	for key, wt := range old.Worktrees {
		if wt != nil && isMoved(wt.HubPath) {
			moved.Worktrees[key] = wt
			delete(old.Worktrees, key)
		}
	}
	kept := old.Hubs[:0]
	for _, h := range old.Hubs {
		if h == nil || !isMoved(h.Path) {
			kept = append(kept, h)
		}
	}
	old.Hubs = kept
	st.Repositories[m.to] = moved
}

// needsRekey reports whether planRekey would move any repository of st.
func needsRekey(fs afero.Fs, st *State) bool {
	moves, _ := planRekey(fs, st)
	return len(moves) > 0
}

// RepoIDCollisions returns the repositories of st recorded under a key
// their hubs' origin does not give, and left there because the key it
// gives is taken (see RepoIDCollision). On a state LoadState returned,
// these are all that differ from what the migration would do.
func RepoIDCollisions(fs afero.Fs, st *State) []RepoIDCollision {
	_, collisions := planRekey(fs, st)
	return collisions
}

// warned holds the collisions already warned about in this process, so a
// command that loads state more than once warns once.
var (
	warnedMu sync.Mutex
	warned   = map[string]bool{}
)

func warnCollisions(collisions []RepoIDCollision) {
	warnedMu.Lock()
	defer warnedMu.Unlock()
	for _, c := range collisions {
		id := c.From + "\x00" + c.To + "\x00" + strings.Join(c.Hubs, "\x00")
		if warned[id] {
			continue
		}
		warned[id] = true
		output.Warn("state: %s records %s, whose origin gives the repo ID %s, which is already recorded; both kept, run 'git hop doctor'",
			c.From, strings.Join(c.Hubs, ", "), c.To)
	}
}
