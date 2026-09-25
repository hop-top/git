# git-hop Cheatsheet — Agent

Quick reference for autonomous agents, scripts, and LLMs consuming git-hop.
Scannable in 30 seconds.

---

## Prerequisites

```bash
/usr/bin/git hop list --json          # verify git-hop is working; lists worktrees
/usr/bin/git hop status --json        # this hub's worktrees (exit 128 outside a hub)
/usr/bin/git hop status --all --json  # every tracked repo's worktrees (works anywhere)
```

Settings: `git config hop.*` (`git config --get-regexp '^hop\.'`); no config file
Global hopspace: `$GIT_HOP_DATA_HOME`

---

## Initialize a Repository (non-interactive)

`git hop init` converts a standard git repo to the worktree layout. On a
standard repo it normally prompts for a structure, so **always pass
`--no-prompt`** from a script or agent:

```bash
/usr/bin/git hop init --no-prompt              # bare repo + worktrees (recommended)
/usr/bin/git hop init --no-prompt --regular    # regular repo + worktrees
/usr/bin/git hop init --no-prompt --dry-run    # preview the conversion plan
/usr/bin/git hop init --no-prompt --hooks none # skip mirroring committed hooks
/usr/bin/git hop init --no-prompt --force      # convert despite uncommitted changes (either layout)
/usr/bin/git hop init --no-prompt --json       # {action, hub, layout, default_branch, backup,
                                               #   backup_kept, registered, worktrees, dry_run}
# action: converted | adopted | already-initialized | restored; -n --json = would-be result, dry_run true
# worktrees: [{branch, path, action: created|carried, moved_from?}]: the one init created, then
#   the linked worktrees a bare conversion carried (nested ones moved to hops/<branch>)
```

A dirty working tree is refused (exit 1) unless `--force` is given.
`--force` carries uncommitted files into the new worktree but not what was
staged; the conversion backup is taken either way.

Without `--no-prompt` and with nothing readable on stdin, init exits
**129** with `fatal: cannot prompt ...` rather than waiting. That is a
missing flag, not a broken repo — re-run with `--no-prompt`.

Piping a choice also works, since a pipe carries a real answer:

```bash
printf '1\n' | /usr/bin/git hop init   # 1=bare  2=regular  3=register as-is  q=quit
```

Note the menu takes `1/2/3/q`, not `y`. Piping `y` is an invalid choice.

Already-initialized repos are idempotent: init reports the structure and
exits 0 without prompting.

---

## Agent Loop Contract

```
1. Check    →  git hop status (--all for full picture)
2. Add      →  git hop add <branch>   (then work in resulting worktree)
3. Work     →  (edit / commit inside worktree)
4. Merge    →  git hop merge <source> <into>
5. Remove   →  git hop remove <branch>  (automatically done by merge)
```

**DO:** use `/usr/bin/git hop` (full path). **DON'T:** call `git worktree` directly.
**DO:** `--dry-run` before destructive ops. **DON'T:** `remove` before merging or archiving.

---

## Worktree Lifecycle

```bash
# Create
/usr/bin/git hop add <branch>                 # create worktree + env; auto-cd if
                                              #   shell integration active
/usr/bin/git hop add <branch> --dry-run       # preview branch + path; no writes, no hooks
/usr/bin/git hop add <branch> -n --json       # would-be {branch, path, base, upstream,
                                              #   created, task?, env_started?, dry_run}
/usr/bin/git hop add <branch> --task <id>     # record task id: hop.json branches.<b>.task,
                                              #   GIT_HOP_TASK in add hooks; id never in name
/usr/bin/git hop add --task <id>              # derive <type>/<slug> branch from the task via
                                              #   `tlc task show <id> --format json`; errors
                                              #   (asks for <branch>) when tlc missing/fails;
                                              #   existing origin/<b> is tracked, not forked
/usr/bin/git hop add <branch> --no-copy-ignored # clean tree: skip copying ignored local
                                              #   files; `#-hop-#` above a .gitignore
                                              #   pattern excludes it permanently

# Inspect
/usr/bin/git hop list --json                  # [{repository, branch, base, type, path, state, status, hub}]
/usr/bin/git hop status --json                # [{branch, base, state, status, path, hub}]
/usr/bin/git hop status <branch> --json       # one-element list; + ports, services

