# Implementation Guides

## Documents

- [Hooks System](hooks-system.md)
  - Hook classification, priority system
  - Hook runner implementation
  - Environment variables, best practices

- [Worktree Verification](worktree-verification.md)
  - FindHub algorithm
  - VerifyWorktree function
  - State write operations
  - Error handling

- [Migration Guide](migration-guide.md)
  - Old data migration
  - Hook migration
  - Backup and rollback
  - Migration process

- [Command Integration](command-integration.md)
  - All command updates
  - Hook integration points
  - State.json updates
  - Error handling

## Implementation Order

### Phase 1: Core Structures
- [ ] internal/config/global.go
- [ ] internal/state/state.go
- [ ] internal/hooks/runner.go
- [ ] internal/hop/paths.go updates

### Phase 2: Hook System
- [ ] ExecuteHook function
- [ ] findHookFile function
- [ ] InstallHooks function
- [ ] git hop install-hooks command

### Phase 3: Verification
- [ ] Update FindHub algorithm
- [ ] VerifyWorktree function
- [ ] rescanAndUpdateWorktree function
- [ ] getRepositoryID function

### Phase 4: Migration
- [ ] MigrateWorktrees function
- [ ] MigrateHooks function
- [ ] git hop migrate command

### Phase 5: Commands
- [ ] Update git hop clone
- [ ] Update git hop add
- [ ] Update git hop remove
- [ ] Update git hop env
- [ ] Update git hop list
- [ ] Update git hop org/repo:branch
- [ ] Update git hop prune
- [ ] Update git hop doctor

### Phase 6: Testing
- [ ] Unit tests for hooks
- [ ] Unit tests for verification
- [ ] Integration tests for migration
- [ ] E2E tests for commands

## File References

### New Files
- [internal/config/global.go](../../../../internal/config/global.go)
- [internal/state/state.go](../../../../internal/state/state.go)
- [internal/hooks/runner.go](../../../../internal/hooks/runner.go)
- [cmd/install-hooks.go](../../../../cmd/install-hooks.go)

### Updated Files
- [cmd/add.go](../../../../cmd/add.go)
- [cmd/remove.go](../../../../cmd/remove.go)
- [cmd/env.go](../../../../cmd/env.go)
- [cmd/list.go](../../../../cmd/list.go)
- [cmd/prune.go](../../../../cmd/prune.go)
- [cmd/doctor.go](../../../../cmd/doctor.go)
- [internal/hop/hub.go](../../../../internal/hop/hub.go)
- [internal/hop/worktree.go](../../../../internal/hop/worktree.go)
- [internal/hop/paths.go](../../../../internal/hop/paths.go)

## Diagrams

All implementation flows documented:

- [Hook Priority System](../diagrams/hook-priority-system.mmd)
- [Worktree Verification Flow](../diagrams/worktree-verification-flow.mmd)
- [State Update Flow](../diagrams/state-update-flow.mmd)
- [Migration Process](../diagrams/migration-process.mmd)

## Related

- [Schema Definitions](../schemas/README.md)
- [Main Plan](../README.md)
- [Error Recovery Plan](../../error-recovery.md)
