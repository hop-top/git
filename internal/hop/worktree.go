package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"github.com/spf13/afero"
)

// WorktreeManager handles worktree operations
type WorktreeManager struct {
	git git.GitInterface
	fs  afero.Fs
}

// NewWorktreeManager creates a new manager
func NewWorktreeManager(fs afero.Fs, g git.GitInterface) *WorktreeManager {
	return &WorktreeManager{
		git: g,
		fs:  fs,
	}
}

// CreateWorktreeTransactional creates a git worktree with validation and auto-cleanup
func (m *WorktreeManager) CreateWorktreeTransactional(hopspace *Hopspace, hubPath string, branch string, locationPattern string, org string, repo string) (string, error) {
	// Validate inputs early (before path computation)
	if hubPath == "" {
		return "", fmt.Errorf("hubPath cannot be empty")
	}
	if branch == "" {
		return "", fmt.Errorf("branch cannot be empty")
	}

	// Step 1: Expand worktree location using ExpandWorktreeLocation
	dataHome := GetGitHopDataHome()
	ctx := WorktreeLocationContext{
		HubPath:  hubPath,
		Branch:   branch,
		Org:      org,
		Repo:     repo,
		DataHome: dataHome,
	}
	worktreePath := ExpandWorktreeLocation(locationPattern, ctx)

	// Clean the path to resolve .. and . segments
	worktreePath = filepath.Clean(worktreePath)

	// Step 2: Pre-flight validation
	validator := NewStateValidator(m.fs, m.git)
	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, branch, worktreePath)
	if err != nil {
		return worktreePath, fmt.Errorf("validation failed: %w", err)
	}

	// Step 3: Auto-cleanup orphaned directories if needed
	if !validation.CanProceed && validation.RequiresCleanup {
		cleanup := NewCleanupManager(m.fs, m.git)
		if err := cleanup.CleanupOrphanedDirectory(worktreePath); err != nil {
			return worktreePath, fmt.Errorf("failed to cleanup orphaned directory: %w", err)
		}
	}

	// Step 4: Call existing CreateWorktree method to do the actual work
	_, err = m.CreateWorktree(hopspace, hubPath, branch, locationPattern, org, repo)
	if err != nil {
		// Return our cleaned path on error
		return worktreePath, err
	}
	// Return our cleaned path on success
	return worktreePath, nil
}

// CreateWorktree creates a git worktree at the configured location
func (m *WorktreeManager) CreateWorktree(hopspace *Hopspace, hubPath string, branch string, locationPattern string, org string, repo string) (string, error) {
	// Validate inputs
	if hubPath == "" {
		return "", fmt.Errorf("hubPath cannot be empty")
	}
	if branch == "" {
		return "", fmt.Errorf("branch cannot be empty")
	}

	// Verify hubPath exists and is a valid git repository
	exists, err := afero.DirExists(m.fs, hubPath)
	if err != nil {
		return "", fmt.Errorf("failed to check hub path: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("hub path does not exist: %s", hubPath)
	}

	// Find an existing worktree in this hub to use as base for git commands
	var baseWorktreePath string

	for _, b := range hopspace.Config.Branches {
		if b.Exists && b.Path != "" {
			// Resolve the path relative to hub if it's not absolute
			branchPath := config.ResolveWorktreePath(b.Path, hubPath)

			// Check if this worktree belongs to the current hub
			if strings.HasPrefix(branchPath, hubPath+string(filepath.Separator)) || strings.HasPrefix(branchPath, hubPath) {
				baseWorktreePath = branchPath
				break
			}
		}
	}

	// If no worktree found in this hub, use any existing worktree
	// (this handles the case where we're adding to a new clone of the same repo)
	if baseWorktreePath == "" {
		for _, b := range hopspace.Config.Branches {
			if b.Exists && b.Path != "" {
				// Resolve the path relative to hub if it's not absolute
				branchPath := config.ResolveWorktreePath(b.Path, hubPath)
				baseWorktreePath = branchPath
				break
			}
		}
	}

	// If no existing worktree found, use the hub path (bare repo) as base
	if baseWorktreePath == "" {
		baseWorktreePath = hubPath
	}

	// Expand worktree location pattern
	dataHome := GetGitHopDataHome()
	ctx := WorktreeLocationContext{
		HubPath:  hubPath,
		Branch:   branch,
		Org:      org,
		Repo:     repo,
		DataHome: dataHome,
	}
	worktreePath := ExpandWorktreeLocation(locationPattern, ctx)

	// Check if already exists
	if exists, _ := afero.Exists(m.fs, worktreePath); exists {
		return worktreePath, fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	// baseWorktreePath is already absolute after resolution by ResolveWorktreePath
	// CreateWorktree will automatically try to link existing branch first, then create if needed
	if err := m.git.CreateWorktree(baseWorktreePath, branch, worktreePath, "HEAD", false); err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	return worktreePath, nil
}

// RemoveWorktree removes a worktree
func (m *WorktreeManager) RemoveWorktree(hopspace *Hopspace, branch string) error {
	// Find the worktree to remove
	branchInfo, exists := hopspace.Config.Branches[branch]
	if !exists {
		return fmt.Errorf("branch %s not found in hopspace", branch)
	}

	worktreePath := branchInfo.Path

	// We need a base path that is NOT the one we are removing
	var basePath string
	for bName, b := range hopspace.Config.Branches {
		if bName != branch && b.Exists && b.Path != "" {
			basePath = b.Path
			break
		}
	}

	if basePath == "" {
		// Cannot remove the last worktree - git worktree remove requires a git context from another worktree
		return fmt.Errorf("cannot remove the last/main worktree via this method")
	}

	// Use absolute path for git commands
	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if err := m.git.WorktreeRemove(absBasePath, worktreePath, true); err != nil {
		return err
	}

	return nil
}
