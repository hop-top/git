# Graceful Error Handling & Recovery for git-hop

## Problem Statement

Git operations currently fail without recovery when partial state exists. The "cat and mouse" problem: if `git worktree add` partially succeeds but later steps fail, subsequent retries fail because artifacts already exist. Users are stuck with no automated recovery path.

**Critical Example (internal/hop/worktree.go:70-75):**
1. `WorktreeAdd` fails (branch doesn't exist)
2. `WorktreeAddCreate` partially succeeds (creates git metadata + directory)
3. Later `RegisterBranch` or config write fails
4. Next retry fails at directory existence check
5. User stuck - manual cleanup required

## Solution Overview

Implement a 3-layer error handling system:
1. **Detection Layer** - Validate state before operations, detect partial artifacts
2. **Transaction Layer** - Multi-step operations with automatic rollback
3. **Recovery Layer** - Cleanup handlers + enhanced error messages with fix instructions

## Implementation Plan

### Phase 1: Error Type System & Detection

#### Create `internal/hop/errors.go`
Define specific error types:

```go
// GitError - parsed git command failures
type GitError struct {
    Operation string
    Stderr    string
    Cause     error
}

// StateError - inconsistent state detected
type StateError struct {
    Type     StateErrorType  // OrphanedDirectory, PartialWorktree, ConfigMismatch
    Path     string
    Message  string
}

type StateErrorType int
const (
    OrphanedDirectory StateErrorType = iota
    PartialWorktree
    ConfigMismatch
    OrphanedGitMetadata
)
```

Error detection utilities:
- `ParseGitError(err error) *GitError` - extract details from stderr
- `IsWorktreeExistsError(err error) bool` - detect "already exists"
- `IsBranchExistsError(err error) bool` - detect branch conflicts

#### Create `internal/hop/validator.go`
Pre-flight state validation:

```go
type StateValidator struct {
    fs  afero.Fs
    git GitInterface
}

// ValidateWorktreeAdd - check state before creating worktree
func (v *StateValidator) ValidateWorktreeAdd(
    hopspace *Hopspace,
    hubPath string,
    branch string,
) (*StateValidation, error)

type StateValidation struct {
    IsClean          bool
    Issues           []StateIssue
    CanProceed       bool
    RequiresCleanup  bool
}

type StateIssue struct {
    Type        StateErrorType
    Description string
    Path        string
    AutoFix     func() error  // nil if manual fix required
}
```

Detection functions:
- `DetectOrphanedDirectories(hopspace)` - directory exists, not in git metadata
- `DetectOrphanedGitMetadata(hopspace)` - git knows about worktree, directory missing
- `DetectConfigMismatch(hub, hopspace)` - hub and hopspace configs diverged

### Phase 2: Cleanup Handlers

#### Create `internal/hop/cleanup.go`
Safe artifact removal:

```go
type CleanupManager struct {
    fs  afero.Fs
    git GitInterface
}

// CleanupOrphanedWorktree - remove partial worktree artifacts
func (c *CleanupManager) CleanupOrphanedWorktree(
    hopspace *Hopspace,
    branch string,
    path string,
) error

// CleanupOrphanedDirectory - remove directory not tracked by git
func (c *CleanupManager) CleanupOrphanedDirectory(path string) error

// CleanupOrphanedGitMetadata - prune git metadata for missing worktree
func (c *CleanupManager) CleanupOrphanedGitMetadata(
    basePath string,
    worktreePath string,
) error
```

Safety checks:
- Never remove directories with uncommitted changes (use `git status --porcelain`)
- Log all cleanup actions
- Support dry-run mode

### Phase 3: Transaction Framework

#### Create `internal/hop/transaction.go`
Implement transaction pattern with rollback:

```go
type Transaction struct {
    steps     []TransactionStep
    rollbacks []RollbackFunc
    completed []int
}

type TransactionStep struct {
    Name     string
    Execute  func() error
    Rollback RollbackFunc
}

type RollbackFunc func() error

func (t *Transaction) Execute() error {
    for i, step := range t.steps {
        if err := step.Execute(); err != nil {
            t.Rollback()
            return err
        }
        t.completed = append(t.completed, i)
        t.rollbacks = append([]RollbackFunc{step.Rollback}, t.rollbacks...)
    }
    return nil
}

func (t *Transaction) Rollback() {
    for _, rollback := range t.rollbacks {
        if rollback != nil {
            if err := rollback(); err != nil {
                output.Error("Rollback failed: %v", err)
            }
        }
    }
}
```

#### Update `internal/hop/worktree.go`
Add transactional worktree operations:

**New method: `CreateWorktreeTransactional`**
```go
func (m *WorktreeManager) CreateWorktreeTransactional(
    hopspace *Hopspace,
    hubPath string,
    branch string,
) (string, error) {
    // 1. Pre-flight validation
    validator := NewStateValidator(m.fs, m.git)
    validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, branch)
    if err != nil {
        return "", err
    }

    if !validation.CanProceed {
        if validation.RequiresCleanup {
            cleanup := NewCleanupManager(m.fs, m.git)
            for _, issue := range validation.Issues {
                if issue.AutoFix != nil {
                    output.Warn("Cleaning up: %s", issue.Description)
                    if err := issue.AutoFix(); err != nil {
                        return "", fmt.Errorf("cleanup failed: %w", err)
                    }
                }
            }
        } else {
            return "", NewStateError(validation.Issues)
        }
    }

    // 2. Execute transactionally
    worktreePath := filepath.Join(hubPath, "worktrees", branch)
    var createdWorktree bool

    tx := NewTransaction()

    // Step 1: Create git worktree
    tx.AddStep(TransactionStep{
        Name: "create-worktree",
        Execute: func() error {
            err := m.createWorktreeInternal(hopspace, hubPath, branch, worktreePath)
            if err == nil {
                createdWorktree = true
            }
            return err
        },
        Rollback: func() error {
            if createdWorktree {
                m.git.WorktreeRemove(hopspace.Config.Repo.URI, worktreePath, true)
                os.RemoveAll(worktreePath)
            }
            return nil
        },
    })

    if err := tx.Execute(); err != nil {
        return "", fmt.Errorf("failed to create worktree: %w", err)
    }

    return worktreePath, nil
}
```

**Add internal helper: `createWorktreeInternal`**
Extracted from current CreateWorktree logic (lines 27-78) with:
- Base worktree detection
- WorktreeAdd attempt
- Fallback to WorktreeAddCreate
- No config updates (handled by caller)

### Phase 4: Command Integration

#### Update `cmd/add.go`
Replace lines 64-77 with transactional version:

```go
// Create worktree with transaction support
wm := hop.NewWorktreeManager(fs, g)
worktreePath, err := wm.CreateWorktreeTransactional(hopspace, hubPath, branch)
if err != nil {
    if stateErr, ok := err.(*hop.StateError); ok {
        output.Error("Cannot create worktree due to state issues:")
        for _, issue := range stateErr.Issues {
            output.Error("  - %s", issue.Description)
        }
        output.Info("\nRun 'git hop doctor --fix' to resolve these issues")
        os.Exit(1)
    }
    output.Fatal("Failed to create worktree: %v", err)
}

// Register in hopspace (now separate from worktree creation)
if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
    // Rollback: remove the worktree we just created
    wm.RemoveWorktreeInternal(hopspace, worktreePath)
    output.Fatal("Failed to register branch: %v", err)
}

// Register in hub
if err := hub.AddBranch(branch, branch, worktreePath); err != nil {
    // Rollback both
    hopspace.UnregisterBranch(branch)
    wm.RemoveWorktreeInternal(hopspace, worktreePath)
    output.Fatal("Failed to add branch to hub: %v", err)
}
```

#### Update `cmd/remove.go`
Replace lines 70-77 with validation and cleanup:

```go
wm := hop.NewWorktreeManager(fs, g)

// Validate before removal
validator := hop.NewStateValidator(fs, g)
validation, err := validator.ValidateWorktreeRemove(hopspace, target)
if err != nil {
    output.Fatal("Validation failed: %v", err)
}

if !validation.CanProceed {
    output.Error("Cannot remove worktree due to uncommitted changes")
    output.Info("Use 'git hop remove --force' to remove anyway")
    os.Exit(1)
}

// Remove worktree
if err := wm.RemoveWorktree(hopspace, target); err != nil {
    output.Error("Failed to remove worktree: %v", err)
    output.Info("Continuing with config cleanup...")
}

// Unregister (even if worktree removal failed)
if err := hopspace.UnregisterBranch(target); err != nil {
    output.Error("Failed to unregister branch: %v", err)
}

// Prune stale git metadata
cleanup := hop.NewCleanupManager(fs, g)
if err := cleanup.PruneWorktrees(hopspace); err != nil {
    output.Warn("Failed to prune worktrees: %v", err)
}
```

### Phase 5: Doctor Command Enhancement

#### Update `cmd/doctor.go`
Add state validation section after line 160:

```go
output.Info("\n=== Checking Worktree State ===")

validator := hop.NewStateValidator(fs, g)
cleanup := hop.NewCleanupManager(fs, g)

// Validate hub/hopspace consistency
validation, err := validator.ValidateConfigConsistency(hub, hopspace)
if err != nil {
    output.Error("Failed to validate state: %v", err)
} else if len(validation.Issues) > 0 {
    for _, issue := range validation.Issues {
        issuesFound = true
        output.Error("%s: %s", issue.Type, issue.Description)

        if doctorFix && issue.AutoFix != nil {
            output.Info("  Fixing...")
            if err := issue.AutoFix(); err != nil {
                output.Error("  Failed to fix: %v", err)
            } else {
                output.Info("  ✓ Fixed")
                fixedIssues++
            }
        } else if !doctorFix {
            output.Info("  Run 'git hop doctor --fix' to resolve")
        }
    }
} else {
    output.Info("✓ Worktree state is consistent")
}

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
            if err := cleanup.CleanupOrphanedDirectory(dir); err != nil {
                output.Error("    Failed to remove: %v", err)
            } else {
                output.Info("    ✓ Removed")
                fixedIssues++
            }
        }
    }
}

// Check for orphaned git metadata
orphanedMeta, err := validator.DetectOrphanedGitMetadata(hopspace)
if err != nil {
    output.Error("Failed to detect orphaned git metadata: %v", err)
} else if len(orphanedMeta) > 0 {
    issuesFound = true
    output.Error("Found %d orphaned git worktree references", len(orphanedMeta))
    for _, meta := range orphanedMeta {
        output.Error("  - %s", meta)
    }
    if doctorFix {
        output.Info("  Pruning stale worktree references...")
        if err := cleanup.PruneWorktrees(hopspace); err != nil {
            output.Error("  Failed to prune: %v", err)
        } else {
            output.Info("  ✓ Pruned")
            fixedIssues++
        }
    }
}
```

### Phase 6: Testing

#### Create `test/unit/validator_test.go`
```go
func TestDetectOrphanedDirectory(t *testing.T) {
    // Setup: create directory without git metadata
    // Execute: validator.DetectOrphanedDirectories()
    // Assert: directory detected as orphaned
}

func TestDetectPartialWorktree(t *testing.T) {
    // Setup: create git metadata, delete directory
    // Execute: validator.ValidateWorktreeAdd()
    // Assert: PartialWorktree state detected
}

func TestValidateWorktreeAddClean(t *testing.T) {
    // Setup: clean state
    // Execute: validator.ValidateWorktreeAdd()
    // Assert: CanProceed=true, Issues=[]
}
```

#### Create `test/unit/cleanup_test.go`
```go
func TestCleanupOrphanedDirectory(t *testing.T) {
    // Setup: create orphaned directory
    // Execute: cleanup.CleanupOrphanedDirectory()
    // Assert: directory removed
}

func TestCleanupSafety(t *testing.T) {
    // Setup: directory with uncommitted changes
    // Execute: cleanup.CleanupOrphanedDirectory()
    // Assert: error returned, directory not removed
}
```

#### Create `test/unit/transaction_test.go`
```go
func TestTransactionRollback(t *testing.T) {
    // Setup: transaction with step that fails
    // Execute: tx.Execute()
    // Assert: prior steps rolled back
}

func TestTransactionSuccess(t *testing.T) {
    // Setup: transaction with all successful steps
    // Execute: tx.Execute()
    // Assert: no rollback, all steps completed
}
```

#### Create `test/e2e/recovery_test.go`
```go
func TestRecoveryFromPartialAdd(t *testing.T) {
    // 1. Mock git to fail after worktree creation
    // 2. Attempt add (should fail, leave partial state)
    // 3. Retry add with recovery (should cleanup and succeed)
}

func TestDoctorFix(t *testing.T) {
    // 1. Create orphaned directory manually
    // 2. Run doctor --fix
    // 3. Assert directory cleaned
}
```

## Critical Files

1. **internal/hop/errors.go** (new) - Error types and parsing
2. **internal/hop/validator.go** (new) - State validation logic
3. **internal/hop/cleanup.go** (new) - Cleanup handlers
4. **internal/hop/transaction.go** (new) - Transaction framework
5. **internal/hop/worktree.go** (modify) - Add transactional methods, internal helpers
6. **cmd/add.go** (modify) - Use transactional worktree creation
7. **cmd/remove.go** (modify) - Add validation and cleanup
8. **cmd/doctor.go** (modify) - Add state checks and fixes

## Verification Plan

### Manual Testing
1. Create worktree, kill process mid-operation → retry should recover
2. Manually delete worktree directory → add should detect and cleanup
3. Manually delete git metadata → remove should detect and cleanup
4. Run `git hop doctor` → should detect all issues
5. Run `git hop doctor --fix` → should fix all auto-fixable issues

### Automated Testing
```bash
# Run all unit tests
go test ./internal/hop/...

# Run E2E recovery tests
go test ./test/e2e -run Recovery

# Run doctor tests
go test ./test/e2e -run Doctor
```

### Success Criteria
- ✅ No operation fails due to pre-existing artifacts
- ✅ All failed operations can be retried successfully
- ✅ `doctor --fix` resolves all common state issues
- ✅ Error messages explain what happened and how to fix
- ✅ Test coverage >80% for new code

## Implementation Order

1. **errors.go** → Define error types first
2. **validator.go** → State detection logic
3. **cleanup.go** → Cleanup handlers
4. **transaction.go** → Transaction framework
5. **worktree.go** → Update with transactional methods
6. **add.go, remove.go** → Command integration
7. **doctor.go** → Enhanced validation
8. **Tests** → Unit tests first, then E2E

## Edge Cases Handled

1. ✅ Directory exists but git doesn't know about it → cleanup
2. ✅ Git knows about worktree but directory missing → prune
3. ✅ Uncommitted changes in worktree → prevent cleanup, warn user
4. ✅ Hub and hopspace configs diverged → detect and report
5. ✅ Multiple simultaneous operations → each validates independently
6. ✅ Disk full during operation → rollback, clear error message
7. ✅ Permission denied → clear error with fix instructions

## Backward Compatibility

- Existing operations continue to work
- New validation is additive (warns but doesn't break)
- Transactional methods are opt-in initially
- Can be enabled by default once proven stable
