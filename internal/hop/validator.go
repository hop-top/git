package hop

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
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

// DetectOrphanedDirectories finds directories under hops/ that hold no
// registered worktree. Results are paths relative to hops/.
//
// A branch with a slash (feat/x) lives at hops/feat/x, so hops/feat is a
// parent of a worktree, not an orphan: the walk descends into such parents
// and reports only what lies beside the registered worktrees. A top-level
// entry named like any registered path's base name is still kept, as it
// always was, for configs that record paths outside hops/.
func (v *StateValidator) DetectOrphanedDirectories(hopspace *Hopspace) ([]string, error) {
	hopsDir := filepath.Join(hopspace.Path, "hops")

	exists, err := afero.DirExists(v.fs, hopsDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []string{}, nil
	}

	registered := make(map[string]bool)
	parents := make(map[string]bool)
	baseNames := make(map[string]bool)
	for _, branch := range hopspace.Config.Branches {
		baseNames[filepath.Base(branch.Path)] = true
		rel, ok := relToHops(hopsDir, branch.Path)
		if !ok {
			continue
		}
		registered[rel] = true
		for dir := filepath.Dir(rel); dir != "."; dir = filepath.Dir(dir) {
			parents[dir] = true
		}
	}

	orphaned := []string{}
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := afero.ReadDir(v.fs, filepath.Join(hopsDir, rel))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			child := filepath.Join(rel, entry.Name())
			switch {
			case registered[child]:
			case parents[child]:
				if err := walk(child); err != nil {
					return err
				}
			case rel == "" && baseNames[entry.Name()]:
			default:
				orphaned = append(orphaned, child)
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	return orphaned, nil
}

// relToHops maps a recorded worktree path to its path under hopsDir.
// Absolute paths must lie inside hopsDir; relative ones are taken as
// relative to hopsDir, with a leading "hops/" (hub-relative) stripped.
func relToHops(hopsDir, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(hopsDir, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return rel, true
	}
	rel := filepath.Clean(path)
	if trimmed, ok := strings.CutPrefix(rel, "hops"+string(filepath.Separator)); ok {
		rel = trimmed
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
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
