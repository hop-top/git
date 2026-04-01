# User Story: Hidden Admin Commands via --admin Flag

**ID:** 014
**Status:** Completed
**Priority:** Medium
**Epic:** CLI UX

## Story

As a developer,
I want `completion` and `upgrade` hidden from the default help output,
and accessible via a hidden `--admin` flag,
So that the default help stays focused on daily-use commands
while admin/tooling commands remain discoverable.

## Acceptance Criteria

- [x] `git hop --help` does NOT list `completion`, `upgrade`, or `help`
- [x] `git hop --admin` (no subcommand) prints only the hidden admin commands
- [x] `--admin` flag is itself hidden from `--help` output
- [x] `git hop --admin` exits 0
- [x] `git hop <subcommand> --admin` is not a valid flag (admin only on root)
- [x] `completion` and `upgrade` commands still work when invoked directly

## Tests

- E2E: `test/e2e/admin_flag_test.go`
  - `TestHelp_HidesAdminCommands`
  - `TestAdminFlag_ShowsHiddenCommands`
  - `TestAdminFlag_NotAvailableOnSubcommand`
  - `TestCompletion_StillWorksDirectly`
