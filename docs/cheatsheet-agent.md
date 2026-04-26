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

# Inspect
/usr/bin/git hop list --json                  # [{branch, path, type, last_access}]
/usr/bin/git hop status --json                # current worktree metadata

# Rename
/usr/bin/git hop move <old-branch> <new-branch>

# Merge + cleanup
/usr/bin/git hop merge <source> <into>        # merge, remove source, symlink current
/usr/bin/git hop merge <source> <into> --no-ff  # force merge commit

# Remove (safety gate — see Error Handling table for blocked cases)
/usr/bin/git hop remove <branch> --no-prompt              # non-interactive delete (clean+merged only)
/usr/bin/git hop remove <branch> --dry-run                # preview
/usr/bin/git hop remove <branch> --force                  # unmerged but pushed
/usr/bin/git hop remove <branch> --no-verify              # merged but dirty / unpushed
/usr/bin/git hop remove <branch> --force --no-verify      # unmerged AND unpushed
```

---

## Environment Management

```bash
/usr/bin/git hop env generate         # write .env + override for current worktree
/usr/bin/git hop env start            # start Docker / services (aliases: up)
/usr/bin/git hop env stop             # stop services (aliases: down)
/usr/bin/git hop env gc --dry-run     # list orphaned deps + disk to reclaim
/usr/bin/git hop env gc --force       # delete orphaned deps, no prompt
```

---

## Diagnostics + Repair

```bash
/usr/bin/git hop doctor --json        # structured diagnostics: paths, hubs, orphans
/usr/bin/git hop doctor --fix         # auto-repair (symlinks, state consistency)
/usr/bin/git hop prune --dry-run      # list orphaned state entries
/usr/bin/git hop prune                # remove orphaned entries
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
/usr/bin/git hop env gc --force
```

---

## Error Handling

| Condition | Handling |
|-----------|----------|
| `remove` fails: worktree still in state | `git hop doctor --fix` |
| Orphaned dirs in state after manual delete | `git hop prune` |
| `remove` blocked: "not merged into default" | add `--force` (loses unmerged commits) |
| `remove` blocked: "uncommitted changes or untracked files" | add `--no-verify` |
| `remove` blocked: "not merged and not pushed" | add `--force --no-verify` |
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
