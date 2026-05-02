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

`git hop` is a Go CLI that wraps `git worktree` to give every branch a deterministic, isolated environment (own ports, volumes, optional Docker stack). It is invoked as `git hop <command>` (a git porcelain subcommand) and MUST adhere to git conventions for output, flags, exit codes, and config (see `## Git porcelain conventions` below).

### Entry point chain
`main.go` → `cmd/root.go` → `internal/cli` (cobra command tree). `main.go` also calls `xrrx.InstallFromEnv()` to wire the [xrr](https://github.com/hop-top/xrr) test-cassette runtime when `XRR_*` env vars are set.

### Two-tier package split
- **`cmd/`** — one file per top-level subcommand (`add.go`, `remove.go`, `list.go`, `status.go`, `prune.go`, `doctor.go`, `merge.go`, `move.go`, `init.go`, `env.go`, `completion.go`). Each file's `init()` registers the cobra command. Thin layer: parse flags, build dependencies, delegate to `internal/`.
- **`internal/`** — the actual logic, organized by concern:
  - `internal/cli` — root cobra command, global flags, version
  - `internal/hop` — core domain (Hub, Hopspace, WorktreeManager, CleanupManager, StateValidator, backup, conversion, paths)
  - `internal/git` — `git.GitInterface` abstracts `os/exec` git calls; `git.Git` is the real impl, `test/mocks.MockGit` is the test double
  - `internal/state` — persistent state tracking across worktrees (system-level state file)
  - `internal/config` — config schema + load/merge from XDG paths and `hop.json`
  - `internal/docker`, `internal/services` — Docker compose orchestration + service lifecycle
  - `internal/detector` — repo type/structure detection (bare vs regular vs already-converted)
  - `internal/output` — output formatting (text, json, porcelain) — all user-facing output goes through here
  - `internal/hooks` — git hook installation/management
  - `internal/shell` — shell integration helpers (bash/zsh/fish auto-cd wrapper)
  - `internal/tui` — interactive prompts (charm bubbletea/lipgloss/bubbles)
  - `internal/xrrx` — xrr cassette installer
  - `internal/events` — event emission for plugins/integrations

### Two domain concepts
- **Hub** — the dir where you run `git hop` from. Holds a `.git` reference (often to a bare repo) and a `hop.json` listing the hub's worktrees.
- **Hopspace** — canonical storage location at `$GIT_HOP_DATA_HOME/<domain>/<org>/<repo>/`. Holds the actual worktree dirs plus `ports.json`, `volumes.json`, and a `hop.json`. All worktrees of a repo share one hopspace; multiple hubs can reference the same hopspace.

The `WorktreeManager` (`internal/hop/worktree.go`) is the central type: `CreateWorktree`, `CreateWorktreeTransactional` (with rollback on failure), `MoveWorktree`, `RemoveWorktree`. It takes `afero.Fs` + `git.GitInterface` so callers can swap in mocks.

### Resource determinism
Ports, volumes, and networks are derived from a stable hash of `(repo, branch)`. Same branch reproduces same allocation; collisions across branches are avoided. Allocation is persisted to `ports.json` / `volumes.json` in the hopspace.

### Configuration hierarchy (highest wins)
1. Env vars (`GIT_HOP_DATA_HOME`, `GIT_HOP_CONFIG_HOME`, `GIT_HOP_CACHE_DIR`, `GIT_HOP_LOG_LEVEL`)
2. CLI flags
3. `$XDG_CONFIG_HOME/git-hop/config.json` (global)
4. Hub-level `hop.json`
5. Hopspace-level `hop.json`

### Testability via interfaces
Every external dependency is behind an interface so unit tests don't shell out:
- `git.GitInterface` — all git operations (mock at `test/mocks/mock_git.go`)
- `afero.Fs` — filesystem (use `afero.NewMemMapFs()` in tests)

Production code constructs concrete impls; tests inject fakes. Add new dependencies the same way: define interface in the package that consumes it, not where the impl lives.

## Git porcelain conventions (MUST follow)

`git hop` is a git subcommand and must behave like one. When adding/modifying commands:

- **Output streams**: results to stdout; progress, hints, warnings to stderr with lowercase `hint:`, `warning:`, `error:`, `fatal:` prefixes (git's actual style).
- **Exit codes**: `0` success, `1` operation failure, `128` fatal git/repo error, `129` usage error (bad flag).
- **Flags**: prefer git-standard names — `-n/--dry-run`, `-v/--verbose`, `-q/--quiet`, `-f/--force`, `--porcelain` (NOT `--json` for stable scripting; use `--format=<fmt>` if structured output is needed alongside porcelain), `--[no-]progress`, `--color[=<when>]`, `--`.
- **Mutate by default**: destructive commands mutate by default with safety nets (backup, dirty-check, lock); preview is opt-in via `-n`. This matches `git gc`, `git fsck`, `git prune`, `git worktree repair`.
- **Config**: tunables go through `git config hop.<command>.<key>`, not env-only or flag-only. Read via the existing `internal/config` package.
- **Hooks**: mutating commands fire `pre-<cmd>` / `post-<cmd>` hooks installed under `.git/hooks/` via `internal/hooks`.
- **No emoji, no Unicode boxes** in output. Plain ASCII like git itself.

## Domain-specific gotchas

- **Bare-worktree repos**: don't conclude a repo is empty just because the root has no source files; the source lives in `hops/<branch>/` (usually `hops/main/`). `git hop init` converts regular repos to this layout.
- **`current` symlink**: each hub has a `current` symlink pointing at the last-hopped worktree. `cmd/remove.go:updateCurrentToDefault` handles fallback when the target of `current` is removed.
- **State files**: legacy state lives in older formats; `cmd/list.go:loadStateOrLegacy` is the migration path. New code reads via `internal/state`.
- **Transactional creates**: `WorktreeManager.CreateWorktreeTransactional` rolls back on failure (cleans up half-created dirs, port allocations, etc.). Prefer it over `CreateWorktree` for any user-facing operation.
- **`go test ./...` vs `make test`**: `make test` only runs `./internal/...`. CI runs the full tree. If your change affects `cmd/` or `test/`, run `go test ./...` locally.

## Conventions

- Commits: Conventional Commits (`feat|fix|refactor|build|ci|chore|docs|style|perf|test`). Squash-merges from PRs are common — `cmd/status.go` should detect them but currently doesn't (T-0066).
- File size budget: keep files under ~500 LOC; split when they grow.
- Never delete or rename unexpected files/state without asking — assume another agent or in-progress work created them.
