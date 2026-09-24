package hop

import (
	"path/filepath"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
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

// GetHopspacePath returns the data-home location for a repo's hopspace.
// It is where a --global clone creates the hopspace; to find the hopspace
// an existing hub uses, call ResolveHopspacePath.
func GetHopspacePath(dataHome, org, repo string) string {
	return filepath.Join(dataHome, org, repo)
}

// ResolveHopspacePath returns the hopspace a hub uses: the data-home
// hopspace when the hub is marked global (repo.mode, written by clone
// --global), else the hub itself, whose hop.json carries the hopspace
// fields. Only the marker decides; a data-home hop.json next to an
// unmarked hub is a stale copy (see StaleHopspaceCopy).
func ResolveHopspacePath(hubPath string, repo config.RepoConfig) string {
	if repo.Mode == config.RepoModeGlobal {
		return GetHopspacePath(GetGitHopDataHome(), repo.Org, repo.Repo)
	}
	return hubPath
}

// StaleHopspaceCopy returns the data-home hop.json path left beside an
// unmarked hub, or "" when there is none or the hub is marked global.
// Such a copy is never read; it is reported so it can be cleaned up.
func StaleHopspaceCopy(fs afero.Fs, repo config.RepoConfig) string {
	if repo.Mode == config.RepoModeGlobal {
		return ""
	}
	path := GetHopspacePath(GetGitHopDataHome(), repo.Org, repo.Repo)
	if exists, _ := afero.Exists(fs, filepath.Join(path, "hop.json")); exists {
		return path
	}
	return ""
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

// RenameBranch rekeys oldBranch's entry to newBranch at newPath; the rest
// of the entry carries over.
func (h *Hopspace) RenameBranch(oldBranch, newBranch, newPath string) error {
	entry, exists := h.Config.Branches[oldBranch]
	if !exists {
		// Not in hopspace — silently skip (same pattern as UnregisterBranch)
		return nil
	}
	entry.Path = newPath
	delete(h.Config.Branches, oldBranch)
	h.Config.Branches[newBranch] = entry
	return h.save(config.RenamedBranch(oldBranch, newBranch))
}

// Save persists the hopspace config
func (h *Hopspace) Save() error {
	return h.save()
}

func (h *Hopspace) save(opts ...config.WriteOption) error {
	return config.NewWriter(h.fs).WriteHopspaceConfig(h.Path, h.Config, opts...)
}
