package hop

import (
	"errors"
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

// ResolveHopspacePath returns the hopspace a hub uses: the data-home
// hopspace when the hub is marked global (repo.mode, written by clone
// --global), else the hub itself, whose hop.json carries the hopspace
// fields. Only the marker decides; a data-home hop.json next to an
// unmarked hub is a stale copy (see StaleHopspaceCopy).
func ResolveHopspacePath(hubPath string, repo config.RepoConfig) string {
	if repo.Mode == config.RepoModeGlobal {
		return GetHopspacePath(GetGitHopDataHome(), RepoRefFor(hubPath, repo))
	}
	return hubPath
}

// StaleHopspaceCopy returns the data-home hop.json path left beside the
// unmarked hub at hubPath, or "" when there is none or the hub is marked
// global. Such a copy is never read; it is reported so it can be cleaned
// up.
func StaleHopspaceCopy(fs afero.Fs, hubPath string, repo config.RepoConfig) string {
	if repo.Mode == config.RepoModeGlobal {
		return ""
	}
	path := GetHopspacePath(GetGitHopDataHome(), RepoRefFor(hubPath, repo))
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

	existed := false
	err := WithHopJSONLock(fs, path, func() error {
		if exists, _ := afero.Exists(fs, filepath.Join(path, "hop.json")); exists {
			existed = true
			return nil
		}
		return config.NewWriter(fs).WriteHopspaceConfig(path, cfg)
	})
	if err != nil {
		return nil, err
	}
	if existed {
		return LoadHopspace(fs, path)
	}

	return &Hopspace{
		Path:   path,
		Config: cfg,
		fs:     fs,
	}, nil
}

// Update applies fn to the hopspace's hop.json as it is on disk now and
// writes the result under its lock; see Hub.Update.
func (h *Hopspace) Update(fn func(cfg *config.HopspaceConfig) error, opts ...config.WriteOption) error {
	return WithHopJSONLock(h.fs, h.Path, func() error {
		cfg, err := config.NewLoader(h.fs).LoadHopspaceConfig(h.Path)
		if err != nil {
			return err
		}
		if cfg.Branches == nil {
			cfg.Branches = make(map[string]config.HopspaceBranch)
		}
		err = fn(cfg)
		if err == nil {
			err = config.NewWriter(h.fs).WriteHopspaceConfig(h.Path, cfg, opts...)
		}
		if err != nil && !errors.Is(err, errUnchanged) {
			return err
		}
		if h.Config == nil {
			h.Config = cfg
		} else {
			*h.Config = *cfg
		}
		return nil
	})
}

// RegisterBranch adds a branch to the hopspace config
func (h *Hopspace) RegisterBranch(branch, worktreePath string) error {
	return h.Update(func(cfg *config.HopspaceConfig) error {
		cfg.Branches[branch] = config.HopspaceBranch{
			Exists:   true,
			Path:     worktreePath,
			LastSync: time.Now(),
		}
		return nil
	})
}

// UnregisterBranch removes a branch from the hopspace config
func (h *Hopspace) UnregisterBranch(branch string) error {
	return h.Update(func(cfg *config.HopspaceConfig) error {
		if _, exists := cfg.Branches[branch]; !exists {
			// Branch doesn't exist in hopspace - this is not an error since it may have
			// already been cleaned up or only existed in the hub config
			return errUnchanged
		}
		delete(cfg.Branches, branch)
		return nil
	})
}

// RenameBranch rekeys oldBranch's entry to newBranch at newPath; the rest
// of the entry carries over.
func (h *Hopspace) RenameBranch(oldBranch, newBranch, newPath string) error {
	return h.Update(func(cfg *config.HopspaceConfig) error {
		entry, exists := cfg.Branches[oldBranch]
		if !exists {
			// Not in hopspace — silently skip (same pattern as UnregisterBranch)
			return errUnchanged
		}
		entry.Path = newPath
		delete(cfg.Branches, oldBranch)
		cfg.Branches[newBranch] = entry
		return nil
	}, config.RenamedBranch(oldBranch, newBranch))
}