# Switch (points `current` at the worktree; shell integration cds there)
/usr/bin/git hop <branch> --json              # {action: switched, branch, path, hub,
                                              #   current_updated, dry_run?}; exit 93 =
                                              #   a hook navigated, result still printed
/usr/bin/git hop --json                       # no <branch>: help, never a result

# Clone / fork-attach (same object as switch; no --dry-run: exit 129)
/usr/bin/git hop <uri> [<path>] --json        # {action: cloned, branch, path, hub, uri,
                                              #   default_branch}; progress on stderr
/usr/bin/git hop <uri> --branch <b> --json    # inside a hub: {action: fork-attached, branch
                                              #   (<b>-fork-<owner>), path, hub, uri, fork_branch}

# Rename
/usr/bin/git hop move <old-branch> <new-branch>
/usr/bin/git hop move <old-branch> <new-branch> --dry-run  # preview; no rename, no hooks
/usr/bin/git hop move <old> <new> --json      # {old_branch, new_branch, old_path, new_path,
                                              #   current_updated, dry_run?}

# Merge + cleanup
/usr/bin/git hop merge <source> <into>        # merge, remove source, symlink current
/usr/bin/git hop merge <source> <into> --no-ff  # force merge commit
/usr/bin/git hop merge <source> <into> --dry-run  # preview: fast-forward / merge commit;
                                              #   exits 1 if it would conflict
# Merge stays local by default: no network calls. Opt in to deleting the
# merged source branch on origin (or set hop.merge.deleteRemote true).
/usr/bin/git hop merge <source> <into> --delete-remote
/usr/bin/git hop merge <source> <into> --delete-remote=false  # override the config default
/usr/bin/git hop merge <source> <into> --json  # {source, into, result, commit, source_removed,
                                              #   branch_deleted, remote_deleted, dry_run?}
# result: up-to-date | fast-forward | merge-commit; conflict: exit 1, no result

# Remove (safety gate — see Error Handling for blocked cases)
# IMPORTANT: --no-prompt only skips the confirmation prompt; it does NOT
# bypass the gate. Risky branches still need --force / --no-verify or
# the command exits 1.
/usr/bin/git hop remove <branch> --no-prompt              # non-interactive delete (gate must already be satisfied)
/usr/bin/git hop remove <branch> --dry-run                # preview
/usr/bin/git hop remove <branch> --force                  # unmerged but pushed, clean (never unlocks a dirty worktree)
/usr/bin/git hop remove <branch> --no-verify              # merged but dirty (never unlocks an unmerged branch)
/usr/bin/git hop remove <branch> --force --no-verify      # unmerged AND (unpushed OR dirty)
/usr/bin/git hop remove <branch> --force --no-verify --no-prompt  # full automation, all flags

# Bulk removal of merged branches (skips default + current)
/usr/bin/git hop remove --merged                                  # interactive
/usr/bin/git hop remove --merged --no-prompt                      # non-interactive

# Result: [{kind, branch, path, removed, branch_deleted, remote_deleted, reason?, dry_run?}]
# kind: worktree | hub | hopspace; one record per branch, --merged candidate, or hub entry
# --merged leaving any candidate in place: records still printed, exit 1
/usr/bin/git hop remove <branch> --no-prompt --json
/usr/bin/git hop remove --merged --no-prompt --dry-run --json     # would-be records, dry_run: true
```

---

## Environment Management

```bash
/usr/bin/git hop env generate         # write .env + override for current worktree
/usr/bin/git hop env start            # start Docker / services (aliases: up)
/usr/bin/git hop env stop             # stop services (aliases: down)
/usr/bin/git hop env start --json     # {branch, path, manager, env_started, ports, services}
/usr/bin/git hop env stop --json      # same, env_stopped; manager "" = no environment (exit 0)
/usr/bin/git hop env generate --json  # {branch, path, generated, env_file, override, ports}
/usr/bin/git hop env gc --dry-run     # list orphaned deps + disk to reclaim
/usr/bin/git hop env gc --no-prompt   # delete orphaned deps, no prompt (--force equivalent)
/usr/bin/git hop env gc --dry-run --json  # [{action, key, size, last_used, path}]
```

---

## Diagnostics + Repair

```bash
/usr/bin/git hop doctor --json        # [{kind, check, subject, message, fixable?}]; [] = healthy; fixable on issues only
                                      #   exit 1 on any issue; warnings alone exit 0
