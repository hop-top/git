# User Story: Worktree Move / Rename

**ID:** 007
**Status:** Completed
**Priority:** High
**Epic:** Worktree Lifecycle

## Story

As a developer,
I want to rename a worktree (and its branch) in one command,
So that I don't have to manually move directories and update all metadata.

## Acceptance Criteria

- [x] `git hop move <new>` renames the current worktree when called from within it
- [x] `git hop move <old> <new>` renames any worktree by branch name when called from anywhere in the hub
- [x] The underlying git branch is renamed (`git branch -m`)
- [x] The worktree directory is moved to the path derived from the new branch name
- [x] Hub config (`hop.json`) is updated: old branch key removed, new key added with new path
- [x] Hopspace config is updated: old branch unregistered, new branch registered
- [x] Global state is updated: old worktree entry removed, new entry added
- [x] Env configs (ports/volumes) are rekeyed from old branch name to new
- [x] The `current` symlink in the hub is updated if it pointed to the old path
- [x] Cannot move/rename the default branch
- [x] Cannot move a worktree to a name that already exists
- [x] `pre-worktree-move` hook runs before the move with `HOP_OLD_BRANCH`, `HOP_NEW_BRANCH`, `HOP_OLD_PATH`, `HOP_NEW_PATH`
- [x] Pre-hook failure blocks the operation
- [x] `post-worktree-move` hook runs after successful move
- [x] Post-hook failure is logged but does not block

## Technical Implementation

### New `GitInterface` methods

```go
WorktreeMove(basePath, oldPath, newPath string) error  // git worktree move
RenameBranch(dir, oldBranch, newBranch string) error   // git branch -m
```

### New `WorktreeManager` method

```go
func (m *WorktreeManager) MoveWorktree(hopspace *Hopspace, hub *Hub, oldBranch, newBranch string, locationPattern, org, repo string) (oldPath, newPath string, err error)
```

Steps:
1. Validate old branch exists, new branch does not, old is not default branch
2. Compute `oldPath` from hub config, `newPath` from location pattern + new branch name
3. Execute `pre-worktree-move` hook
4. `git branch -m old new`
5. `git worktree move oldPath newPath`
6. Update hub config
7. Update hopspace config
8. Update global state
9. Rekey ports/volumes env config
10. Update `current` symlink if needed
11. Execute `post-worktree-move` hook

### New command

`cmd/move.go` — `cobra.RangeArgs(1, 2)`:
- 1 arg: infer old branch from `GetCurrentBranch(cwd)`, require cwd to be inside a known worktree
- 2 args: old=args[0], new=args[1]

### Hook variables

| Variable | Value |
|---|---|
| `GIT_HOP_OLD_BRANCH` | old branch name |
| `GIT_HOP_NEW_BRANCH` | new branch name |
| `GIT_HOP_OLD_PATH` | old worktree path |
| `GIT_HOP_NEW_PATH` | new worktree path |
| Standard vars | `GIT_HOP_REPO_ID`, `GIT_HOP_HUB_PATH` |

## Example Use Cases

### Rename a branch from within the worktree

```bash
cd ~/hubs/myrepo/hops/feature-login
git hop move feature-login-v2
# renames branch, moves dir, updates all metadata
```

### Rename from the hub

```bash
cd ~/hubs/myrepo
git hop move feature-login feature-login-v2
```

### Hook: update external tracking

```bash
#!/bin/bash
# pre-worktree-move
echo "Moving $GIT_HOP_OLD_BRANCH → $GIT_HOP_NEW_BRANCH"
```

## Tests

- Unit: `internal/hop/worktree_test.go` — `TestMoveWorktree_*`
- Unit: `internal/git/wrapper_test.go` — `TestWorktreeMove`, `TestRenameBranch`
- E2E: `test/e2e/move_test.go` — see story 008
