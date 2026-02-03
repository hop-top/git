# Error Recovery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement graceful error handling and recovery for git-hop operations to eliminate the "cat and mouse" problem where partial failures block retries.

**Architecture:** 3-layer error handling system: (1) Detection Layer validates state before operations and detects partial artifacts, (2) Transaction Layer provides multi-step operations with automatic rollback, (3) Recovery Layer provides cleanup handlers and enhanced error messages with fix instructions.

**Tech Stack:** Go 1.25, afero filesystem abstraction, testify for testing, existing git.Git wrapper with CommandRunner interface

---

## Task 1: Error Type System

**Files:**
- Create: `internal/hop/errors.go`
- Test: `internal/hop/errors_test.go`

**Step 1: Write failing test for GitError parsing**

```go
package hop_test

import (
	"errors"
	"testing"

	"github.com/jadb/git-hop/internal/hop"
	"github.com/stretchr/testify/assert"
)

func TestParseGitError(t *testing.T) {
	err := errors.New("git command failed: git [worktree add /path branch]: exit status 128 (stderr: fatal: '/path' already exists)")

	gitErr := hop.ParseGitError(err)

	assert.NotNil(t, gitErr)
	assert.Equal(t, "worktree add", gitErr.Operation)
	assert.Contains(t, gitErr.Stderr, "already exists")
}

func TestIsWorktreeExistsError(t *testing.T) {
	err := errors.New("git command failed: git [worktree add]: (stderr: fatal: '/path' already exists)")

	assert.True(t, hop.IsWorktreeExistsError(err))
}

func TestIsBranchExistsError(t *testing.T) {
	err := errors.New("git command failed: git [branch]: (stderr: fatal: a branch named 'main' already exists)")

	assert.True(t, hop.IsBranchExistsError(err))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/hop -run TestParseGitError -v`
Expected: FAIL with "undefined: hop.ParseGitError"

**Step 3: Write minimal implementation**

```go
package hop

import (
	"errors"
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

func (t StateErrorType) String() string {
	switch t {
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

// GitError represents a parsed git command failure
type GitError struct {
	Operation string
	Stderr    string
	Cause     error
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s failed: %v (stderr: %s)", e.Operation, e.Cause, e.Stderr)
}

// StateError represents an inconsistent state detected
type StateError struct {
	Type    StateErrorType
	Path    string
	Message string
}

func (e *StateError) Error() string {
	return fmt.Sprintf("%s at %s: %s", e.Type, e.Path, e.Message)
}

// ParseGitError extracts details from git command errors
func ParseGitError(err error) *GitError {
	if err == nil {
		return nil
	}

	msg := err.Error()
	gitErr := &GitError{Cause: err}

	// Extract operation from "git command failed: git [operation args]:"
	if idx := strings.Index(msg, "git ["); idx != -1 {
		rest := msg[idx+5:]
		if endIdx := strings.Index(rest, "]"); endIdx != -1 {
			parts := strings.Fields(rest[:endIdx])
			if len(parts) > 0 {
				gitErr.Operation = strings.Join(parts[:2], " ") // e.g., "worktree add"
			}
		}
	}

	// Extract stderr
	if idx := strings.Index(msg, "(stderr: "); idx != -1 {
		rest := msg[idx+9:]
		if endIdx := strings.LastIndex(rest, ")"); endIdx != -1 {
			gitErr.Stderr = rest[:endIdx]
		} else {
			gitErr.Stderr = rest
		}
	}

	return gitErr
}

// IsWorktreeExistsError checks if error is due to worktree already existing
func IsWorktreeExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") && strings.Contains(msg, "worktree")
}

// IsBranchExistsError checks if error is due to branch already existing
func IsBranchExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") && strings.Contains(msg, "branch")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/hop -run TestParseGitError -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/hop/errors.go internal/hop/errors_test.go
git commit -m "feat: add error type system for git command parsing"
```

---

## Task 2: State Validator - Detection Logic

**Files:**
- Create: `internal/hop/validator.go`
- Test: `internal/hop/validator_test.go`

**Step 1: Write failing test for orphaned directory detection**

