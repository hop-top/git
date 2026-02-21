# User Story: Extensible Detector Interface

**ID:** 002  
**Status:** Completed  
**Priority:** High  
**Epic:** Branch Type Detection System

## Story

As a team using a custom workflow,  
I want to plug custom branch type detectors into git-hop,  
So that our non-git-flow workflow is also supported.

## Acceptance Criteria

- [x] Detector interface allows custom implementations
- [x] Detectors are registered with a Manager
- [x] Detectors are sorted by priority
- [x] First matching detector wins
- [x] Future detectors can be added without modifying core code

## Technical Implementation

- Created `internal/detector/detector.go` with `Detector` interface
- Interface methods: `Name()`, `Priority()`, `IsAvailable()`, `Detect()`, `OnAdd()`, `OnRemove()`
- Manager coordinates multiple detectors
- Priority sorting ensures deterministic detection order

## Interface Definition

```go
type Detector interface {
    Name() string
    Priority() int
    IsAvailable(repoPath string) bool
    Detect(branch string, repoPath string) (*BranchTypeInfo, error)
    OnAdd(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error
    OnRemove(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error
}
```

## Tests

- Unit: `internal/detector/detector_test.go` - `TestManager_Register`
