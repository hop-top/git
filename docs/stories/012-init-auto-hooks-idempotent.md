# User Story: Init Auto-Installs Hooks and Is Idempotent

**ID:** 012
**Status:** Completed
**Priority:** High
**Epic:** Repository Initialization

## Story

As a developer,
I want `git hop init` to automatically create the `.git-hop/hooks/` directory
and succeed without error when run again inside an already-initialized repo,
So that hooks are always ready to use and re-running init is safe.

## Acceptance Criteria

- [x] `git hop init` creates `.git-hop/hooks/` in the main worktree (bare) or
  repo root (regular/register-as-is)
- [x] Post-init stdout names the hooks dir and lists available hook names
- [x] Re-running `git hop init` inside a `BareWorktreeRoot` prints
  "already initialized" and exits 0
- [x] Re-running inside a linked `WorktreeChild` also exits 0
- [x] Idempotent run ensures `.git-hop/hooks/` still exists (creates if absent)

## Tests

- Unit: `cmd/init_hooks_test.go`
  - `TestHooksInstalledAfterRegisterAsIs`
  - `TestHooksInstalledAfterBareConversion`
  - `TestHooksInstalledAfterRegularConversion`
  - `TestHooksInstallFailsGracefully`
- Unit: `cmd/init_idempotent_test.go`
  - `TestHandleAlreadyInitialized_BareWorktreeRoot`
  - `TestHandleAlreadyInitialized_WorktreeChild`
- E2E: `test/e2e/init_hooks_test.go`
  - `TestInit_CreatesHooksDir`
  - `TestInit_Idempotent_AlreadyInitialized`
  - `TestInit_Idempotent_EnsuresHooksDirExists`