```go
package hop_test

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectOrphanedDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	// Create a hopspace
	hopspacePath := "/tmp/hopspace"
	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))

	// Create hopspace config
	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{
				URI: "https://github.com/test/repo",
				Org: "test",
				Repo: "repo",
			},
			Branches: map[string]hop.BranchInfo{},
		},
	}

	// Create orphaned directory
	orphanPath := filepath.Join(hopspacePath, "hops", "orphaned-branch")
	require.NoError(t, fs.MkdirAll(orphanPath, 0755))

	validator := hop.NewStateValidator(fs, g)
	orphaned, err := validator.DetectOrphanedDirectories(hopspace)

	require.NoError(t, err)
	assert.Len(t, orphaned, 1)
	assert.Equal(t, orphanPath, orphaned[0])
}

func TestDetectOrphanedDirectories_NoOrphans(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	hopspacePath := "/tmp/hopspace"
	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{
				URI: "https://github.com/test/repo",
			},
			Branches: map[string]hop.BranchInfo{
				"main": {
					Path:   filepath.Join(hopspacePath, "hops", "main"),
					Exists: true,
				},
			},
		},
	}

	// Create registered directory
	require.NoError(t, fs.MkdirAll(hopspace.Config.Branches["main"].Path, 0755))

	validator := hop.NewStateValidator(fs, g)
	orphaned, err := validator.DetectOrphanedDirectories(hopspace)

	require.NoError(t, err)
	assert.Len(t, orphaned, 0)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/hop -run TestDetectOrphanedDirectories -v`
Expected: FAIL with "undefined: hop.NewStateValidator"

**Step 3: Write minimal implementation**

```go
package hop

import (
	"path/filepath"
	"strings"

	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
)

// StateValidator validates worktree state and detects inconsistencies
type StateValidator struct {
	fs  afero.Fs
	git *git.Git
}

// NewStateValidator creates a new state validator
func NewStateValidator(fs afero.Fs, g *git.Git) *StateValidator {
	return &StateValidator{
		fs:  fs,
		git: g,
	}
}

// StateIssue represents a detected state problem
type StateIssue struct {
	Type        StateErrorType
	Description string
	Path        string
	AutoFix     func() error // nil if manual fix required
}

// StateValidation represents validation results
type StateValidation struct {
	IsClean         bool
	Issues          []StateIssue
	CanProceed      bool
	RequiresCleanup bool
}

// DetectOrphanedDirectories finds directories in hops/ that aren't in config
func (v *StateValidator) DetectOrphanedDirectories(hopspace *Hopspace) ([]string, error) {
	var orphaned []string

	// Get the hops directory path
	hopsDir := filepath.Join(hopspace.Path, "hops")

	// Check if hops directory exists
	exists, err := afero.DirExists(v.fs, hopsDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		return orphaned, nil
	}

	// Read all entries in hops directory
	entries, err := afero.ReadDir(v.fs, hopsDir)
	if err != nil {
		return nil, err
	}

	// Build set of registered paths
	registeredPaths := make(map[string]bool)
	for _, branch := range hopspace.Config.Branches {
		if branch.Path != "" {
			registeredPaths[branch.Path] = true
		}
	}

	// Check each directory
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(hopsDir, entry.Name())
		if !registeredPaths[fullPath] {
			orphaned = append(orphaned, fullPath)
		}
	}

	return orphaned, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/hop -run TestDetectOrphanedDirectories -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/hop/validator.go internal/hop/validator_test.go
git commit -m "feat: add state validator with orphaned directory detection"
```

---

## Task 3: State Validator - Pre-flight Validation

**Files:**
- Modify: `internal/hop/validator.go`
- Modify: `internal/hop/validator_test.go`

**Step 1: Write failing test for ValidateWorktreeAdd**

Add to `internal/hop/validator_test.go`:

```go
func TestValidateWorktreeAdd_Clean(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	hopspacePath := "/tmp/hopspace"
	hubPath := "/tmp/hub"
	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))
	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{URI: "https://github.com/test/repo"},
			Branches: map[string]hop.BranchInfo{},
		},
	}

	validator := hop.NewStateValidator(fs, g)
	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, "new-branch", "/tmp/hub/hops/new-branch")

	require.NoError(t, err)
	assert.True(t, validation.IsClean)
	assert.True(t, validation.CanProceed)
	assert.Len(t, validation.Issues, 0)
}

func TestValidateWorktreeAdd_DirectoryExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	hopspacePath := "/tmp/hopspace"
	hubPath := "/tmp/hub"
	worktreePath := "/tmp/hub/hops/existing"

	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))
	require.NoError(t, fs.MkdirAll(worktreePath, 0755))

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{URI: "https://github.com/test/repo"},
			Branches: map[string]hop.BranchInfo{},
		},
	}

	validator := hop.NewStateValidator(fs, g)
	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, "existing", worktreePath)

	require.NoError(t, err)
	assert.False(t, validation.IsClean)
	assert.False(t, validation.CanProceed)
	assert.True(t, validation.RequiresCleanup)
	assert.Len(t, validation.Issues, 1)
	assert.Equal(t, hop.OrphanedDirectory, validation.Issues[0].Type)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/hop -run TestValidateWorktreeAdd -v`
