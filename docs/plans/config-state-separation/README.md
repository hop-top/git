# Configuration and State Separation Plan

## Overview

Separate git-hop's configuration (user preferences) from state (repository tracking) per XDG Base Directory spec.

## Goals

1. Clean separation: config vs state
2. XDG compliance for directories
3. Config portability
4. Efficient hub discovery

## Structure

- [Schema Definitions](schemas/README.md)
- [Implementation Guides](implementation/README.md)
- [Diagrams](diagrams/README.md)

## Quick Reference

### Directory Layout

See [Directory Structure Diagram](diagrams/directory-structure.mmd)

### Config Schema

Full schema: [config.json](schemas/config-json.md)

### State Schema

Full schema: [state.json](schemas/state-json.md)

### Implementation

- [Hooks System](implementation/hooks-system.md)
- [Worktree Verification](implementation/worktree-verification.md)
- [Migration Guide](implementation/migration-guide.md)
- [Command Integration](implementation/command-integration.md)

## Migration Phases

### Phase 1: Structures
- [ ] Add internal/config/global.go
- [ ] Add internal/state/state.go
- [ ] Add internal/hooks/runner.go
- [ ] Implement XDG resolution

### Phase 2: Code Updates
- [ ] Migrate LoadGlobalConfig
- [ ] Replace registry.json
- [ ] Update FindHub
- [ ] Update CloneWorktree

### Phase 3: Data Migration
- [ ] Create git hop migrate
- [ ] Scan worktrees
- [ ] Migrate hooks
- [ ] Backup old data

### Phase 4: Commands
- [ ] Update git hop clone
- [ ] Update git hop add
- [ ] Update git hop remove
- [ ] Update git hop list
- [ ] Update git hop org/repo:branch
- [ ] Update git hop prune
- [ ] Update git hop doctor
- [ ] Add git hop install-hooks

## Benefits

1. Config portable
2. State machine-specific
3. XDG compliant
4. Better discovery
5. Multi-hub support
6. Orphan detection
7. Future-proof
8. Hook extensibility
9. Hook portability
10. Priority system

## Open Questions

1. Handle repos without remote?
2. Track worktree statistics?
3. Orphan cleanup aggressiveness?
4. Cache git worktree list?
5. Verification failure behavior?
6. Auto-install hooks?
7. Hook versioning?
8. Hook templates?

## Related

- [Error Recovery Plan](../error-recovery.md)
- [Implementation Status](../implementation-status.md)
