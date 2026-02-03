# Worktree Verification

## Overview

Verify and correct worktree paths in state.json. Handles moved/deleted worktrees.

## FindHub Algorithm

```go
func FindHub(
    fs afero.Fs,
    g GitInterface,
    startPath string,
) (string, error)
```

**Location:** [FindHub](../../internal/hop/hub.go) (updated)

### Steps

1. Search up directory tree for .git
2. Check same level for hop.json (local hub)
3. Get repository ID from git remote or PWD
4. Look up repository in state.json
5. Find matching hub (ancestor check)
6. Update lastAccessed timestamp

See [State Update Flow](../diagrams/state-update-flow.mmd)

## VerifyWorktree Function

```go
func VerifyWorktree(
    state *State,
    repoID string,
    branch string,
    g GitInterface,
) (string, error)
```

**Location:** [VerifyWorktree](../../internal/hop/worktree.go) (updated)

### Verification Steps

1. Look up worktree in state.json
2. Verify path exists on filesystem
3. Verify valid git worktree via git worktree list
4. Update lastAccessed if valid
5. Rescan if invalid

### Path Missing Scenario

If path does not exist:
- Run rescan operation
- git worktree list on hub path
- Find matching branch
- Update state.json with new path
- Return updated path

### Git Validation Fails

If git worktree list doesn't find branch:
- Return error: worktree not found
- User must create worktree first

## rescanAndUpdateWorktree Function

```go
func rescanAndUpdateWorktree(
    state *State,
    repoID string,
    branch string,
    g GitInterface,
) (string, error)
```

**Location:** [rescanAndUpdateWorktree](../../internal/hop/worktree.go) (new)

### Rescan Steps

1. Get hub path from repo state
2. Run git worktree list on hub
3. Iterate through worktrees
4. Find matching branch name
5. Update Worktrees map with new path
6. Save state.json
7. Return new path

## getRepositoryID Function

```go
func getRepositoryID(
    g GitInterface,
    gitDir string,
    fallbackPath string,
) string
```

### ID Resolution Priority

1. Try git remote origin
   - Parse: git@github.com:org/repo.git
   - Extract: github.com/org/repo

2. Fallback to directory structure
   - Parse: /path/to/org/repo
   - Use default domain from config
   - Return: github.com/org/repo

3. Return empty if resolution fails

## State.json Write Operations

### Atomic Write Pattern

```go
func SaveState(fs afero.Fs, state *State) error {
    // 1. Write to temp file
    tmpPath := filepath.Join(GetStateHome(), "state.json.tmp")
    if err := writeJSON(tmpPath, state); err != nil {
        return err
    }
    
    // 2. Atomic rename
    if err := os.Rename(tmpPath, statePath); err != nil {
        return err
    }
    
    return nil
}
```

Benefits:
- No partial writes on crash
- Always valid state file
- Safe concurrent access

### Update Operations

See [State Update Flow](../diagrams/state-update-flow.mmd)

Operations that write state:
- git hop add: Add worktree entry
- git hop remove: Delete worktree entry
- git hop clone: Add repository entry
- git hop org/repo:branch: Update lastAccessed
- git hop verify: Re-scan worktrees

## Error Handling

### Repo Not Found

```
Error: repository not registered with git-hop: github.com/org/repo
```

Action: Run git hop clone first

### Branch Not Found

```
Error: worktree not found: feature-x
```

Action: Run git hop add feature-x

### Verification Failure

Multiple consecutive failures:
- Mark worktree as invalid in state
- Remove from worktrees map
- Log error to orphaned array
- User intervention required

## Diagrams

- [Worktree Verification Flow](../diagrams/worktree-verification-flow.mmd)
- [State Update Flow](../diagrams/state-update-flow.mmd)

## Related

- [Hooks System](hooks-system.md)
- [Migration Guide](migration-guide.md)
- [State Schema](../schemas/state-json.md)
