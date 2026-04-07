# Testing

git-hop's tests fall into three tiers, each with a different cost,
purpose, and CI lane.

## Tiers

### 1. Unit + fast integration

Live in `internal/<pkg>` and run on every PR via `.github/workflows/ci.yml`.

- Pure-Go, no network, no docker, no shell escapes beyond local `git`.
- Run by default with `go test ./...` (no build tags).
- Sub-second per test; whole suite under 30s.
- Required for PR merge.

```sh
go test ./...                       # whole tree, default
go test ./internal/hop/             # one package
go test -run TestDetectRepoStructure ./internal/hop/
```

### 2. End-to-end (e2e), local-git only

Live in `test/e2e/*.go` (NOT `test/e2e/docker/`). They drive the
compiled `git-hop` binary as a subprocess and assert on filesystem
state, stdout, and exit codes.

- Use real local `git`, no docker, no network.
- Run by default with `go test ./...` (no build tags).
- 1-2 seconds per test; whole suite ~2 minutes.
- Required for PR merge.

```sh
go test ./test/e2e/                 # all e2e
go test -run TestInit_ ./test/e2e/  # init suite only
```

`SetupTestEnv` (in `test/e2e/utils.go`) builds the `git-hop` binary
once per test, creates a temp dir, and overrides `HOME`,
`GIT_HOP_DATA_HOME`, and the `XDG_*` vars in BOTH the parent test
process (via `t.Setenv`) and the child binary (via `cmd.Env`). This
isolation is mandatory: tests that read global state via the parent
process MUST see the same per-test paths the child binary writes to.

### 3. Docker e2e

Live in `test/e2e/docker/*.go` and `test/e2e/e2e_test.go`. They boot
real `docker compose` stacks, allocate real ports, persist real
volumes, and make real HTTP calls to running containers.

- Require a local docker daemon and docker compose v2.
- Gated behind `//go:build dockere2e` — **excluded from default `go test ./...`**.
- 1-3 minutes per test; whole suite 5-10 minutes.
- **Not required for PR merge.** Run nightly via
  `.github/workflows/dockere2e.yml`, manually via `workflow_dispatch`,
  or per-PR by labeling with `needs:docker-tests`.

```sh
go test -tags dockere2e ./test/e2e/docker/...
go test -tags dockere2e -run TestDockerIsolation_PortIsolation ./test/e2e/docker/...
go test -tags dockere2e ./test/e2e/                # also includes the gated e2e_test.go
```

These tests assert on real OS-allocated state (port numbers, container
IDs, volume directory contents, HTTP responses) which cannot be
deterministically replayed via cassettes. Build-tag gating is the
correct trade-off: PR CI stays fast, regressions surface nightly.

## CI workflows

| Workflow | Trigger | Tag | Required |
|---|---|---|---|
| `ci.yml` Build & Test | push, pull_request | none | yes |
| `ci.yml` Build Matrix (linux/mac/win) | push, pull_request | none | yes |
| `dockere2e.yml` Docker E2E | nightly cron, workflow_dispatch, PR label `needs:docker-tests` | `dockere2e` | no |

The default PR run executes everything in tiers 1 and 2. The
`dockere2e.yml` workflow exists so docker regressions get caught
without slowing every PR down.

## xrr-aware test runtime

The `git-hop` binary includes an opt-in cross-process xrr seam (see
`internal/xrrx/install.go`). When `XRR_MODE` and `XRR_CASSETTE_DIR`
are both set, every internal `git`/`docker` invocation flows through
an `xrrx.Runner` that records or replays interactions to/from the
cassette directory.

| Var | Values | Effect |
|---|---|---|
| `XRR_MODE` | `record` \| `replay` \| `passthrough` \| `off` (or unset) | session mode; `off`/unset = production default |
| `XRR_CASSETTE_DIR` | absolute path | required when `XRR_MODE` is set; cassette read/write root |

Misconfiguration (mode set without dir, or invalid mode) makes the
binary exit 2 with a clear stderr message. The seam is intentionally
fail-loud so a misconfigured test harness cannot silently fall back
to live calls.

This is **infrastructure only** — git-hop's current test suites do not
record cassettes (see `git-test-determinism` track T-0017 for the
rationale: target tests are already fast and use only local `git`,
making the cassette overhead net-negative). The seam exists for future
test classes that hit slow or expensive APIs whose responses are
deterministic.

## Common gotchas

- **CI env vars leaking in.** Tests under `internal/shell/` consult
  `CI`, `HOP_NO_SHELL_INTEGRATION`, and `HOP_WRAPPER_ACTIVE`. Test
  fixtures explicitly clear these via `t.Setenv("...", "")` before
  applying case-specific overrides. Don't add a fixture that omits
  this and expects "interactive" behavior — it'll pass locally and
  fail in CI.
- **Parent vs child state.** Any e2e test that calls
  `state.LoadState(afero.NewOsFs())` directly reads the parent test
  process's environment. `SetupTestEnv` already mirrors the right
  env vars into the parent via `t.Setenv`; if you write a new e2e
  test that bypasses `SetupTestEnv`, you have to do this yourself.
- **`go build` per test.** `SetupTestEnv` rebuilds `main.go` for
  each test, which dominates per-test cost (~700ms each). If a test
  doesn't need a fresh binary, find a way to reuse the parent's
  `git-hop` build instead.
- **Worktree leaks.** Tests that crash mid-run leave temp worktrees
  on disk under `os.TempDir()`. The `t.Cleanup` registered in
  `SetupTestEnv` removes them on normal exit; for crashes,
  periodically `find $TMPDIR -name 'git-hop-e2e-*' -type d -mtime +1 -exec rm -rf {} +`.

## Adding a new test

1. Decide the tier first (unit, e2e local-git, docker e2e). Most new
   tests belong in tier 1 — only escalate if you genuinely need a
   spawned binary or a real docker daemon.
2. For tier 2: import `hop.top/git/test/e2e` and use `SetupTestEnv`.
3. For tier 3: place the file under `test/e2e/docker/`, start the
   file with `//go:build dockere2e`, and use the helpers in
   `test/e2e/docker/docker_helpers.go`.
4. Run locally before pushing. For tier 3 tests, ensure docker is up
   and you have at least 2GB free RAM for compose stacks.
