package services

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// unknownHubCreated orders a hub state has no record of after every hub
// it has: of two claims to a port, the recorded hub's comes first.
var unknownHubCreated = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// EnvClaim is one entry of a ports.json: the ports a worktree holds.
type EnvClaim struct {
	Hopspace   string // resolved (state.ResolvePath)
	Key        string
	Hub        string // the hub the worktree belongs to; "" when unknown
	HubCreated time.Time
	Branch     string
	// WorktreePath is the worktree's path, "" when not known.
	WorktreePath string
	Org, Repo    string
	Entry        config.BranchPorts
	// Volumes are the volume directories volumes.json records under the
	// same key, by volume name.
	Volumes map[string]string
}

// before orders claims to one port: the claim of the hub created first
// wins, then the lower hub path, then the lower key.
func (c EnvClaim) before(o EnvClaim) bool {
	if !c.HubCreated.Equal(o.HubCreated) {
		return c.HubCreated.Before(o.HubCreated)
	}
	if c.Hub != o.Hub {
		return c.Hub < o.Hub
	}
	return c.Key < o.Key
}

func (c EnvClaim) same(o EnvClaim) bool { return c.Hopspace == o.Hopspace && c.Key == o.Key }

// PortConflict is a port a worktree holds that an earlier claim holds too.
type PortConflict struct {
	Port    int
	Service string
	Other   EnvClaim
}

// knownHub is a hub whose hopspace's allocations count.
type knownHub struct {
	Path      string
	Created   time.Time
	Hopspace  string // resolved
	Org, Repo string
	Branches  map[string]string // branch -> worktree path
}

// EnvRecords is every port allocation recorded in the hopspace of a hub
// git-hop knows of: every hub in state, plus the one a command runs in.
// Allocating against it is what keeps two hubs, or two repositories, off
// each other's ports.
type EnvRecords struct {
	Claims []EnvClaim
	Hubs   []knownHub
}

// LoadEnvRecords reads the allocations of every hub state records and of
// the hub at currentHub ("" for none). An unreadable state is returned as
// the error alongside the records of currentHub alone, so a caller can
// warn and carry on with what it can see.
func LoadEnvRecords(fs afero.Fs, currentHub string) (*EnvRecords, error) {
	r := &EnvRecords{}
	seen := map[string]bool{}
	add := func(path string, created time.Time, mode, org, repo, uri string) {
		key := state.ResolvePath(path)
		if path == "" || seen[key] {
			return
		}
		seen[key] = true
		h := knownHub{Path: path, Created: created, Org: org, Repo: repo, Branches: map[string]string{}}
		hopspace := path
		if hub, err := hop.LoadHub(fs, path); err == nil {
			hopspace = hop.ResolveHopspacePath(path, hub.Config.Repo)
			h.Org, h.Repo = hub.Config.Repo.Org, hub.Config.Repo.Repo
			for branch := range hub.Config.Branches {
				h.Branches[branch] = config.ResolveWorktreePath(hub.Config.Branches[branch].Path, path)
			}
		} else if mode == state.HubModeGlobal {
			hopspace = hop.GetHopspacePath(hop.GetGitHopDataHome(), hop.NewRepoRef(uri, org, repo).In(path))
		}
		h.Hopspace = state.ResolvePath(hopspace)
		r.Hubs = append(r.Hubs, h)
	}

	st, err := state.LoadState(fs)
	if err == nil {
		for _, repo := range st.Repositories {
			for _, h := range repo.Hubs {
				if h != nil {
					add(h.Path, h.CreatedAt, h.Mode, repo.Org, repo.Repo, repo.URI)
				}
			}
		}
	}
	add(currentHub, unknownHubCreated, "", "", "", "")
	sort.SliceStable(r.Hubs, func(i, j int) bool {
		a, b := r.Hubs[i], r.Hubs[j]
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		return a.Path < b.Path
	})

	loader := config.NewLoader(fs)
	read := map[string]bool{}
	for _, h := range r.Hubs {
		if read[h.Hopspace] {
			continue
		}
		read[h.Hopspace] = true
		cfg, lerr := loader.LoadPortsConfig(h.Hopspace)
		if lerr != nil {
			continue
		}
		vols, _ := loader.LoadVolumesConfig(h.Hopspace)
		keys := make([]string, 0, len(cfg.Branches))
		for k := range cfg.Branches {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c := r.claim(h.Hopspace, k, cfg.Branches[k])
			if vols != nil {
				c.Volumes = vols.Branches[k].Volumes
			}
			r.Claims = append(r.Claims, c)
		}
	}
	return r, err
}

