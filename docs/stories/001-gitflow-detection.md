# User Story: Git-Flow Branch Detection

**ID:** 001  
**Status:** Completed  
**Priority:** High  
**Epic:** Branch Type Detection System

## Story

As a developer using git-flow-next,  
I want git-hop to automatically detect my branch types,  
So that worktree operations integrate seamlessly with my git-flow workflow.

## Acceptance Criteria

- [x] Git-hop detects branch types by reading git-flow-next configuration
- [x] Detection works for standard types: feature, release, hotfix, support
- [x] Detection works for custom branch types configured in git-flow
- [x] `git hop add feature/my-feature` runs `git flow feature start my-feature`
- [x] `git hop remove feature/my-feature` runs `git flow feature finish my-feature`
- [x] Non-git-flow branches work normally without git-flow commands

## Technical Implementation

- Created `internal/detector/gitflow_next.go` with `GitFlowNextDetector`
- Reads `gitflow.branch.*.prefix` config via `git config --get-regexp`
- Handles longest-prefix matching for overlapping prefixes
- Falls back gracefully when git-flow not initialized

## Tests

- Unit: `internal/detector/detector_test.go` - `TestGitFlowNextDetector_*`
- E2E: `test/e2e/gitflow_hooks_test.go` - `TestGitFlowIntegration_*`

## Documentation

- Updated `docs/hooks.md` with git-flow integration section
