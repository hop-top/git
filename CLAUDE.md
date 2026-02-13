# Development Notes

## Git Interface

The `git.Git` implementation has been refactored to use the `git.GitInterface` interface for better testability.

**Architecture:**

- `git.GitInterface`: Defines all Git operations
- `git.Git`: Real implementation using `os/exec`
- `mocks.MockGit`: Test implementation in `test/mocks/mock_git.go`

**Usage:**

All functions that need Git operations now accept `git.GitInterface` instead of `*git.Git`. This allows easy mocking in tests:

```go
// Production code
g := git.New()
manager := hop.NewWorktreeManager(fs, g)

// Test code
mockGit := mocks.NewMockGit()
manager := hop.NewWorktreeManager(fs, mockGit)  // ✅ Works!
```

**Benefits:**

- Unit tests can use `MockGit` without actual Git commands
- Better separation of concerns
- Easier to test edge cases and error conditions
- No need for integration tests for simple unit testing
