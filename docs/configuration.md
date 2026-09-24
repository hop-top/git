# Configuration

Git-hop separates three types of data across your system. Here's where they live and what you need to know.

## Quick Start

**TL;DR:** Your preferences are `hop.*` keys in git config. Everything else is managed automatically.

```bash
# View your settings
git config --get-regexp '^hop\.'

# Change a setting for every repository
git config --global hop.worktreeLocation '{hubPath}/hops/{branch}'

# Change it for one repository only (run inside its hub)
git config hop.add.fetch false

# See where a setting comes from
git config --show-origin --get-all hop.gitDomain
```

---

## Where Does git-hop Store Things?

git-hop uses different directories for different types of data:

### Configuration (User Preferences)

**Linux/Unix:**
```
~/.config/git-hop/
├── managers.json      # Custom package / environment managers (optional)
└── hooks/             # Global hooks
```

**macOS:**
```
~/Library/Preferences/git-hop/
├── managers.json
└── hooks/
```

**Environment variable override:** `$XDG_CONFIG_HOME/git-hop/`

### Data (Repository Storage)

**Linux/Unix:**
```
~/.local/share/git-hop/
└── <org>/<repo>/
    ├── hop.json              # Hopspace config
    ├── deps/                 # Shared dependencies
    │   └── .registry.json    # Dependency tracking
    ├── hops/                 # Worktrees
    │   ├── main/
    │   └── feature-x/
    └── hooks/                # Hopspace-level hooks
```

**macOS:**
```
~/Library/Application Support/git-hop/
└── <org>/<repo>/
    ├── hop.json
    ├── deps/
    ├── hops/
    └── hooks/
```

**Environment variable override:** `$GIT_HOP_DATA_HOME`

### State (Tracking)

**Linux/Unix:**
```
~/.local/state/git-hop/
└── state.json         # Repository tracking
```

**macOS:**
```
~/Library/Application Support/git-hop/state/
└── state.json
```

**Environment variable override:** `$XDG_STATE_HOME/git-hop/`

### Cache (Temporary Data)

**Linux/Unix:**
```
~/.cache/git-hop/
```

**macOS:**
```
~/Library/Caches/git-hop/
```

**Environment variable override:** `$XDG_CACHE_HOME/git-hop/`

## Environment Variables

Override default directory locations:

