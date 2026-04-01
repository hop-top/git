# User Story: Doctor --fix for Missing Worktrees

**ID:** 010
**Status:** Completed
**Priority:** High
**Epic:** Worktree Lifecycle

## Story

As a developer,
I want `git hop doctor --fix` to detect and resolve worktrees missing from disk,
So that stale state entries are cleaned up automatically or with minimal prompting.

## Acceptance Criteria

- [x] Missing worktrees (in state, not on disk) detected and reported
- [x] Branch merged into default branch → entry auto-deleted; user informed
- [x] Branch not merged → interactive prompt: relocate, delete, or skip
- [x] "Provide new location" validates path exists before updating state
- [x] "Delete the entry" removes entry from state
- [x] "Keep as-is (skip)" leaves state unchanged
- [x] Without `--fix`, issues reported but state not modified
- [x] `findGitDirForRepo` tries hub path first, falls back to hopspace
- [x] `isBranchMerged` uses `git branch --merged <defaultBranch>`
- [x] All tests pass

## Technical Implementation

### `fixMissingWorktrees(fs, g, st)`

Iterates `st.Repositories`; for each worktree whose path is absent:

1. Calls `findGitDirForRepo(fs, repoID, wt.HubPath)` to locate a git dir.
2. If dir found and `isBranchMerged(g, dir, branch, defaultBranch)` → deletes entry, prints info.
3. Otherwise → `output.Select` with three options; handles each accordingly.

Returns count of resolved (deleted or relocated) entries.

### `isBranchMerged(g, dir, branch, defaultBranch)`

Runs `git branch --merged <base>` in `dir`; scans output lines for `branch`.
Falls back to `HEAD` when `defaultBranch` is empty.

### `findGitDirForRepo(fs, repoID, hubPath)`

1. If `hubPath` non-empty and exists on disk → return it.
2. Extract `org/repo` from `repoID` (format: `host/org/repo`).
3. Derive hopspace path via `hop.GetHopspacePath`; return if it exists.
4. Return empty string if neither found.

### Integration in `doctor` command (`cmd/doctor.go`)

Under `=== Checking State ===`, when `doctorFix` is true:

```pseudocode
missingFixed = fixMissingWorktrees(fs, g, st)
worktreesPruned = pruneOrphanedWorktrees(fs, st)
hubsPruned = pruneOrphanedHubs(fs, st)
if any resolved → state.SaveState(fs, st)
```

## Test Coverage

### `test/e2e/doctor_missing_test.go`

| Test | Coverage |
|---|---|
| `TestDoctor_MissingWorktree_Merged_AutoDeleted` | Merged branch + removed dir → auto-delete entry; output contains merge/auto-remove info |
| `TestDoctor_MissingWorktree_Present_NoAction` | Worktree dir exists → no false positive; state unchanged |
| `TestDoctor_NoFix_ReportsIssue` | Without `--fix`, missing worktree reported; state not modified |
