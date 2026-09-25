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

**macOS** (when `XDG_CONFIG_HOME` is unset):
```
~/Library/Application Support/git-hop/
├── managers.json
└── hooks/
```

**Environment variable override:** `$XDG_CONFIG_HOME/git-hop/`

### Data (Repository Storage)

A default clone keeps its hopspace in the hub (`hop.json`, `ports.json`,
`volumes.json`, `deps/`, with worktrees under `hops/`). The data home holds
each repository's hopspace hooks, and the whole hopspace of a hub cloned
with `--global`:

**Linux/Unix:**
```
~/.local/share/git-hop/
└── <org>/<repo>/
    ├── hooks/                # Hopspace-level hooks
    ├── hop.json              # --global only: hopspace config
    ├── ports.json            # --global only
    ├── volumes.json          # --global only
    └── deps/                 # --global only: shared dependencies
        └── .registry.json
```

**macOS:**
```
~/Library/Application Support/git-hop/
└── <org>/<repo>/
    ├── hooks/
    ├── hop.json
    ├── ports.json
    ├── volumes.json
    └── deps/
```

Worktrees live here only when `hop.worktreeLocation` puts them in the
hopspace (`{hopspace}/hops/{branch}`).

**Environment variable override:** `$GIT_HOP_DATA_HOME`