| Variable | Description | Default (Linux/Unix) | Default (macOS) |
|----------|-------------|---------------------|-----------------|
| `GIT_HOP_CONFIG_HOME` | Configuration directory | `~/.config/git-hop` | `~/Library/Preferences/git-hop` |
| `GIT_HOP_DATA_HOME` | Data/repository storage | `~/.local/share/git-hop` | `~/Library/Application Support/git-hop` |
| `XDG_STATE_HOME` | State tracking | `~/.local/state` | `~/Library/Application Support` |
| `XDG_CACHE_HOME` | Cache directory | `~/.cache` | `~/Library/Caches` |
| `GIT_HOP_LOG_LEVEL` | Logging verbosity | `info` | `info` |
| `GIT_HOP_AUTO_ENV_START` | Overrides `hop.env.autoStart` (`true`/`false`, git's boolean spellings); `--[no-]env-start` still wins | unset | unset |

Example usage:

```bash
export GIT_HOP_DATA_HOME=/mnt/storage/git-hop
export GIT_HOP_LOG_LEVEL=debug
git hop clone https://github.com/org/repo.git
```

## Customize Your Settings

Preferences are `hop.*` keys in git config, the same store git plugins such
as git-lfs use. Set them with `git config --global <key> <value>` for every
repository, or with `git config <key> <value>` inside a hub for one
repository. An unset key uses the default below; remove a key with
`git config --global --unset <key>` to go back to the default.

Setting a key to an empty value is not the same as unsetting it: the empty
value is used as-is. For `hop.worktreeLocation` an empty value selects the
centralized layout (`{dataHome}/{org}/{repo}/hops/{branch}`).

### Settings Reference

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `hop.gitDomain` | string | `github.com` | Git hosting domain used to expand `org/repo` shorthands |
| `hop.worktreeLocation` | string | `{hubPath}/hops/{branch}` | Where `git hop add` and `git hop move` put worktrees. Variables: `{hubPath}`, `{branch}`, `{org}`, `{repo}`, `{dataHome}`. A relative result is resolved against the hub |
| `hop.add.defaultStartPoint` | string | `default-branch` | Start-point for new branches: `default-branch`, `initial` (root commit), or any ref / SHA |
| `hop.env.autoStart` | boolean | `false` | Whether `git hop add` and clone start the new worktree's environment (the same start as `git hop env start`) once the worktree exists. Overridden by `GIT_HOP_AUTO_ENV_START` and, for one run, `--env-start` / `--no-env-start` |
| `hop.hooks.installMode` | string | `prompt` | How committed `.git-hop/hooks/` scripts are mirrored on clone / init: `prompt`, `symlink`, `copy`, `none` |
| `hop.shellIntegration.status` | string | `unknown` | Shell wrapper state: `unknown` (offer to install), `approved`, `declined`, `disabled`. Written by git-hop when you answer the prompt |

The former `bareRepo` setting is gone: clones always create a bare hub (see
[story 015](stories/015-hopspace-shape-contract.md)). A leftover `bareRepo`
in an old config file, or `hop.bareRepo` in git config, is ignored.

These settings were never acted on and are gone too: `hop.showAllManagedRepos`,
`hop.unusedThresholdDays`, `hop.conventionWarning`,
`hop.enforceCleanForConversion`, `hop.conversion.enforceClean`,
`hop.conversion.allowDirtyForce`, `hop.conversion.autoRollback`,
`hop.backup.enabled` and `hop.backup.preserveStashes`. Old config files and
git config that still carry them load as before; the keys are ignored, and
`git hop doctor --fix` unsets the ones the `global.json` migration wrote.

`hop.autoEnvStart` is retired too. git-hop never acted on it, yet earlier
releases wrote it to `--global` (as `true`) when installing shell
integration, so it cannot tell a choice from a leftover. It is no longer
read; the setting that starts the environment on add and clone is
`hop.env.autoStart`. Remove a leftover copy with
`git config --global --unset hop.autoEnvStart`.

### Package and Environment Managers

Custom package managers and environment managers are lists, so they live in
`managers.json` in the config directory (`$XDG_CONFIG_HOME/git-hop/managers.json`)
instead of git config. See [Dependency Sharing](dependency-sharing.md) for
details.

```json
{
  "packageManagers": [
    {
      "name": "custom-pm",
      "detectFiles": ["custom.lock"],
      "lockFiles": ["custom.lock"],
      "depsDir": "dependencies",
      "installCmd": ["custom-pm", "install"]
    }
  ],
  "environmentManagers": []
}
```

### Legacy `global.json`

Older releases kept these settings in `$XDG_CONFIG_HOME/git-hop/global.json`.
The first git-hop run that finds that file migrates it once:

- each setting the file contains is written to the matching `hop.*` key in
  `git config --global`; settings the file does not contain are left unset,
  so they keep their defaults. An empty string counts as not set, except for
  `worktreeLocation`, where it selects the centralized layout;
- `packageManagers` and `environmentManagers` move to `managers.json`;
- `hop.migrated=true` is recorded in git config and the file is renamed to
  `global.json.bak`.

git-hop does not read `global.json` after that. Edit git config and
`managers.json` instead; a new `global.json` is ignored once `hop.migrated`
is set.

## git config Settings

Command-specific tunables, set the same way as the settings above: `git
config --global <key> <value>`, or per-repo with `git config <key> <value>`
inside any hub for that repository.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `hop.repair.backupRetention` | duration | `720h` (30 days) | Max age of repair backup snapshots (`repair-*` directories under `$XDG_STATE_HOME/git-hop/repair/<hub>/backups/`, or a legacy `<hub>/.hop/backups/`) before `git hop prune` deletes them. Go duration syntax (e.g. `720h`, `168h` for 7 days). Set to `0` to disable auto-pruning of repair backups. |
| `hop.remote.timeout` | integer (seconds) | `10` | Deadline for git subcommands that contact a remote (`ls-remote`, `push --delete`, the `fetch` in `git hop add`). Prevents an unreachable or slow origin from hanging a command indefinitely. Set to `0` to wait without a deadline. |
| `hop.backup.keepBackup` | boolean | `false` | Keep the conversion backup `git hop init` takes after a successful conversion, as if `--keep-backup` were passed. An explicit `--keep-backup` / `--keep-backup=false` overrides this. See [Conversion backups](#conversion-backups-hopbackup). |
| `hop.backup.path` | path | `$XDG_CACHE_HOME/git-hop` | Directory `git hop init` puts conversion backups under (`<path>/<org>-<repo>/<timestamp>/`). `~/` is expanded the way git expands path values; a relative path is refused. See [Conversion backups](#conversion-backups-hopbackup). |
| `hop.backup.maxBackups` | integer | `3` | Conversion backups `git hop prune` keeps per repository, newest first. `0` turns the count limit off. |
| `hop.backup.cleanupAgeDays` | integer | `30` | Age in days past which `git hop prune` removes a conversion backup. `0` turns the age limit off. |
| `hop.merge.deleteRemote` | boolean | `false` | Make `git hop merge` delete the merged source branch on `origin` by default, as if `--delete-remote` were passed. An explicit `--delete-remote` / `--delete-remote=false` on the command line overrides this. |
| `hop.add.copyIgnored` | boolean | `true` | Make `git hop add` seed the new worktree with the git-ignored local files (`.env`, tool config, small caches) present in the worktree it forks from. `--copy-ignored` / `--no-copy-ignored` on the command line override this. |
| `hop.add.copyIgnoredMaxSize` | size | `10m` | Per-entry ceiling for that copy. An ignored file or directory above it is skipped and reported. |
| `hop.add.fetch` | boolean | unset (auto) | Make `git hop add` run `git fetch origin` before resolving the start-point (`true`) or never (`false`). Unset, it fetches only when the start-point is an origin ref: the default branch or an explicit `origin/<branch>`. `--fetch` / `--no-fetch` on the command line override this. A failed fetch is fatal when requested (`true` or `--fetch`) and only a warning under the automatic default. |
| `hop.gitflow.enabled` | boolean | `false` | Let `git hop add` / `git hop remove` run `git flow <type> start` / `git flow <type> finish` for branches whose prefix matches a git-flow-next type. Off, git-hop only detects the branch type (for the `GIT_HOP_BRANCH_*` hook variables) and runs no git-flow command. See [`hop.gitflow.enabled`](#hopgitflowenabled). |
| `hop.events.sink` | `jsonl` \| `none` | `none` | Append every lifecycle event (worktree created/removed/merged/moved/switched, env started/stopped, ...) to a JSONL file, so external tools can react without file hooks. See [`hop.events.sink`](#hopeventssink). |
| `hop.events.path` | path | `$XDG_STATE_HOME/git-hop/events.jsonl` | File `hop.events.sink=jsonl` appends to. `~/` is expanded the way git expands path values. |

### `hop.remote.timeout`

Git applies no wall-clock deadline to remote operations, so an
unreachable host or a stalled SSH/TLS handshake blocks forever.
Commands that contact a remote run under `hop.remote.timeout` and fail
with a stated cause when it expires.

```bash
# Allow 30s for a slow remote
git config --global hop.remote.timeout 30

# Wait indefinitely (pre-existing behavior)
git config --global hop.remote.timeout 0
```

Note that neither `git hop remove` nor `git hop merge` contacts the
remote at all unless you ask for it with `--delete-remote` (or, for
merge, `hop.merge.deleteRemote`); both are local operations by default.

### `hop.add.copyIgnored`

A new worktree starts as a clean checkout, so the ignored local state you
rely on — `.env`, editor and tool settings, small caches — is missing
until you copy it by hand. `git hop add` does that copy for you: it asks
git which entries in the fork-source worktree are ignored and copies them
into the new one, never overwriting anything, skipping directories the
dependency layer owns, and skipping anything over
`hop.add.copyIgnoredMaxSize`.

```bash
# Opt out for one worktree, or for good
git hop add feature-x --no-copy-ignored
git config --global hop.add.copyIgnored false

# Allow larger entries
git config hop.add.copyIgnoredMaxSize 50m
```

#### Keeping an ignored path out of the copy: `#-hop-#`

Some ignored paths belong to one worktree only — a local task database,
a per-checkout scratch directory. Put `#-hop-#` on a comment line
directly above the pattern and `git hop add` leaves everything that
pattern ignores behind:

```gitignore
# task-tracker state, one per checkout
#-hop-#
.tlc/

#-hop-# scratch space, never worth copying
tmp/
```

The marker applies to the comment block immediately above a pattern; a
blank line or another pattern ends the block. It works in every file git
reads ignore rules from — the root `.gitignore`, nested `.gitignore`
files, `.git/info/exclude`, and the global excludes file — because
`git hop add` asks `git check-ignore` which rule decided each entry and
checks that rule's file.

The marker cannot share the pattern's own line. Git only treats `#` as a
comment at the start of a line, so `.tlc/ #-hop-#` would become a pattern
matching a path literally named `.tlc/ #-hop-#` and stop ignoring `.tlc/`.

### `hop.add.fetch`

A hub clones origin once; after that its `origin/*` refs only move when
something fetches. `git hop add` therefore runs `git fetch origin` before
resolving the start-point whenever that start-point is an origin ref — the
default branch (resolved through `origin/<default>`) or an explicit
`--from origin/<branch>` — so a new worktree does not start from days-old
code. A local branch, tag, SHA or `initial` start-point is used as-is.

The fetch is bounded by `hop.remote.timeout`. What a failure (offline,
origin unreachable) does depends on who asked for the fetch:

- `--fetch` or `hop.add.fetch true`: `add` stops with `fatal: could not
  fetch origin` and exit status 1, before creating anything. Fix the
  remote, or pass `--no-fetch` to start from the local refs.
- the automatic default: `add` prints a warning and carries on from the
  refs already present.

`--dry-run` reports that it would fetch but does not.

```bash
# Never fetch in this repo; fetch for one add anyway
git config hop.add.fetch false
git hop add feature-x --fetch

# Always fetch, whatever the start-point; skip it for one add
git config --global hop.add.fetch true
git hop add feature-x --no-fetch
```

### `hop.merge.deleteRemote`

A merge happens either locally or on the remote — everything after it
is syncing. Once you have merged a branch that was pushed, deleting it
on `origin` finishes the job. Git treats remote deletion as a separate
destructive act, so `git hop merge` keeps it opt-in: pass
`--delete-remote` per invocation, or set this key to make it the
default.

```bash
# Always delete the merged source branch on origin
git config --global hop.merge.deleteRemote true
```

The flag wins over the config key in both directions, so a repo
configured to delete by default can still skip it for one merge:

```bash
git hop merge feature-x main --delete-remote=false
```

With the key unset and no flag, merge never contacts the network. When
remote deletion does run, the probe and the delete push are bounded by
[`hop.remote.timeout`](#hopremotetimeout).

### `hop.gitflow.enabled`

In a repository set up for [git-flow-next](https://github.com/gittower/git-flow-next)
(`gitflow.initialized=true`), git-hop reads the git-flow branch types to
detect what kind of branch you are adding or removing, and passes that to
hooks. It does not run git-flow itself unless you ask:
`git flow <type> finish` merges the branch into its parent, which removing
a worktree should not do on its own.

```bash
# This repository: add runs 'git flow <type> start', remove runs 'finish'
git config hop.gitflow.enabled true

# Back to detection only
git config --unset hop.gitflow.enabled
```

The key is read from the hub's repository, the same place the `gitflow.*`
settings live, so `--global` works too. `--dry-run` lists the git-flow
step only when the key is on. With it off, `--verbose` prints a single
`hint:` when a command skipped a git-flow action. See
[Git-Flow Integration](hooks.md#7-git-flow-integration) for the full
behavior, including the current bare-hub limitation.

### `hop.repair.backupRetention`

`git hop repair` snapshots state into
`$XDG_STATE_HOME/git-hop/repair/<hub-basename>-<hash>/backups/repair-<UTC-timestamp>/`
(default `~/.local/state/git-hop/...`; never inside the hub) before
mutating, so `git hop repair --undo` can restore it. Those backups
accumulate. `git hop prune` cleans up any backup directory
older than `hop.repair.backupRetention`.

```bash
# Keep repair backups for 7 days instead of 30
git config --global hop.repair.backupRetention 168h

# Keep them for an hour (CI/scratch repos)
git config --global hop.repair.backupRetention 1h

# Disable auto-pruning of repair backups
git config --global hop.repair.backupRetention 0
```

The setting is read at prune time. `git hop prune` walks every hub
recorded in state and reads `hop.repair.backupRetention` from the
first hub that has it set, falling back to the 720h default if none
do. There is no separate per-repo override mechanism beyond setting
the value inside that repo's hub.

### Conversion backups (`hop.backup.*`)

`git hop init` copies the repository to a backup directory before it
converts it:

```
<hop.backup.path>/<org>-<repo>/<YYYY-MM-DD_HH-MM-SS>/
```

`hop.backup.path` defaults to `$XDG_CACHE_HOME/git-hop` (`~/.cache/git-hop`
on Linux, `~/Library/Caches/git-hop` on macOS). `~/` in the value expands
the way git expands path values. A relative value is refused, and so is
one inside the repository being converted: init runs inside the repository
and prune runs from anywhere, so a relative root would name a different
directory each time. `git hop init -n` prints the root it would use.

```bash
# Keep conversion backups on another disk
git config --global hop.backup.path ~/backups/git-hop
```

The backup is always taken, `--force` included: a failed conversion rolls
back from it. After a successful conversion it is deleted unless it is
kept, and init prints `Backup preserved at: <path>` only when it is still
on disk (kept, or the conversion failed).

```bash
# Keep every conversion backup by default
git config --global hop.backup.keepBackup true

# ...but not for this one conversion
git hop init --no-prompt --keep-backup=false
```

The flag wins over the key in both directions. The `hop.backup.*` keys
are read from the repository being converted, so a repo-local `git config
hop.backup.keepBackup true` applies to that repository alone.

`git hop init --restore <backup-dir>` takes the backup's full path, so it
works wherever the backup lives.

#### Retention

Kept backups accumulate. `git hop prune` removes a repository's
conversion backups beyond the newest `hop.backup.maxBackups` (default 3)
and those older than `hop.backup.cleanupAgeDays` (default 30); either
limit is enough, and `0` turns a limit off. The count is per repository.
The settings are read through the repository's hub, so a repo-local value
overrides the global one; with no hub left on disk, global config
applies.

Prune looks under `hop.backup.path` and under the default cache root, so
backups taken before the path changed are still aged out. A backup belongs
to a repository when it sits in that repository's `<org>-<repo>`
directory or when the path it copied (`originalPath` in its
`backup-info.json`) is one of the repository's hubs; the second rule
covers a hub whose `org/repo` no longer matches the name its backup
directory was given, for example after its remote changed. Prune
reports each backup it removes as a `conversion-backup` record, and
`--dry-run` only lists them.

Prune never removes:

- the backup of a **failed** conversion: init marks it (a
  `conversion-failed` file in the backup) because it is what the
  automatic rollback restored from, and the only copy if that rollback
  failed. Prune prints a `hint:` naming it; remove it by hand once the
  repository is intact. Backups of conversions that failed before this
  marker existed carry no mark and are treated like any other.
- a backup still being written: init writes `backup-info.json` last, and
  a backup without it is skipped. A conversion in progress is also the
  newest backup, which `hop.backup.maxBackups` of 1 or more keeps.

Only repositories prune can see are covered: those in git-hop's state,
scoped like the rest of prune (the current one, or all with `--all`). A
repository converted with `git hop init` enters state once a worktree
command such as `git hop add` has run in it.

```bash
# Keep only the latest conversion backup of every repository
git config --global hop.backup.maxBackups 1

# Never age backups out of this repository
git config hop.backup.cleanupAgeDays 0
```

### `hop.events.sink`

git-hop publishes a lifecycle event after each successful mutation
(topics and payloads: [Lifecycle events](hooks.md#lifecycle-events)).
By default they stay in-process. Set `hop.events.sink` to `jsonl` to
append each one, as a single JSON line, to `hop.events.path`:

```bash
git config --global hop.events.sink jsonl
git config --global hop.events.path ~/.local/state/git-hop/events.jsonl  # optional; this is the default

# Turn it off again
git config --global hop.events.sink none
```

Use an absolute or `~/` path; a relative one resolves against whatever
directory the command happens to run in.

Guarantees:

- **Never fails the command.** An unwritable path, a missing directory
  that cannot be created, or an unknown `hop.events.sink` value leaves
  the command's output and exit status untouched. The problem is
  reported on stderr only with `--verbose` (`-V`).
- **Never stalls the command.** Each write is abandoned after 250ms
  (relevant only for a path on a hung network mount).
- **Nothing under `--dry-run`.** A preview publishes no events and no
  sink is attached.
- **Durable per event.** Each line is on disk when the command moves on,
  even when git-hop exits early afterwards. Parallel git-hop processes
  can share one file: each line is appended with a single write.

The file is append-only and never rotated by git-hop; rotate it with
`logrotate` or similar if it grows.

**Environment override.** git-hop's event bus also honors kit's
`KIT_BUS_SINK=jsonl` + `KIT_BUS_SINK_PATH=<file>`. When `KIT_BUS_SINK`
is set, it wins and `hop.events.*` is ignored, so events are never
written twice. Two differences from the git config sink: kit creates the
file on every invocation (even ones that publish nothing), and it
buffers lines until the process exits normally, so an event published
right before an early exit can be lost.

## Hopspace Configuration

**This file is managed automatically by git-hop. Do not edit it manually.**

Each repository has its own hopspace configuration that tracks branches and metadata. You won't need to touch this; git-hop maintains it.

### Location

Where the hopspace lives depends on how the hub was cloned:

- **Default clone**: the hub's own `<hub-path>/hop.json` holds both the
  hub and the hopspace fields. Nothing is written under
  `$GIT_HOP_DATA_HOME`.
- **`clone --global`**: `$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json`, and
  the hub's `hop.json` carries `"repo": {"mode": "global"}`.

Only that marker decides. A `$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json`
next to a hub without the marker is a stale copy: git-hop never reads
it, and `git hop doctor` reports it as a warning (check `hopspace`).

### Schema

```json
{
  "repo": {
    "uri": "https://github.com/org/repo.git",
    "org": "org",
    "repo": "repo",
    "defaultBranch": "main"
  },
  "branches": {
    "main": {
      "exists": true,
      "path": "/home/user/.local/share/git-hop/github.com/org/repo/hops/main",
      "lastSync": "2026-02-01T10:00:00Z"
    },
    "feature-x": {
      "exists": true,
      "path": "/home/user/.local/share/git-hop/github.com/org/repo/hops/feature-x",
      "lastSync": "2026-02-02T14:30:00Z"
    }
  },
  "forks": {}
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `repo.uri` | string | Remote repository URL |
| `repo.org` | string | Organization or user name |
| `repo.repo` | string | Repository name |
| `repo.defaultBranch` | string | Default branch (usually `main` or `master`) |
| `branches` | object | Map of branch names to branch metadata |
| `branches[].exists` | boolean | Whether the worktree exists on disk |
| `branches[].path` | string | Absolute path to the worktree |
| `branches[].lastSync` | string | ISO 8601 timestamp of last sync |
| `forks` | object | Fork repositories (for PR testing) |

## Hub Configuration

Each hub (workspace) has its own configuration.

### Location

`<hub-path>/hop.json`

Example: `~/projects/myrepo/hop.json`

### Schema

```json
{
  "repo": {
    "uri": "https://github.com/org/repo.git",
    "org": "org",
    "repo": "repo",
    "defaultBranch": "main"
  },
  "branches": {
    "main": {
      "path": "main",
      "hopspaceBranch": "main"
    },
    "feature-x": {
      "path": "feature-x",
      "hopspaceBranch": "feature-x"
    }
  },
  "settings": {
    "compareBranch": "main",
    "envPatterns": ["*.env", ".env.*"]
  },
  "migrated": true
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `branches` | object | Map of branch names to hub branch info |
| `branches[].path` | string | Full path to the worktree directory |
| `branches[].hopspaceBranch` | string | Corresponding branch name in hopspace |
| `branches[].fork` | string | Fork URI if this is a fork branch |
| `branches[].task` | string | Task id recorded by `git hop add --task`; omitted when none |
| `settings.compareBranch` | string | Default branch for comparisons |
| `settings.envPatterns` | array | Glob patterns for environment files |
| `migrated` | boolean | Whether this hub has been migrated to the registry system |
| `repo.mode` | string | `global` when cloned with `--global`: the hopspace lives in `$GIT_HOP_DATA_HOME/<org>/<repo>`. Omitted by default, when the hub is its own hopspace |

## State Tracking

**This file is managed automatically by git-hop. Do not edit it manually.**

The state file tracks all repositories and their locations across your system so git-hop can find them quickly.

### Location

**Linux/Unix:** `~/.local/state/git-hop/state.json`
**macOS:** `~/Library/Application Support/git-hop/state/state.json`

### Schema

```json
{
  "version": "1.0.0",
  "lastUpdated": "2026-02-03T12:00:00Z",
  "repositories": {
    "github.com/org/repo": {
      "uri": "https://github.com/org/repo.git",
      "org": "org",
      "repo": "repo",
      "defaultBranch": "main",
      "worktrees": {
        "main": {
          "path": "/home/user/.local/share/git-hop/github.com/org/repo/hops/main",
          "type": "bare",
          "hubPath": "/home/user/projects/repo",
          "createdAt": "2026-01-15T09:00:00Z",
          "lastAccessed": "2026-02-03T11:30:00Z"
        }
      },
      "hubs": [
        {
          "path": "/home/user/projects/repo",
          "mode": "local",
          "createdAt": "2026-01-15T09:00:00Z",
          "lastAccessed": "2026-02-03T11:30:00Z"
        }
      ],
      "globalHopspace": {
        "enabled": true,
        "path": "/home/user/.local/share/git-hop/github.com/org/repo"
      }
    }
  },
  "orphaned": []
}
```

### Purpose

The state file enables:
- Fast hub discovery without scanning the filesystem
- Tracking of repository locations across the system
- Detection of orphaned worktrees and hubs
- Multi-hub support for the same repository

## Dependency Registry

Tracks shared dependencies across worktrees. See [Dependency Sharing](dependency-sharing.md) for details.

### Location

`$GIT_HOP_DATA_HOME/<org>/<repo>/deps/.registry.json`

### Schema

```json
{
  "entries": {
    "node_modules.abc123": {
      "lockfileHash": "abc123",
      "lockfilePath": "package-lock.json",
      "usedBy": ["main", "feature-x"],
      "lastUsed": "2026-02-02T10:30:00Z",
      "installedAt": "2026-01-15T09:00:00Z"
    }
  }
}
```

## Ports and Volumes

Port and volume configurations are stored per repository for deterministic allocation.

### Ports Configuration

`$GIT_HOP_DATA_HOME/<org>/<repo>/ports.json`

```json
{
  "allocationMode": "hash-based",
  "baseRange": {
    "start": 10000,
    "end": 15000
  },
  "branches": {
    "main": {
      "ports": {
        "api": 10234,
        "db": 10235,
        "redis": 10236
      }
    }
  },
  "services": ["api", "db", "redis"]
}
```

### Volumes Configuration

`$GIT_HOP_DATA_HOME/<org>/<repo>/volumes.json`

```json
{
  "basePath": "/home/user/.local/share/git-hop/github.com/org/repo/volumes",
  "branches": {
    "main": {
      "volumes": {
        "postgres_data": "main_postgres_data",
        "redis_data": "main_redis_data"
      }
    }
  }
}
```

## Override Settings for Specific Situations

Settings follow a hierarchy — git-hop uses the first one it finds:

1. **Environment variables** — for one command
2. **Hub config** (`<hub>/hop.json`) — for one workspace
3. **Hopspace config** (`$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json`) — for one repository
4. **git config** (`hop.*` keys; `--global` for all repositories, repo-local for one)
5. **Built-in defaults** — fallback

**Example:** Change port base for one repo only (don't affect others):

```bash
# Edit ~/.local/share/git-hop/github.com/org/repo/hop.json
# Add this to the JSON: "portBase": 20000
```

**Example:** Use environment variable for a single command:

```bash
GIT_HOP_PORT_BASE=20000 git hop add feature-x
```

## Best Practices

### 1. Version Control Separation

**DO commit:**
- Repo-level hooks (`.git-hop/hooks/`)
- Environment file patterns in hub settings
- Documentation about repository-specific configuration

**DO NOT commit:**
- Machine-specific paths in `managers.json`
- State tracking (`state.json`)
- Dependency registry (`.registry.json`)
- Personal overrides

### 2. Portable Configuration

Your settings travel with your git config. Keep them in a file you sync
and include it from `~/.gitconfig`:

```bash
# Collect current hop.* settings into a shareable file
git config --global --get-regexp '^hop\.' \
  | grep -v '^hop.migrated ' \
  | while read -r key value; do git config -f ~/Dropbox/git-hop.gitconfig "$key" "$value"; done

# On another machine
git config --global include.path ~/Dropbox/git-hop.gitconfig
```

Copy `managers.json` alongside it if you define custom managers.

### 3. Team Sharing

For team-wide conventions:

1. Document recommended global settings in your repository README
2. Use repo-level hooks (`.git-hop/hooks/`) for team-wide automation
3. Share Docker Compose files for consistent environments

### 4. Debugging Configuration

Check effective configuration:

```bash
# Show every hop.* key and the file that sets it
git config --show-origin --get-regexp '^hop\.'

# Show one setting (prints nothing and exits 1 when unset: the default applies)
git config --get hop.worktreeLocation
```

## Troubleshooting

### Finding Configuration Files

```bash
# Where each hop.* setting is defined
git config --show-origin --get-regexp '^hop\.'

# Verify XDG directories
echo $XDG_CONFIG_HOME
echo $XDG_DATA_HOME
echo $XDG_STATE_HOME
echo $XDG_CACHE_HOME
```

### Resetting Configuration

To go back to the defaults, remove the `hop.*` keys you set:

```bash
# Back up current settings
git config --global --get-regexp '^hop\.' > ~/git-hop-config-backup.txt

# Remove one setting (its default applies again)
git config --global --unset hop.worktreeLocation

# Remove every hop.* setting in the global scope
git config --global --remove-section hop
```

`--remove-section hop` only removes top-level keys such as
`hop.worktreeLocation`; subsections like `hop.add.*` or `hop.backup.*` are
removed with `git config --global --remove-section hop.add`, and so on.

### Invalid JSON

If git-hop ignores your custom managers, check that `managers.json` parses:

```bash
# Validate the file
jq . ~/.config/git-hop/managers.json
```

Fix any syntax errors, or restore from backup.

## Implementation Details

For developers interested in the implementation:

- **Global config loader**: `internal/config/global.go` (defaults table: `internal/config/gitconfig.go`)
- **State management**: `internal/state/state.go`
- **Hub config**: `internal/config/config.go` (`HubConfig`)
- **Hopspace config**: `internal/config/config.go` (`HopspaceConfig`)
- **XDG directory resolution**: `internal/state/state.go` (`GetStateHome()`)

The configuration system:
- Follows XDG Base Directory specification
- Uses JSON for human-readable config files
- Provides atomic writes for config updates
- Supports environment variable overrides
- Maintains backward compatibility with legacy config
