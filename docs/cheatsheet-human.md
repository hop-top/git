# git-hop Cheatsheet — Human

Quick reference for daily worktree + env workflows. Scannable in 30 seconds.

---

## Setup

```bash
git hop init                          # convert existing repo → bare+worktree structure
git hop init --enable-chdir           # also install shell wrapper for auto-cd
git hop init --no-hooks               # skip creating .git-hop/hooks/ directory
```

Config: `$XDG_CONFIG_HOME/git-hop/config.json`

---

## Clone

```bash
git hop <uri> [path]                  # bare+worktree clone (recommended)
git hop github.com/org/repo ./dest    # shorthand with domain flag optional
```

---

## Worktree — Daily Use

```bash
git hop add <branch>                  # create worktree + env (aliases: create, new)
git hop add <branch> --dry-run        # preview without applying
git hop remove <branch>               # delete worktree + env (aliases: rm, delete, del)
git hop remove <branch> --no-prompt   # skip confirmation prompt only — does NOT bypass safety gate
git hop remove <branch> --force       # allow unmerged branch removal (real guardrail bypass)
git hop remove <branch> --no-verify   # allow dirty worktree / unpushed commits (real guardrail bypass)
git hop list                          # show all worktrees (aliases: ls, all)
git hop list --json                   # machine-readable
git hop status                        # current worktree info (aliases: st, info)
git hop status --all                  # system-wide: all repos, config, resource usage
                                      # (hub view shows sync status per branch)
```

---

## Branch Operations

```bash
git hop merge <source> <into>         # merge source → into, remove source worktree,
                                      #   symlink "current" → into worktree
git hop merge <into>                  # uses current branch as source
git hop merge <source> <into> --no-ff # force merge commit (no fast-forward)
git hop move <old> <new>              # rename branch + worktree (aliases: rename, mv)
```

---

## Environment (Docker / Services)

```bash
git hop env generate                  # write .env + override for current worktree
git hop env start                     # start services (aliases: up)
git hop env stop                      # stop services (aliases: down)
git hop env gc                        # GC orphaned deps (aliases: cleanup, clean)
git hop env gc --dry-run              # preview what would be freed
git hop env gc --force                # skip confirmation
```

---

## Maintenance

```bash
git hop doctor                        # diagnose paths, hubs, hopspaces, orphans
git hop doctor --fix                  # auto-repair issues (aliases: check, repair)
git hop prune                         # remove orphaned worktrees/hubs from state
git hop prune --dry-run               # preview without applying (aliases: cleanup, clean)
git hop upgrade                       # check + install newer version
git hop upgrade --auto                # non-interactive upgrade
```

---

## Output Modes (global flags)

| Flag           | Effect                               |
|----------------|--------------------------------------|
| `--json`       | JSON output                          |
| `--porcelain`  | machine-readable (stable format)     |
| `--dry-run`    | preview changes, no writes           |
| `--force`      | bypass safety checks                 |
| `-q, --quiet`  | suppress non-error output            |
| `-v, --verbose`| extra diagnostic output              |
| `-g, --global` | use global hopspace (`$GIT_HOP_DATA_HOME`) |

---

## Shell Integration (auto-cd)

Pass `--enable-chdir` to `git hop init` to install the shell wrapper. `git hop add`/`remove`/`merge` then auto-`cd` to the resulting worktree.

```bash
git hop init --enable-chdir           # bash / zsh / fish — install on first run
git hop init                          # re-run on existing repo: installs wrapper if --enable-chdir
```

---

## Hooks

Priority: repo override → hopspace hook → global hook.

```
.git-hop/hooks/         (repo-level, created by git hop init)
$XDG_DATA_HOME/git-hop/<org>/<repo>/hooks/
$XDG_CONFIG_HOME/git-hop/hooks/
```

Available hooks:
- `pre-worktree-add`, `post-worktree-add`
- `pre-env-start`, `post-env-start`
- `pre-env-stop`, `post-env-stop`

---

## Common Tips and Failure Modes

| Symptom | Fix |
|---------|-----|
| `cd` not happening after add/merge | Run `git hop init --enable-chdir` |
| Wrong config loaded | `--config <path>` or set `XDG_CONFIG_HOME` |
| Orphaned worktrees in state | `git hop prune` |
| Stale state after manual branch delete | `git hop doctor --fix` |
| Services still up after worktree remove | `git hop env stop` first |
| `remove` blocked: "not merged into default" | `--force` (only if you really want to lose those commits) |
| `remove` blocked: "uncommitted changes or untracked files" | `--no-verify` |
| `remove` blocked: "not merged and not pushed" | `--force --no-verify` |
| `remove --no-prompt` exited 1 on a risky branch | Add `--force` / `--no-verify`; `--no-prompt` is **not** a gate bypass |
| Need to see changes before committing | `git hop <cmd> --dry-run` |
