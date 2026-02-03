# Test Consolidation Plan

## Goal

Migrate all unit tests from `test/unit/` to co-located test files in their respective package directories, following Go best practices and matching the pattern already established in `internal/` packages.

## Background

The project currently has mixed test organization:
- ✓ `internal/` packages: Tests are co-located (e.g., `internal/config/config_test.go`)
- ✗ `cmd/` tests: Located in `test/unit/` (e.g., `test/unit/cmd_doctor_test.go`)
- ✓ New state/hooks/migrate tests: Co-located (correct pattern)

Co-located tests are the Go community standard because they:
- Make it immediately clear what's tested
- Work better with Go tooling (`go test ./...`)
- Keep related code together for easier maintenance
- Allow testing both public API (`package_test`) and internals (same package name)

## Current State

### Files in test/unit/ to migrate:
```
test/unit/
├── cmd_doctor_test.go          → cmd/doctor_test.go
├── cmd_env_test.go             → cmd/env_test.go
├── cmd_helpers_test.go         → cmd/helpers_test.go (or inline into specific cmd tests)
├── cmd_list_test.go            → cmd/list_test.go (merge with existing)
├── cmd_prune_test.go           → cmd/prune_test.go (merge with existing)
├── cmd_remove_test.go          → cmd/remove_test.go
├── deps_registry_test.go       → internal/services/deps_registry_test.go
├── hopspace_init_test.go       → internal/hop/hopspace_init_test.go
├── logger_test.go              → internal/output/logger_test.go
├── package_managers_*.go       → internal/services/package_managers_test.go (consolidate)
├── paths_test.go               → internal/hop/paths_test.go
├── stash_test.go               → internal/hop/stash_test.go
├── cli_shorthand_test.go       → cmd/cli_shorthand_test.go or internal/cli/
├── backup_test.go              → internal/hop/backup_test.go or cmd/migrate_test.go
└── clone_hopspace_integration_test.go → Keep in test/unit or move to test/integration
```

### Files already co-located (keep as-is):
```
cmd/
├── add_state_test.go           ✓
├── doctor_state_test.go        ✓
├── install_hooks_test.go       ✓
├── list_test.go                ✓
└── prune_test.go               ✓

internal/config/
├── config_test.go              ✓
├── global_test.go              ✓
└── schema_config_test.go       ✓

internal/hooks/
└── runner_test.go              ✓

internal/hop/
├── migrate_test.go             ✓
└── verify_test.go              ✓

internal/state/
└── state_test.go               ✓
```

## Migration Steps

### Phase 1: Identify Duplicates
1. Check for overlapping tests between `test/unit/cmd_list_test.go` and new `cmd/list_test.go`
2. Check for overlapping tests between `test/unit/cmd_prune_test.go` and new `cmd/prune_test.go`
3. Identify any tests that depend on shared helpers in `test/unit/cmd_helpers_test.go`

### Phase 2: Migrate Simple Cases
Move tests that have no dependencies or conflicts:

```bash
# Example migration
git mv test/unit/cmd_doctor_test.go cmd/doctor_test.go
git mv test/unit/cmd_env_test.go cmd/env_test.go
git mv test/unit/cmd_remove_test.go cmd/remove_test.go
git mv test/unit/deps_registry_test.go internal/services/deps_registry_test.go
git mv test/unit/logger_test.go internal/output/logger_test.go
git mv test/unit/paths_test.go internal/hop/paths_test.go
git mv test/unit/stash_test.go internal/hop/stash_test.go
```

After each move:
- Update package declarations if needed
- Update import paths if needed
- Run `go test` to verify tests still pass

### Phase 3: Merge Duplicates
For files with duplicates (list, prune):

1. **cmd/list_test.go**:
   - Currently has basic state-focused tests
   - `test/unit/cmd_list_test.go` likely has more comprehensive CLI tests
   - Merge both into `cmd/list_test.go`, keeping all unique test cases

2. **cmd/prune_test.go**:
   - Currently has unit tests for prune functions
   - `test/unit/cmd_prune_test.go` likely has CLI integration tests
   - Merge both into `cmd/prune_test.go`

### Phase 4: Handle Helpers
Review `test/unit/cmd_helpers_test.go`:
- If helpers are used by multiple cmd tests, create `cmd/testing.go` (not `_test.go`) with shared utilities
- If helpers are command-specific, inline them into the specific test files
- Test helpers can be in non-`_test.go` files to be shared across test files

### Phase 5: Consolidate Package Manager Tests
Merge all `package_managers_*.go` tests into `internal/services/package_managers_test.go`:
```
test/unit/package_managers_test.go
test/unit/package_managers_config_test.go
test/unit/package_managers_hash_test.go
test/unit/package_managers_override_test.go
```

### Phase 6: Handle Special Cases

1. **backup_test.go**: Determine if it tests backup functionality in migrate or a general backup utility
2. **cli_shorthand_test.go**: Move to `cmd/` or `internal/cli/` depending on what it tests
3. **clone_hopspace_integration_test.go**: This is an integration test, could stay in `test/` but rename directory to `test/integration/` for clarity

### Phase 7: Update test/unit Directory
After migration:
```
test/
├── e2e/                    # End-to-end tests (keep)
├── integration/            # Integration tests (rename from unit if any remain)
└── mocks/                  # Test mocks (keep)
```

Remove empty `test/unit/` directory.

## Testing Strategy

For each migration:
1. Move file(s)
2. Update package declarations and imports
3. Run `go test ./...` to ensure all tests pass
4. Run `go test -race ./...` to check for race conditions
5. Commit with clear message: `test: migrate X tests to co-located pattern`

## Rollback Plan

If issues arise:
```bash
git revert <commit-hash>
```

Each migration should be a separate commit for easy rollback.

## Success Criteria

- [ ] All unit tests moved from `test/unit/` to package directories
- [ ] No duplicate tests between old and new locations
- [ ] All tests passing: `go test ./...`
- [ ] No race conditions: `go test -race ./...`
- [ ] Test coverage maintained or improved
- [ ] `test/unit/` directory removed
- [ ] Documentation updated if needed

## Timeline

- Phase 1 (Identify): 30 minutes
- Phase 2 (Simple moves): 1 hour
- Phase 3 (Merge duplicates): 1 hour
- Phase 4 (Helpers): 30 minutes
- Phase 5 (Package managers): 1 hour
- Phase 6 (Special cases): 1 hour
- Phase 7 (Cleanup): 15 minutes

**Total estimated time: 5-6 hours**

## Notes

- This work should be done in a separate branch
- Create PR for review before merging
- May discover additional test coverage gaps during migration
- Good opportunity to improve test organization and coverage
