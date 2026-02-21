package hop

import (
	"path/filepath"

	"hop.top/git/internal/git"
	"github.com/spf13/afero"
)

// StateIssue represents a detected state inconsistency
type StateIssue struct {
	Type        StateErrorType
	Description string
	Path        string
	AutoFix     bool
}

// StateValidation represents the result of a state validation check
type StateValidation struct {
	IsClean         bool
	Issues          []StateIssue
	CanProceed      bool
	RequiresCleanup bool
}

// StateValidator validates the consistency of hopspace state
type StateValidator struct {
	fs  afero.Fs
	git git.GitInterface
}

// NewStateValidator creates a new state validator
func NewStateValidator(fs afero.Fs, g git.GitInterface) *StateValidator {
	return &StateValidator{
		fs:  fs,
		git: g,
	}
}

// DetectOrphanedDirectories finds directories in hops/ that are not registered in config
func (v *StateValidator) DetectOrphanedDirectories(hopspace *Hopspace) ([]string, error) {
	hopsDir := filepath.Join(hopspace.Path, "hops")

	// Check if hops directory exists
	exists, err := afero.DirExists(v.fs, hopsDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []string{}, nil
	}

	// Read all entries in hops directory
	entries, err := afero.ReadDir(v.fs, hopsDir)
	if err != nil {
		return nil, err
	}

	// Build a set of registered paths for quick lookup
	registeredPaths := make(map[string]bool)
	for _, branch := range hopspace.Config.Branches {
		// Extract just the directory name from the path
		// (handles both relative paths like "feature-1" and absolute paths like "/tmp/hopspace/hops/feature-1")
		dirName := filepath.Base(branch.Path)
		registeredPaths[dirName] = true
	}

	// Find orphaned directories
	var orphaned []string
	for _, entry := range entries {
		// Skip files, only check directories
		if !entry.IsDir() {
			continue
		}

		// Check if this directory is registered
		if !registeredPaths[entry.Name()] {
			orphaned = append(orphaned, entry.Name())
		}
	}

	return orphaned, nil
}

// ValidateWorktreeAdd performs pre-flight validation before creating a worktree
func (v *StateValidator) ValidateWorktreeAdd(hopspace *Hopspace, hubPath string, branch string, worktreePath string) (*StateValidation, error) {
	validation := &StateValidation{
		IsClean:         true,
		CanProceed:      true,
		RequiresCleanup: false,
		Issues:          []StateIssue{},
	}

	// Check if worktree path already exists
	exists, err := afero.Exists(v.fs, worktreePath)
	if err != nil {
		return nil, err
	}

	if exists {
		validation.IsClean = false
		validation.RequiresCleanup = true

		// Check if path is registered in config
		registered := false
		for _, configBranch := range hopspace.Config.Branches {
			if configBranch.Path == worktreePath {
				registered = true
				break
			}
		}

		// If not registered, it's an orphaned directory - cannot proceed
		if !registered {
			validation.CanProceed = false
			validation.Issues = append(validation.Issues, StateIssue{
				Type:        OrphanedDirectory,
				Description: "Orphaned directory exists but is not registered in hopspace config",
				Path:        worktreePath,
				AutoFix:     false,
			})
		}
	}

	return validation, nil
}
