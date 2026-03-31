# User Stories Index

Branch Type Detection System implementation stories.

## Stories

| ID | Title | Status | Priority |
|----|-------|--------|----------|
| [001](001-gitflow-detection.md) | Git-Flow Branch Detection | Completed | High |
| [002](002-extensible-interface.md) | Extensible Detector Interface | Completed | High |
| [003](003-generic-detector.md) | Generic Branch Detection | Completed | Medium |
| [004](004-detector-env-vars.md) | Detector Environment Variables for Hooks | Completed | Medium |
| [005](005-worktree-remove-hooks.md) | Pre/Post Worktree-Remove Hooks | Completed | High |
| [006](006-e2e-testing.md) | E2E Git-Flow Integration Testing | Completed | High |
| [007](007-worktree-move.md) | Worktree Move / Rename | Completed | High |
| [008](008-move-e2e-testing.md) | E2E Move Command Testing | Completed | High |
| [009](009-worktree-merge.md) | Worktree Merge | Completed | High |
| [010](010-doctor-missing-worktrees.md) | Doctor --fix for Missing Worktrees | Completed | High |

## Summary

Implemented extensible branch type detection with:

- **GitFlowNextDetector**: Auto-discovers git-flow-next configuration
- **GenericDetector**: Default prefixes for common workflows
- **Detector Interface**: Pluggable architecture for custom detectors
- **Hook Integration**: Detector env vars available to all hooks
- **New Hooks**: `pre-worktree-remove` and `post-worktree-remove`
- **Comprehensive Testing**: 12 e2e tests + unit tests
