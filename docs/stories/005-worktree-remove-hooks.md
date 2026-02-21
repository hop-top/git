# User Story: Pre/Post Worktree-Remove Hooks

**ID:** 005  
**Status:** Completed  
**Priority:** High  
**Epic:** Hooks System

## Story

As a developer,  
I want hooks to run before and after worktree removal,  
So that I can perform cleanup or integration operations.

## Acceptance Criteria

- [x] `pre-worktree-remove` hook runs before worktree removal
- [x] `post-worktree-remove` hook runs after successful removal
- [x] Pre-hook failure blocks the removal operation
- [x] Post-hook failure is logged but doesn't block
- [x] Hooks receive standard environment variables
- [x] Hooks receive detector environment variables if branch type detected

## Technical Implementation

- Added `pre-worktree-remove` and `post-worktree-remove` to `ValidHookNames`
- Updated `cmd/remove.go` to execute hooks at appropriate times
- Integrated with detector manager for branch type awareness

## Hook Execution Order

**On `git hop remove feature/my-feature`:**
1. Pre-remove detector runs (e.g., `git flow feature finish my-feature`)
2. `pre-worktree-remove` hook runs
3. Worktree is removed
4. `post-worktree-remove` hook runs

## Example Use Cases

### Cleanup Temporary Files
```bash
#!/bin/bash
# pre-worktree-remove
cd "$GIT_HOP_WORKTREE_PATH"
rm -rf tmp/* logs/*.log
```

### Notify Team
```bash
#!/bin/bash
# post-worktree-remove
if [ "$GIT_HOP_BRANCH_TYPE" = "release" ]; then
    slack-notify "Release $GIT_HOP_BRANCH_NAME merged and cleaned up"
fi
```

## Tests

- Unit: `internal/hooks/runner_test.go` - `TestValidateHookName`
- E2E: `test/e2e/hooks_gitflow_test.go` - `TestHooks_PreWorktreeRemove_GitFlow`, `TestHooks_PostWorktreeRemove`
- E2E: `test/e2e/gitflow_hooks_test.go` - `TestGitFlowIntegration_PreWorktreeRemove_HookExecution`
