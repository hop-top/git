# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & test

The Makefile is the source of truth. From this directory (the `hops/main/` worktree):

```bash
make build          # go build with version ldflags → ./git-hop
make test           # go test -v ./internal/... (excludes cmd/, test/e2e/, test/integration/)
make lint           # go vet + staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)
make fmt            # go fmt ./...
make install        # copy binary to $GOBIN
make lint-links     # lychee link-check on docs/**/*.md (requires `brew install lychee`)
```

CI (`.github/workflows/ci.yml`) runs `go build ./...`, `go vet ./...`, `staticcheck ./...`, and `go test -coverprofile=coverage.out ./...` (note: full tree, not just `./internal/...`). Replicate locally before pushing:

```bash
go test ./...                                    # full suite
go test -run TestRemoveCommand_PartialFailureHandling ./cmd/...   # single test
go test -count=1 ./internal/hop/...              # bypass test cache
```

E2E tests in `test/e2e/` and Docker tests in `test/e2e/docker/` build a real binary and exercise it. Docker E2E has its own workflow (`.github/workflows/dockere2e.yml`).

## Repository layout (this is a bare-worktree repo)

The labspace dir `~/.w/ideacrafterslabs/git/` is a `git hop`-managed bare worktree repo. The git internals (`hooks/`, `objects/`, `refs/`, `hop.json`) live at the root; **all source code lives under `hops/main/`**. When working in this repo, treat `hops/main/` as the project root.

## High-level architecture

`git hop` is a Go CLI that wraps `git worktree` to give every branch an isolated environment (own ports, volumes, optional Docker stack). It is invoked as `git hop <command>` (a git porcelain subcommand) and MUST adhere to git conventions for output, flags, exit codes, and config (see `## Git porcelain conventions` below).