// claim resolves whose entry key is in hopspace. An entry names its hub;
// one an earlier release wrote does not, and belongs to the hopspace's
// hub when that is a hub's own hopspace, else to the first-created hub of
// the hopspace with a worktree of the branch.
func (r *EnvRecords) claim(hopspace, key string, entry config.BranchPorts) EnvClaim {
	c := EnvClaim{Hopspace: hopspace, Key: key, Entry: entry, Branch: entry.Branch, Hub: entry.Hub, HubCreated: unknownHubCreated}
	if c.Branch == "" && !filepath.IsAbs(key) {
		c.Branch = key
	}
	var owner *knownHub
	for i := range r.Hubs {
		h := &r.Hubs[i]
		if h.Hopspace != hopspace {
			continue
		}
		switch {
		case c.Hub != "":
			if state.SamePath(h.Path, c.Hub) {
				owner = h
			}
		case state.SamePath(h.Path, hopspace):
			owner = h
		case owner == nil:
			if _, ok := h.Branches[c.Branch]; ok {
				owner = h
			}
		}
	}
	if owner == nil && c.Hub == "" {
		for i := range r.Hubs {
			if r.Hubs[i].Hopspace == hopspace {
				owner = &r.Hubs[i]
				break
			}
		}
	}
	if owner != nil {
		c.Hub, c.HubCreated, c.Org, c.Repo = owner.Path, owner.Created, owner.Org, owner.Repo
	}
	switch {
	case entry.Worktree != "":
		c.WorktreePath = entry.Worktree
	case filepath.IsAbs(key):
		c.WorktreePath = key
	case owner != nil:
		c.WorktreePath = owner.Branches[c.Branch]
	}
	return c
}

// Self is the claim of the worktree at worktreePath on branch, in the hub
// at hubPath, whose hopspace is hopspacePath: its recorded entry when it
// has one (Found), else an empty claim under the key it will be recorded
// under.
func (r *EnvRecords) Self(hopspacePath, hubPath, worktreePath, branch string) (EnvClaim, bool) {
	hopspace := state.ResolvePath(hopspacePath)
	key := hop.HopspaceKey(hopspacePath, hubPath, worktreePath, branch)
	for _, c := range r.Claims {
		if c.Hopspace == hopspace && c.Key == key {
			return c, true
		}
	}
	if key != branch {
		// An entry an earlier release wrote in a shared hopspace, keyed by
		// branch: this worktree's if its hub is the one it belongs to.
		for _, c := range r.Claims {
			if c.Hopspace == hopspace && c.Key == branch && c.Entry.Hub == "" && state.SamePath(c.Hub, hubPath) {
				return c, true
			}
		}
	}
	self := EnvClaim{Hopspace: hopspace, Key: key, Hub: hubPath, HubCreated: unknownHubCreated, Branch: branch}
	for _, h := range r.Hubs {
		if state.SamePath(h.Path, hubPath) {
			self.HubCreated = h.Created
		}
	}
	return self, false
}

// Conflicts returns the ports of self that a claim ordered before it
// holds too (EnvClaim.before), by port.
func (r *EnvRecords) Conflicts(self EnvClaim) []PortConflict {
	var out []PortConflict
	for _, svc := range sortedServices(self.Entry.Ports) {
		port := self.Entry.Ports[svc]
		for _, c := range r.Claims {
			if c.same(self) || !c.before(self) {
				continue
			}
			if holdsPort(c.Entry.Ports, port) {
				out = append(out, PortConflict{Port: port, Service: svc, Other: c})
				break
			}
		}
	}
	return out
}

// Reserved returns every port held by a claim other than self.
func (r *EnvRecords) Reserved(self EnvClaim) map[int]bool {
	used := map[int]bool{}
	for _, c := range r.Claims {
		if c.same(self) {
			continue
		}
		for _, p := range c.Entry.Ports {
			used[p] = true
		}
	}
	return used
}

// LookupEnvEntry returns the entry of cfg recording the worktree at
// worktreePath on branch, in the hub at hubPath: under its key
// (hop.HopspaceKey), else under the branch, as an earlier release keyed it.
func LookupEnvEntry(cfg *config.PortsConfig, hopspacePath, hubPath, worktreePath, branch string) (config.BranchPorts, bool) {
	if cfg == nil {
		return config.BranchPorts{}, false
	}
	if e, ok := cfg.Branches[hop.HopspaceKey(hopspacePath, hubPath, worktreePath, branch)]; ok {
		return e, true
	}
	e, ok := cfg.Branches[branch]
	if ok && e.Hub != "" && hubPath != "" && !state.SamePath(e.Hub, hubPath) {
		return config.BranchPorts{}, false
	}
	return e, ok
}

