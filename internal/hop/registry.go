package hop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/spf13/afero"
)

// Registry manages the global hops registry
type Registry struct {
	Config *config.HopsConfig
	fs     afero.Fs
}

// LoadRegistry loads or creates the global hops registry
func LoadRegistry(fs afero.Fs) *Registry {
	path := GetHopsRegistryPath()
	cfg := &config.HopsConfig{Hops: make(map[string]config.HopEntry)}

	if content, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(content, cfg); err != nil {
			fmt.Printf("Warning: failed to parse hops registry: %v\n", err)
		}
	}

	return &Registry{
		Config: cfg,
		fs:     fs,
	}
}

// Save persists the registry to disk
func (r *Registry) Save() error {
	path := GetHopsRegistryPath()
	dir := filepath.Dir(path)

	if err := r.fs.MkdirAll(dir, 0755); err != nil {
		return err
	}

	content, err := json.MarshalIndent(r.Config, "", "  ")
	if err != nil {
		return err
	}

	return afero.WriteFile(r.fs, path, content, 0644)
}

// AddHop adds a new hop to the registry
func (r *Registry) AddHop(repo, branch, path string) error {
	key := repo + ":" + branch

	r.Config.Hops[key] = config.HopEntry{
		Repo:         repo,
		Branch:       branch,
		Path:         path,
		AddedAt:      time.Now(),
		LastSeen:     time.Now(),
		EnvState:     "none",
		HasDockerEnv: false,
	}

	return r.Save()
}

// RemoveHop removes a hop from the registry
func (r *Registry) RemoveHop(repo, branch string) error {
	key := repo + ":" + branch
	delete(r.Config.Hops, key)
	return r.Save()
}

// FindByPath finds a hop by its absolute path
func (r *Registry) FindByPath(path string) (*config.HopEntry, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	for _, hop := range r.Config.Hops {
		if hop.Path == absPath {
			return &hop, nil
		}
	}

	return nil, fmt.Errorf("no hop found at path: %s", path)
}

// FindByRepo finds all hops for a given repo
func (r *Registry) FindByRepo(repo string) []*config.HopEntry {
	var hops []*config.HopEntry

	for _, hop := range r.Config.Hops {
		if hop.Repo == repo {
			hops = append(hops, &hop)
		}
	}

	return hops
}

// FindByBranch finds hops for a specific branch across all repos
func (r *Registry) FindByBranch(repo, branch string) []*config.HopEntry {
	var hops []*config.HopEntry

	for _, hop := range r.Config.Hops {
		if (repo == "" || hop.Repo == repo) && hop.Branch == branch {
			hops = append(hops, &hop)
		}
	}

	return hops
}

// FindAll returns all hops in the registry
func (r *Registry) FindAll() []*config.HopEntry {
	var hops []*config.HopEntry

	for _, hop := range r.Config.Hops {
		hops = append(hops, &hop)
	}

	return hops
}

// UpdateEnvState updates the environment state for a hop
func (r *Registry) UpdateEnvState(repo, branch, state string) error {
	key := repo + ":" + branch

	if hop, ok := r.Config.Hops[key]; ok {
		hop.EnvState = state
		r.Config.Hops[key] = hop
		return r.Save()
	}

	return fmt.Errorf("hop not found: %s:%s", repo, branch)
}

// UpdateLastSeen updates the last seen timestamp for a hop
func (r *Registry) UpdateLastSeen(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	for key, hop := range r.Config.Hops {
		if hop.Path == absPath {
			hop.LastSeen = time.Now()
			r.Config.Hops[key] = hop
			return r.Save()
		}
	}

	return fmt.Errorf("hop not found at path: %s", path)
}

// UpdateDockerEnv updates the docker environment detection flag
func (r *Registry) UpdateDockerEnv(repo, branch string, hasEnv bool) error {
	key := repo + ":" + branch

	if hop, ok := r.Config.Hops[key]; ok {
		hop.HasDockerEnv = hasEnv
		r.Config.Hops[key] = hop
		return r.Save()
	}

	return fmt.Errorf("hop not found: %s:%s", repo, branch)
}

// ListForRepo returns hops for a specific repo, sorted by last seen
func (r *Registry) ListForRepo(repo string) []*config.HopEntry {
	hops := r.FindByRepo(repo)
	sortByLastSeen(hops)
	return hops
}

// ListAll returns all hops, sorted by last seen
func (r *Registry) ListAll() []*config.HopEntry {
	hops := r.FindAll()
	sortByLastSeen(hops)
	return hops
}

// SortByCurrentRepo sorts hops with current repo's branches first
func (r *Registry) SortByCurrentRepo(currentRepo string, hops []*config.HopEntry) []*config.HopEntry {
	sort.SliceStable(hops, func(i, j int) bool {
		// Current repo's branches first
		if hops[i].Repo == currentRepo && hops[j].Repo != currentRepo {
			return true
		}
		if hops[i].Repo != currentRepo && hops[j].Repo == currentRepo {
			return false
		}

		// Within same repo, sort by last seen (most recent first)
		return hops[i].LastSeen.After(hops[j].LastSeen)
	})

	return hops
}

// sortByLastSeen sorts hops by last seen timestamp (most recent first)
func sortByLastSeen(hops []*config.HopEntry) {
	sort.Slice(hops, func(i, j int) bool {
		return hops[i].LastSeen.After(hops[j].LastSeen)
	})
}