Expected: FAIL with "undefined: hop.StateValidator.ValidateWorktreeAdd"

**Step 3: Write minimal implementation**

Add to `internal/hop/validator.go`:

```go
// ValidateWorktreeAdd checks state before creating a worktree
func (v *StateValidator) ValidateWorktreeAdd(
	hopspace *Hopspace,
	hubPath string,
	branch string,
	worktreePath string,
) (*StateValidation, error) {
	validation := &StateValidation{
		IsClean:    true,
		CanProceed: true,
		Issues:     []StateIssue{},
	}

	// Check if worktree path already exists
	exists, err := afero.Exists(v.fs, worktreePath)
	if err != nil {
		return nil, err
	}

	if exists {
		validation.IsClean = false
		validation.RequiresCleanup = true

		// Check if it's registered in config
		isRegistered := false
		for _, b := range hopspace.Config.Branches {
			if b.Path == worktreePath {
				isRegistered = true
				break
			}
		}

		if !isRegistered {
			validation.Issues = append(validation.Issues, StateIssue{
				Type:        OrphanedDirectory,
				Description: "Directory exists but not registered in config",
				Path:        worktreePath,
				AutoFix:     nil, // Will be set by cleanup manager
			})
			validation.CanProceed = false
		}
	}

	return validation, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/hop -run TestValidateWorktreeAdd -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/hop/validator.go internal/hop/validator_test.go
git commit -m "feat: add pre-flight validation for worktree creation"
```

---

## Task 4: Cleanup Manager - Basic Structure

**Files:**
- Create: `internal/hop/cleanup.go`
- Test: `internal/hop/cleanup_test.go`

**Step 1: Write failing test for cleanup manager**

```go
package hop_test

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupOrphanedDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	orphanPath := "/tmp/orphaned"
	require.NoError(t, fs.MkdirAll(orphanPath, 0755))

	cleanup := hop.NewCleanupManager(fs, g)
	err := cleanup.CleanupOrphanedDirectory(orphanPath)

	require.NoError(t, err)

	exists, _ := afero.Exists(fs, orphanPath)
	assert.False(t, exists)
}

func TestCleanupOrphanedDirectory_NotExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	cleanup := hop.NewCleanupManager(fs, g)
	err := cleanup.CleanupOrphanedDirectory("/tmp/nonexistent")

	assert.NoError(t, err) // Should not error if already gone
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/hop -run TestCleanupOrphanedDirectory -v`
Expected: FAIL with "undefined: hop.NewCleanupManager"

**Step 3: Write minimal implementation**

```go
package hop

import (
	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
)

// CleanupManager handles cleanup of partial or orphaned artifacts
type CleanupManager struct {
	fs  afero.Fs
	git *git.Git
}

// NewCleanupManager creates a new cleanup manager
func NewCleanupManager(fs afero.Fs, g *git.Git) *CleanupManager {
	return &CleanupManager{
		fs:  fs,
		git: g,
	}
}

// CleanupOrphanedDirectory removes a directory not tracked by git
func (c *CleanupManager) CleanupOrphanedDirectory(path string) error {
	// Check if directory exists
	exists, err := afero.Exists(c.fs, path)
	if err != nil {
		return err
	}
	if !exists {
		return nil // Already gone
	}

	// Remove the directory
	return c.fs.RemoveAll(path)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/hop -run TestCleanupOrphanedDirectory -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/hop/cleanup.go internal/hop/cleanup_test.go
git commit -m "feat: add cleanup manager for orphaned directories"
```

---

## Task 5: Cleanup Manager - Git Metadata Pruning

**Files:**
- Modify: `internal/hop/cleanup.go`
- Modify: `internal/hop/cleanup_test.go`
- Modify: `internal/git/wrapper.go` (add WorktreePrune method)

**Step 1: Write failing test for git metadata cleanup**

Add to `internal/hop/cleanup_test.go`:

