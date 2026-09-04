# git-hop Cheatsheet — Agent

Quick reference for autonomous agents, scripts, and LLMs consuming git-hop.
Scannable in 30 seconds.

---

## Prerequisites

```bash
/usr/bin/git hop list --json          # verify git-hop is working; lists worktrees
/usr/bin/git hop status --json        # current worktree context
/usr/bin/git hop status --all --json  # full system snapshot (all repos + config)
```

Config: `$XDG_CONFIG_HOME/git-hop/config.json`
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
```

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
/usr/bin/git hop add <branch> --dry-run       # preview path + env without writing
/usr/bin/git hop add <branch> --no-copy-ignored # clean tree: skip copying ignored local
                                              #   files; `#-hop-#` above a .gitignore
                                              #   pattern excludes it permanently

# Inspect
/usr/bin/git hop list --json                  # [{branch, path, type, last_access}]
/usr/bin/git hop status --json                # current worktree metadata

# Rename
/usr/bin/git hop move <old-branch> <new-branch>

# Merge + cleanup
/usr/bin/git hop merge <source> <into>        # merge, remove source, symlink current
/usr/bin/git hop merge <source> <into> --no-ff  # force merge commit
# Merge stays local by default: no network calls. Opt in to deleting the
# merged source branch on origin (or set hop.merge.deleteRemote true).
/usr/bin/git hop merge <source> <into> --delete-remote
/usr/bin/git hop merge <source> <into> --delete-remote=false  # override the config default

# Remove (safety gate — see Error Handling for blocked cases)
# IMPORTANT: --no-prompt only skips the confirmation prompt; it does NOT
# bypass the gate. Risky branches still need --force / --no-verify or
# the command exits 1.
/usr/bin/git hop remove <branch> --no-prompt              # non-interactive delete (gate must already be satisfied)
/usr/bin/git hop remove <branch> --dry-run                # preview
/usr/bin/git hop remove <branch> --force                  # unmerged but pushed
/usr/bin/git hop remove <branch> --no-verify              # merged but dirty / unpushed
/usr/bin/git hop remove <branch> --force --no-verify      # unmerged AND unpushed
/usr/bin/git hop remove <branch> --force --no-verify --no-prompt  # full automation, all flags

# Bulk removal of merged branches (skips default + current)
/usr/bin/git hop remove --merged                                  # interactive
/usr/bin/git hop remove --merged --no-prompt                      # non-interactive
```

---

## Environment Management

```bash
/usr/bin/git hop env generate         # write .env + override for current worktree
/usr/bin/git hop env start            # start Docker / services (aliases: up)
/usr/bin/git hop env stop             # stop services (aliases: down)
/usr/bin/git hop env gc --dry-run     # list orphaned deps + disk to reclaim
/usr/bin/git hop env gc --no-prompt   # delete orphaned deps, no prompt (--force equivalent)
```

---

## Diagnostics + Repair

```bash
/usr/bin/git hop doctor --json        # structured diagnostics: paths, hubs, orphans
/usr/bin/git hop doctor --fix         # auto-repair (symlinks, state, current hub's hop.json)
/usr/bin/git hop doctor --fix --dry-run  # preview those repairs; writes nothing, no backups
/usr/bin/git hop prune --dry-run      # list this repo's orphaned state + hop.json entries
/usr/bin/git hop prune                # remove them (clears status Missing rows); current repo only
/usr/bin/git hop prune --all          # sweep every registered repo; state removals are not undoable
```

---

## Output Modes

| Flag           | Notes                                           |
|----------------|-------------------------------------------------|
| `--json`       | structured JSON; parse with `jq`                |
| `--porcelain`  | stable line-format; safer for scripting         |
| `--dry-run`    | preview only; no filesystem or state changes    |
| `--force`      | bypass confirmations + safety checks            |
| `-q`           | suppress non-error output                       |
| `-g, --global` | target global hopspace (`$GIT_HOP_DATA_HOME`)   |

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

# Full system snapshot for context
/usr/bin/git hop status --all --json | jq .

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
| `remove --no-prompt` exited 1 | `--no-prompt` is NOT a gate bypass — combine with `--force` / `--no-verify` |
| `remove` exited 129: "cannot prompt for confirmation" | prompt hit a non-interactive stdin — add `--no-prompt` |
| `env gc` exited 129: "cannot prompt for confirmation" | same cause — add `--no-prompt` (or `--force`) |
| `init` exited 129: "cannot prompt for confirmation" | conversion menu hit a non-interactive stdin — add `--no-prompt` |
| `init` seems to ignore piped `y` | the menu is `1/2/3/q`, not yes/no — pipe `1`, or use `--no-prompt` |
| Wrong config targeted | pass `--config <path>` explicitly |
| Services not stopped before remove | `git hop env stop` then retry remove |
| Unexpected state / unknown branch | `git hop list --json` to enumerate; stop + ask |

---

## Key Paths

| Variable | Default | Purpose |
|----------|---------|---------|
| `$XDG_CONFIG_HOME/git-hop/config.json` | `~/.config/git-hop/config.json` | main config |
| `$GIT_HOP_DATA_HOME` | XDG data home / git-hop | global hopspace |
| `.git-hop/hooks/` | repo-relative | repo-level hook overrides |
