# User Story: E2E Move Command Testing

**ID:** 008
**Status:** Completed
**Priority:** High
**Epic:** Worktree Lifecycle

## Story

As a maintainer,
I want comprehensive e2e tests for the `git hop move` command,
So that all rename scenarios are verified to work correctly end-to-end.

## Acceptance Criteria

- [x] Test: 2-arg form renames a worktree from outside it
- [x] Test: 1-arg form renames the current worktree when called from within it
- [x] Test: git branch is renamed after move
- [x] Test: worktree directory is at new path, old path gone
- [x] Test: hub config reflects new branch name and path
- [x] Test: `current` symlink updated when it pointed to moved worktree
- [x] Test: default branch cannot be moved (error)
- [x] Test: move to an already-existing branch name fails (error)
- [x] Test: 1-arg form fails gracefully when not inside a known worktree
- [x] Test: `pre-worktree-move` hook runs with correct env vars
- [x] Test: failing `pre-worktree-move` hook blocks the move
- [x] Test: `post-worktree-move` hook runs after successful move
- [x] Test: failing `post-worktree-move` hook is logged but move still succeeds
- [x] All tests pass

## Test Coverage

### `test/e2e/move_test.go`

| Test | Coverage |
|---|---|
| `TestMove_TwoArg_RenamesWorktree` | 2-arg rename: branch, dir, hub config all updated |
| `TestMove_OneArg_FromInsideWorktree` | 1-arg rename: detects current branch from cwd |
| `TestMove_OneArg_FailsOutsideWorktree` | Error when cwd is not inside any known worktree |
| `TestMove_DefaultBranch_Blocked` | Cannot rename the default branch |
| `TestMove_NewNameAlreadyExists_Blocked` | Cannot rename to an existing branch name |
| `TestMove_UpdatesCurrentSymlink` | `current` symlink updated when it targeted the moved worktree |
| `TestMove_CurrentSymlink_UnrelatedNotChanged` | `current` symlink unchanged when pointing elsewhere |
| `TestMove_PreHook_EnvVars` | `pre-worktree-move` receives `HOP_OLD_BRANCH`, `HOP_NEW_BRANCH`, `HOP_OLD_PATH`, `HOP_NEW_PATH` |
| `TestMove_PreHook_Failure_Blocks` | Failing pre-hook prevents move; old state preserved |
| `TestMove_PostHook_RunsAfterSuccess` | `post-worktree-move` hook runs on success |
| `TestMove_PostHook_Failure_NonFatal` | Failing post-hook does not cause error exit; move is complete |
| `TestMove_GitBranchRenamed` | `git branch --list` shows new name, old name gone |
| `TestMove_OldPathGone_NewPathExists` | Filesystem state correct after move |
