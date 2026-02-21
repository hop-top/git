# User Story: Detector Environment Variables for Hooks

**ID:** 004  
**Status:** Completed  
**Priority:** Medium  
**Epic:** Branch Type Detection System

## Story

As a hook author,  
I want access to detected branch type information in my hooks,  
So that I can write branch-type-aware automation.

## Acceptance Criteria

- [x] Hooks receive `GIT_HOP_BRANCH_TYPE` when branch type detected
- [x] Hooks receive `GIT_HOP_BRANCH_NAME` (name without prefix)
- [x] Hooks receive `GIT_HOP_BRANCH_PREFIX` (matched prefix)
- [x] Hooks receive `GIT_HOP_BRANCH_PARENT` (parent branch)
- [x] Hooks receive `GIT_HOP_DETECTOR_SOURCE` (which detector matched)
- [x] Variables are empty/absent when no branch type detected

## Environment Variables

| Variable                      | Description                        | Example        |
|-------------------------------|------------------------------------|----------------|
| `GIT_HOP_BRANCH_TYPE`         | Detected branch type               | `feature`      |
| `GIT_HOP_BRANCH_NAME`         | Branch name without prefix         | `my-feature`   |
| `GIT_HOP_BRANCH_PREFIX`       | Matched prefix                     | `feature/`     |
| `GIT_HOP_BRANCH_PARENT`       | Parent branch                      | `develop`      |
| `GIT_HOP_BRANCH_START_POINT`  | Branch to start from               | `develop`      |
| `GIT_HOP_DETECTOR_SOURCE`     | Which detector matched             | `gitflow-next` |

## Example Hook

```bash
#!/bin/bash
# post-worktree-add - Branch-type-aware setup

if [ "$GIT_HOP_BRANCH_TYPE" = "feature" ]; then
    echo "Setting up feature branch: $GIT_HOP_BRANCH_NAME"
    cd "$GIT_HOP_WORKTREE_PATH"
    npm run setup:feature
elif [ "$GIT_HOP_BRANCH_TYPE" = "release" ]; then
    echo "Setting up release branch: $GIT_HOP_BRANCH_NAME"
    cd "$GIT_HOP_WORKTREE_PATH"
    npm run setup:release
fi
```

## Technical Implementation

- `Manager.GetDetectorEnvVars()` converts `BranchTypeInfo` to env vars
- `hooks.Runner.ExecuteHookWithDetector()` merges detector env with base env
- Updated `cmd/add.go` and `cmd/remove.go` to pass detector env to hooks

## Tests

- Unit: `internal/detector/detector_test.go` - `TestManager_GetDetectorEnvVars`
- E2E: `test/e2e/hooks_gitflow_test.go` - `TestHooks_EnvironmentVariables`
