# Development Notes

## Testing with MockGit

MockGit in `test/mocks/mock_git.go` is a concrete struct, not interface. Cannot be used directly with functions expecting `*git.Git`.

**Workaround options:**

1. **Refactor to interface**: Create `git.GitInterface`, make `Git` and `MockGit` implement it
2. **Integration tests**: Test actual Git operations with real repos in temp dirs
3. **Wrapper pattern**: Create test-specific wrappers accepting interface

**Current limitation**: Unit tests requiring MockGit cannot test `WorktreeManager` directly. Use integration tests or manual testing for worktree operations.

**Example of what doesn't work:**
```go
mockGit := mocks.NewMockGit()
manager := hop.NewWorktreeManager(fs, mockGit)  // ❌ Type mismatch
```

**TODO**: Consider refactoring `git.Git` to interface for better testability.