### Entry point chain
`main.go` → `cmd/root.go` → `internal/cli` (cobra command tree). `main.go` also calls `xrrx.InstallFromEnv()` to wire the [xrr](https://github.com/hop-top/xrr) test-cassette runtime when `XRR_*` env vars are set.

### Two-tier package split
- **`cmd/`** — one file per top-level subcommand (`add.go`, `remove.go`, `list.go`, `status.go`, `prune.go`, `doctor.go`, `repair.go`, `merge.go`, `move.go`, `init.go`, `env.go`, `completion.go`), plus `<cmd>_*.go` splits and hidden shell-integration helpers (`current_path.go`, `notify_chdir.go`). Each file's `init()` registers the cobra command. Thin layer: parse flags, build dependencies, delegate to `internal/`.
- **`internal/`** — the actual logic, organized by concern:
  - `internal/cli` — root cobra command (clone mode `git hop <uri>`, switch mode `git hop <branch>`), global flags, version, usage-error/exit-code mapping (`usage.go`)
  - `internal/hop` — core domain (Hub, Hopspace, WorktreeManager, CleanupManager, StateValidator, backup, conversion, paths)
  - `internal/git` — `git.GitInterface` abstracts `os/exec` git calls; `git.Git` is the real impl, `test/mocks.MockGit` is the test double
  - `internal/state` — `$XDG_STATE_HOME/git-hop/state.json`: every repo, hub and worktree git-hop knows (read by `list`, `status --all`, `prune`)
  - `internal/config` — config schema + load/merge from XDG paths and `hop.json`
  - `internal/docker`, `internal/services` — Docker compose orchestration + service lifecycle
  - `internal/detector` — repo type/structure detection (bare vs regular vs already-converted)
  - `internal/output` — output formatting (text, json, porcelain) — all user-facing output goes through here
  - `internal/hooks` — git-hop lifecycle hooks: resolution (`FindHookFile`), runner, committed-hook mirroring
  - `internal/shell` — shell integration helpers (bash/zsh/fish auto-cd wrapper)
  - `internal/tui` — interactive prompts (charm bubbletea/lipgloss/bubbles)
  - `internal/xrrx` — xrr cassette installer
  - `internal/events` — event emission for plugins/integrations
  - `internal/task` — `tlc` lookup behind `add --task`
  - `internal/testenv` — isolated HOME/XDG env for tests (`TestMain`)
  - `internal/repoid` — the repo ID (`<host>/<org>/<repo>`) state keys and `GIT_HOP_REPO_ID`, plus origin-URL host parsing

### Two domain concepts
- **Hub** — the dir where you run `git hop` from, found by walking up for `hop.json`. Usually a bare repo with worktrees under `hops/<branch>/`, a `hop.json` listing them (`branches`), and a `current` symlink.
- **Hopspace** — where `ports.json`, `volumes.json` and the hopspace `hop.json` live (`hop.ResolveHopspacePath`). By default the hub itself is its hopspace. Only a hub marked `repo.mode: global` in `hop.json` (cloned with `-g/--global`) uses a data-home hopspace, shared by every global hub of that repo; a data-home copy beside an unmarked hub is never read (doctor warns). Its path is `$GIT_HOP_DATA_HOME/<layout>`, from the `hop.dataLayout` template (default `{org}/{repo}`; `{host}/{org}/{repo}` also works). `hop.dataLayout` resolves per repository (hub config over `--global`, `git -c` over both), so set `RepoRef.Dir` (`ref.In(hubPath)`) wherever a hub exists. Every per-repo data-home path goes through `hop.GetHopspacePath(dataHome, hop.RepoRef)`; never hand-build `filepath.Join(dataHome, org, repo)`.

The `WorktreeManager` (`internal/hop/worktree.go`) is the central type: `CreateWorktree`, `CreateWorktreeTransactional` (with rollback on failure), `MoveWorktree`, `RemoveWorktree`. It takes `afero.Fs` + `git.GitInterface` so callers can swap in mocks.

### Resource allocation
Ports are recorded per hopspace (so per hub, unless `repo.mode: global`) but allocated against every hub git-hop knows (the hubs in state plus the current one; `services.LoadEnvRecords`), so a new allocation never takes a port another worktree holds. `services.GenerateWorktreeEnv` (via `SetUpWorktree` for add, clone and init; directly for `env generate`) lets a worktree keep the ports it already has, unless a hub set up earlier holds one (then it gets new ones, with a warning); a worktree without ports gets the next block above the highest port any hub holds (default `incremental` mode, range 10000–20000; `allocationMode: hash` starts where repo, hub and branch hash to). Volumes are `hop_<branch>_<name>` dirs under the hopspace's `volumes/`. The allocation is recorded in `ports.json` / `volumes.json` and written to the worktree's `.env` (plus a compose override for hardcoded host ports). Only a worktree with a Docker environment gets any.

### Configuration hierarchy (highest wins)
1. CLI flags
2. Env vars (`GIT_HOP_DATA_HOME`, `GIT_HOP_AUTO_ENV_START`, `GIT_HOP_HOOKS`, `GIT_HOP_ADD_FROM`)
3. Hub-level `hop.json`
4. Hopspace-level `hop.json`
5. `git config hop.*` (repo-local, then `--global`); a legacy `global.json` is migrated into it once; package/env managers live in `$XDG_CONFIG_HOME/git-hop/managers.json`
6. Built-in defaults

No other settings file: `$XDG_CONFIG_HOME/git-hop/config.json` is not read (doctor warns if one exists, never deletes it), nor is the retired hub registry `$XDG_CONFIG_HOME/git-hop/hops.json`, which nothing writes either (state records every hub; doctor warns the same way), and kit's `-c/--config` is accepted, hidden from help and ignored with a `warning:`. A one-off setting is `git -c hop.<key>=<value> hop ...`. The global flags have no environment variables, except `GIT_HOP_VERBOSE`: a debug switch like git's `GIT_TRACE` (`true`/`yes`/`on`, `false`/`no`/`off`/empty, any case, or a `-V` count; anything else warns and counts as 0), applied as the root viper's `verbose` default at the start of the pre-run (`applyVerboseEnv`), so a `--verbose` on the command line overrides it.

### Testability via interfaces
Every external dependency is behind an interface so unit tests don't shell out:
- `git.GitInterface` — all git operations (mock at `test/mocks/mock_git.go`)
- `afero.Fs` — filesystem (use `afero.NewMemMapFs()` in tests)

Production code constructs concrete impls; tests inject fakes. Add new dependencies the same way: define interface in the package that consumes it, not where the impl lives.

## Git porcelain conventions (MUST follow)

`git hop` is a git subcommand and must behave like one. When adding/modifying commands:

- **Output streams**: results to stdout; progress, hints, warnings to stderr with lowercase `hint:`, `warning:`, `error:`, `fatal:` prefixes (git's actual style).
- **Exit codes**: `0` success, `1` operation failure, `128` fatal git/repo error, `129` usage error (bad flag, unanswerable prompt). `93` is reserved for a `post-worktree-switch` hook that navigated itself (`hooks.ExitNavigationHandled`).
- **Flags**: prefer git-standard names — `-n/--dry-run`, `-q/--quiet`, `--force`, `--porcelain` (NOT `--json` for stable scripting; use `--format=<fmt>` if structured output is needed alongside porcelain), `--[no-]progress`, `--color[=<when>]`, `--`. Deviations: `-V/--verbose` (count, `-VV` trace), since `-v` is `--version`; `--force` has no `-f`.
- **Mutate by default**: destructive commands mutate by default with safety nets (backup, dirty-check, lock); preview is opt-in via `-n`. This matches `git gc`, `git fsck`, `git prune`, `git worktree repair`.
- **Config**: tunables go through `git config hop.<command>.<key>`, not env-only or flag-only. Read via the existing `internal/config` package.
- **Hooks**: mutating commands fire `pre-<cmd>` / `post-<cmd>` hooks via `internal/hooks`. These are git-hop's own lifecycle hooks, NOT git's — they never live in `.git/hooks/`. `FindHookFile` resolves repo (`<worktree>/.git-hop/hooks/`, plus a parent-dir walk) → hopspace (`hop.HopspaceHooksDir`, i.e. `<hopspace>/hooks/`, then the pre-layout `$GIT_HOP_DATA_HOME/github.com/<org>/<repo>/hooks/`) → global (`$XDG_CONFIG_HOME/git-hop/hooks/`). Exception: `pre-clone` has no repo level (the repo does not exist yet), so it resolves only at hopspace and global. `ValidHookNames` in `internal/hooks/runner.go` is the authority on which names exist; see `docs/hooks.md` for which are actually dispatched and at which levels.
- **No emoji, no Unicode boxes** in output. Plain ASCII like git itself.

## Domain-specific gotchas

- **Bare-worktree repos**: don't conclude a repo is empty just because the root has no source files; the source lives in `hops/<branch>/` (usually `hops/main/`). `git hop init` converts regular repos to this layout.
- **`current` symlink**: each hub has a `current` symlink pointing at the last-hopped worktree. `cmd/remove.go:updateCurrentToDefault` handles fallback when the target of `current` is removed.
- **State v2 keyed by worktree path**: `state.json` format 2.x keys each repo's `worktrees` by absolute worktree path (`state.WorktreeKey`), not branch, so two hubs can both record the same branch. Look entries up with `RepositoryState.Worktree(hubPath, branch)` / `WorktreeAt(path)` and compare paths with `state.SamePath`. `state.LoadState` migrates branch-keyed entries in memory (`internal/state/migrate.go`); the next save backs the old file up to `$XDG_STATE_HOME/git-hop/backups/state-<ts>.json`. Change state only through `state.Update` (fresh load, change, atomic save under `state.json.lock`), never `LoadState` then `SaveState`: a save of a state loaded earlier undoes what another run saved since. Keep git commands, hooks and prompts out of the callback; a slow scan records its decisions and replays them in it (`cmd/state_edits.go`).
- **Repo IDs carry the origin's host**: a repository's state key and `GIT_HOP_REPO_ID` are `<host>/<org>/<repo>`, the host from the origin URL or `hop.gitDomain` when it has none (resolved per hub by `repoid.GitDomainIn`, the same `config.ResolveScoped` resolver as `hop.dataLayout`). Build one only with `repoid.New` / `NewIn` / `For`; never `fmt.Sprintf` it. `state.LoadState` moves repositories an earlier release keyed `github.com/...` to their origin's host (`internal/state/rekey.go`), keeping both entries when the new key is taken (a doctor issue).
- **Transactional creates**: `WorktreeManager.CreateWorktreeTransactional` rolls back on failure (cleans up half-created dirs, port allocations, etc.). Prefer it over `CreateWorktree` for any user-facing operation.
- **`go test ./...` vs `make test`**: `make test` only runs `./internal/...`. CI runs the full tree. If your change affects `cmd/` or `test/`, run `go test ./...` locally.

## Conventions

- Commits: Conventional Commits (`feat|fix|refactor|build|ci|chore|docs|style|perf|test`). Squash-merges from PRs are common; `cmd/status.go` and `cmd/remove_safety.go` detect them by content equivalence when commit topology says otherwise.
- File size budget: keep files under ~500 LOC; split when they grow.
- Never delete or rename unexpected files/state without asking — assume another agent or in-progress work created them.
