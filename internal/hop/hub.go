package hop

import (
	"fmt"
	"path/filepath"

	"hop.top/git/internal/config"
	"github.com/spf13/afero"
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

// RemoveBranch removes a branch from the hub
func (h *Hub) RemoveBranch(branchName string) error {
	// Update config - no symlinks to remove
	delete(h.Config.Branches, branchName)

	return h.Save()
}

// RenameBranch updates the hub config to reflect a branch rename.
// The old key is removed and a new key is added with the updated path.
func (h *Hub) RenameBranch(oldBranch, newBranch, newPath string) error {
	old, exists := h.Config.Branches[oldBranch]
	if !exists {
		return fmt.Errorf("branch %s not found in hub", oldBranch)
	}
	delete(h.Config.Branches, oldBranch)
	h.Config.Branches[newBranch] = config.HubBranch{
		Path:           newPath,
		HopspaceBranch: old.HopspaceBranch,
	}
	return h.Save()
}

// Save persists the hub config
func (h *Hub) Save() error {
	writer := config.NewWriter(h.fs)
	return writer.WriteHubConfig(h.Path, h.Config)
}