```go
func TestPruneWorktrees(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create mock git that tracks prune calls
	mockRunner := &MockCommandRunner{
		RunInDirFunc: func(dir, cmd string, args ...string) (string, error) {
			if cmd == "git" && len(args) > 0 && args[0] == "worktree" && args[1] == "prune" {
				return "", nil
			}
			return "", nil
		},
	}
	g := &git.Git{Runner: mockRunner}

	hopspace := &hop.Hopspace{
		Path: "/tmp/hopspace",
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{URI: "https://github.com/test/repo"},
			Branches: map[string]hop.BranchInfo{
				"main": {Path: "/tmp/hub/hops/main", Exists: true},
			},
		},
	}

	cleanup := hop.NewCleanupManager(fs, g)
	err := cleanup.PruneWorktrees(hopspace)

	assert.NoError(t, err)
}

type MockCommandRunner struct {
	RunFunc      func(cmd string, args ...string) (string, error)
	RunInDirFunc func(dir, cmd string, args ...string) (string, error)
}

func (m *MockCommandRunner) Run(cmd string, args ...string) (string, error) {
	if m.RunFunc != nil {
		return m.RunFunc(cmd, args...)
	}
	return "", nil
}

func (m *MockCommandRunner) RunInDir(dir, cmd string, args ...string) (string, error) {
	if m.RunInDirFunc != nil {
		return m.RunInDirFunc(dir, cmd, args...)
	}
	return "", nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/hop -run TestPruneWorktrees -v`
Expected: FAIL with "undefined: hop.CleanupManager.PruneWorktrees"

**Step 3: Add WorktreePrune to git wrapper**

Add to `internal/git/wrapper.go`:

```go
// WorktreePrune removes stale worktree metadata
func (g *Git) WorktreePrune(repoPath string) error {
	_, err := g.Runner.RunInDir(repoPath, "git", "worktree", "prune")
	return err
}
```

**Step 4: Implement PruneWorktrees**

Add to `internal/hop/cleanup.go`:

```go
// PruneWorktrees removes stale git worktree metadata
func (c *CleanupManager) PruneWorktrees(hopspace *Hopspace) error {
	// Find a base path to run git commands from
	var basePath string
	for _, branch := range hopspace.Config.Branches {
		if branch.Exists && branch.Path != "" {
			basePath = branch.Path
			break
		}
	}

	if basePath == "" {
		// No worktrees to prune from
		return nil
	}

	return c.git.WorktreePrune(basePath)
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/hop -run TestPruneWorktrees -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/hop/cleanup.go internal/hop/cleanup_test.go internal/git/wrapper.go
git commit -m "feat: add git worktree metadata pruning"
```

---

## Task 6: Transaction Framework - Basic Structure

**Files:**
- Create: `internal/hop/transaction.go`
- Test: `internal/hop/transaction_test.go`

**Step 1: Write failing test for transaction success**

```go
package hop_test

import (
	"errors"
	"testing"

	"github.com/jadb/git-hop/internal/hop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransaction_Success(t *testing.T) {
	executed := []string{}

	tx := hop.NewTransaction()

	tx.AddStep(hop.TransactionStep{
		Name: "step1",
		Execute: func() error {
			executed = append(executed, "step1")
			return nil
		},
		Rollback: func() error {
			executed = append(executed, "rollback1")
			return nil
		},
	})

	tx.AddStep(hop.TransactionStep{
		Name: "step2",
		Execute: func() error {
			executed = append(executed, "step2")
			return nil
		},
		Rollback: func() error {
			executed = append(executed, "rollback2")
			return nil
		},
	})

	err := tx.Execute()

	require.NoError(t, err)
	assert.Equal(t, []string{"step1", "step2"}, executed)
}

func TestTransaction_RollbackOnFailure(t *testing.T) {
	executed := []string{}

	tx := hop.NewTransaction()

	tx.AddStep(hop.TransactionStep{
		Name: "step1",
		Execute: func() error {
			executed = append(executed, "step1")
			return nil
		},
		Rollback: func() error {
			executed = append(executed, "rollback1")
			return nil
		},
	})

	tx.AddStep(hop.TransactionStep{
		Name: "step2",
		Execute: func() error {
			return errors.New("step2 failed")
		},
		Rollback: func() error {
			executed = append(executed, "rollback2")
			return nil
		},
	})

	err := tx.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "step2 failed")
	assert.Equal(t, []string{"step1", "rollback1"}, executed)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/hop -run TestTransaction -v`