/usr/bin/git hop doctor --fix         # auto-repair (symlinks, state, current hub's hop.json; records an unlisted hub)
                                      #   exit 0 only if every issue was fixed
/usr/bin/git hop doctor --fix --dry-run  # preview those repairs; writes nothing, no backups
                                      #   exit 0 only if every issue would be fixed
/usr/bin/git hop prune --dry-run      # list this repo's orphaned state + hop.json entries
/usr/bin/git hop prune                # remove them (clears status Missing rows); current repo only
/usr/bin/git hop prune --all          # sweep every registered repo (+ aged state backups, state.json temp files); state removals are not undoable
/usr/bin/git hop prune --dry-run --json  # [{action, kind, repository, branch, path, reason?}]
# action: pruned | would-prune | skipped (locked worktree kept; reason says why)
# kind: worktree | hub | hop-json-entry | hopspace-record | ports-entry | volumes-entry | repair-backup | conversion-backup | state-backup | temp-file
# hopspace-record/ports-entry/volumes-entry = orphaned --global hub's records in the shared hopspace; volume dirs kept
# conversion-backup = init backup beyond hop.backup.maxBackups (3) / cleanupAgeDays (30)
# temp-file = hop.json (or, --all, state.json) temp file an interrupted save left, 1h+ old; kept while a save holds the lock
/usr/bin/git hop repair -n --json     # plan: [{status, path, kind, old, new, reason}]
```

---

## Output Modes

| Flag           | Notes                                           |
|----------------|-------------------------------------------------|
| `--json`       | = `--format json`; one document on stdout, nothing else; parse with `jq` |
| `--format`     | `json`, `yaml`, `csv`, `text`; `table`/`human` = default human view |
| `--porcelain`  | one tab-separated record per line, no header; stable columns |
| `--cols a,b`   | pick/order columns (csv, text, porcelain); unknown column = exit 129 |
| `-n, --dry-run` | preview only; no filesystem or state changes; a command with no preview refuses it (exit 129) |
| `--force`      | bypass confirmations + safety checks            |
| `-q`           | suppress non-error output                       |
| `-g, --global` | target global hopspace (`$GIT_HOP_DATA_HOME`)   |

Structured output rules:

- Every command that does something has a result (see Result Shapes).
  `upgrade` and `upgrade preamble` have none: `--json`, `--porcelain` or a
  structured `--format` there exits 129 before anything runs. `completion`,
  `help`, `__current-path`, `__notify-chdir` accept the flags and print
  what they always print. Bare `git hop` = help, never a result.
- Object or list per the Result Shapes table. Empty list = `[]`, never empty stdout.
- Structured `init` on a standard repo needs `--no-prompt` (else exit 129, nothing converted).
- `--dry-run` with a result: same shape, would-be values; `dry_run: true`
  in json/yaml where the shape has it, `would-*` actions in the list shapes.
- Refusal / failure: exit status unchanged, error on stderr, no result.
- `--json` + `--porcelain`, `--json` + another `--format`, an unknown format
  or column: exit 129 before anything changes.
- `repair --list-backups` / `--undo` in a structured mode: exit 129.
- Structured `env gc` without `--no-prompt`: exit 129 (cannot confirm).
- Usage error (unknown flag, wrong operand count, unknown subcommand):
  exit 129 and an `error:` line on stderr. A human run also gets the
  command's `usage:` block after it; with `--porcelain` or a structured
  `--format` the `error:` line is all of stderr. In JSON mode (`--json`,
  or `--format json` on a command with a result) stderr is instead the
  one JSON error record any failure emits: `{"level":"error","msg":...}`.
- Full field reference, example JSON: `docs/09-reference.mdx#structured-output`.

---

## Result Shapes

Result schema 1.14 (MINOR = fields or enum values added, MAJOR = renamed or
removed). Columns = `csv`, `text`, `--porcelain` order; other fields are
json/yaml only.

| Command | Result | Porcelain columns |
|---------|--------|-------------------|
| `git hop <branch>`, `git hop <uri>` | object | `action branch path hub` |
| `git hop add` | object | `branch path base upstream created` |
| `git hop status` | list | `branch base state status path` |
| `git hop list` | list | `repository branch base type path state status` |
| `git hop remove` | list | `kind branch path removed branch_deleted remote_deleted` |
| `git hop move` | object | `old_branch new_branch old_path new_path current_updated` |
| `git hop merge` | object | `source into result commit source_removed branch_deleted remote_deleted` |
| `git hop init` | object | `action hub layout default_branch backup` |
| `git hop doctor` | list | `kind check subject message` |
| `git hop prune` | list | `action kind repository branch path` |
| `git hop repair` | list | `status path kind old new` |
| `git hop env start` | object | `branch path manager env_started` |
| `git hop env stop` | object | `branch path manager env_stopped` |
| `git hop env generate` | object | `branch path generated env_file override` |
| `git hop env gc` | list | `action key size last_used path` |

