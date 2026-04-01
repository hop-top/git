# User Story: Init --no-hooks and --enable-chdir Flags

**ID:** 013
**Status:** Completed
**Priority:** High
**Epic:** Repository Initialization

## Story

As a developer,
I want control over what `git hop init` sets up automatically,
So that I can skip hooks in CI environments and opt into shell integration
during initial setup without a separate command.

## Acceptance Criteria

- [x] `git hop init --no-hooks` skips `.git-hop/hooks/` creation
- [x] `git hop init` (without `--no-hooks`) creates `.git-hop/hooks/`
- [x] `git hop init --enable-chdir` installs shell wrapper for auto-cd
- [x] `git hop init` (without `--enable-chdir`) does NOT modify shell RC files
- [x] Idempotent run with `--no-hooks` does not create hooks dir
- [x] Idempotent run without `--no-hooks` ensures hooks dir exists
- [x] `install-hooks`, `install-shell-integration`, `uninstall-shell-integration`
  commands are removed

## Tests

- Unit: `cmd/init_flags_test.go`
  - `TestNoHooksFlag_SkipsHookInstall`
  - `TestNoHooksFlagAbsent_InstallsHooks`
  - `TestEnableChdirFlag_InstallsShellIntegration`
  - `TestEnableChdirFlagAbsent_SkipsShellIntegration`
  - `TestIdempotentRun_NoHooksFlag_DoesNotInstallHooks`
  - `TestIdempotentRun_NoFlagsInstallsHooks`
- E2E: `test/e2e/init_flags_test.go`
  - `TestInit_NoHooks_SkipsHooksDir`
  - `TestInit_EnableChdir_InstallsShellWrapper`
  - `TestInit_DefaultsNoShellIntegration`