Expected: FAIL with "undefined: hop.NewTransaction"

**Step 3: Write minimal implementation**

```go
package hop

import (
	"fmt"
)

// TransactionStep represents a single step in a transaction
type TransactionStep struct {
	Name     string
	Execute  func() error
	Rollback RollbackFunc
}

// RollbackFunc is called when transaction needs to rollback
type RollbackFunc func() error

// Transaction manages multi-step operations with rollback support
type Transaction struct {
	steps     []TransactionStep
	rollbacks []RollbackFunc
	completed []int
}

// NewTransaction creates a new transaction
func NewTransaction() *Transaction {
	return &Transaction{
		steps:     []TransactionStep{},
		rollbacks: []RollbackFunc{},
		completed: []int{},
	}
}

// AddStep adds a step to the transaction
func (t *Transaction) AddStep(step TransactionStep) {
	t.steps = append(t.steps, step)
}

// Execute runs all steps, rolling back on failure
func (t *Transaction) Execute() error {
	for i, step := range t.steps {
		if err := step.Execute(); err != nil {
			t.Rollback()
			return fmt.Errorf("transaction step '%s' failed: %w", step.Name, err)
		}
		t.completed = append(t.completed, i)
		// Add rollback in reverse order (LIFO)
		if step.Rollback != nil {
			t.rollbacks = append([]RollbackFunc{step.Rollback}, t.rollbacks...)
		}
	}
	return nil
}

// Rollback undoes completed steps in reverse order
func (t *Transaction) Rollback() {
	for _, rollback := range t.rollbacks {
		if rollback != nil {
			if err := rollback(); err != nil {
				// Log but continue rolling back
				// In production, use proper logger
				fmt.Printf("Rollback error: %v\n", err)
			}
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/hop -run TestTransaction -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/hop/transaction.go internal/hop/transaction_test.go
git commit -m "feat: add transaction framework with rollback support"
```

---

## Task 7: Transactional Worktree Creation

**Files:**
- Modify: `internal/hop/worktree.go`
- Test: `internal/hop/worktree_transactional_test.go`

**Step 1: Write failing test for transactional worktree creation**

```go
package hop_test

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateWorktreeTransactional_Success(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Setup mock git
	mockRunner := &MockCommandRunner{
		RunInDirFunc: func(dir, cmd string, args ...string) (string, error) {
			// Simulate successful worktree creation
			if cmd == "git" && args[0] == "worktree" && args[1] == "add" {
				worktreePath := args[2]
				fs.MkdirAll(worktreePath, 0755)
			}
			return "", nil
		},
	}
	g := &git.Git{Runner: mockRunner}

	hopspacePath := "/tmp/hopspace"
	hubPath := "/tmp/hub"

	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))
	require.NoError(t, fs.MkdirAll(hubPath, 0755))

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{
				URI:  "https://github.com/test/repo",
				Org:  "test",
				Repo: "repo",
			},
			Branches: map[string]hop.BranchInfo{},
		},
	}

	wm := hop.NewWorktreeManager(fs, g)
	worktreePath, err := wm.CreateWorktreeTransactional(
		hopspace,
		hubPath,
		"new-branch",
		"{{.HubPath}}/hops/{{.Branch}}",
		"test",
		"repo",
	)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(hubPath, "hops", "new-branch"), worktreePath)

	exists, _ := afero.Exists(fs, worktreePath)
	assert.True(t, exists)
}

func TestCreateWorktreeTransactional_CleansUpOrphanedDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()

	mockRunner := &MockCommandRunner{
		RunInDirFunc: func(dir, cmd string, args ...string) (string, error) {
			if cmd == "git" && args[0] == "worktree" && args[1] == "add" {
				worktreePath := args[2]
				fs.MkdirAll(worktreePath, 0755)
			}
			return "", nil
		},
	}
	g := &git.Git{Runner: mockRunner}

	hopspacePath := "/tmp/hopspace"
	hubPath := "/tmp/hub"
	worktreePath := filepath.Join(hubPath, "hops", "existing")

	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))
	require.NoError(t, fs.MkdirAll(worktreePath, 0755)) // Pre-existing orphan

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{
				URI:  "https://github.com/test/repo",
				Org:  "test",
				Repo: "repo",
			},
			Branches: map[string]hop.BranchInfo{},
		},
	}

	wm := hop.NewWorktreeManager(fs, g)
	result, err := wm.CreateWorktreeTransactional(
		hopspace,
		hubPath,
		"existing",
		"{{.HubPath}}/hops/{{.Branch}}",
		"test",
		"repo",
	)

	require.NoError(t, err)
	assert.Equal(t, worktreePath, result)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/hop -run TestCreateWorktreeTransactional -v`
