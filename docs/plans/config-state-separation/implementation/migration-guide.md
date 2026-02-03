# Migration Guide

## Overview

Migrate existing git-hop data to new XDG-based structure. Preserves all user data.

## Migration Tool

Command: `git hop migrate`

**Location:** [cmd/migrate.go](../../cmd/migrate.go) (new)

## Phase 1: Load Old Data

### Read Registry

```go
oldRegistry := LoadRegistry(fs)
```

Validates:
- Old registry.json exists
- Contains repository entries
- No corruption detected

### Check for Data

If no old data:
- Exit with message: No old data found
- Suggest: Run git hop init on repositories

## Phase 2: Initialize New State

```go
newState := State{
    Version: "1.0.0",
    LastUpdated: time.Now(),
    Repositories: make(map[string]RepositoryState),
    Orphaned: []OrphanedEntry{},
}
```

## Phase 3: Repository Migration

```go
func MigrateWorktrees(
    oldRegistry Registry,
    newState *State,
    g GitInterface,
) error
```

**Location:** [MigrateWorktrees](../../internal/hop/migration.go) (new)

### Repository Conversion

For each repository in registry:

1. Extract repo ID (org/repo)
2. Parse git URI for org/repo
3. Determine default branch from git remote
4. Create new RepositoryState entry

### Worktree Scanning

```go
worktrees, err := g.WorktreeList(hubPath)
```

Scan each hub for:
- Bare repository (main)
- All linked worktrees

### Worktree Conversion

For each worktree:

1. Determine type:
   - Parent empty → bare
   - Has parent → linked

2. Create WorktreeState:
   - Path: worktree.Path
   - Type: bare/linked
   - HubPath: hubPath
   - CreatedAt: time.Now()
   - LastAccessed: time.Now()

3. Add to Repositories[repoID].Worktrees

### Hub Conversion

For each repository hub:

1. Create Hub entry:
   - Path: repo.Path
   - Mode: local
   - CreatedAt: time.Now()
   - LastAccessed: time.Now()

2. Add to Repositories[repoID].Hubs

## Phase 4: Hook Migration

### MigrateHooks Function

```go
func MigrateHooks(
    oldRegistry Registry,
    fs afero.Fs,
) error
```

**Location:** [MigrateHooks](../../internal/hop/migration.go) (new)

### Hook Detection

Scan each worktree:

```bash
# Check for git-hop hooks in .git/hooks/
if [ -d ".git/hooks" ]; then
    for hook in pre-worktree-add post-worktree-add; do
        if [ -f ".git/hooks/$hook" ]; then
            # This is a git-hop hook, migrate it
        fi
    done
fi
```

### Hook Migration Steps

1. Create .git-hop/hooks/ directory
2. Move git-hop hooks from .git/hooks/
3. Install wrapper scripts in .git/hooks/
4. Preserve user's native hooks

### Wrapper Script

```bash
#!/usr/bin/env bash
set -e

# Run git-hop hook if available
if command -v git-hop &> /dev/null; then
    git-hop hook run $HOOK_NAME "$@"
fi

exit $?
```

## Phase 5: Backup & Write

### Backup Old Data

```bash
# Create backup directory
BACKUP_DIR="$HOME/.git-hop-backup/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"

# Backup old files
cp ~/.git-hop/registry.json "$BACKUP_DIR/"
cp ~/.git-hop/hops.json "$BACKUP_DIR/" 2>/dev/null || true
```

### Write New State

```go
func SaveNewState(
    fs afero.Fs,
    newState *State,
) error
```

Steps:
1. Write to temporary file
2. Atomic rename to state.json
3. Verify write succeeded

### Move Old Configs

```bash
# Move old configs to .old extensions
mv ~/.git-hop/registry.json ~/.git-hop/registry.json.old
mv ~/.git-hop/hops.json ~/.git-hop/hops.json.old 2>/dev/null || true
```

## Migration Process Flow

See [Migration Process Diagram](../diagrams/migration-process.mmd)

## Migration Summary

After completion, show:

### Repositories Migrated

```
Migrated 3 repositories:
- github.com/jadb/git-hop (2 worktrees)
- github.com/user/project (1 worktree)
```

### Hooks Migrated

```
Migrated 5 hooks:
- pre-worktree-add (2 repos)
- post-worktree-add (3 repos)
```

### Verification Steps

1. Run git hop doctor --fix
2. Verify all worktrees accessible
3. Test git hop add/remove
4. Test git hop org/repo:branch

## Rollback

If migration fails:

1. Restore from backup:
   ```bash
   rm ~/.git-hop/state.json
   cp ~/.git-hop-backup/*/registry.json ~/.git-hop/state.json
   ```

2. Re-run migration
3. Report error for investigation

## Open Questions

1. Migrate hooks automatically or require manual install?
2. Keep old hooks in .git/hooks/ or remove?
3. Validate migrated worktrees exist before adding to state?

## Diagrams

- [Migration Process](../diagrams/migration-process.mmd)
- [Directory Structure](../diagrams/directory-structure.mmd)

## Related

- [Hooks System](hooks-system.md)
- [Worktree Verification](worktree-verification.md)
- [State Schema](../schemas/state-json.md)
