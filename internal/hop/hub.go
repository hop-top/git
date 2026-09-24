package hop

import (
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
	if IsHub(fs, path) {
		return nil, fmt.Errorf("hub already exists at %s", path)
	}

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
		Settings: config.HubSettings{
			EnvPatterns: []string{"dev", "staging", "qa"}, // Defaults
		},
	}

	writer := config.NewWriter(fs)
	if err := writer.WriteHubConfig(path, cfg); err != nil {
		return nil, err
	}

	// Create .gitignore to ignore symlinks if needed, though usually hubs are not git repos themselves
	// unless they are the root of a repo. But in git-hop, the hub IS the entry point.
	// If the user initializes a hub inside an existing repo, they might want to ignore it.
	// But typically a hub is a directory containing symlinks.

	return &Hub{
		Path:   path,
		Config: cfg,
		fs:     fs,
	}, nil
}

// AddBranch adds a branch to the hub config
func (h *Hub) AddBranch(branchName, hopspaceBranch, worktreePath string) error {
	// Update config - no symlinks needed, worktrees are accessed directly
	h.Config.Branches[branchName] = config.HubBranch{
		Path:           worktreePath, // Full path to worktree
		HopspaceBranch: hopspaceBranch,
	}

	return h.Save()
}

// SetBranchBase records the branch this worktree was forked from, for
// use as the comparison target in status/list. base="" clears the
// field (falls back to hub default). Returns an error if the branch is
// not registered in this hub. Persists immediately.
func (h *Hub) SetBranchBase(branchName, base string) error {
	b, ok := h.Config.Branches[branchName]
	if !ok {
		return fmt.Errorf("branch %q not in hub", branchName)
	}
	if base == "" {
		b.Base = nil
	} else {
		b.Base = &base
	}
	h.Config.Branches[branchName] = b
	return h.Save()
}

// SetBranchTask records the task a worktree was added for. task=""
// clears it. Returns an error if the branch is not registered in this
// hub. Persists immediately.
func (h *Hub) SetBranchTask(branchName, task string) error {
	b, ok := h.Config.Branches[branchName]
	if !ok {
		return fmt.Errorf("branch %q not in hub", branchName)
	}
	b.Task = task
	h.Config.Branches[branchName] = b
	return h.Save()
}

// RemoveBranch removes a branch from the hub
func (h *Hub) RemoveBranch(branchName string) error {
	// Update config - no symlinks to remove
	delete(h.Config.Branches, branchName)

	return h.Save()
}

// RenameBranch rekeys oldBranch's entry to newBranch at newPath. The rest
// of the entry (base, fork) carries over. HopspaceBranch follows the rename
// only when it named oldBranch itself; a fork entry's HopspaceBranch names
// the fork-side branch, which a hub rename does not touch.
func (h *Hub) RenameBranch(oldBranch, newBranch, newPath string) error {
	entry, exists := h.Config.Branches[oldBranch]
	if !exists {
		return fmt.Errorf("branch %s not found in hub", oldBranch)
	}
	entry.Path = newPath
	if entry.HopspaceBranch == oldBranch {
		entry.HopspaceBranch = newBranch
	}
	delete(h.Config.Branches, oldBranch)
	h.Config.Branches[newBranch] = entry
	return h.Save()
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

// Save persists the hub config
func (h *Hub) Save() error {
	writer := config.NewWriter(h.fs)
	return writer.WriteHubConfig(h.Path, h.Config)
}