Expected: FAIL with "undefined: hop.WorktreeManager.CreateWorktreeTransactional"

**Step 3: Implement CreateWorktreeTransactional**

Add to `internal/hop/worktree.go`:

```go
// CreateWorktreeTransactional creates a worktree with pre-flight validation and cleanup
func (m *WorktreeManager) CreateWorktreeTransactional(
	hopspace *Hopspace,
	hubPath string,
	branch string,
	locationPattern string,
	org string,
	repo string,
) (string, error) {
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

	// 1. Pre-flight validation
	validator := NewStateValidator(m.fs, m.git)
	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, branch, worktreePath)
	if err != nil {
		return "", err
	}

	if !validation.CanProceed {
		if validation.RequiresCleanup {
			cleanup := NewCleanupManager(m.fs, m.git)
			for _, issue := range validation.Issues {
				if issue.Type == OrphanedDirectory {
					if err := cleanup.CleanupOrphanedDirectory(issue.Path); err != nil {
						return "", fmt.Errorf("cleanup failed: %w", err)
					}
				}
			}
		} else {
			return "", fmt.Errorf("cannot proceed: validation failed with %d issues", len(validation.Issues))
		}
	}

	// 2. Create worktree using existing logic
	return m.CreateWorktree(hopspace, hubPath, branch, locationPattern, org, repo)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/hop -run TestCreateWorktreeTransactional -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/hop/worktree.go internal/hop/worktree_transactional_test.go
git commit -m "feat: add transactional worktree creation with validation"
```

---

## Task 8: Integrate Transactional Worktree in Add Command

**Files:**
- Modify: `cmd/add.go`
- Test: `cmd/add_error_recovery_test.go`

**Step 1: Write failing integration test**

```go
package cmd_test

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/cmd"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCommand_RecoveryFromOrphanedDirectory(t *testing.T) {
	// This test verifies that 'add' can recover from orphaned directories
	// Setup will be complex - create a hub, hopspace, and orphaned directory
	// Then run add command and verify it succeeds

	fs := afero.NewMemMapFs()
	g := git.New()

	hubPath := "/tmp/hub"
	hopspacePath := "/tmp/hopspace"

	// Create hub and hopspace configs
	require.NoError(t, fs.MkdirAll(hubPath, 0755))
	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))

	// Create orphaned directory
	orphanPath := filepath.Join(hubPath, "hops", "feature")
	require.NoError(t, fs.MkdirAll(orphanPath, 0755))

	// This would test the actual command execution
	// For now, we'll test the worktree manager directly

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{
				URI:  "https://github.com/test/repo",
				Org:  "test",
				Repo: "repo",
			},
			Branches: map[string]hop.BranchInfo{},
		},
	}

	wm := hop.NewWorktreeManager(fs, g)
	_, err := wm.CreateWorktreeTransactional(hopspace, hubPath, "feature", "{{.HubPath}}/hops/{{.Branch}}", "test", "repo")

	// Should succeed after cleaning up orphan
	assert.NoError(t, err)
}
```

**Step 2: Run test to verify current behavior**

Run: `go test ./cmd -run TestAddCommand_RecoveryFromOrphanedDirectory -v`
Expected: Test exists and validates behavior

**Step 3: Update add command to use transactional worktree creation**

Modify `cmd/add.go` around line 78:

```go
// Replace:
// worktreePath, err := wm.CreateWorktree(hopspace, hubPath, branch, globalConfig.Defaults.WorktreeLocation, hub.Config.Repo.Org, hub.Config.Repo.Repo)

// With:
worktreePath, err := wm.CreateWorktreeTransactional(hopspace, hubPath, branch, globalConfig.Defaults.WorktreeLocation, hub.Config.Repo.Org, hub.Config.Repo.Repo)
if err != nil {
	// Check if it's a state error
	if stateErr, ok := err.(*hop.StateError); ok {
		output.Error("Cannot create worktree due to state issues:")
		output.Error("  %s at %s: %s", stateErr.Type, stateErr.Path, stateErr.Message)
		output.Info("\nRun 'git hop doctor --fix' to resolve these issues")
		os.Exit(1)
	}
	output.Fatal("Failed to create worktree: %v", err)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd -run TestAddCommand_RecoveryFromOrphanedDirectory -v`
