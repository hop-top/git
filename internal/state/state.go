package state

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
	"hop.top/kit/go/core/xdg"

	"hop.top/git/internal/config"
)

// Version is the state.json format this release writes. 2.x keys a
// repository's worktrees by path (see RepositoryState.Worktrees); 1.x
// keyed them by branch.
const Version = "2.0.0"

// State represents the git-hop state tracking repositories and their locations
type State struct {
	Version      string                      `json:"version"`
	LastUpdated  time.Time                   `json:"lastUpdated"`
	Repositories map[string]*RepositoryState `json:"repositories"`
	Orphaned     []*OrphanedEntry            `json:"orphaned"`
}

// RepositoryState represents the state of a single repository.
//
// Worktrees is keyed by the worktree's path (WorktreeKey), so every hub
// of the repository can record its own worktree of a branch. Use the
// methods in worktrees.go to find, add and remove entries.
type RepositoryState struct {
	URI            string                    `json:"uri"`
	Org            string                    `json:"org"`
	Repo           string                    `json:"repo"`
	DefaultBranch  string                    `json:"defaultBranch"`
	Worktrees      map[string]*WorktreeState `json:"worktrees"`
	Hubs           []*HubState               `json:"hubs"`
	GlobalHopspace *GlobalHopspaceState      `json:"globalHopspace"`
}

// WorktreeState represents the state of a single worktree
type WorktreeState struct {
	Path string `json:"path"`
	// Branch checked out in the worktree. Empty only in an entry written
	// by a release that keyed worktrees by branch (LoadState migrates it).
	Branch       string    `json:"branch"`
	Type         string    `json:"type"` // "bare", "main" or "linked"
	HubPath      string    `json:"hubPath"`
	CreatedAt    time.Time `json:"createdAt"`
	LastAccessed time.Time `json:"lastAccessed"`
}

// Hub modes: where a hub keeps its hopspace. HubModeGlobal is the value
// of the hub's repo.mode marker (config.RepoModeGlobal).
const (
	HubModeLocal  = "local"               // in the hub's own hop.json (the default)
	HubModeGlobal = config.RepoModeGlobal // in $GIT_HOP_DATA_HOME/<org>/<repo> (clone --global)
)

// HubState represents a hub location for a repository
type HubState struct {
	Path         string    `json:"path"`
	Mode         string    `json:"mode"` // HubModeLocal or HubModeGlobal
	CreatedAt    time.Time `json:"createdAt"`
	LastAccessed time.Time `json:"lastAccessed"`
}

// GlobalHopspaceState represents global hopspace configuration
type GlobalHopspaceState struct {
	Enabled bool    `json:"enabled"`
	Path    *string `json:"path"`
}

// OrphanedEntry represents a detected orphaned artifact
type OrphanedEntry struct {
	Path       string    `json:"path"`
	DetectedAt time.Time `json:"detectedAt"`
	Reason     string    `json:"reason"`
}

func GetStateHome() string {
	dir, err := xdg.StateDir("git-hop")
	if err != nil {
		return filepath.Join(".local", "state", "git-hop")
	}
	return dir
}

// statePath is where state.json lives.
func statePath() string {
	return filepath.Join(GetStateHome(), "state.json")
}

// LoadState loads the state from disk or returns a new empty state.
//
// A file an earlier release wrote is migrated in memory: every caller
// sees worktrees keyed by path (migrate) and repositories keyed by the
// host of their origin (rekeyRepoIDs). Nothing is written here; the next
// SaveState persists the migration, backing the old file up first.
func LoadState(fs afero.Fs) (*State, error) {
	path := statePath()

	exists, err := afero.Exists(fs, path)
	if err != nil {
		return nil, fmt.Errorf("failed to check state file: %w", err)
	}

	if !exists {
		return NewState(), nil
	}

	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	state, err := parseState(data)
	if err != nil {
		return nil, err
	}
	if !newerThanSupported(state.Version) {
		migrate(state)
		_, collisions := rekeyRepoIDs(fs, state)
		warnCollisions(collisions)
		state.Version = Version
	}
	return state, nil
}

