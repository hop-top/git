package hop

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
)

// Hub represents a git-hop hub
type Hub struct {
	Path   string
	Config *config.HubConfig
	fs     afero.Fs
}

// LoadHub loads a hub from the given path
func LoadHub(fs afero.Fs, path string) (*Hub, error) {
	loader := config.NewLoader(fs)
	cfg, err := loader.LoadHubConfig(path)
	if err != nil {
		return nil, err
	}
	return &Hub{
		Path:   path,
		Config: cfg,
		fs:     fs,
	}, nil
}

// WorktreePaths maps each branch in the hub to its worktree directory.
// hop.json records paths both absolute (`git hop add`) and relative to
// the hub (`git hop init`); both resolve through
// config.ResolveWorktreePath, so neither lands on <hub>/<absolute path>.
func (h *Hub) WorktreePaths() map[string]string {
	paths := make(map[string]string, len(h.Config.Branches))
	for name, b := range h.Config.Branches {
		paths[name] = config.ResolveWorktreePath(b.Path, h.Path)
	}
	return paths
}

// IsHub checks if a directory is a hub
func IsHub(fs afero.Fs, path string) bool {
	exists, _ := afero.Exists(fs, filepath.Join(path, "hop.json"))
	return exists
}

// FindHub searches up the directory tree from the given path to find a hub
// Returns the hub path if found, empty string if not found
func FindHub(fs afero.Fs, startPath string) (string, error) {
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return "", err
	}

	currentPath := absPath
	for {
		if IsHub(fs, currentPath) {
			return currentPath, nil
		}

		// Move up one directory
		parentPath := filepath.Dir(currentPath)

		// Check if we've reached the root
		if parentPath == currentPath {
			return "", fmt.Errorf("no hub found in directory tree from %s", startPath)
		}

		currentPath = parentPath
	}
}

// CreateHub initializes a new hub
func CreateHub(fs afero.Fs, path string, repoURI, org, repo, defaultBranch string) (*Hub, error) {
	if err := fs.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	cfg := &config.HubConfig{
		Repo: config.RepoConfig{
			URI:           repoURI,
			Org:           org,
			Repo:          repo,
			DefaultBranch: defaultBranch,
		},
		Branches: make(map[string]config.HubBranch),
	}

	err := WithHopJSONLock(fs, path, func() error {
		if IsHub(fs, path) {
			return fmt.Errorf("hub already exists at %s", path)
		}
		return config.NewWriter(fs).WriteHubConfig(path, cfg)
	})
	if err != nil {
		return nil, err
	}

	return &Hub{
		Path:   path,
		Config: cfg,
		fs:     fs,
	}, nil
}

// errUnchanged, returned by an Update fn, ends the update without
// writing hop.json.
var errUnchanged = errors.New("hop.json unchanged")

// Update applies fn to hop.json as it is on disk now and writes the
// result, all under the hub's hop.json lock (WithHopJSONLock), then makes
// it h.Config. fn never sees a copy loaded before another run's write, so
// it cannot undo that write. fn must only modify cfg: no git, no hooks,
// no other hop.json update.
func (h *Hub) Update(fn func(cfg *config.HubConfig) error, opts ...config.WriteOption) error {
	return WithHopJSONLock(h.fs, h.Path, func() error {
		cfg, err := config.NewLoader(h.fs).LoadHubConfig(h.Path)
		if err != nil {
			return err
		}
		if cfg.Branches == nil {
			cfg.Branches = make(map[string]config.HubBranch)
		}
		err = fn(cfg)
		if err == nil {
			err = config.NewWriter(h.fs).WriteHubConfig(h.Path, cfg, opts...)
		}
		if err != nil && !errors.Is(err, errUnchanged) {
			return err
		}
		h.setConfig(cfg)
		return nil
	})
}

// setConfig replaces h.Config's contents in place, so a caller holding
// the h.Config pointer sees the fresh copy too.
func (h *Hub) setConfig(cfg *config.HubConfig) {
	if h.Config == nil {
		h.Config = cfg
		return
	}
	*h.Config = *cfg
}

// AddBranch adds a branch to the hub config
func (h *Hub) AddBranch(branchName, hopspaceBranch, worktreePath string) error {
	return h.Update(func(cfg *config.HubConfig) error {
		// No symlinks needed, worktrees are accessed directly
		cfg.Branches[branchName] = config.HubBranch{
			Path:           worktreePath, // Full path to worktree
			HopspaceBranch: hopspaceBranch,
		}
		return nil
	})
}

// SetBranchBase records the branch this worktree was forked from, for
// use as the comparison target in status/list. base="" clears the
// field (falls back to hub default). Returns an error if the branch is
// not registered in this hub. Persists immediately.
func (h *Hub) SetBranchBase(branchName, base string) error {
	return h.Update(func(cfg *config.HubConfig) error {
		b, ok := cfg.Branches[branchName]
		if !ok {
			return fmt.Errorf("branch %q not in hub", branchName)
		}
		if base == "" {
			b.Base = nil
		} else {
			b.Base = &base
		}
		cfg.Branches[branchName] = b
		return nil
	})
}

// SetBranchTask records the task a worktree was added for. task=""
// clears it. Returns an error if the branch is not registered in this
// hub. Persists immediately.
func (h *Hub) SetBranchTask(branchName, task string) error {
	return h.Update(func(cfg *config.HubConfig) error {
		b, ok := cfg.Branches[branchName]
		if !ok {
			return fmt.Errorf("branch %q not in hub", branchName)
		}
		b.Task = task
		cfg.Branches[branchName] = b
		return nil
	})
}

// RemoveBranch removes a branch from the hub
func (h *Hub) RemoveBranch(branchName string) error {
	return h.Update(func(cfg *config.HubConfig) error {
		if _, ok := cfg.Branches[branchName]; !ok {
			return errUnchanged
		}
		delete(cfg.Branches, branchName)
		return nil
	})
}

// RenameBranch rekeys oldBranch's entry to newBranch at newPath. The rest
// of the entry (base, fork) carries over. HopspaceBranch follows the rename
// only when it named oldBranch itself; a fork entry's HopspaceBranch names
// the fork-side branch, which a hub rename does not touch.
func (h *Hub) RenameBranch(oldBranch, newBranch, newPath string) error {
	return h.Update(func(cfg *config.HubConfig) error {
		entry, exists := cfg.Branches[oldBranch]
		if !exists {
			return fmt.Errorf("branch %s not found in hub", oldBranch)
		}
		entry.Path = newPath
		if entry.HopspaceBranch == oldBranch {
			entry.HopspaceBranch = newBranch
		}
		delete(cfg.Branches, oldBranch)
		cfg.Branches[newBranch] = entry
		return nil
	}, config.RenamedBranch(oldBranch, newBranch))
}

// BranchPath resolves a registered branch's worktree to an absolute
// path, applying the same relative-to-hub-root rule the repair planner
// uses. Returns "" when the branch is not registered in this hub.
func (h *Hub) BranchPath(branchName string) string {
	b, ok := h.Config.Branches[branchName]
	if !ok {
		return ""
	}
	return absHubBranchPath(h.Path, b.Path)
}