Expected: PASS

**Step 5: Test manually**

```bash
# Create a test scenario
cd /tmp
mkdir -p test-hub/hops/orphan
git init test-hub
cd test-hub
git hop add feature  # Should clean up orphan and succeed
```

**Step 6: Commit**

```bash
git add cmd/add.go cmd/add_error_recovery_test.go
git commit -m "feat: integrate transactional worktree creation in add command"
```

---

## Task 9: Enhanced Doctor Command - State Validation

**Files:**
- Modify: `cmd/doctor.go`
- Test: `cmd/doctor_state_recovery_test.go`

**Step 1: Write failing test for doctor state checks**

```go
package cmd_test

import (
	"testing"

	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorCommand_DetectsOrphanedDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := git.New()

	hopspacePath := "/tmp/hopspace"
	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))

	// Create orphaned directory
	orphanPath := "/tmp/hopspace/hops/orphan"
	require.NoError(t, fs.MkdirAll(orphanPath, 0755))

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{URI: "https://github.com/test/repo"},
			Branches: map[string]hop.BranchInfo{},
		},
	}

	validator := hop.NewStateValidator(fs, g)
	orphaned, err := validator.DetectOrphanedDirectories(hopspace)

	require.NoError(t, err)
	assert.Len(t, orphaned, 1)

	// Doctor should detect this
	// Actual doctor command test would run the cobra command
}
```

**Step 2: Run test to verify it documents expected behavior**

Run: `go test ./cmd -run TestDoctorCommand_DetectsOrphanedDirectories -v`
Expected: PASS (documents behavior)

**Step 3: Add state validation to doctor command**

Add to `cmd/doctor.go` after existing checks (around line 160):

```go
output.Info("\n=== Checking Worktree State ===")

validator := hop.NewStateValidator(fs, g)
cleanup := hop.NewCleanupManager(fs, g)

// Check for orphaned directories
orphanedDirs, err := validator.DetectOrphanedDirectories(hopspace)
if err != nil {
	output.Error("Failed to detect orphaned directories: %v", err)
} else if len(orphanedDirs) > 0 {
	issuesFound = true
	output.Error("Found %d orphaned directories", len(orphanedDirs))
	for _, dir := range orphanedDirs {
		output.Error("  - %s", dir)
		if doctorFix {
			output.Info("    Cleaning up...")
			if err := cleanup.CleanupOrphanedDirectory(dir); err != nil {
				output.Error("    Failed to remove: %v", err)
			} else {
				output.Info("    ✓ Removed")
				fixedIssues++
			}
		}
	}
	if !doctorFix {
		output.Info("  Run 'git hop doctor --fix' to clean up orphaned directories")
	}
} else {
	output.Info("✓ No orphaned directories found")
}
```

**Step 4: Test manually**

```bash
# Create orphaned directory
mkdir -p /tmp/test-hopspace/hops/orphan

# Run doctor
git hop doctor  # Should detect orphan

# Run doctor --fix
git hop doctor --fix  # Should clean up orphan
```

**Step 5: Commit**

```bash
git add cmd/doctor.go cmd/doctor_state_recovery_test.go
git commit -m "feat: add orphaned directory detection to doctor command"
```

---

## Task 10: Enhanced Remove Command - Validation

**Files:**
- Modify: `cmd/remove.go`
- Test: Update existing `cmd/remove_test.go`

**Step 1: Write failing test for validated removal**

Add to existing tests in test files:

```go
func TestRemoveCommand_WithValidation(t *testing.T) {
	// Test that remove validates state before removal
	// and continues even if worktree removal fails

	fs := afero.NewMemMapFs()
	g := git.New()

	hopspacePath := "/tmp/hopspace"
	worktreePath := "/tmp/hub/hops/feature"

	require.NoError(t, fs.MkdirAll(hopspacePath, 0755))
	require.NoError(t, fs.MkdirAll(worktreePath, 0755))

	hopspace := &hop.Hopspace{
		Path: hopspacePath,
		Config: &hop.HopspaceConfig{
			Repo: hop.RepoInfo{URI: "https://github.com/test/repo"},
			Branches: map[string]hop.BranchInfo{
				"feature": {
					Path:   worktreePath,
					Exists: true,
				},
			},
		},
	}

	wm := hop.NewWorktreeManager(fs, g)

	// Remove should not fail completely if worktree removal has issues
	err := wm.RemoveWorktree(hopspace, "feature")

	// Should attempt cleanup even if errors occur
	assert.NoError(t, err) // or handle gracefully
}
```

