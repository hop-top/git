package hop

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
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
// writes the result under its lock; see Hub.Update. hubPath is the hub
// the change is for: in a hopspace it shares with other hubs, entries
// an earlier release keyed by branch are first moved to their worktree's
// path (migrateSharedEntries), so fn only sees path keys there.
func (h *Hopspace) Update(hubPath string, fn func(cfg *config.HopspaceConfig) error, opts ...config.WriteOption) error {
	return WithHopJSONLock(h.fs, h.Path, func() error {
		cfg, err := config.NewLoader(h.fs).LoadHopspaceConfig(h.Path)
		if err != nil {
			return err
		}
		if cfg.Branches == nil {
			cfg.Branches = make(map[string]config.HopspaceBranch)
		}
		if sharedHopspace(h.Path, hubPath) {
			if err := h.migrateLocked(cfg, hubPath); err != nil {
				return err
			}
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

// RegisterBranch records the worktree at worktreePath, on branch, of the
// hub at hubPath (HopspaceKey). The entry's other members carry over.
func (h *Hopspace) RegisterBranch(hubPath, branch, worktreePath string) error {
	return h.Update(hubPath, func(cfg *config.HopspaceConfig) error {
		key := HopspaceKey(h.Path, hubPath, worktreePath, branch)
		if old, _, ok := findEntry(cfg, h.Path, hubPath, branch, worktreePath); ok && old != key {
			// The same worktree under another spelling of its path.
			cfg.Branches[key] = cfg.Branches[old]
			delete(cfg.Branches, old)
		}
		entry := cfg.Branches[key]
		entry.Exists = true
		entry.Path = worktreePath
		entry.LastSync = time.Now()
		if sharedHopspace(h.Path, hubPath) {
			entry.Branch = branch
			entry.Hub = state.WorktreeKey(hubPath)
		}
		cfg.Branches[key] = entry
		return nil
	})
}

// UnregisterBranch removes the record of the worktree at worktreePath, on
// branch, of the hub at hubPath. Another hub's worktree of the same
// branch keeps its record.
func (h *Hopspace) UnregisterBranch(hubPath, branch, worktreePath string) error {
	return h.Update(hubPath, func(cfg *config.HopspaceConfig) error {
		key, _, ok := findEntry(cfg, h.Path, hubPath, branch, worktreePath)
		if !ok {
			// Not recorded: already cleaned up, or only ever in the hub.
			return errUnchanged
		}
		delete(cfg.Branches, key)
		return nil
	})
}

// RenameBranch moves the record of the hub's worktree of oldBranch at
// oldPath to newBranch at newPath; the rest of the entry carries over.
func (h *Hopspace) RenameBranch(hubPath, oldBranch, newBranch, oldPath, newPath string) error {
	renames := make(map[string]string)
	return h.Update(hubPath, func(cfg *config.HopspaceConfig) error {
		key, entry, ok := findEntry(cfg, h.Path, hubPath, oldBranch, oldPath)
		if !ok {
			// Not in hopspace: silently skip, as UnregisterBranch does.
			return errUnchanged
		}
		newKey := HopspaceKey(h.Path, hubPath, newPath, newBranch)
		entry.Path = newPath
		if entry.Branch != "" {
			entry.Branch = newBranch
		}
		delete(cfg.Branches, key)
		cfg.Branches[newKey] = entry
		renames[key] = newKey
		return nil
	}, config.RenamedBranches(renames))
}