`<org>/<repo>` is the default `hop.dataLayout`; with `{host}/{org}/{repo}`
each repository lives under `<host>/<org>/<repo>/` instead (see
[Settings Reference](#settings-reference)).

### State (Tracking)

**Linux/Unix:**
```
~/.local/state/git-hop/
└── state.json         # Repository tracking
```

**macOS** (when `XDG_STATE_HOME` is unset):
```
~/Library/Application Support/git-hop/
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

Directories follow the XDG base directories, with `git-hop/` under each;
only the data home has a git-hop variable of its own:

| Variable | Description | Default (Linux/Unix) | Default (macOS) |
|----------|-------------|---------------------|-----------------|
| `XDG_CONFIG_HOME` | Configuration (`managers.json`, global hooks) | `~/.config` | `~/Library/Application Support` |
| `GIT_HOP_DATA_HOME` | Data/repository storage; wins over `XDG_DATA_HOME` | `$XDG_DATA_HOME/git-hop` | `$XDG_DATA_HOME/git-hop` |
| `XDG_DATA_HOME` | Data, when `GIT_HOP_DATA_HOME` is unset | `~/.local/share` | `~/Library/Application Support` |
| `XDG_STATE_HOME` | State tracking | `~/.local/state` | `~/Library/Application Support` |
| `XDG_CACHE_HOME` | Cache directory | `~/.cache` | `~/Library/Caches` |
| `GIT_HOP_ADD_FROM` | Start-point for `git hop add`'s new branches; overrides `hop.add.defaultStartPoint`, `--from` wins | unset | unset |
| `GIT_HOP_HOOKS` | How clone and init mirror committed hooks; overrides `hop.hooks.installMode`, `--hooks` wins | unset | unset |
| `GIT_HOP_AUTO_ENV_START` | Overrides `hop.env.autoStart` (`true`/`false`, git's boolean spellings); `--[no-]env-start` still wins | unset | unset |
| `GIT_HOP_VERBOSE` | Debug switch, like git's `GIT_TRACE`: stands in for `-V` on every run. `true`/`yes`/`on` is `-V`, `false`/`no`/`off` or empty is off (any case), a number is the `-V` count (`2` is `-VV`); anything else prints a warning and counts as off. A `--verbose` on the command line wins | unset | unset |

No other global flag has an environment variable: `-q`, `--format`,
`--no-color` and the rest are set on the command line only.

Example usage:

```bash
export GIT_HOP_DATA_HOME=/mnt/storage/git-hop
export GIT_HOP_VERBOSE=1        # debug output, as with -V
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
centralized layout (`{hopspace}/hops/{branch}`: inside the repository's
data-home hopspace, so it follows `hop.dataLayout`).

### Settings Reference

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `hop.gitDomain` | string | `github.com` | Git hosting domain used to expand `org/repo` shorthands, and the host of the repo ID (`<host>/<org>/<repo>`) of a repository whose origin has none: a local path, a `file://` URL, no origin. Resolved like `hop.dataLayout`: a hub's own value overrides `--global`, and `git -c` overrides both; outside a repository (a clone shorthand, a hub whose repo cannot be read) only `--global` and `git -c` count |
| `hop.dataLayout` | string | `{org}/{repo}` | Where each repository's data (hopspace `hop.json`, `ports.json`, `volumes.json`, `deps/`, `hooks/`) lives under the data home. Variables: `{host}` (the host of the origin URL, or `hop.gitDomain` for a local path), `{org}`, `{repo}`; `{org}` and `{repo}` are required. Resolved like any setting: a hub's own value overrides `--global`, and `git -c` overrides both; a `--global` clone, which has no hub yet, reads `--global` and `git -c` only. Hubs sharing one repository's data should agree on it. An invalid value falls back to the `--global` value (or the default when that is invalid too) and `git hop doctor` warns, naming the scope. Changing it does not move data already stored under the old layout: `git hop doctor` warns about a `--global` hub's hopspace left there, and `git hop doctor --fix` moves it |
| `hop.worktreeLocation` | string | `{hubPath}/hops/{branch}` | Where `git hop add` and `git hop move` put worktrees. Variables: `{hubPath}`, `{branch}`, `{org}`, `{repo}`, `{dataHome}`, `{hopspace}` (the repository's data-home hopspace, `{dataHome}` plus `hop.dataLayout`; use it rather than `{dataHome}/{org}/{repo}` so worktrees follow the layout). A relative result is resolved against the hub |
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
git config that still carry them load as before; the keys are ignored.

`hop.autoEnvStart` is retired too. git-hop never acted on it, yet earlier
releases wrote it to `--global` (as `true`) when installing shell
integration, so it cannot tell a choice from a leftover. It is no longer
read; the setting that starts the environment on add and clone is
`hop.env.autoStart`.

`git hop doctor` warns about each of these retired keys, whatever its
value, in `git config --global` and in the current hub's own config, and
`git hop doctor --fix` unsets every value of it (`--unset-all`) in the
scope that holds it. Nothing reads them, and git-hop wrote several on its
own (the old `global.json` migration, shell integration), so no value is
kept as the user's choice. To remove one by hand, run for example
`git config --global --unset-all hop.autoEnvStart`.

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

### `config.json` is not read

git-hop does not read `$XDG_CONFIG_HOME/git-hop/config.json`: its settings
live in git config `hop.*` keys only. A `config.json` left there changes
nothing; `git hop doctor` warns while it exists, and never deletes it,
`--fix` included. To keep a value it held, set the matching `hop.*` key,
then delete the file by hand.

### `hops.json` is retired

Earlier releases kept a hub registry in `$XDG_CONFIG_HOME/git-hop/hops.json`,
written on clone and init. git-hop no longer writes or reads it: state
(`$XDG_STATE_HOME/git-hop/state.json`) records every hub and worktree. A
`hops.json` left there changes nothing; `git hop doctor` warns while it
exists and never deletes it, `--fix` included. Delete it by hand.

git-hop's own `-c`/`--config` flag is ignored too. It is still accepted, so
scripts that pass it keep running with their usual exit status, but each run
that passes it prints a `warning:` on stderr, even with `-q`. For a one-off
setting, use git's `-c` (see below).

## git config Settings

Command-specific tunables, set the same way as the settings above: `git
config --global <key> <value>`, or per-repo with `git config <key> <value>`
inside any hub for that repository.

For a single run, pass the key to git itself, before `hop`:

```bash
git -c hop.remote.timeout=30 hop add feat/x
```

The `-c` goes before `hop`: git consumes it. git-hop's own `-c`/`--config`,
after `hop`, is [ignored with a warning](#configjson-is-not-read).

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `hop.repair.backupRetention` | duration | `720h` (30 days) | Max age of repair backup snapshots (`repair-*` directories under `$XDG_STATE_HOME/git-hop/repair/<hub>/backups/`, or a legacy `<hub>/.hop/backups/`) before `git hop prune` deletes them. Go duration syntax (e.g. `720h`, `168h` for 7 days). `0` (or a negative duration) turns pruning of repair backups off; a value that is not a duration is ignored, as if unset. |
| `hop.remote.timeout` | integer (seconds) | `10` | Deadline for git subcommands that contact a remote (`ls-remote`, `push --delete`, the `fetch` in `git hop add`). Prevents an unreachable or slow origin from hanging a command indefinitely. Set to `0` to wait without a deadline. |
| `hop.backup.keepBackup` | boolean | `false` | Keep the conversion backup `git hop init` takes after a successful conversion, as if `--keep-backup` were passed. An explicit `--keep-backup` / `--keep-backup=false` overrides this. See [Conversion backups](#conversion-backups-hopbackup). |
| `hop.backup.path` | path | `$XDG_CACHE_HOME/git-hop` | Directory `git hop init` puts conversion backups under (`<path>/<org>-<repo>/<timestamp>/`). `~/` is expanded the way git expands path values; a relative path is refused. See [Conversion backups](#conversion-backups-hopbackup). |
| `hop.backup.maxBackups` | integer | `3` | Conversion backups `git hop prune` keeps per repository, newest first. `0` turns the count limit off. |
| `hop.backup.cleanupAgeDays` | integer | `30` | Age in days past which `git hop prune` removes a conversion backup. `0` turns the age limit off. |
| `hop.merge.deleteRemote` | boolean | `false` | Make `git hop merge` delete the merged source branch on `origin` by default, as if `--delete-remote` were passed. An explicit `--delete-remote` / `--delete-remote=false` on the command line overrides this. |
| `hop.add.copyIgnored` | boolean | `true` | Make `git hop add` seed the new worktree with the git-ignored local files (`.env`, tool config, small caches) present in the worktree it forks from. `--copy-ignored` / `--no-copy-ignored` on the command line override this. |
| `hop.add.copyIgnoredMaxSize` | size | `10m` | Per-entry ceiling for that copy. An ignored file or directory above it is skipped and reported. |
| `hop.add.fetch` | boolean | unset (auto) | Make `git hop add` run `git fetch origin` before resolving the start-point (`true`) or never (`false`). Unset, it fetches only when the start-point is an origin ref: the default branch or an explicit `origin/<branch>`. `--fetch` / `--no-fetch` on the command line override this. A failed fetch is fatal when requested (`true` or `--fetch`) and only a warning under the automatic default. In a hub with no `origin` remote a requested fetch is skipped with a hint. |
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

A hub with no `origin` remote at all (a converted local-only repo) has
nothing to fetch from, which is not a failure: a requested fetch is skipped
with `hint: no origin remote; skipping the requested fetch` and `add`
carries on from the local refs. A global `hop.add.fetch true` therefore
still works in remote-less hubs. An `origin` that is configured but
unreachable stays fatal.

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

# Never prune repair backups
git config --global hop.repair.backupRetention 0
```

`0` or a negative duration turns pruning off: `git hop prune` keeps every
repair backup, and `--dry-run` lists none. A value that is not a Go
duration (a bare `30`, a typo) is ignored, as if unset.

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

`git hop init --restore <backup-dir>` takes the backup's path, so it
works wherever the backup lives, and restores to the location recorded in
the backup's `backup-info.json`, not the current directory. When that
location exists and is not empty (after a successful conversion it holds
the new hub), restore refuses. `--force` moves what is there aside to
`<location>.pre-restore-<UTC time>` (for example
`proj.pre-restore-20260924T101500Z`, with `-1`, `-2`, ... appended if
that name is taken), then restores the backup into the freed location.
Nothing is deleted; restore prints the moved-aside path with the commands
to remove it or swap it back. If the move fails, restore stops without
changing anything. A backup stored inside the location it restores to is
refused even with `--force`; move the backup elsewhere first, and so is
a backup missing its `original/` copy. With `-n`/`--dry-run`, restore
makes the same checks and refuses the same way, with the same exit
status, then prints the target, whether it is occupied, and the
`.pre-restore-<UTC time>` name it would be moved to, changing nothing.
When a backup is kept, init prints the restore command as a hint.

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

Each repository has its own hopspace configuration that tracks branches and
metadata. git-hop maintains it; the only part meant for editing is
`packageManagers` (see [Package Manager Overrides](package-manager-overrides.md)).

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
      "path": "/home/user/src/repo/hops/main",
      "lastSync": "2026-02-01T10:00:00Z"
    },
    "feature-x": {
      "exists": true,
      "path": "/home/user/src/repo/hops/feature-x",
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
| `packageManagers.<pm>.installCmd` | array | Install command for package manager `<pm>` in this repository |
| `branches[].packageManagers.<pm>.installCmd` | array | The same for one branch; wins over the repository's |

## Hub Configuration

Each hub (workspace) has its own configuration.

### Location

`<hub-path>/hop.json`

Example: `~/projects/myrepo/hop.json`

Several git-hop commands can run in one hub at once. Each change to
`hop.json` re-reads the file and rewrites it while holding a lock on
`hop.json.lock` beside it, so one command never undoes another's change.
The lock file exists only while a change is being written; the lock
belongs to the process holding it and goes away when that process exits.
A command that waits more than 30 seconds for the lock fails.

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
    "environmentManager": "docker-compose",
    "environmentConfig": {
      "hooks": {
        "preStart": ["scripts/load-secrets.sh"],
        "postStop": ["scripts/cleanup.sh"]
      }
    }
  }
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
| `branches[].base` | string | Branch the worktree was created from; `status` and `list` compare against it |
| `settings.compareBranch` | string | Comparison branch for worktrees without a `base`; default `repo.defaultBranch` |
| `settings.environmentManager` | string | Environment manager `env start`/`stop` use, by name (built-in `docker-compose` or one from `managers.json`); `none` disables it. Unset: detected from the worktree's files |
| `settings.environmentConfig.hooks` | object | Command lists `preStart`, `postStart`, `preStop`, `postStop` run around `env start`/`stop` |
| `settings.envPatterns` | array | Written by git-hop with its defaults; not read |
| `migrated` | boolean | Legacy; not read |
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
  "version": "2.0.0",
  "lastUpdated": "2026-02-03T12:00:00Z",
  "repositories": {
    "github.com/org/repo": {
      "uri": "https://github.com/org/repo.git",
      "org": "org",
      "repo": "repo",
      "defaultBranch": "main",
      "worktrees": {
        "/home/user/projects/repo/hops/main": {
          "path": "/home/user/projects/repo/hops/main",
          "branch": "main",
          "type": "bare",
          "hubPath": "/home/user/projects/repo",
          "createdAt": "2026-01-15T09:00:00Z",
          "lastAccessed": "2026-02-03T11:30:00Z"
        },
        "/home/user/scratch/repo/hops/main": {
          "path": "/home/user/scratch/repo/hops/main",
          "branch": "main",
          "type": "bare",
          "hubPath": "/home/user/scratch/repo",
          "createdAt": "2026-01-20T10:00:00Z",
          "lastAccessed": "2026-01-20T10:00:00Z"
        }
      },
      "hubs": [
        {
          "path": "/home/user/projects/repo",
          "mode": "local",
          "createdAt": "2026-01-15T09:00:00Z",
          "lastAccessed": "2026-02-03T11:30:00Z"
        },
        {
          "path": "/home/user/scratch/repo",
          "mode": "local",
          "createdAt": "2026-01-20T10:00:00Z",
          "lastAccessed": "2026-01-20T10:00:00Z"
        }
      ],
      "globalHopspace": null
    }
  },
  "orphaned": []
}
```

Each repository is keyed by its repo ID, `<host>/<org>/<repo>`: the host
of its origin URL (`gitlab.example.com` for
`git@gitlab.example.com:acme/widgets.git`), or `hop.gitDomain` when the
origin has none (a local path, a `file://` URL, no origin). The same ID
reaches hooks as `GIT_HOP_REPO_ID`. Two repositories with one org/repo on
two hosts are two entries.

A repository's `hubs` lists every hub of it, and `worktrees` every
worktree of those hubs, keyed by the worktree's path. `branch` names the
branch checked out there and `hubPath` the hub it belongs to, so two hubs
of one repository each keep their own worktree of `main`.

### Format versions and migration

`version` is the format. Releases before 2.0.0 keyed `worktrees` by
branch, so a repository could record only one worktree per branch across
all its hubs. git-hop reads such a file as it is and migrates it in
memory: each entry's old key becomes its `branch`, an entry without a
`hubPath` gets the deepest recorded hub containing its path, and two
entries for the same worktree become one. Nothing is dropped. Commands
that only read state (`list`, `status`, `doctor` without `--fix`) write
nothing; the next command that saves state writes the file in the current
format.

Before that first save, the old file is copied to
`$XDG_STATE_HOME/git-hop/backups/state-<UTC timestamp>.json`. A backup is
written once and never overwritten. `git hop prune --all` removes backups
older than `hop.repair.backupRetention` (default 30 days; `0` or a
negative value keeps them).

git-hop never saves over a `state.json` it cannot parse, or one a newer
release wrote (a higher major `version`); the command warns and leaves
the file as it is.

Releases before repo IDs carried the origin's host keyed every repository
`github.com/<org>/<repo>`, whatever its origin. On load, git-hop reads each
recorded hub's `hop.json` and moves a repository whose hubs' origin gives
another key to that key. When one entry held hubs of two hosts, each hub
moves to its own host's entry with its worktrees. A hub whose `hop.json`
cannot be read keeps its key. The move follows the rules above: nothing
is written until a command saves state, and that save backs the old file
up first. Whether a repository needs moving is read from its hubs, not
from `version`, so running it again changes nothing and `version` stays
`2.0.0`. `hop.gitDomain` is resolved in each hub, so changing it (at
`--global`, or in one hub) moves the repositories whose origin has no
host the same way.

When the key a repository's origin gives is already taken, git-hop does
not merge the two entries: both are kept, every command that loads state
warns, and `git hop doctor` reports it as an issue (exit 1; `--fix`
leaves it). Merge the entries by hand: copy `state.json`, move the hubs
doctor names, and their worktrees, from the old entry into the one under
the new key, and delete the old entry.

An older release can still read a file in the current format: it shows
worktree paths where it used to show branches, and entries it adds are
migrated again by the next current release.

### Purpose

The state file enables:
- Fast hub discovery without scanning the filesystem
- Tracking of repository locations across the system
- Detection of orphaned worktrees and hubs
- Multi-hub support for the same repository

## Dependency Registry

Tracks shared dependencies across worktrees. See [Dependency Sharing](dependency-sharing.md) for details.

### Location

`<hopspace>/deps/.registry.json`: `<hub>/deps/.registry.json` for a default clone, `$GIT_HOP_DATA_HOME/<org>/<repo>/deps/.registry.json` for a `--global` one.

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

Port and volume allocations are recorded in the hopspace: `<hub>` for a
default clone, `$GIT_HOP_DATA_HOME/<org>/<repo>` for a `--global` one.
git-hop creates both files the first time it generates a worktree's Docker
environment.

### Ports Configuration

`<hopspace>/ports.json`

```json
{
  "allocationMode": "incremental",
  "baseRange": {
    "start": 10000,
    "end": 20000
  },
  "branches": {
    "main": {
      "ports": {
        "api": 10234,
        "db": 10235,
        "redis": 10236
      },
      "overrideDir": "/home/user/.cache/git-hop/org/repo/app-1a2b3c4d/main",
      "project": "org-repo-app-1a2b3c4d-main",
      "branch": "main",
      "worktree": "/home/user/src/app/hops/main",
      "hub": "/home/user/src/app"
    }
  },
  "services": ["api", "db", "redis"]
}
```

| Field | Description |
|-------|-------------|
| `allocationMode` | `incremental` (the default): new ports go after the highest port in use. `hash` (any other value): where the repository, hub and branch hash to |
| `baseRange.start` / `baseRange.end` | Port range new ports come from; default `10000` / `20000`. Edit to move it |
| `branches` | One entry per worktree (see below) |
| `services` | Service names ports are allocated for |

An entry is keyed by branch in a hub's own hopspace, and by worktree path
in a `--global` hopspace, which every `--global` hub of the repository
shares: each hub's worktree of a branch has its own entry.

Ports are allocated against every hub git-hop knows of (all hubs in its
state, of every repository), so no two worktrees get the same port:

- A worktree keeps the ports its entry records, each time its environment
  is generated.
- New ports go after the highest port in use (`incremental`, the default)
  or where the repository, hub and branch hash to (`hash`), skipping any
  port in use; when the range runs out, the first free block is used.
- Where two entries already hold one port, the hub set up first keeps it.
  The other hub gets new ports the next time its environment is generated
  (`git hop env generate`), with a warning. Until then `git hop doctor`
  lists the port (`--fix` does not re-port a running environment) and
  `git hop env start` warns about it.
- `git hop remove` drops the worktree's entries, freeing its ports;
  removing a `--global` hub drops its entries from the hopspace the other
  hubs keep.

`overrideDir` is where the branch's compose override is cached, when its
compose file has hardcoded host ports: `$XDG_CACHE_HOME/git-hop/<org>/<repo>/<hub key>/<branch>`,
where the hub key is the hub directory's name and a short hash of its
path, so two hubs of one repository never share an override. `project` is
the compose project the environment runs as (`docker compose -p`):
`<org>-<repo>-<hub key>-<branch>`, so two hubs never share containers,
networks or named volumes.

An entry an earlier release wrote has only `ports`. It keeps its ports and
runs as `<org>-<repo>-<branch>`, with its override in
`$XDG_CACHE_HOME/git-hop/<org>/<repo>/<branch>`, until its environment is
generated again; then the other fields are filled in, the ports and
project unchanged.

### Volumes Configuration

`<hopspace>/volumes.json`, keyed as `ports.json` is.

```json
{
  "basePath": "/home/user/src/app/volumes",
  "branches": {
    "main": {
      "volumes": {
        "postgres_data": "/home/user/src/app/volumes/hop_main_postgres_data",
        "cache": "/home/user/src/app/volumes/hop_main_cache"
      }
    }
  }
}
```

Each volume of the compose file, and each `${HOP_VOLUME_<NAME>}` it
references, gets a directory `hop_<branch>_<name>` in `basePath`
(`<hopspace>/volumes`): the hub's own for a default clone. Hubs sharing a
`--global` hopspace each get a subdirectory named by their hub key, so no
two hubs share a volume directory.

A worktree keeps the directories its entry records; git-hop never moves
or deletes volume data. Earlier releases put `${HOP_VOLUME_*}` directories
in `$GIT_HOP_DATA_HOME/volumes/<branch>/<name>`, one for every repository
and hub, and gave every `--global` hub of a repository the same
directories. An entry that records such a directory keeps it, unless a hub
set up earlier records it too: that hub keeps it, with its data, and the
other gets a new directory, with a warning, when its environment is
generated.

## Override Settings for Specific Situations

Settings follow a hierarchy — git-hop uses the first one it finds:

1. **Environment variables** — for one command
2. **Hub config** (`<hub>/hop.json`) — for one workspace
3. **Hopspace config** (the hub's `hop.json`, or `$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json` for a `--global` hub) — for one repository
4. **git config** (`hop.*` keys; `--global` for all repositories, repo-local for one)
5. **Built-in defaults** — fallback

**Example:** Change the port range for one repo only (don't affect others):
edit `baseRange` in that repository's `ports.json` (see
[Ports Configuration](#ports-configuration)).

**Example:** Use an environment variable for a single command:

```bash
GIT_HOP_AUTO_ENV_START=true git hop add feature-x
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
