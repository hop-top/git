package hop

import (
	"path/filepath"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/spf13/afero"
)

// Hopspace represents a git-hop hopspace
type Hopspace struct {
	Path   string
	Config *config.HopspaceConfig
	fs     afero.Fs
}

// LoadHopspace loads a hopspace from the given path
func LoadHopspace(fs afero.Fs, path string) (*Hopspace, error) {
	loader := config.NewLoader(fs)
	cfg, err := loader.LoadHopspaceConfig(path)
	if err != nil {
		return nil, err
	}
	return &Hopspace{
		Path:   path,
		Config: cfg,
		fs:     fs,
	}, nil
}

// GetHopspacePath calculates the hopspace path based on data home and repo identity
func GetHopspacePath(dataHome, org, repo string) string {
	return filepath.Join(dataHome, org, repo)
}

// InitHopspace initializes a new hopspace
func InitHopspace(fs afero.Fs, path, repoURI, org, repo, defaultBranch string) (*Hopspace, error) {
	if err := fs.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	// Check if already exists
	if exists, _ := afero.Exists(fs, filepath.Join(path, "hop.json")); exists {
		return LoadHopspace(fs, path)
	}

	cfg := &config.HopspaceConfig{
		Repo: config.RepoConfig{
			URI:           repoURI,
			Org:           org,
			Repo:          repo,
			DefaultBranch: defaultBranch,
		},
		Branches: make(map[string]config.HopspaceBranch),
		Forks:    make(map[string]config.HopspaceFork),
	}

	writer := config.NewWriter(fs)
	if err := writer.WriteHopspaceConfig(path, cfg); err != nil {
		return nil, err
	}

	return &Hopspace{
		Path:   path,
		Config: cfg,
		fs:     fs,
	}, nil
}

// RegisterBranch adds a branch to the hopspace config
func (h *Hopspace) RegisterBranch(branch, worktreePath string) error {
	h.Config.Branches[branch] = config.HopspaceBranch{
		Exists:   true,
		Path:     worktreePath,
		LastSync: time.Now(),
	}
	return h.Save()
}

// UnregisterBranch removes a branch from the hopspace config
func (h *Hopspace) UnregisterBranch(branch string) error {
	if _, exists := h.Config.Branches[branch]; !exists {
		// Branch doesn't exist in hopspace - this is not an error since it may have
		// already been cleaned up or only existed in the hub config
		return nil
	}
	delete(h.Config.Branches, branch)
	return h.Save()
}

// Save persists the hopspace config
func (h *Hopspace) Save() error {
	writer := config.NewWriter(h.fs)
	return writer.WriteHopspaceConfig(h.Path, h.Config)
}
