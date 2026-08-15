# Testing

git-hop's tests fall into four tiers, each with a different cost,
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

### 3. Scripted e2e (`.txtar`)

Live in `test/script/testdata/*.txtar` and run the real CLI against real
git repositories, driven by
[`rogpeppe/go-internal/testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript).

- Use real local `git`, no docker, no network.
- Run by default with `go test ./...` (no build tags).
- ~1 second per script; whole suite under 10s.
- Required for PR merge.

Prefer this tier over tier 2 for any new end-to-end case. A script says
in a dozen readable lines what a Go e2e test needs a hundred lines of
process plumbing to say, and it reads like the session a user would type.
Reach for tier 2 only when the assertion genuinely needs Go — parsing
JSON state, comparing structs, or driving concurrency.

```sh
go test ./test/script/                                     # all scripts
go test ./test/script/ -run 'TestScripts/add_branch_slash' # one script
go test -v ./test/script/                                  # show each command as it runs
```

The subtest name is the filename without its extension, so
`testdata/repair_base_inference.txtar` runs as
`TestScripts/repair_base_inference`.

#### How the binary gets wired in

`TestMain` registers the production entry point as an in-script command
via `testscript.Main`, so scripts invoke `git-hop ...` directly. There is
no `go build` step and no binary artifact to go stale: the command runs
this package's own build of `cmd.Execute`, in a separate process so it is
free to call `os.Exit`. `testdata/harness_binary.txtar` guards that
wiring.

`Setup` (in `test/script/script_test.go`) redirects `HOME`, the `XDG_*`
dirs, `GIT_HOP_DATA_HOME`, and the git global config under each script's
own `$WORK`. Without it a script would allocate ports and write worktrees
into your real hopspace.

#### Debugging a failing script

On failure testscript prints the commands from the most recent `#` comment
onward, plus the captured output. Two flags help:

```sh
go test -v ./test/script/ -run 'TestScripts/status_merged_vs_behind'
```

`-v` echoes every command and its output, not just the failing phase. To
keep the work directory for inspection, pass `-testwork`; the test logs
the `$WORK` path it kept.

#### Updating expected output

Assertions written as `stdout`/`stderr` regexes are edited by hand. For
scripts that compare against a file with `cmp`, the expected content can
be regenerated:

```sh
UPDATE_SCRIPTS=1 go test ./test/script/
```

That rewrites the `cmp` targets inside the `.txtar` files to match actual
output. **Read the resulting diff before committing it** — the flag makes
whatever the code currently does the new expectation, so it will happily
enshrine a regression.

#### Adding a case

1. Create `test/script/testdata/<name>.txtar`. Name it after the behavior,
   not the command (`repair_legacy_refspec`, not `repair3`).
2. Build the git history the case needs with plain `exec git ...`, then
   drive the CLI with `exec git-hop ...`. `RequireExplicitExec` is on, so
   every invocation needs the `exec` prefix.
3. Assert with `stdout`/`stderr` regexes, `exists`/`! exists`, and `!` for
   commands that must fail. Open each phase with a `#` comment saying what
   it pins — that comment is what testscript shows on failure.
4. Prove the script can fail. Break the production behavior it covers,
   confirm the script goes red, then restore. A script that passes against
   the bug is decoration.

Two portability notes. `$WORK` expands to a path containing regex
metacharacters, so match paths with a loose pattern (`.*/hub/hops/x`)
rather than embedding `$WORK` in a regex; single quotes suppress expansion
entirely. And if a script ranks anything by commit time, pin
`GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` — git timestamps have one-second
resolution, and back-to-back commits otherwise land in the same second and
make the outcome a race against the clock.

### 4. Docker e2e

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

The default PR run executes everything in tiers 1, 2, and 3. The
`dockere2e.yml` workflow exists so docker regressions get caught
without slowing every PR down.

`test/script` needs no dedicated CI step. The package contains only
`_test.go` files, which `go list ./...` omits but `go test ./...` still
builds and runs — so the existing `Run Tests` step already covers it, and
a separate step would only run the scripts twice.

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
record cassettes: the target tests are already fast and use only local
`git`, making the cassette overhead net-negative. The seam exists for future
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

1. Decide the tier first (unit, Go e2e, scripted e2e, docker e2e). Most
   new tests belong in tier 1 — only escalate if you genuinely need a
   spawned binary or a real docker daemon.
2. When you do need an end-to-end case, default to tier 3 (a `.txtar`
   script). Drop to tier 2 only when the assertion needs Go: parsing
   state files into structs, comparing values, or driving concurrency.
3. For tier 2: import `hop.top/git/test/e2e` and use `SetupTestEnv`.
4. For tier 3: add a file under `test/script/testdata/` — see "Adding a
   case" above.
5. For tier 4: place the file under `test/e2e/docker/`, start the
   file with `//go:build dockere2e`, and use the helpers in
   `test/e2e/docker/docker_helpers.go`.
6. Run locally before pushing. For tier 4 tests, ensure docker is up
   and you have at least 2GB free RAM for compose stacks.
