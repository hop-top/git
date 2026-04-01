# User Story: Worktree Merge

**ID:** 009
**Status:** Completed
**Priority:** High
**Epic:** Worktree Lifecycle

## Story

As a developer,
I want to merge a feature worktree branch into a receiving branch via `git hop merge`,
So that the merge, cleanup, and symlink update happen atomically without manual steps.

## Acceptance Criteria

- [x] 2-arg form: `git hop merge <source> <into>` merges source into into-branch
- [x] 1-arg form: `git hop merge <into>` infers source from cwd branch
- [x] Source worktree directory removed after merge
- [x] Into-branch worktree remains intact
- [x] `hop.json` no longer contains the source branch after merge
- [x] `current` symlink updated to point to into-branch worktree
- [x] `--no-ff` flag forces merge commit
- [x] Same-branch guard: `git hop merge main main` exits with error
- [x] Source branch not in hub guard: exits with error
- [x] 2-arg form: cannot be run from inside the source worktree
- [x] Source worktree must be clean (no uncommitted changes) before merge
- [x] Local and remote source branch deleted after merge
- [x] Hopspace stale data pruned
- [x] Global state updated

## Technical Implementation

- `cmd/merge.go`: cobra command with `RangeArgs(1, 2)`
- 1-arg: calls `g.GetCurrentBranch(cwd)`, verifies cwd is inside source worktree
- 2-arg: source and into from args; guards against running from inside source worktree
- Merge: `g.RunInDir(intoPath, "git", "merge", [--no-ff], sourceBranch)`
- Cleanup: `g.WorktreeRemove`, `fs.RemoveAll`, `hub.RemoveBranch`
- Branch deletion: `g.DeleteLocalBranch`, `g.DeleteRemoteBranch` (if remote exists)
- Symlink: `hop.UpdateCurrentSymlink(fs, hubPath, intoPath)`
- State: `state.RemoveWorktree`, hopspace `UnregisterBranch` + prune

## Test Coverage

### `test/e2e/merge_test.go`

| Test | Coverage |
|---|---|
| `TestMerge_TwoArg_MergesAndCleansUp` | 2-arg merge: source dir gone, hub config updated, current symlink correct |
| `TestMerge_OneArg_FromInsideWorktree` | 1-arg: infers source from cwd, merge succeeds |
| `TestMerge_SourceDirRemoved` | Source worktree directory physically absent after merge |
| `TestMerge_IntoBranchWorktreeIntact` | Into-branch worktree still exists and accessible |
| `TestMerge_HubConfigUpdated` | `hop.json` has no source branch entry after merge |
| `TestMerge_CurrentSymlinkUpdated` | `current` symlink resolves to into-branch path |
| `TestMerge_SameBranch_Blocked` | `git hop merge main main` exits non-zero with error message |
| `TestMerge_SourceNotInHub_Blocked` | Non-existent source branch exits with error |
| `TestMerge_NoFF_Flag` | `--no-ff` produces a merge commit in receiving branch |