func holdsPort(ports map[string]int, port int) bool {
	for _, p := range ports {
		if p == port {
			return true
		}
	}
	return false
}

func sortedServices(ports map[string]int) []string {
	out := make([]string, 0, len(ports))
	for s := range ports {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// RekeyEnvEntry moves the ports.json and volumes.json entries of the
// worktree at oldPath on oldBranch, in the hub at hubPath, to newPath on
// newBranch, for a worktree move. A missing file or entry is left alone.
func RekeyEnvEntry(fs afero.Fs, hopspacePath, hubPath, oldPath, newPath, oldBranch, newBranch string) error {
	oldKey := hop.HopspaceKey(hopspacePath, hubPath, oldPath, oldBranch)
	newKey := hop.HopspaceKey(hopspacePath, hubPath, newPath, newBranch)
	loader, writer := config.NewLoader(fs), config.NewWriter(fs)
	if cfg, err := loader.LoadPortsConfig(hopspacePath); err == nil {
		if entry, ok := cfg.Branches[oldKey]; ok {
			delete(cfg.Branches, oldKey)
			if entry.Branch != "" {
				entry.Branch = newBranch
			}
			if entry.Worktree != "" {
				entry.Worktree = state.WorktreeKey(newPath)
			}
			cfg.Branches[newKey] = entry
			if err := writer.WritePortsConfig(hopspacePath, cfg); err != nil {
				return err
			}
		}
	}
	if cfg, err := loader.LoadVolumesConfig(hopspacePath); err == nil {
		if entry, ok := cfg.Branches[oldKey]; ok {
			delete(cfg.Branches, oldKey)
			cfg.Branches[newKey] = entry
			return writer.WriteVolumesConfig(hopspacePath, cfg)
		}
	}
	return nil
}

// PortCollision is a claim holding a port an earlier claim holds too.
type PortCollision struct {
	Claim EnvClaim
	PortConflict
}

// Collisions returns every port two claims hold, reported against the
// later claim (EnvClaim.before), the one `env generate` re-ports.
func (r *EnvRecords) Collisions() []PortCollision {
	var out []PortCollision
	for _, c := range r.Claims {
		for _, conflict := range r.Conflicts(c) {
			out = append(out, PortCollision{Claim: c, PortConflict: conflict})
		}
	}
	return out
}

// Worktree returns the claim's worktree path, or its key when that is
// not known.
func (c EnvClaim) Worktree() string {
	if c.WorktreePath != "" {
		return c.WorktreePath
	}
	return c.Key
}

// DropEnvEntry removes the ports.json and volumes.json entries of the
// worktree at worktreePath on branch, in the hub at hubPath, so its ports
// are free for others. An entry an earlier release wrote in a shared
// hopspace goes only if it is this hub's (EnvRecords.Self). A missing
// file or entry is left alone.
func DropEnvEntry(fs afero.Fs, hopspacePath, hubPath, worktreePath, branch string) error {
	recs, _ := LoadEnvRecords(fs, hubPath)
	self, found := recs.Self(hopspacePath, hubPath, worktreePath, branch)
	if !found {
		return nil
	}
	_, err := dropEnvEntries(fs, hopspacePath, func(key string, _ config.BranchPorts) bool { return key == self.Key })
	return err
}

// VolumeConflict is a volume directory a worktree records that an
// earlier claim records too.
type VolumeConflict struct {
	Name, Path string
	Other      EnvClaim
}

// VolumeConflicts returns the directories of volumes, by name, that a
// claim ordered before self (EnvClaim.before) records too: earlier
// releases gave every worktree of a branch the same fallback directory.
func (r *EnvRecords) VolumeConflicts(self EnvClaim, volumes map[string]string) []VolumeConflict {
	var out []VolumeConflict
	for _, name := range sortedKeys(volumes) {
		path := volumes[name]
		for _, c := range r.Claims {
			if c.same(self) || !c.before(self) {
				continue
			}
			if holdsDir(c.Volumes, path) {
				out = append(out, VolumeConflict{Name: name, Path: path, Other: c})
				break
			}
		}
	}
	return out
}

func holdsDir(dirs map[string]string, dir string) bool {
	for _, d := range dirs {
		if state.SamePath(d, dir) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
