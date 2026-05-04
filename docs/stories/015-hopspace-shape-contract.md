# User Story: Hopspace Shape Contract

**ID:** 015
**Status:** Completed
**Priority:** High
**Epic:** Clone

## Story

As a developer using `git hop`,
I want every freshly created hopspace to follow the same shape regardless of
which clone path produced it,
So that downstream commands (`add`, `repair`, `merge`, `move`, etc.) operate
on a single, well-defined layout — and so that `git status` at the hub root
is never spuriously dirty.

## Contract

Every `git hop`-managed hopspace MUST satisfy these invariants immediately
after creation:

1. `<hubPath>` is a **bare** git repository
   (`git rev-parse --is-bare-repository` reports `true`).
2. `<hubPath>` contains only git internals (`HEAD`, `config`, `description`,
   `hooks/`, `info/`, `objects/`, `packed-refs`, `refs/`, etc.) plus
   git-hop additions (`hops/`, `hop.json`, `current` symlink). No
   working-tree files leaked from the upstream repo.
3. `<hubPath>/hops/<defaultBranch>/` exists as a regular (non-bare) worktree
   on `<defaultBranch>`, tracking `origin/<defaultBranch>`.
4. `git status --porcelain` in `<hubPath>/hops/<defaultBranch>/` is empty
   (clean working tree).

This applies to both clone paths (`Defaults.BareRepo: true` and `false`) and
to the conversion path (`git hop init`).

## Why this story exists

Two prior commits attempted to make the regular-clone path "work" by
producing a non-bare hopspace with detached HEAD (T-0213, T-0214). Both
shipped because tests asserted *symptoms* (specific files exist, specific
branches not checked out) rather than the *contract* the system must
satisfy.

This story codifies the contract as a single shared assertion battery
(`AssertHopspaceShape` in `test/e2e/hopspace_invariants_test.go`) used by
every test that produces a hopspace.

## Acceptance Criteria

- [x] `AssertHopspaceShape` helper in `test/e2e/hopspace_invariants_test.go`
      asserts all four contract clauses against a hub directory.
- [x] `TestHopspaceShape_AfterClone` exercises `git hop <repo>` end to end
      and runs the battery.
- [x] Existing `TestClone_OutsideRepo` (which previously asserted symptoms
      only) is now backed by the invariant test for full shape coverage.
- [x] `cloneRegularRepo` produces a bare hopspace by delegating to
      `cloneBareRepo`. The `Defaults.BareRepo` flag is effectively a no-op
      and remains only for persisted-config compatibility.
- [x] `cloneBareRepo` re-establishes the standard `+refs/heads/*:refs/remotes/origin/*`
      fetch refspec after `git clone --bare` and re-fetches, so
      `setUpstreamTracking` against `origin/<defaultBranch>` works
      regardless of upstream URL scheme (file path, ssh, https).

## Tests

- `test/e2e/hopspace_invariants_test.go`
  - `AssertHopspaceShape(t, hubPath, defaultBranch)` — shared post-condition
    battery.
  - `TestHopspaceShape_AfterClone` — full e2e through `git hop <repo>`.

## Reverts / supersedes

- T-0213 (commit `4a6c30e`) — wrong-shape attempt: detached non-bare clone
  at root + worktree at hops/main.
- T-0214 (commit `fc1f15e`) — partial follow-up to T-0213; same wrong shape.

Both are superseded by this story's commit, which restores the bare-only
contract.

## Pre-existing follow-ups (out of scope)

- `git hop repair -n` shorthand bug (revealed when this fix made
  `setupRepairEnv` no longer skip): unknown shorthand flag `n`. Track
  separately.