```bash
/usr/bin/git hop add feat/x --json
# {"branch":"feat/x","path":"/src/widget/hops/feat/x","base":"main","upstream":"","created":true}
/usr/bin/git hop add feat/x --porcelain
# feat/x<TAB>/src/widget/hops/feat/x<TAB>main<TAB><TAB>true
```

---

## Common Patterns

```bash
# Create, work, merge — minimal cycle
/usr/bin/git hop add feat/foo
cd <path from list>
# ... edits + commits ...
/usr/bin/git hop merge feat/foo main

# Non-interactive remove (post-merge by script)
/usr/bin/git hop remove feat/foo --no-prompt

# Every tracked worktree, all repos
/usr/bin/git hop status --all --json | jq .

# Healthy? (exit status is the verdict)
/usr/bin/git hop --quiet doctor

# Dry-run everything before committing to a destructive step
/usr/bin/git hop remove feat/foo --dry-run
/usr/bin/git hop prune --dry-run

# GC orphaned deps after bulk branch cleanup
/usr/bin/git hop env gc --dry-run
/usr/bin/git hop env gc --no-prompt
```

---

## Error Handling

| Condition | Handling |
|-----------|----------|
| `remove` fails: worktree still in state | `git hop doctor --fix` |
| Orphaned dirs in state after manual delete | `git hop prune` |
| `remove` blocked: "not merged into default" | add `--force --no-prompt` (loses unmerged commits) |
| `remove` blocked: "uncommitted changes or untracked files" | add `--no-verify --no-prompt` |
| `remove` blocked: "not merged and not pushed" | add `--force --no-verify --no-prompt` |
| `remove` blocked: "not merged into default" (with `--no-verify` set) | `--no-verify` does not cover unmerged; add `--force` |
| `remove` blocked: "not merged into default, and worktree has uncommitted changes" | add `--force --no-verify --no-prompt` (loses unmerged commits AND uncommitted files); `--force` alone never discards dirty files |
| `remove` blocked: "git flow finish runs in it" (`hop.gitflow.enabled`) | commit or stash; no flag covers it. Unfinished removal: `git -c hop.gitflow.enabled=false hop remove <b> --force --no-verify` |
| `remove --no-prompt` exited 1 | `--no-prompt` is NOT a gate bypass — combine with `--force` / `--no-verify` |
| `remove` exited 129: "cannot prompt for confirmation" | prompt hit a non-interactive stdin — add `--no-prompt` |
| `env gc` exited 129: "cannot prompt for confirmation" | same cause — add `--no-prompt` (or `--force`) |
| `init` exited 129: "cannot prompt for confirmation" | conversion menu hit a non-interactive stdin — add `--no-prompt` |
| `init` seems to ignore piped `y` | the menu is `1/2/3/q`, not yes/no — pipe `1`, or use `--no-prompt` |
| Setting not taking effect | settings are `git config hop.*` only; `-c/--config` and `config.json` are ignored — for one run use `git -c hop.<key>=<value> hop ...` |
| Services not stopped before remove | `git hop env stop` then retry remove |
| Unexpected state / unknown branch | `git hop list --json` to enumerate; stop + ask |
| exited 129: "--dry-run is not supported by ..." | that command has no preview (e.g. `env start`); nothing ran — decide, then run it without `--dry-run` |
| exited 129: "--json is not supported by ...: it has no structured result" | that command (`upgrade`) has no result shape; nothing ran — run it without `--json` / `--porcelain` / `--format` |

---

## Key Paths

| Variable | Default | Purpose |
|----------|---------|---------|
| `$XDG_CONFIG_HOME/git-hop/managers.json` | `~/.config/git-hop/managers.json` | custom package / environment managers |
| `$GIT_HOP_DATA_HOME` | XDG data home / git-hop | global hopspace |
| `.git-hop/hooks/` | repo-relative | repo-level hook overrides |
