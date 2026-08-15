# Hooks System

## Overview

git-hop includes a flexible hooks system that allows you to run custom scripts at specific points in the worktree lifecycle. Hooks can be configured at three different levels with a clear priority system.

> **Not to be confused with**: git-hop has a *second*, unrelated hook mechanism for environment services — shell command **strings declared in config**, not executable files on a hook path. It shares the "pre/post start/stop" vocabulary but nothing else. See [Two different hook systems](#two-different-hook-systems) before you go looking for a `post-env-start` file that will never fire.

## Available Hooks

This table is exhaustive against `ValidHookNames` in `internal/hooks/runner.go`. Every name accepted by `ValidateHookName` appears here, including the four that the validator accepts but nothing ever dispatches.

| Hook Name | When It Runs | Resolvable levels |
|-----------|--------------|-------------------|
| `pre-worktree-add` | `git hop add`, before the worktree is created. Non-zero exit aborts the add. | repo (via parent walk only — the worktree does not exist yet), hopspace, global |
| `post-worktree-add` | `git hop add`, after the worktree exists. Also fired during `git hop clone`, after the committed-hook mirror. Failure warns, does not roll back. | repo, hopspace, global |
| `pre-worktree-remove` | `git hop remove`, before the worktree is deleted. Non-zero exit aborts the remove. | repo, hopspace, global |
| `post-worktree-remove` | `git hop remove`, after the worktree is gone and state is updated. Failure warns. | hopspace, global (the repo-level file was inside the worktree that was just deleted) |
| `pre-worktree-move` | `git hop move`, before the rename. Non-zero exit aborts the move. Path is the OLD worktree. | repo, hopspace, global |
| `post-worktree-move` | `git hop move`, after the rename, symlink, state, and port/volume rekey. Path is the NEW worktree. Failure warns. | repo, hopspace, global |
| `pre-worktree-switch` | `git hop <branch>`, before the `current` symlink is rewritten. Non-zero exit aborts the switch. **Never fires for a plain `cd`** — see [Switch hooks](#switch-hooks). | repo, hopspace, global |
| `post-worktree-switch` | `git hop <branch>` after the symlink is written, and on a plain `cd` into a registered worktree. Failure warns. The only hook that may exit [93](#the-navigation-handled-directive-exit-93). | repo, hopspace, global |
| `pre-clone` | `git hop clone`, before any filesystem work. Non-zero exit aborts the clone. | hopspace, global **only** — see [Clone hooks](#clone-hooks) |
| `post-clone` | `git hop clone`, last of all, after state, symlink, mirror, and `post-worktree-add`. Failure warns. | repo, hopspace, global |
| `pre-repair` | `git hop repair`, before mutations are applied. Non-zero exit aborts. **Resolved by a separate code path** — global only. See [The repair hooks are different](#the-repair-hooks-are-different). | global **only** |
| `post-repair` | `git hop repair`, after mutations and post-verification. Exit status ignored entirely. **Global only**, same separate path. | global **only** |
| `pre-env-start` | **Never dispatched.** Accepted by `ValidateHookName` and mirrored by the installer, but no code fires it. | — |
| `post-env-start` | **Never dispatched.** Same. | — |
| `pre-env-stop` | **Never dispatched.** Same. | — |
| `post-env-stop` | **Never dispatched.** Same. | — |

The four `env-*` names are a reserved surface, not a working one. `git hop env start` / `env stop` run the *config-declared* hooks described under [Two different hook systems](#two-different-hook-systems); they never call the file-based runner. A file at `~/.config/git-hop/hooks/post-env-start` is valid, installable, mirrorable — and dead.

## Hook Priority System

When git-hop looks for a hook to execute, it searches in this order (first found wins):

1. **Repo-level override** — `.git-hop/hooks/<hook-name>` inside the worktree (the runner also walks parent directories so a hub-level `.git-hop/hooks/` is picked up)
2. **Hopspace-level hook** — `$XDG_DATA_HOME/git-hop/<host>/<org>/<repo>/hooks/<hook-name>` (only matches when the repoID has 3 slash-separated parts; see [Repository identifier](#repository-identifier))
3. **Global hook** — `$XDG_CONFIG_HOME/git-hop/hooks/<hook-name>`

This allows you to:
- Set global defaults for all repositories
- Override for specific repositories (hopspace)
- Override for specific worktrees (repo-level)

### Directory Locations by OS

git-hop resolves all paths through the XDG Base Directory specification. Linux and macOS share the same layout because the underlying `hop.top/kit/xdg` package follows XDG everywhere, including macOS — it does **not** use `~/Library/Preferences` or `~/Library/Application Support`.

**Linux and macOS:**

| Level | Default path |
|-------|--------------|
| Global | `~/.config/git-hop/hooks/` |
| Hopspace | `~/.local/share/git-hop/<host>/<org>/<repo>/hooks/` |
| Repo | `<worktree>/.git-hop/hooks/` |

Override with the standard XDG environment variables:

- `XDG_CONFIG_HOME` — relocates the global hooks dir (e.g. `$XDG_CONFIG_HOME/git-hop/hooks/`)
- `XDG_DATA_HOME` — relocates the hopspace base
- `GIT_HOP_DATA_HOME` — git-hop-specific override that wins over `XDG_DATA_HOME` for hopspace lookup

**Windows:**

The XDG kit maps to platform-native locations under the hood (typically `%APPDATA%` for config and `%LOCALAPPDATA%` for data). For the canonical resolution see `internal/hop/paths.go`. The repo-level path (`<worktree>/.git-hop/hooks/`) is the same on every platform.

### Repository identifier

The hopspace-level lookup keys off a 3-part repository identifier of the shape `<host>/<org>/<repo>` — for example `github.com/acme/widgets`. The runner splits the ID on `/` and only resolves a hopspace hook when there are at least three parts. So a hook for `github.com/acme/widgets` is looked up at:

```
~/.local/share/git-hop/github.com/acme/widgets/hooks/<hook-name>
```

A 2-part identifier such as `acme/widgets` **silently skips the hopspace lookup** — `FindHookFile` falls through to the global hook with no warning. For that reason, callers inside git-hop (e.g. `cmd/add.go`) always construct the repoID as `github.com/<org>/<repo>` so the hopspace lookup actually fires. If you are creating hopspace hooks by hand, mirror that 3-part shape on disk.

## Choosing a hook level

The three levels look interchangeable in the priority list, but they answer different questions. Pick the level that matches who needs the hook and when it must fire.

| Level | Storage | Versioned? | Best for |
|-------|---------|------------|----------|
| Repo | `<worktree>/.git-hop/hooks/` | Yes — committed in the repo | Team-shared hooks that travel with the codebase |
| Hopspace | `~/.local/share/git-hop/<host>/<org>/<repo>/hooks/` | No — local to your machine | Per-repo hooks that must fire on every `git hop add`, including the very first worktree |
| Global | `~/.config/git-hop/hooks/` | No — local to your machine | Defaults that apply to every repo on this machine unless overridden |

### The `post-worktree-add` chicken-and-egg trap

Repo-level hooks have a sharp edge for `post-worktree-add`. The hook file lives inside the worktree at `.git-hop/hooks/post-worktree-add`. When `git hop add` creates a fresh worktree from a branch that pre-dates the commit introducing the hook, that file is **not present** in the just-created worktree, so `FindHookFile` does not see it and the hook never fires. The first `git hop add` after introducing the hook silently skips it.

The runner does walk parent directories from the worktree path looking for a `.git-hop/hooks/` dir, so a hub-level repo hook can paper over the gap if you maintain one. But the canonical fix is to put the hook somewhere that does not depend on the worktree's content existing first — i.e. at the hopspace level.

### Recommendation

For any hook that **must** fire on every `git hop add` — bootstrap scripts, dependency installers, env-file copiers — install it at the hopspace level. The hopspace path is resolved from the repoID before the worktree is created, so it works on the very first `add` and on every `add` thereafter, regardless of which branch you start from.

For hooks that should travel with the repository so the whole team gets them, commit the canonical script at `<worktree>/.git-hop/hooks/<name>`. To get the best of both worlds, symlink the hopspace path to the committed file:

```bash
# One-time setup per machine, after cloning
mkdir -p ~/.local/share/git-hop/github.com/acme/widgets/hooks
ln -s "$(pwd)/.git-hop/hooks/post-worktree-add" \
  ~/.local/share/git-hop/github.com/acme/widgets/hooks/post-worktree-add
```

That way the committed hook is the single source of truth, and the hopspace symlink covers the bootstrap-time chicken-and-egg gap as well as the case where a teammate runs `git hop add <existing-old-branch>`.

**You usually do not have to do this by hand any more.** `git hop clone` and `git hop init` mirror committed `.git-hop/hooks/` into the hopspace for you — the symlink above is what `--hooks=symlink` automates. See [Committed-hook mirroring](#committed-hook-mirroring).

## Two different hook systems

git-hop has two mechanisms that both call themselves "hooks". They share vocabulary and nothing else. Everything in this document describes the **first** one unless it says otherwise.

| | **File-based lifecycle hooks** (this document) | **Config-declared environment hooks** |
|---|---|---|
| What a hook *is* | An executable file named after the hook | A shell command string in config |
| Where it lives | `.git-hop/hooks/<name>`, hopspace, or `~/.config/git-hop/hooks/<name>` | `hop.json` → `settings.environment.hooks.{preStart,postStart,preStop,postStop}` (arrays) |
| Names | `pre-worktree-add`, `post-worktree-switch`, … | `preStart`, `postStart`, `preStop`, `postStop` |
| Env var prefix | `GIT_HOP_*` (`GIT_HOP_WORKTREE_PATH`, `GIT_HOP_BRANCH`, …) | `HOP_*` (`HOP_WORKTREE_PATH`, `HOP_BRANCH`, `HOP_REPO_PATH`, `HOP_COMMAND`) |
| Fired by | `add`, `remove`, `move`, `<branch>`, `clone`, `repair`, plain `cd` | `git hop env start` / `git hop env stop` only |
| Implementation | `internal/hooks/runner.go` | `internal/services/env_hooks.go`, driven from `internal/services/env_managers.go` |
| Timeout | none | 5 minutes per hook list |

The trap: `ValidHookNames` contains `pre-env-start` / `post-env-start` / `pre-env-stop` / `post-env-stop`, so a file with one of those names installs cleanly and mirrors cleanly, and looks for all the world like it will run when you start services. It will not. The env lifecycle only ever consults the config-declared list. If you want a script to run around `env start`, declare it in config:

```jsonc
// hop.json
{
  "settings": {
    "environment": {
      "hooks": {
        "preStart":  ["scripts/load-secrets.sh"],
        "postStart": ["bash scripts/seed-db.sh"],
        "preStop":   ["scripts/flush-cache.sh"],
        "postStop":  []
      }
    }
  }
}
```

Paths are resolved relative to the worktree, and the command runs with the worktree as its working directory.

(There is a third, separate thing again: `HooksSchema` in `internal/config/schema_config.go` declares `preWorktreeAdd` / `preEnvStart` / … fields. Nothing reads them. They are inert config surface.)

## Switch hooks

`pre-worktree-switch` and `post-worktree-switch` fire when the user moves between worktrees. There are two ways that happens, and they are not symmetric.

### `git hop <branch>` — both hooks fire

Dispatched from `internal/cli/root.go`, in this order:

1. `pre-worktree-switch` — a non-zero exit aborts the switch **before** the `current` symlink is rewritten. The symlink is the load-bearing step: the binary's own `os.Chdir` only moves the git-hop process, while the shell wrapper navigates by resolving `current` after the binary exits. Vetoing before the symlink write is therefore a real veto.
2. `current` symlink updated.
3. `post-worktree-switch` — a failure warns and the switch still stands. This hook may exit [93](#the-navigation-handled-directive-exit-93).

`GIT_HOP_TRIGGER=hop`.

### Plain `cd` — only `post-worktree-switch` fires

The installed shell integration notices when `$PWD` lands inside a registered worktree and calls a hidden subcommand (`git hop __notify-chdir <path>`, `cmd/notify_chdir.go`), which dispatches `post-worktree-switch` with `GIT_HOP_TRIGGER=chdir`.

`pre-worktree-switch` **never** fires on this path, by design. A pre- hook exists to veto a switch that has not happened yet; by the time a chdir handler observes `$PWD`, the `cd` is already done and cannot be taken back. Offering a veto nothing can honour would be worse than offering none.

Other differences on the chdir path:

- The `current` symlink is **not** updated. A plain `cd` is not a hop.
- The handled-navigation directive is meaningless and is swallowed — nothing is waiting to `cd`, since the user already moved themselves.
- A failing hook prints `warning: hook post-worktree-switch failed: …` and nothing else. The `cd` already succeeded and is not undoable, so it must not be made to look broken.
- Moving between subdirectories of the *same* worktree reports nothing — the shell handler and the binary both check that the from- and to-worktrees differ.

### From-state variables

Both paths add these to the hook environment via `hooks.SwitchEnvVars`:

| Variable | Description |
|----------|-------------|
| `GIT_HOP_FROM_BRANCH` | Branch of the worktree being left |
| `GIT_HOP_FROM_WORKTREE_PATH` | Absolute path of the worktree being left |
| `GIT_HOP_TRIGGER` | `hop` (explicit `git hop <branch>`) or `chdir` (plain `cd`) |

**The from-state variables are absent, not empty, when there is no previous worktree.** This is the whole point of the design, and it is the difference between `[ -z "$GIT_HOP_FROM_BRANCH" ]` and `[ -v GIT_HOP_FROM_BRANCH ]` doing what you meant. `SwitchEnvVars` omits an empty field from the map entirely rather than exporting it as `""`, so the key never reaches the child process. A hook can therefore distinguish:

- **no previous worktree** — key unset. First hop after a clone, a fresh shell, or a `cd` in from somewhere that was not a registered worktree.
- **a previous worktree that happens to be named `""`** — impossible in practice, but the encoding does not conflate the two, so a hook that tests for presence stays correct.

Test presence, not emptiness:

```bash
#!/bin/bash
# post-worktree-switch
if [ -n "${GIT_HOP_FROM_WORKTREE_PATH+set}" ]; then
    echo "left $GIT_HOP_FROM_BRANCH for $GIT_HOP_BRANCH (via $GIT_HOP_TRIGGER)"
else
    echo "arrived at $GIT_HOP_BRANCH from outside any worktree"
fi
```

`GIT_HOP_TRIGGER` is likewise omitted when the trigger is empty, though in practice both dispatch sites always set it.

Where the from-state comes from differs by path, and the difference matters:

- **hop path**: read from the hub's `current` symlink *before* it is rewritten. A missing or dangling `current` is normal (first hop after a clone) and yields no from-state.
- **chdir path**: read from the shell's previous directory, passed explicitly as `GIT_HOP_CHDIR_FROM` (`$OLDPWD` is the fallback for anyone invoking the subcommand by hand). `current` cannot serve here: a plain `cd` never updates it, so after you hop and then `cd` away it still names the destination, not the origin.

### The navigation-handled directive (exit 93)

A `post-worktree-switch` hook that exits **93** is telling git-hop's shell wrapper: *I already moved the user. Do not `cd`.*

**Why it exists.** The wrapper's only channel back from the binary is `$?` — it runs `command git hop "$@"`, captures neither stream, and decides whether to `cd` only after the process is gone. Under a window-per-worktree integration (tmux, say), a `post-worktree-switch` hook selects the target worktree's own window; the user is already there. If the wrapper then also resolves `current` and `cd`s, it drags the **originating** window's shell into the same worktree. Two windows in one worktree, and the window you hopped away from is now silently pointed somewhere it never agreed to go.

**What happens.** The wrapper sees 93, returns **0** to the user's shell, and skips the `cd`. The switch itself already succeeded — symlink written, event published, success reported — so 93 is a success signal carrying extra information, not a failure. `RunResult.NavigationHandled` carries it as a typed result rather than folding it into the error return, and `internal/cli/root.go` re-raises it as git-hop's own exit status so it survives to the wrapper.

**Scope.** Honoured for `post-worktree-switch` and nothing else (`navigationHandledFor` in `internal/hooks/runner.go`). Any other hook exiting 93 is a plain failure, handled exactly like any other non-zero exit. Every other hook name runs in a context where nothing is about to navigate, so there is nothing to have "already handled" — honouring 93 there would only convert a genuine failure into a silent success.

**The compatibility guarantee.** A hook that knows nothing about this mechanism is unaffected. 93 is not a code anything else claims: git porcelain conventions fix 0/1/128/129, and 128+N is the shell's signal band. 93 sits outside both. Ignore the directive entirely and behaviour is exactly what it was before — exit 0, the wrapper `cd`s, done.

**When the integration is inactive, exit 0, not 93.** This is the part that is easy to get backwards. If tmux is not installed, or no tmux server is running, the hook has navigated *nothing* — so the wrapper's `cd` is the correct fallback and must be allowed to happen. Claiming 93 there strands the user in their original directory with a success message. Exit 93 only on the path where you actually moved them:

```bash
#!/bin/bash
# post-worktree-switch
command -v tmux >/dev/null 2>&1 || exit 0   # no tmux: not handled
tmux has-session 2>/dev/null || exit 0      # no server: not handled

tmux select-window -t "=$session:=$window" >/dev/null 2>&1 || exit 0
exit 93                                      # handled: stand down
```

The `93` constant is `hooks.ExitNavigationHandled`. See `examples/tmux/hooks/post-worktree-switch` for the full worked version, including the case where the user is running outside tmux while a session exists elsewhere — which also exits 0, because the *calling* shell was not moved.

## Clone hooks

`git hop clone` runs the widest hook sequence in git-hop, and **the ordering is load-bearing**:

```
pre-clone
  ↓  (clone; hopspace init; state; current symlink)
committed-hook mirror
  ↓
post-worktree-add
  ↓
post-clone
```

Dispatched from `internal/hop/clone_worktree.go`. Because `internal/hooks` already imports `internal/hop` (for `LooksLikeGitCheckout`), `internal/hop` cannot import `internal/hooks` back without an import cycle — so the dispatch is injected as callbacks (`HookDispatchOptions`), built by `buildHookDispatch` in `internal/cli/root.go`.

### Why mirror-then-fire

`post-worktree-add` fires **after** the committed-hook mirror on purpose. That single ordering is what lets a repo-level hook carried *by the clone* apply to the very worktree that carried it.

Walk it through. A repo commits `.git-hop/hooks/post-worktree-add`. You clone it. At the moment the initial worktree appears, that file is on disk inside it — but it is a *repo-level* hook, and nothing has yet made it visible at the hopspace level. The mirror step copies (or symlinks) it into `~/.local/share/git-hop/<host>/<org>/<repo>/hooks/`. Only after that does `post-worktree-add` dispatch, and now `FindHookFile` finds it — at repo level directly, and at hopspace level for every subsequent `git hop add` on any branch.

Move the dispatch above the mirror and the hook silently does not fire on the first worktree. Not an error — a silence. The repo's own bootstrap script skips exactly once, on the clone, which is the one run where you most need it.

### Committed-hook mirroring

Controlled by flags on `clone` (and equivalents on `init`):

| Flag | Effect |
|------|--------|
| `--hooks=symlink` | Hopspace hook becomes a symlink to the committed file (committed file stays the single source of truth) |
| `--hooks=copy` | Hopspace hook is a copy of the committed file |
| `--hooks=prompt` | Ask per hook. Degrades to `none` with a notice when stdin is not interactive |
| `--hooks=none` | Skip mirroring |
| `--hooks-overwrite` | Replace an existing hopspace hook whose content differs (symlink/copy modes only) |

Default is `prompt`. Only filenames in `ValidHookNames` are mirrored; anything else in `.git-hop/hooks/` is ignored. A repo with no `.git-hop/hooks/` directory is a silent no-op — most repos commit no hooks.

### `pre-clone` cannot resolve at repo level

There is no worktree yet. Nothing has been cloned, nothing is on disk. So the repo-level tier of `FindHookFile` has nothing to match against, and `pre-clone` can only ever resolve at **hopspace** or **global** level.

Put a `pre-clone` in the repo you are about to clone and it will not fire — it does not exist locally until after the step it was meant to precede.

### The parent-walk hazard

`FindHookFile` does not stop at the worktree. When no hook is found at `<worktree>/.git-hop/hooks/<name>`, it walks *parent directories* looking for `.git-hop/hooks/<name>` — which is deliberate and useful (a hub-level `.git-hop/hooks/` covers all its worktrees), but the walk **climbs all the way to the filesystem root**. It does not stop at the hub, at a repository boundary, or at `$HOME`.

For most hooks that is tolerable, because the anchor is a real worktree deep in a known tree. For `pre-clone` it would be actively dangerous: the natural anchor would be the caller's current directory, and a stray `.git-hop/hooks/pre-clone` in *any* ancestor of wherever the user happened to be standing would execute on every clone they run from that subtree.

So `pre-clone` is deliberately anchored on the **intended project root** — the directory the clone is about to create — rather than on the cwd. That directory does not exist yet and has no `.git-hop` anywhere below it, which makes the walk deterministic: it can only ever fall through to hopspace or global. That is precisely the intended reach for a `pre-clone` hook.

`GIT_HOP_BRANCH` is empty for `pre-clone`: resolving the default branch requires talking to the remote, which has not happened yet at that point in the sequence.

## The repair hooks are different

`pre-repair` and `post-repair` are dispatched (`cmd/repair.go`, in `runRepair` around the mutation-apply step) but they **do not go through `Runner` at all**. `runRepairHook` is a separate, hand-rolled path, and the differences are not cosmetic:

| | Every other hook | `pre-repair` / `post-repair` |
|---|---|---|
| Discovery | `Runner.FindHookFile` — repo → hopspace → global | `filepath.Join(hop.GetHooksDir(), name)` — **global only** |
| Repo-level hooks | honoured | **ignored** |
| Hopspace-level hooks | honoured | **ignored** |
| Parent-directory walk | yes | no |
| Working directory | inherited from git-hop's cwd | **set to the hub path** |
| Executable-bit check | enforced (non-Windows), error if missing | not checked — a non-executable file simply fails to run |
| Hook name validation | `ValidateHookName` | none (name is a literal in the source) |
| `stdout` | the process's stdout | **redirected to stderr** |
| Env vars | full `GIT_HOP_*` set | **none** — the hook inherits the ambient environment only |

Consequences worth internalising:

- A `.git-hop/hooks/pre-repair` committed in your repo **will never run.** Neither will a hopspace one. Only `~/.config/git-hop/hooks/pre-repair` is consulted.
- The hook receives **no** `GIT_HOP_WORKTREE_PATH`, `GIT_HOP_REPO_ID`, `GIT_HOP_BRANCH`, or `GIT_HOP_HOOK_NAME`. It gets `$PWD` set to the hub, and that is its entire context.
- `pre-repair` is a real veto: a non-zero exit aborts the repair before any mutation or backup. It only runs when the plan actually has mutations and `--dry-run` was not passed.
- `post-repair` is advisory in the strongest sense — its exit status is discarded (`_ = firePostRepairHook(hubPath)`).

This asymmetry is documented because it is a trap, not because it is a design people should copy.

## Shell integration and the chdir handler

`post-worktree-switch` on a plain `cd` depends on the shell integration installed by `git hop init --enable-chdir`. Mechanics, since they affect what your hook can assume:

- **bash** — `PROMPT_COMMAND`. bash has no chpwd hook, so the handler runs on every prompt, not only on a directory change.
- **zsh** — a real `chpwd` hook via `add-zsh-hook`, so it runs only on an actual directory change.
- **fish** — `function __git_hop_chdir --on-variable PWD`.

The handler must be cheap, because it runs at prompt rate. It answers "is `$PWD` a registered worktree?" with **shell builtins only** — no `stat`, no subshell, no fork — by prefix-testing `$PWD` against an array slurped once per session from a cache file (`internal/shell/roots.go`, `worktree-roots` under git-hop's cache dir, one absolute path per line, sorted and deduplicated). Only a match forks, into `git hop __notify-chdir`, which re-derives every field from `hop.json` before firing anything. The shell's job is to be cheap; the binary's is to be right — a cache entry naming a worktree that has since been removed is expected, not an error, and exits quietly.

The cache is written by the binary on each switch (`shell.MergeRootsCache`) and **merges** rather than replaces, so hopping in repo A does not blind the handler to repo B's worktrees. Entries only accumulate; pruning is the removing code's job, not the prompt-hot path's.

Practical consequences for hook authors:

- **A hook that `cd`s is fine.** A re-entrancy guard (`__git_hop_in_chdir`) makes the handler a no-op while it is already running, so a hook that changes directory does not recurse.
- **Before the first `git hop` of a session there may be no cache**, in which case a plain `cd` detects nothing. That is the normal pre-first-hop state.
- **Escape hatch**: set `GIT_HOP_NO_CHDIR_HOOK` to any non-empty value and the handler returns immediately, no reinstall needed. It is the first thing checked, so ruling it out as the cause of a slow prompt costs one string test.

  ```bash
  GIT_HOP_NO_CHDIR_HOOK=1 exec $SHELL   # this session only
  ```

## Worked example: tmux

[`examples/tmux/`](../examples/tmux/) is a complete integration exercising the full surface — one tmux session per repository, one window per worktree — built entirely from hooks, with no git-hop code involved.

It is the reference for several things this document only describes:

| Hook | What the example does |
|------|------------------------|
| `post-worktree-add` | Creates the worktree's window |
| `post-worktree-remove` | Kills that window |
| `post-worktree-move` | Renames the window in place (never kill-and-recreate — the window holds the user's dev server, REPL, editor buffers) |
| `post-worktree-switch` | Selects the window, then exits 93 — or 0 when tmux is unavailable |
| `post-clone` | Bootstraps the session for a freshly cloned repo |

Also worth reading there: why the hooks install at hopspace level rather than repo level, how tmux target names are derived so that `/` and `.` in branch names and repo IDs stay addressable, and the full reasoning behind the exit-93 asymmetry. `examples/tmux/test.sh` exercises the scripts directly by setting the `GIT_HOP_*` variables by hand — a useful template for testing your own hooks.

## Creating Hooks

### 1. Global Hooks

Global hooks apply to all repositories unless overridden:

```bash
# Create hooks directory (same path on Linux and macOS)
mkdir -p ~/.config/git-hop/hooks

# Create a hook
cat > ~/.config/git-hop/hooks/post-worktree-add << 'EOF'
#!/bin/bash
echo "Worktree ready for $GIT_HOP_BRANCH in $GIT_HOP_WORKTREE_PATH"
EOF

# Make it executable
chmod +x ~/.config/git-hop/hooks/post-worktree-add
```

### 2. Hopspace Hooks

Hopspace hooks apply to a specific repository across all worktrees:

```bash
# Example for github.com/org/repo
mkdir -p ~/.local/share/git-hop/github.com/org/repo/hooks

cat > ~/.local/share/git-hop/github.com/org/repo/hooks/post-worktree-add << 'EOF'
#!/bin/bash
# Run database migrations after creating a new worktree
cd "$GIT_HOP_WORKTREE_PATH"
npm run db:migrate
EOF

chmod +x ~/.local/share/git-hop/github.com/org/repo/hooks/post-worktree-add
```

### 3. Repo-Level Overrides

Repo-level hooks are checked into version control and override all others:

```bash
# From within a worktree
mkdir -p .git-hop/hooks

cat > .git-hop/hooks/pre-worktree-add << 'EOF'
#!/bin/bash
# Load secrets before the worktree is created
./scripts/load-secrets.sh
EOF

chmod +x .git-hop/hooks/pre-worktree-add

# Commit to version control
git add .git-hop/hooks/pre-worktree-add
git commit -m "Add pre-worktree-add hook"
```

**Note:** Repo-level hooks in `.git-hop/hooks/` can be committed to version control, making them available to all team members.

## Hook Environment Variables

All hooks receive these environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `GIT_HOP_HOOK_NAME` | Name of the hook being executed | `post-worktree-add` |
| `GIT_HOP_WORKTREE_PATH` | Absolute path to the worktree | `/home/user/projects/org/repo/feature-x` |
| `GIT_HOP_REPO_ID` | Repository identifier | `github.com/org/repo` |
| `GIT_HOP_BRANCH` | Branch name | `feature-x` |

Two exceptions:

- **`pre-clone`** receives an empty `GIT_HOP_BRANCH` (the default branch is not known until the remote is queried, which happens later) and a `GIT_HOP_WORKTREE_PATH` naming the project root that is about to be created — a directory that does not exist yet.
- **`pre-repair` / `post-repair`** receive **none** of these. See [The repair hooks are different](#the-repair-hooks-are-different).

### Move Variables

`pre-worktree-move` and `post-worktree-move` additionally receive:

| Variable | Description |
|----------|-------------|
| `GIT_HOP_OLD_BRANCH` | Branch name before the move |
| `GIT_HOP_NEW_BRANCH` | Branch name after the move |
| `GIT_HOP_OLD_PATH` | Worktree path before the move |
| `GIT_HOP_NEW_PATH` | Worktree path after the move |

`GIT_HOP_WORKTREE_PATH` and `GIT_HOP_BRANCH` track the *current* state: the old worktree for `pre-worktree-move`, the new one for `post-worktree-move`.

### Switch Variables

`pre-worktree-switch` and `post-worktree-switch` additionally receive `GIT_HOP_FROM_BRANCH`, `GIT_HOP_FROM_WORKTREE_PATH`, and `GIT_HOP_TRIGGER`. The from-state pair is **absent rather than empty** when there is no previous worktree — see [From-state variables](#from-state-variables).

### Branch Type Detection Variables

When a branch type is detected (via git-flow-next or custom prefixes), these additional variables are available:

| Variable | Description | Example |
|----------|-------------|---------|
| `GIT_HOP_BRANCH_TYPE` | Detected branch type | `feature` |
| `GIT_HOP_BRANCH_NAME` | Branch name without prefix | `my-feature` |
| `GIT_HOP_BRANCH_PREFIX` | Matched prefix | `feature/` |
| `GIT_HOP_BRANCH_PARENT` | Parent branch for this type | `develop` |
| `GIT_HOP_BRANCH_START_POINT` | Branch to start from | `develop` |
| `GIT_HOP_DETECTOR_SOURCE` | Which detector matched | `gitflow-next` |

Example hook using these variables:

```bash
#!/bin/bash
echo "Hook: $GIT_HOP_HOOK_NAME"
echo "Repo: $GIT_HOP_REPO_ID"
echo "Branch: $GIT_HOP_BRANCH"
echo "Path: $GIT_HOP_WORKTREE_PATH"

# Branch type detection (if available)
if [ -n "$GIT_HOP_BRANCH_TYPE" ]; then
    echo "Branch Type: $GIT_HOP_BRANCH_TYPE"
    echo "Branch Name: $GIT_HOP_BRANCH_NAME"
    echo "Parent Branch: $GIT_HOP_BRANCH_PARENT"
    echo "Detected by: $GIT_HOP_DETECTOR_SOURCE"
fi

# Change to worktree directory
cd "$GIT_HOP_WORKTREE_PATH"

# Branch-specific logic
if [ "$GIT_HOP_BRANCH" = "main" ]; then
    echo "Running production setup..."
elif [ "$GIT_HOP_BRANCH_TYPE" = "feature" ]; then
    echo "Running feature setup..."
else
    echo "Running development setup..."
fi
```

## Hook Execution

### Success and Failure

- **Exit code 0**: Hook succeeded, operation continues.
- **Non-zero exit code**: Hook failed.
- **Exit code 93** from `post-worktree-switch` only: success, plus "I handled navigation" — see [the navigation-handled directive](#the-navigation-handled-directive-exit-93). From any other hook, 93 is an ordinary failure.

What "failed" costs you depends on whether the hook is a `pre-` or a `post-`:

| | On non-zero exit |
|---|---|
| `pre-worktree-add`, `pre-worktree-remove`, `pre-worktree-move`, `pre-worktree-switch`, `pre-clone`, `pre-repair` | Operation **aborts**. Nothing is mutated. |
| `post-worktree-add`, `post-worktree-remove`, `post-worktree-move`, `post-worktree-switch`, `post-clone` | Warning printed; the operation **stands**. There is no rollback — the worktree/clone/switch already happened. |
| `post-repair` | Exit status **discarded entirely**. Not even a warning. |

A missing hook is not a failure: `FindHookFile` returns empty and the run silently succeeds. A hook file that exists but is not executable **is** a failure on non-Windows (checked before execution) — except on the repair path, which does not check.

Example blocking hook:

```bash
#!/bin/bash
# Block worktree creation if branch name doesn't follow convention

if [[ ! "$GIT_HOP_BRANCH" =~ ^(feature|bugfix|hotfix)/ ]]; then
    echo "Error: Branch name must start with feature/, bugfix/, or hotfix/"
    exit 1
fi

exit 0
```

### Hook Output

- `stdout` and `stderr` from hooks are displayed to the user
- Use this to provide feedback about what the hook is doing

### Execution Permissions

Hooks must be executable:

```bash
chmod +x path/to/hook
```

On Unix-like systems, git-hop verifies the executable bit before running a hook. On Windows, this check is skipped.

## Example Use Cases

### 1. Database Seeding

Seed the database once a new worktree exists:

```bash
#!/bin/bash
# post-worktree-add

cd "$GIT_HOP_WORKTREE_PATH"

echo "Seeding database for $GIT_HOP_BRANCH..."
npm run db:seed
```

> To run something around `git hop env start` / `env stop` instead, use the **config-declared** `preStart`/`postStart`/`preStop`/`postStop` lists — a file named `post-env-start` will never fire. See [Two different hook systems](#two-different-hook-systems).

### 2. Cleanup Before Removal

Clean up temporary files and containers before a worktree is torn down:

```bash
#!/bin/bash
# pre-worktree-remove

cd "$GIT_HOP_WORKTREE_PATH" || exit 0

echo "Cleaning up temporary files..."
rm -rf tmp/* logs/*.log
```

### 3. Environment-Specific Setup

Load different configurations per branch:

```bash
#!/bin/bash
# post-worktree-add

cd "$GIT_HOP_WORKTREE_PATH"

if [ "$GIT_HOP_BRANCH" = "main" ]; then
    cp .env.production .env
elif [ "$GIT_HOP_BRANCH" = "staging" ]; then
    cp .env.staging .env
else
    cp .env.development .env
fi

echo "Environment configured for $GIT_HOP_BRANCH"
```

### 4. Notification on Switch

Send a notification when you land in a different worktree — including via a plain `cd`:

```bash
#!/bin/bash
# post-worktree-switch

msg="Now on $GIT_HOP_BRANCH"
if [ -n "${GIT_HOP_FROM_BRANCH+set}" ]; then
    msg="$GIT_HOP_FROM_BRANCH -> $GIT_HOP_BRANCH ($GIT_HOP_TRIGGER)"
fi

# macOS notification
osascript -e "display notification \"$msg\" with title \"git-hop\""

# Linux notification (requires notify-send)
# notify-send "git-hop" "$msg"

exit 0   # 0, not 93: we navigated nothing
```

### 5. Dependency Installation

Install dependencies after creating a worktree:

```bash
#!/bin/bash
# post-worktree-add

cd "$GIT_HOP_WORKTREE_PATH"

echo "Installing dependencies for $GIT_HOP_BRANCH..."

# Check for package.json
if [ -f package.json ]; then
    npm ci
fi

# Check for go.mod
if [ -f go.mod ]; then
    go mod download
fi

echo "Dependencies installed"
```

### 6. Branch Name Validation

Enforce branch naming conventions:

```bash
#!/bin/bash
# pre-worktree-add

VALID_PREFIXES="^(feature|bugfix|hotfix|release)/"

if [[ ! "$GIT_HOP_BRANCH" =~ $VALID_PREFIXES ]]; then
    echo "❌ Invalid branch name: $GIT_HOP_BRANCH"
    echo "Branch must start with: feature/, bugfix/, hotfix/, or release/"
    exit 1
fi

echo "✓ Branch name is valid"
exit 0
```

### 7. Git-Flow Integration

git-hop has **built-in integration** with [git-flow-next](https://github.com/gittower/git-flow-next) that automatically detects branch types and runs appropriate git-flow commands.

#### Built-in Detection

When you run `git hop add feature/my-feature`, git-hop:

1. **Detects the branch type** by reading your git-flow configuration
2. **Runs `git flow feature start my-feature`** automatically
3. **Creates the worktree**
4. **Sets environment variables** for hooks to use

Similarly, `git hop remove feature/my-feature` will run `git flow feature finish my-feature` before removing the worktree.

This works with **any branch types configured in git-flow-next**, including custom types:

```bash
# Configure a custom branch type in git-flow
git config gitflow.branch.bugfix.type topic
git config gitflow.branch.bugfix.parent develop
git config gitflow.branch.bugfix.prefix bugfix/

# git-hop automatically detects it
git hop add bugfix/fix-login  # Runs: git flow bugfix start fix-login
```

#### Environment Variables

When a branch type is detected, hooks receive these additional variables:

| Variable | Description |
|----------|-------------|
| `GIT_HOP_BRANCH_TYPE` | The detected branch type (feature, release, etc.) |
| `GIT_HOP_BRANCH_NAME` | Branch name without prefix |
| `GIT_HOP_BRANCH_PARENT` | Parent branch from git-flow config |
| `GIT_HOP_DETECTOR_SOURCE` | Which detector matched (`gitflow-next` or `generic`) |

#### Example: Extend Git-Flow Behavior

Hooks can extend the built-in git-flow integration:

```bash
#!/bin/bash
# post-worktree-add - Run tests after feature branch starts

# Only for feature branches
if [ "$GIT_HOP_BRANCH_TYPE" = "feature" ]; then
    cd "$GIT_HOP_WORKTREE_PATH"
    
    echo "Running initial tests for $GIT_HOP_BRANCH_NAME..."
    npm test
fi
```

#### Example: Custom Validation

```bash
#!/bin/bash
# pre-worktree-add - Validate branch names

# Use detected branch type info
if [ -n "$GIT_HOP_BRANCH_TYPE" ]; then
    echo "Detected $GIT_HOP_BRANCH_TYPE branch: $GIT_HOP_BRANCH_NAME"
    
    # Ensure release branches follow semver
    if [ "$GIT_HOP_BRANCH_TYPE" = "release" ]; then
        if [[ ! "$GIT_HOP_BRANCH_NAME" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            echo "Error: Release must use semver (e.g., v1.2.3)"
            exit 1
        fi
    fi
fi

exit 0
```

#### Workflow

| Command | Built-in Action | Git-Hop Action |
|---------|-----------------|----------------|
| `git hop add feature/my-feature` | `git flow feature start my-feature` | Creates worktree |
| `git hop remove feature/my-feature` | `git flow feature finish my-feature` | Removes worktree |
| `git hop add release/v1.0.0` | `git flow release start v1.0.0` | Creates worktree |
| `git hop remove release/v1.0.0` | `git flow release finish v1.0.0` | Removes worktree |

#### Manual Hook Integration (Optional)

If you need custom git-flow behavior not handled by the built-in detector, you can still use hooks:

```bash
#!/bin/bash
# pre-worktree-add - Custom git-flow logic

# Skip if git-flow-next already handled it
if [ "$GIT_HOP_DETECTOR_SOURCE" = "gitflow-next" ]; then
    exit 0  # Already handled by built-in detector
fi

# Your custom logic here
```

## Installing Hook Directories

The `.git-hop/hooks` directory is created automatically by `git hop init`.
To skip this, use `--no-hooks`:

```bash
git hop init           # creates .git-hop/hooks/ automatically
git hop init --no-hooks  # skip hook directory creation
```

Re-running `git hop init` on an already-initialized repo also ensures the
hooks directory exists (unless `--no-hooks` is passed).

## Debugging Hooks

### Verbose Output

Add debugging to your hooks:

```bash
#!/bin/bash
set -x  # Print each command before executing

echo "Starting hook: $GIT_HOP_HOOK_NAME"
# ... rest of hook
```

### Testing Hooks Manually

You can test hooks manually by setting the environment variables:

```bash
export GIT_HOP_HOOK_NAME="post-worktree-add"
export GIT_HOP_WORKTREE_PATH="/path/to/worktree"
export GIT_HOP_REPO_ID="github.com/org/repo"
export GIT_HOP_BRANCH="feature-x"

# Run the hook
~/.config/git-hop/hooks/post-worktree-add
```

For a switch hook, add the from-state — and note that testing the "no previous worktree" case means leaving the variables **unset**, not setting them empty:

```bash
export GIT_HOP_HOOK_NAME="post-worktree-switch"
export GIT_HOP_TRIGGER="hop"
export GIT_HOP_FROM_BRANCH="main"
export GIT_HOP_FROM_WORKTREE_PATH="/path/to/main"

~/.config/git-hop/hooks/post-worktree-switch
echo "exit: $?"     # 93 means the hook claims it handled navigation

# The first-hop case:
unset GIT_HOP_FROM_BRANCH GIT_HOP_FROM_WORKTREE_PATH
~/.config/git-hop/hooks/post-worktree-switch
```

`examples/tmux/test.sh` is a fuller worked version of this pattern.

### Common Issues

**Hook not executing:**
- Check that the hook file exists in one of the priority locations
- Verify the hook is executable: `ls -l path/to/hook`
- Ensure the hook name is spelled correctly (`ValidHookNames` in `internal/hooks/runner.go` is the list)
- Check for syntax errors in the script

**Hook named `pre-env-start` / `post-env-start` / `pre-env-stop` / `post-env-stop` never runs:**
Expected. Nothing dispatches those names — see [Two different hook systems](#two-different-hook-systems).

**`pre-repair` / `post-repair` in the repo or hopspace never runs:**
Expected. Those two resolve at the global level only — see [The repair hooks are different](#the-repair-hooks-are-different).

**`post-worktree-switch` does not fire on a plain `cd`:**
The shell integration is what detects that. Confirm it is installed (`git hop init --enable-chdir`), that `GIT_HOP_NO_CHDIR_HOOK` is unset, and that you have run `git hop <branch>` at least once so the worktree-roots cache exists. Moving between subdirectories of the same worktree is deliberately silent.

**`pre-worktree-switch` does not fire on a plain `cd`:**
By design, and it never will — the `cd` already happened and is not abortable.

**Permission denied:**
```bash
chmod +x path/to/hook
```

**Wrong hook directory:**
- Verify you're using the correct XDG directory for your OS
- Check `echo $XDG_CONFIG_HOME` and `echo $XDG_DATA_HOME`

## Security Considerations

### Repo-Level Hooks and Version Control

Repo-level hooks in `.git-hop/hooks/` can be committed to version control. This is convenient for sharing hooks with your team, but consider:

- **Code review:** Review hook scripts carefully before merging
- **Trust:** Only commit hooks from trusted sources
- **Permissions:** Users must explicitly make hooks executable on their machine

### Global and Hopspace Hooks

Global and hopspace hooks are stored locally and never committed to version control:

- Safe to include sensitive operations (API keys, credentials)
- Use environment variables for secrets, not hardcoded values
- Consider using dedicated secret management tools

## Known limitations

- **`git hop add --dry-run` still creates the worktree.** `cmd/add.go` does not read the flag at all — the worktree, port allocation, and both `worktree-add` hooks run as if `--dry-run` were not passed. Treat the flag as a no-op on `add`.
- **`pre-env-start` / `post-env-start` / `pre-env-stop` / `post-env-stop` are accepted but never dispatched.** They validate, install, and mirror; nothing fires them. See [Two different hook systems](#two-different-hook-systems).
- **`pre-repair` / `post-repair` resolve at the global level only** and receive no `GIT_HOP_*` variables, unlike every other hook. See [The repair hooks are different](#the-repair-hooks-are-different).
- **`pre-clone` cannot resolve at repo level**, since no worktree exists when it runs. Hopspace and global only.
- **`FindHookFile`'s parent walk climbs to the filesystem root** — it does not stop at the hub or at `$HOME`. See [The parent-walk hazard](#the-parent-walk-hazard).
- **Repo-level `post-worktree-remove` cannot fire**, because the file lived inside the worktree that was just deleted. Install it at hopspace or global level.

Resolved by the committed-hook mirror, previously listed here:

- ~~Repo-level `post-worktree-add` does not fire on the bootstrap worktree.~~ `git hop clone` and `git hop init` now mirror committed `.git-hop/hooks/` into the hopspace *before* dispatching `post-worktree-add`, so a hook carried by the clone applies to the worktree that carried it. The manual symlink is now `--hooks=symlink`. See [Committed-hook mirroring](#committed-hook-mirroring). The chicken-and-egg trap still applies to `git hop add <old-branch>` if you never mirrored — [Choosing a hook level](#choosing-a-hook-level) still stands as guidance.

## Implementation Details

For developers interested in the implementation:

| Concern | Where |
|---|---|
| Hook name list (the authority) | `ValidHookNames`, `internal/hooks/runner.go` |
| Name validation | `ValidateHookName()`, same file |
| Discovery / priority | `FindHookFile()`, plus `findHookInParentDirs()` for the parent walk |
| Execution, env, exit-code handling | `Runner.run()`, behind `ExecuteHook` / `ExecuteHookWithDetector` |
| Navigation directive | `ExitNavigationHandled`, `RunResult`, `navigationHandledFor()` |
| Switch env vars | `SwitchEnvVars()` — omits empty fields rather than exporting them empty |
| Hooks-dir creation | `InstallHooks()`; `git hop init` calls it unless `--no-hooks` |
| Committed-hook mirror | `MirrorCommittedHooks()`, `internal/hooks/install.go` |
| Clone dispatch (callback injection) | `HookDispatchOptions`, `internal/hop/clone_worktree.go`; wired by `buildHookDispatch()` in `internal/cli/root.go` |
| Switch dispatch (`git hop <branch>`) | `internal/cli/root.go` |
| Switch dispatch (plain `cd`) | `cmd/notify_chdir.go` |
| Repair dispatch (separate path) | `runRepairHook()`, `cmd/repair.go` |
| Shell wrapper + chdir handler | `internal/shell/wrapper.go`, `internal/shell/chpwd.go` |
| Worktree-roots cache | `internal/shell/roots.go` |
| Config-declared env hooks (the *other* system) | `internal/services/env_hooks.go`, `internal/services/env_managers.go` |

`internal/hop` cannot import `internal/hooks` — `internal/hooks` already imports `internal/hop` for `LooksLikeGitCheckout`, so the reverse edge would be a cycle. That is why clone-time dispatch is injected as callbacks from `internal/cli` rather than called directly.

The hooks system:
- Uses the standard Unix executable model
- Provides environment variables for context
- Follows XDG Base Directory specification
- Supports all scripting languages (bash, python, node, etc.)