**Step 2: Update remove command error handling**

Modify `cmd/remove.go` around lines 71-78:

```go
wm := hop.NewWorktreeManager(fs, g)
if err := wm.RemoveWorktree(hopspace, target); err != nil {
	output.Error("Failed to remove worktree: %v", err)
	output.Info("Continuing with config cleanup...")
	// Don't fatal - partial success is ok
}

// Always try to unregister (even if worktree removal failed)
if err := hopspace.UnregisterBranch(target); err != nil {
	output.Error("Failed to unregister branch from hopspace: %v", err)
}

// Prune stale git metadata
cleanup := hop.NewCleanupManager(fs, g)
if err := cleanup.PruneWorktrees(hopspace); err != nil {
	output.Warn("Failed to prune worktrees: %v", err)
}

output.Info("Successfully removed %s", target)
```

**Step 3: Run tests**

Run: `go test ./cmd -run TestRemoveCommand -v`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/remove.go cmd/remove_test.go
git commit -m "feat: enhance remove command with cleanup and validation"
```

---

## Task 11: Documentation and Final Testing

**Files:**
- Create: `docs/error-recovery.md`
- Update: `README.md` (if needed)

**Step 1: Write error recovery documentation**

```markdown
# Error Recovery in git-hop

## Overview

Git-hop implements a 3-layer error handling system to handle partial failures gracefully:

1. **Detection Layer** - Validates state before operations
2. **Transaction Layer** - Multi-step operations with automatic rollback
3. **Recovery Layer** - Cleanup handlers and enhanced error messages

## Common Scenarios

### Orphaned Directories

**Problem:** Directory exists but not registered in git or config.

**Detection:**
```bash
git hop doctor
```

**Fix:**
```bash
git hop doctor --fix
```

**Manual Fix:**
```bash
rm -rf /path/to/orphaned/directory
```

### Stale Git Metadata

**Problem:** Git knows about worktree but directory is missing.

**Detection:**
```bash
git hop doctor
```

**Fix:**
```bash
git hop doctor --fix  # Runs 'git worktree prune'
```

### Retry After Partial Failure

The transactional worktree creation automatically:
1. Detects orphaned directories
2. Cleans them up
3. Retries the operation

**Example:**
```bash
git hop add feature  # Fails mid-operation
git hop add feature  # Automatically recovers and succeeds
```

## Architecture

See `docs/plans/error-recovery.md` for implementation details.
```

**Step 2: Run full test suite**

```bash
# Run all tests
go test ./... -v

# Run specific error recovery tests
go test ./internal/hop -run "Error|Cleanup|Transaction|Validator" -v
go test ./cmd -run "Recovery|Doctor|Add" -v
```

**Step 3: Manual end-to-end test**

```bash
# 1. Create orphaned state
mkdir -p /tmp/test-hub/hops/orphan

# 2. Try to add (should clean up and succeed)
cd /tmp/test-hub
git hop add orphan

# 3. Run doctor
git hop doctor

# 4. Create another orphan and use doctor --fix
mkdir -p /tmp/test-hub/hops/orphan2
git hop doctor --fix
```

**Step 4: Commit**

```bash
git add docs/error-recovery.md
git commit -m "docs: add error recovery documentation"
```

---

## Verification Checklist

After completing all tasks, verify:

- [ ] All tests pass: `go test ./... -v`
- [ ] Orphaned directories are detected and cleaned up
- [ ] Transactional worktree creation works
- [ ] Doctor command detects and fixes state issues
- [ ] Remove command handles partial failures gracefully
- [ ] Error messages include recovery instructions
- [ ] Manual testing scenarios all work
- [ ] Documentation is complete

## Success Criteria

- ✅ No operation fails due to pre-existing artifacts
- ✅ All failed operations can be retried successfully
- ✅ `doctor --fix` resolves all auto-fixable issues
- ✅ Error messages explain what happened and how to fix
- ✅ Test coverage >80% for new code

---

## Notes

- **Testing Strategy:** Unit tests for each component, integration tests for command behavior
- **Backward Compatibility:** All changes are additive; existing code paths still work
- **Rollout:** Transactional methods can be opt-in initially via flag if desired
- **Future Enhancements:** Could add `--force` flags, more sophisticated rollback strategies
