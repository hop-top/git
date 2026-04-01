# User Story: Remove Branch When All Other Worktrees Are Missing

**ID:** 011
**Status:** Completed
**Priority:** High
**Epic:** Worktree Management

## Story

As a developer,
I want `git hop remove <branch>` to succeed cleanly even when all other
tracked worktrees are missing from disk,
So that I can clean up stale hub state without chdir errors or ghost entries.

## Acceptance Criteria

- [x] `git hop remove <branch>` succeeds when no other worktree dirs exist on disk
- [x] No `chdir: no such file or directory` warning is emitted
- [x] `git hop status` does NOT show the removed branch after removal
- [x] Removing the last non-default branch leaves an empty (or absent) branch list
- [x] Hub config (`hop.json`) no longer contains the removed branch after removal

## Root Cause (Discovered)

`cmd/remove.go` picks a `basePath` for git commands by:
1. Looking for `hub.Config.Repo.DefaultBranch` in `hub.Config.Branches`
2. Falling back to any other branch entry

When the hub has no `main` entry (bare-repo hub without a main worktree) and
all other branch entries point to missing directories, the fallback picks a
random Missing path → every git subcommand (`worktree remove`, `branch -D`,
`worktree prune`) fails with `chdir: no such file or directory`.

Fix: fall back to `hubPath` itself (the bare repo) when no live worktree is
available as a working directory.

## Tests

- E2E: `test/e2e/remove_missing_test.go`
  - `TestRemove_AllWorktreesMissing_NoChdir` — covers bug 1
  - `TestRemove_BranchDisappearsFromStatus` — covers bug 2
