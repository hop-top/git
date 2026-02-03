package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/afero"
)

// State represents the git-hop state tracking repositories and their locations
type State struct {
	Version      string                      `json:"version"`
	LastUpdated  time.Time                   `json:"lastUpdated"`
	Repositories map[string]*RepositoryState `json:"repositories"`
	Orphaned     []*OrphanedEntry            `json:"orphaned"`
}

// RepositoryState represents the state of a single repository
type RepositoryState struct {
	URI            string                   `json:"uri"`
	Org            string                   `json:"org"`
	Repo           string                   `json:"repo"`
	DefaultBranch  string                   `json:"defaultBranch"`
	Worktrees      map[string]*WorktreeState `json:"worktrees"`
	Hubs           []*HubState              `json:"hubs"`
	GlobalHopspace *GlobalHopspaceState     `json:"globalHopspace"`
}

// WorktreeState represents the state of a single worktree
type WorktreeState struct {
	Path         string    `json:"path"`
	Type         string    `json:"type"` // "bare" or "linked"
	HubPath      string    `json:"hubPath"`
	CreatedAt    time.Time `json:"createdAt"`
	LastAccessed time.Time `json:"lastAccessed"`
}

// HubState represents a hub location for a repository
type HubState struct {
	Path         string    `json:"path"`
	Mode         string    `json:"mode"` // "local" or "global"
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

// GetStateHome returns the XDG state home directory for git-hop
func GetStateHome() string {
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return filepath.Join(env, "git-hop")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	if home == "" {
		return filepath.Join(".local", "state", "git-hop")
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/git-hop/state
		return filepath.Join(home, "Library", "Application Support", "git-hop", "state")
	default:
		// Linux/Unix: ~/.local/state/git-hop
		return filepath.Join(home, ".local", "state", "git-hop")
	}
}

// LoadState loads the state from disk or returns a new empty state
func LoadState(fs afero.Fs) (*State, error) {
	statePath := filepath.Join(GetStateHome(), "state.json")

	// Return new empty state if file doesn't exist
	exists, err := afero.Exists(fs, statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to check state file: %w", err)
	}

	if !exists {
		return NewState(), nil
	}

	data, err := afero.ReadFile(fs, statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// SaveState saves the state to disk atomically
func SaveState(fs afero.Fs, state *State) error {
	stateDir := GetStateHome()
	statePath := filepath.Join(stateDir, "state.json")
	tmpPath := filepath.Join(stateDir, "state.json.tmp")

	// Ensure directory exists
	if err := fs.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Update timestamp
	state.LastUpdated = time.Now()

	// Write to temp file
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := afero.WriteFile(fs, tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	// Atomic rename
	if err := fs.Rename(tmpPath, statePath); err != nil {
		return fmt.Errorf("failed to save state file: %w", err)
	}

	return nil
}

// NewState creates a new empty state
func NewState() *State {
	return &State{
		Version:      "1.0.0",
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

// AddWorktree adds a worktree to a repository
func (s *State) AddWorktree(repoID, branch string, worktree *WorktreeState) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	if repo.Worktrees == nil {
		repo.Worktrees = make(map[string]*WorktreeState)
	}

	repo.Worktrees[branch] = worktree
	s.LastUpdated = time.Now()

	return nil
}

// RemoveWorktree removes a worktree from a repository
func (s *State) RemoveWorktree(repoID, branch string) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	delete(repo.Worktrees, branch)
	s.LastUpdated = time.Now()

	return nil
}

// UpdateLastAccessed updates the last accessed timestamp for a worktree and hub
func (s *State) UpdateLastAccessed(repoID, branch, hubPath string) error {
	repo, exists := s.Repositories[repoID]
	if !exists {
		return fmt.Errorf("repository not found: %s", repoID)
	}

	// Update worktree last accessed
	if worktree, exists := repo.Worktrees[branch]; exists {
		worktree.LastAccessed = time.Now()
	}

	// Update hub last accessed
	for _, hub := range repo.Hubs {
		if hub.Path == hubPath {
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

	// Check if hub already exists
	for _, existingHub := range repo.Hubs {
		if existingHub.Path == hub.Path {
			return nil // Already exists
		}
	}

	repo.Hubs = append(repo.Hubs, hub)
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