// parseState decodes a state file without migrating it.
func parseState(data []byte) (*State, error) {
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}
	if state.Repositories == nil {
		state.Repositories = make(map[string]*RepositoryState)
	}
	return &state, nil
}

// SaveState saves the state to disk atomically.
//
// It refuses to replace a state file it cannot parse, or one a newer
// release wrote: saving over either would lose what it records. Before
// replacing a file that still needs a migration LoadState does (entries
// in the branch-keyed format, repositories under a github.com key their
// origin does not give), it copies the file to a backup
// (backupLegacyState).
func SaveState(fs afero.Fs, state *State) error {
	stateDir := GetStateHome()
	path := statePath()
	tmpPath := filepath.Join(stateDir, "state.json.tmp")

	if err := guardExisting(fs, path); err != nil {
		return err
	}

	if err := fs.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// A state built in memory in the old shape is written in the new one.
	migrate(state)
	state.Version = Version
	state.LastUpdated = time.Now()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := afero.WriteFile(fs, tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	if err := fs.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to save state file: %w", err)
	}

	return nil
}

// guardExisting checks the state file SaveState is about to replace: it
// must parse and must not come from a newer release. When it still holds
// branch-keyed entries, or repositories keyed by another host than their
// origin's, it is backed up first.
func guardExisting(fs afero.Fs, path string) error {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		if exists, _ := afero.Exists(fs, path); !exists {
			return nil
		}
		return fmt.Errorf("state not saved: cannot read %s: %w", path, err)
	}
	onDisk, err := parseState(data)
	if err != nil {
		return fmt.Errorf("state not saved: %s cannot be parsed, and saving would replace it: %w", path, err)
	}
	if newerThanSupported(onDisk.Version) {
		return fmt.Errorf("state not saved: %s was written by a newer git-hop (format %s)", path, onDisk.Version)
	}
	if hasLegacyEntries(onDisk) || needsRekey(fs, onDisk) {
		if _, err := backupLegacyState(fs, data, time.Now()); err != nil {
			return fmt.Errorf("state not saved: back up %s: %w", path, err)
		}
	}
	return nil
}

// NewState creates a new empty state
func NewState() *State {
	return &State{
		Version:      Version,
		LastUpdated:  time.Now(),
		Repositories: make(map[string]*RepositoryState),
		Orphaned:     make([]*OrphanedEntry, 0),
	}
}

// AddRepository adds a repository to the state
func (s *State) AddRepository(repoID string, repo *RepositoryState) {
	s.Repositories[repoID] = repo
	s.LastUpdated = time.Now()
}

// UpdateLastAccessed updates the last accessed timestamp for the hub's
// worktree of branch and for the hub.
func (s *State) UpdateLastAccessed(repoID, branch, hubPath string) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	if worktree, ok := repo.Worktree(hubPath, branch); ok {
		worktree.LastAccessed = time.Now()
	}

	for _, hub := range repo.Hubs {
		if SamePath(hub.Path, hubPath) {
			hub.LastAccessed = time.Now()
			break
		}
	}

	s.LastUpdated = time.Now()

	return nil
}

// AddHub adds a hub to a repository if it doesn't already exist
func (s *State) AddHub(repoID string, hub *HubState) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	if repo.Hubs == nil {
		repo.Hubs = make([]*HubState, 0)
	}

	for _, existingHub := range repo.Hubs {
		if existingHub != nil && SamePath(existingHub.Path, hub.Path) {
			return nil
		}
	}

	repo.Hubs = append(repo.Hubs, hub)
	s.LastUpdated = time.Now()

	return nil
}

// RemoveRepository removes a repository and all its worktrees from the state
func (s *State) RemoveRepository(repoID string) error {
	if _, exists := s.Repositories[repoID]; !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	delete(s.Repositories, repoID)
	s.LastUpdated = time.Now()

	return nil
}

// AddOrphanedEntry adds an orphaned entry to the state
func (s *State) AddOrphanedEntry(entry *OrphanedEntry) {
	if s.Orphaned == nil {
		s.Orphaned = make([]*OrphanedEntry, 0)
	}

	s.Orphaned = append(s.Orphaned, entry)
	s.LastUpdated = time.Now()
}
