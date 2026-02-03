package hop

import (
	"fmt"
	"strings"
)

// StateErrorType represents different types of state inconsistencies
type StateErrorType int

const (
	OrphanedDirectory StateErrorType = iota
	PartialWorktree
	ConfigMismatch
	OrphanedGitMetadata
)

// String returns the string representation of StateErrorType
func (s StateErrorType) String() string {
	switch s {
	case OrphanedDirectory:
		return "OrphanedDirectory"
	case PartialWorktree:
		return "PartialWorktree"
	case ConfigMismatch:
		return "ConfigMismatch"
	case OrphanedGitMetadata:
		return "OrphanedGitMetadata"
	default:
		return "Unknown"
	}
}

// GitError represents a parsed git command error
type GitError struct {
	Operation string // The git operation that failed (e.g., "worktree add")
	Stderr    string // The stderr output from git
	Cause     string // The underlying error cause (e.g., "exit status 128")
}

// Error implements the error interface
func (e *GitError) Error() string {
	return fmt.Sprintf("git %s failed: %s (stderr: %s)", e.Operation, e.Cause, e.Stderr)
}

// Unwrap returns nil since we store Cause as a string
func (e *GitError) Unwrap() error {
	return nil
}

// StateError represents an inconsistency in the state
type StateError struct {
	Type    StateErrorType
	Path    string
	Message string
}

// Error implements the error interface
func (e *StateError) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Type.String(), e.Path, e.Message)
}

// ParseGitError extracts operation and stderr from git command errors
// Expected format: "git command failed: git [operation args]: error (stderr: message)"
func ParseGitError(err error) *GitError {
	if err == nil {
		return nil
	}

	errStr := err.Error()

	// Check if this is a git command error
	if !strings.HasPrefix(errStr, "git command failed: git [") {
		return nil
	}

	// Extract operation: between "git [" and "]:"
	opStart := strings.Index(errStr, "git [")
	if opStart == -1 {
		return nil
	}
	opStart += len("git [")

	opEnd := strings.Index(errStr[opStart:], "]:")
	if opEnd == -1 {
		return nil
	}
	operation := errStr[opStart : opStart+opEnd]

	// Extract stderr: between "(stderr: " and ")"
	stderrStart := strings.Index(errStr, "(stderr: ")
	if stderrStart == -1 {
		return nil
	}
	stderrStart += len("(stderr: ")

	stderrEnd := strings.LastIndex(errStr, ")")
	if stderrEnd == -1 || stderrEnd <= stderrStart {
		return nil
	}
	stderr := errStr[stderrStart:stderrEnd]

	// Extract cause: between "]: " and " (stderr:"
	causeStart := opStart + opEnd + len("]: ")
	causeEnd := strings.Index(errStr[causeStart:], " (stderr:")
	if causeEnd == -1 {
		return nil
	}
	cause := errStr[causeStart : causeStart+causeEnd]

	return &GitError{
		Operation: operation,
		Stderr:    stderr,
		Cause:     cause,
	}
}

// IsWorktreeExistsError checks if the error indicates a worktree already exists
func IsWorktreeExistsError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "already exists") && strings.Contains(errStr, "worktree")
}

// IsBranchExistsError checks if the error indicates a branch already exists
func IsBranchExistsError(err error) bool {
	if err == nil {
		return false
	}

	gitErr := ParseGitError(err)
	if gitErr == nil {
		return false
	}

	stderrLower := strings.ToLower(gitErr.Stderr)
	return strings.Contains(stderrLower, "already exists") && strings.Contains(stderrLower, "branch")
}
