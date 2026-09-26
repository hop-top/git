# Hooks System

## Overview

git-hop includes a flexible hooks system that allows you to run custom scripts at specific points in the worktree lifecycle. Hooks can be configured at three different levels with a clear priority system.

> **Not to be confused with**: git-hop has a *second*, unrelated hook mechanism for environment services — shell command **strings declared in config**, not executable files on a hook path. It shares the "pre/post start/stop" vocabulary but nothing else. See [Two different hook systems](#two-different-hook-systems) before you go looking for a `post-env-start` file that will never fire.

## Available Hooks

This table is exhaustive against `ValidHookNames` in `internal/hooks/runner.go`. Every name accepted by `ValidateHookName` appears here, including the four that the validator accepts but nothing ever dispatches.

| Hook Name | When It Runs | Resolvable levels |
|-----------|--------------|-------------------|
| `pre-worktree-add` | `git hop add`, before the worktree is created (and, with `hop.gitflow.enabled`, before `git flow start` creates the branch). Non-zero exit aborts the add. | repo (via parent walk only — the worktree does not exist yet), hopspace, global |
| `post-worktree-add` | `git hop add`, after the worktree exists and its environment is set up: `.env`, compose override and linked shared deps exist when the hook runs — see [Add hooks](#add-hooks). Also fired by `git hop clone` and by `git hop init` (bare or regular conversion) for the initial worktree, after the same set-up and the committed-hook mirror — see [Clone hooks](#clone-hooks) and [Init hooks](#init-hooks). Failure warns, does not roll back. | repo, hopspace, global |
| `pre-worktree-remove` | `git hop remove`, before the worktree is deleted. Non-zero exit aborts the remove. | repo, hopspace, global |
| `post-worktree-remove` | `git hop remove`, after the worktree is gone and state is updated. Failure warns. | hopspace, global (the repo-level file was inside the worktree that was just deleted) |
| `pre-worktree-move` | `git hop move`, before the rename and after the move's own refusals (target already registered, target an existing local branch the worktree does not have checked out, hopspace unreadable), so a move git-hop rejects never fires it. Non-zero exit aborts the move. Path is the OLD worktree. | repo, hopspace, global |
| `post-worktree-move` | `git hop move`, after the rename, symlink, state, and port/volume rekey. Path is the NEW worktree. Failure warns. | repo, hopspace, global |
| `pre-worktree-switch` | `git hop <branch>`, before the `current` symlink is rewritten. Non-zero exit aborts the switch. **Never fires for a plain `cd`** — see [Switch hooks](#switch-hooks). | repo, hopspace, global |
| `post-worktree-switch` | `git hop <branch>` after the symlink is written, and on a plain `cd` into a registered worktree. Failure warns. The only hook that may exit [93](#the-navigation-handled-directive-exit-93). | repo, hopspace, global |
| `pre-clone` | `git hop clone`, before any filesystem work. Non-zero exit aborts the clone. | hopspace, global **only** — no repo level and no parent walk; see [`pre-clone` has no repo level](#pre-clone-has-no-repo-level) |
| `post-clone` | `git hop clone`, after state, symlink, mirror, the initial worktree's environment set-up, and `post-worktree-add`: its `.env`, compose override and linked shared deps exist when the hook runs. The optional environment start (`--env-start`, `hop.env.autoStart`) comes after it. Failure warns. | repo, hopspace, global |
| `pre-repair` | `git hop repair`, before the backup and any mutation; only when the plan has mutations and `--dry-run` was not passed. Non-zero exit aborts. See [Repair hooks](#repair-hooks). | repo (anchored on the hub: `<hub>/.git-hop/hooks/`, plus parent walk), hopspace, global |
| `post-repair` | `git hop repair`, after mutations and post-verification. Exit status ignored entirely. | repo (anchored on the hub), hopspace, global |
| `pre-env-start` | **Never dispatched.** Accepted by `ValidateHookName` and mirrored by the installer, but no code fires it. | — |
| `post-env-start` | **Never dispatched.** Same. | — |
| `pre-env-stop` | **Never dispatched.** Same. | — |
| `post-env-stop` | **Never dispatched.** Same. | — |

The four `env-*` names are a reserved surface, not a working one. `git hop env start` / `env stop` run the *config-declared* hooks described under [Two different hook systems](#two-different-hook-systems); they never call the file-based runner. A file at `~/.config/git-hop/hooks/post-env-start` is valid, installable, mirrorable — and dead.

## Hook Priority System

When git-hop looks for a hook to execute, it searches in this order (first found wins):

1. **Repo-level override** — `.git-hop/hooks/<hook-name>` inside the worktree (the runner also walks parent directories so a hub-level `.git-hop/hooks/` is picked up)
2. **Hopspace-level hook** — `$XDG_DATA_HOME/git-hop/<org>/<repo>/hooks/<hook-name>` with the default [`hop.dataLayout`](configuration.md#settings-reference), then the location earlier releases used, `$XDG_DATA_HOME/git-hop/github.com/<org>/<repo>/hooks/<hook-name>` (only matches when the repoID has 3 slash-separated parts; see [Repository identifier](#repository-identifier))
3. **Global hook** — `$XDG_CONFIG_HOME/git-hop/hooks/<hook-name>`

One exception: `pre-clone` skips tier 1 entirely (no repo-level lookup, no parent walk), because the repo is not on disk yet. See [`pre-clone` has no repo level](#pre-clone-has-no-repo-level).

This allows you to:
- Set global defaults for all repositories
- Override for specific repositories (hopspace)
- Override for specific worktrees (repo-level)

### Directory Locations by OS

git-hop resolves its paths through the `hop.top/kit/xdg` package. A set `XDG_*` variable wins on every platform; unset, the default is the platform's own location, so macOS uses `~/Library/Application Support`, not `~/.config` or `~/.local/share`.

| Level | Linux default | macOS default |
|-------|---------------|---------------|
| Global | `~/.config/git-hop/hooks/` | `~/Library/Application Support/git-hop/hooks/` |
| Hopspace | `~/.local/share/git-hop/<org>/<repo>/hooks/` | `~/Library/Application Support/git-hop/<org>/<repo>/hooks/` |
| Repo | `<worktree>/.git-hop/hooks/` | `<worktree>/.git-hop/hooks/` |

The hopspace directory follows `hop.dataLayout`. On macOS global and hopspace hooks share one base directory: `git-hop/hooks/` there holds the global hooks, `git-hop/<org>/<repo>/hooks/` a repository's hopspace hooks.

Override with the standard XDG environment variables (the macOS defaults above apply only while they are unset):

- `XDG_CONFIG_HOME` — relocates the global hooks dir (e.g. `$XDG_CONFIG_HOME/git-hop/hooks/`)
- `XDG_DATA_HOME` — relocates the hopspace base
- `GIT_HOP_DATA_HOME` — git-hop-specific override that wins over `XDG_DATA_HOME` for hopspace lookup

**Windows:**

The XDG kit maps to platform-native locations under the hood (typically `%APPDATA%` for config and `%LOCALAPPDATA%` for data). For the canonical resolution see `internal/hop/paths.go`. The repo-level path (`<worktree>/.git-hop/hooks/`) is the same on every platform.

### Repository identifier

The hopspace-level lookup keys off a 3-part repository identifier of the shape `<host>/<org>/<repo>` — for example `gitlab.example.com/acme/widgets`. The host is the one of the repository's origin URL (`gitlab.example.com` for `git@gitlab.example.com:acme/widgets.git`), or `hop.gitDomain` (default `github.com`, resolved like any setting: the hub's own value over `--global`, `git -c` over both) when the origin has none: a local path, a `file://` URL, no origin. It is the ID state keys the repository by, and hooks see it as `GIT_HOP_REPO_ID`.

The runner splits the ID on `/` and only resolves a hopspace hook when there are three parts. The org and repo come from the ID; the directory is the repository's hopspace in the data home, laid out by `hop.dataLayout` (default `{org}/{repo}`). So a hook for `gitlab.example.com/acme/widgets` is looked up at:

```
~/.local/share/git-hop/acme/widgets/hooks/<hook-name>
~/.local/share/git-hop/github.com/acme/widgets/hooks/<hook-name>   # earlier releases
```

The first match wins. With `hop.dataLayout` set to `{host}/{org}/{repo}` the first directory is `~/.local/share/git-hop/gitlab.example.com/acme/widgets/hooks/`. The second is always under `github.com`, whatever the origin: earlier releases gave every repository a `github.com` ID.

Earlier releases mirrored hooks to `github.com/<org>/<repo>/hooks/` under the data home. Those hooks keep firing. `git hop doctor` warns about each such directory, and `git hop doctor --fix` moves it to the `hop.dataLayout` location when nothing is there yet (`--fix --dry-run` previews the move). With hooks at both locations it moves nothing and names both, so you can merge them by hand.

A 2-part identifier such as `acme/widgets` **silently skips the hopspace lookup** — `FindHookFile` falls through to the global hook with no warning. For that reason, callers inside git-hop (e.g. `cmd/add.go`) always build the repoID in full, `<host>/<org>/<repo>`, so the hopspace lookup actually fires.

## Choosing a hook level

The three levels look interchangeable in the priority list, but they answer different questions. Pick the level that matches who needs the hook and when it must fire.

| Level | Storage | Versioned? | Best for |
|-------|---------|------------|----------|
| Repo | `<worktree>/.git-hop/hooks/` | Yes — committed in the repo | Team-shared hooks that travel with the codebase |
| Hopspace | `~/.local/share/git-hop/<org>/<repo>/hooks/` | No — local to your machine | Per-repo hooks that must fire on every `git hop add`, including the very first worktree |
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
| Where it lives | `.git-hop/hooks/<name>`, hopspace, or `~/.config/git-hop/hooks/<name>` | `hop.json` → `settings.environmentConfig.hooks.{preStart,postStart,preStop,postStop}` (arrays) |
| Names | `pre-worktree-add`, `post-worktree-switch`, … | `preStart`, `postStart`, `preStop`, `postStop` |
| Env var prefix | `GIT_HOP_*` (`GIT_HOP_WORKTREE_PATH`, `GIT_HOP_BRANCH`, …) | `HOP_*` (`HOP_WORKTREE_PATH`, `HOP_BRANCH`, `HOP_REPO_PATH`, `HOP_COMMAND`) |
| Fired by | `add`, `remove`, `move`, `<branch>`, `clone`, `init`, `repair`, plain `cd` | `git hop env start` / `git hop env stop` only |
| Implementation | `internal/hooks/runner.go` | `internal/services/env_hooks.go`, driven from `internal/services/env_managers.go` |
| Timeout | none | 5 minutes per hook list; a hook still running then is killed with every process it started (process group on unix, job object on Windows, best-effort) |

The trap: `ValidHookNames` contains `pre-env-start` / `post-env-start` / `pre-env-stop` / `post-env-stop`, so a file with one of those names installs cleanly and mirrors cleanly, and looks for all the world like it will run when you start services. It will not. The env lifecycle only ever consults the config-declared list. If you want a script to run around `env start`, declare it in config:

```jsonc
// hop.json
{
  "settings": {
    "environmentConfig": {
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

## Add hooks

`git hop add` sets up the new worktree's environment **before** any hook of that worktree fires, so every hook finds it ready:

```
pre-worktree-add
  ↓  (worktree created; ignored local files copied from the source worktree)
environment set-up: ports, volumes, .env, compose override, then shared deps
  ↓
post-worktree-add
  ↓  (hop.json, state, current symlink; worktree.created, deps.installed)
environment start, only with --env-start / hop.env.autoStart
```

What exists when each hook runs:

| Hook | Worktree | `.env` + compose override | Shared deps (`node_modules`, ...) | Registered in `hop.json` | Environment running |
|---|---|---|---|---|---|
| `pre-worktree-add` | no | no | no | no | no |
| `post-worktree-add` | yes | yes | yes (linked) | no | no |
| `post-worktree-add` from `git hop init` (bare: `hops/<branch>`; `--regular`: the repo root) | yes | yes | yes (linked) | yes | no |

The `.env` and override exist only for a worktree with a Docker environment (a compose file); the deps only for a worktree with a detected package manager and lockfile. The set-up never fails the add: a generation or deps failure is reported, and `post-worktree-add` still fires, finding whatever did get set up.

> **Behaviour change.** `post-worktree-add` used to run before the set-up. A hook that writes its own `.env` now finds git-hop's already there, with the allocated `HOP_PORT_*` values: append to it (`>>`) rather than overwrite it, or the ports are lost. A hook that installs dependencies now finds them linked into the hopspace's shared store; installing again writes through that link into the store other worktrees share, so leave dependencies to git-hop.

## Clone hooks

`git hop clone` runs the widest hook sequence in git-hop, and **the ordering is load-bearing**:

```
pre-clone
  ↓  (clone; hopspace init; state; current symlink)
committed-hook mirror
  ↓  (environment set-up: ports, volumes, .env, compose override, shared deps;
      then events: hopspace.initialized, worktree.created, deps.installed)
post-worktree-add
  ↓
post-clone
  ↓  (environment start, only with --env-start / hop.env.autoStart)
```

Dispatched from `internal/hop/clone_worktree.go`. Because `internal/hooks` already imports `internal/hop` (for `LooksLikeGitCheckout`), `internal/hop` cannot import `internal/hooks` back without an import cycle — so the dispatch is injected as callbacks (`HookDispatchOptions`), built by `BuildHookDispatch` in `internal/cli/root.go`. `git hop init` reuses the same builder — see [Init hooks](#init-hooks).

The environment is set up before `post-worktree-add` through the same function as `git hop add` (`services.SetUpWorktree`), so both `post-worktree-add` and `post-clone` can read the allocated ports from the worktree's `.env` and find its shared deps linked. A set-up failure is reported and the clone continues; both hooks still fire. The set-up is injected like the hooks (`HookDispatchOptions.SetUpEnv`) and is not itself a hook. The [behaviour change](#add-hooks) noted for add applies here too. The [lifecycle events](#lifecycle-events) of the hub and its initial worktree are published right after the set-up, in the order add publishes its own; the hub and worktree are registered by then, but unlike add's, these events precede `post-worktree-add`.

### Why mirror-then-fire

`post-worktree-add` fires **after** the committed-hook mirror on purpose. That single ordering is what lets a repo-level hook carried *by the clone* apply to the very worktree that carried it.

Walk it through. A repo commits `.git-hop/hooks/post-worktree-add`. You clone it. At the moment the initial worktree appears, that file is on disk inside it — but it is a *repo-level* hook, and nothing has yet made it visible at the hopspace level. The mirror step copies (or symlinks) it into `~/.local/share/git-hop/<org>/<repo>/hooks/`. Only after that does `post-worktree-add` dispatch, and now `FindHookFile` finds it — at repo level directly, and at hopspace level for every subsequent `git hop add` on any branch.

Move the dispatch above the mirror and the hook silently does not fire on the first worktree. Not an error — a silence. The repo's own bootstrap script skips exactly once, on the clone, which is the one run where you most need it.

### Committed-hook mirroring

Controlled by flags on `clone` (and equivalents on `init`):

| Flag | Effect |
|------|--------|
| `--hooks=symlink` | Hopspace hook becomes a symlink to the committed file (committed file stays the single source of truth) |
| `--hooks=copy` | Hopspace hook is a copy of the committed file |
| `--hooks=prompt` | Ask per hook. Degrades to `none` with a `hint:` when stdin is not interactive |
| `--hooks=none` | Skip mirroring |
| `--hooks-overwrite` | Replace an existing hopspace hook whose content differs (symlink/copy modes only) |

Default is `prompt`. Only filenames in `ValidHookNames` are mirrored; anything else in `.git-hop/hooks/` is ignored. A repo with no `.git-hop/hooks/` directory is a silent no-op — most repos commit no hooks.

When committed hooks are not mirrored (mode `none`, a non-interactive `prompt`, or a hook that is not executable), a `hint:` names the remedy: run `git hop init --hooks=<mode>` inside the worktree, which mirrors again without re-cloning. `-q` drops the hint. A prompt that gets no answer (stdin at end of input, e.g. `</dev/null`) leaves the unanswered hooks unmirrored with a `warning:`, and its hint also names `git config hop.hooks.installMode symlink`, which mirrors without asking; the hooks answered before it are mirrored.

Every hook is decided first, prompts included, and the hooks are then written together while holding the `hop.json.lock` of the data-home hopspace the hooks directory belongs to (`$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json.lock` by default), never while a prompt waits. `git hop doctor --fix` moves hooks directories under the same lock, so a move waits for the hooks being written and carries them, and a mirror waits for a move in progress. A mirror that finds the directory moved away once it holds the lock writes nothing there: it looks up where `hop.dataLayout` now puts the directory and writes there. A hook that appeared meanwhile with different content is kept, with a `warning:`, unless `--hooks-overwrite` or a prompt answered for it allows replacing it.

### `pre-clone` has no repo level

There is no worktree yet. Nothing has been cloned, nothing is on disk. So `FindHookFile` skips the repo tier for `pre-clone` altogether: no `<path>/.git-hop/hooks/pre-clone` lookup and no parent walk. `pre-clone` resolves at **hopspace** or **global** level only.

Put a `pre-clone` in the repo you are about to clone and it will not fire — it does not exist locally until after the step it was meant to precede. Put one in the directory you run the clone from, or in any ancestor of it, and it will not fire either: those directories are not part of the repo being cloned.

### The parent-walk hazard

For every other hook, `FindHookFile` does not stop at the worktree. When no hook is found at `<worktree>/.git-hop/hooks/<name>`, it walks *parent directories* looking for `.git-hop/hooks/<name>` — which is deliberate and useful (a hub-level `.git-hop/hooks/` covers all its worktrees), but the walk **climbs all the way to the filesystem root**. It does not stop at the hub, at a repository boundary, or at `$HOME`.

That is tolerable when the anchor is a real worktree deep in a known tree. For `pre-clone` it would be actively dangerous: the anchor sits in the directory the clone runs from, so a stray `.git-hop/hooks/pre-clone` in *any* ancestor of wherever the user happened to be standing would execute on every clone they run from that subtree. That is why `pre-clone` has no repo tier at all rather than a walk from a carefully chosen anchor.

`pre-clone` still receives the **intended project root** — the directory the clone is about to create, which does not exist yet — as `GIT_HOP_WORKTREE_PATH`. `GIT_HOP_BRANCH` is empty: resolving the default branch requires talking to the remote, which has not happened yet at that point in the sequence.

## Init hooks

`git hop init` fires `post-worktree-add` for the conversion's initial worktree, through the same dispatch as clone (`BuildHookDispatch`), so the hook sees the same variables: `GIT_HOP_WORKTREE_PATH` is the initial worktree, `GIT_HOP_BRANCH` its branch, `GIT_HOP_REPO_ID` `<host>/<org>/<repo>` (see [Repository identifier](#repository-identifier)).

| Conversion | Initial worktree (`GIT_HOP_WORKTREE_PATH`) |
|---|---|
| bare (default) | the new `<repo>/hops/<branch>` worktree |
| regular (`--regular`) | the repo root, which stays the current branch's working tree |

```
(conversion; hop.json; state; current symlink when bare)
  ↓
committed-hook mirror
  ↓  (environment set-up: ports, volumes, .env, compose override, shared deps)
post-worktree-add
```

The initial worktree is set up before `post-worktree-add` through the same function as `git hop add` and `git hop clone` (`services.SetUpWorktree`), so the hook finds its `.env`, compose override and linked shared deps (see [what exists when each hook runs](#add-hooks)). A set-up failure is reported and the conversion stands; the hook still fires. The set-up publishes `git.runtime.deps.installed` when it linked deps. The [behaviour change](#add-hooks) noted for add applies here too.

The dispatch follows the mirror for the reason given in [Why mirror-then-fire](#why-mirror-then-fire): a `post-worktree-add` committed to the repo being converted applies to the worktree that carries it. A failing hook warns; the conversion stands.

- Both conversions set up and fire it. Register-as-is and a re-run on an already-initialized repo convert nothing, set up nothing and fire nothing.
- Like clone, init fires no `pre-worktree-add`. It fires no `pre-clone` / `post-clone` either: nothing is cloned.
- `git hop init --dry-run` lists the set-up (the plan's last step) and the `post-worktree-add` hook it would run (when one resolves), and runs neither.
- `git hop init --no-hooks` fires nothing: no hooks directory, no committed-hook mirror (unless `--hooks` names a mode), and no `post-worktree-add`. The set-up is not a hook and still runs: the worktree is left as ready as one `git hop add` creates. The `--dry-run` preview agrees: it lists the set-up and no hook.

## Repair hooks

`pre-repair` and `post-repair` are dispatched from `runRepair` in `cmd/repair.go`, through the shared `Runner` like every other hook (`runRepairHook`). What is specific to them:

| | `pre-repair` / `post-repair` |
|---|---|
| Discovery | `Runner.FindHookFile` — repo → hopspace → global |
| Repo-level anchor | the **hub**: `<hub>/.git-hop/hooks/<name>`, then the parent walk above it. A hook inside a worktree's `.git-hop/hooks/` is not consulted — including the `hops/<branch>/.git-hop/hooks/` a bare `git hop init` creates, which is why init's hint sends repair hooks to the hub. |
| `GIT_HOP_WORKTREE_PATH` | the hub path |
| `GIT_HOP_BRANCH` | set and **empty** — a repair spans every branch |
| `GIT_HOP_REPO_ID` | `<host>/<org>/<repo>` from the hub's `hop.json` (see [Repository identifier](#repository-identifier)), read at dispatch time; empty when it cannot be read |
| Working directory | inherited from git-hop's cwd, as for every hook; `cd "$GIT_HOP_WORKTREE_PATH"` to work in the hub |
| Executable-bit check, name validation, output | same as every hook |

Consequences worth internalising:

- `pre-repair` is a real veto: a non-zero exit aborts the repair before any backup or mutation. It only runs when the plan actually has mutations and `--dry-run` was not passed.
- `pre-repair` reads `hop.json` *before* the fix, when it may be as broken as the reason repair was invoked. An empty `GIT_HOP_REPO_ID` there is expected, and a hopspace-level `pre-repair` is then skipped (repo and global still resolve). `post-repair` reads the repaired config, so its repo ID resolves.
- `post-repair` is advisory in the strongest sense — its exit status is discarded (`_ = firePostRepairHook(fs, hubPath)`), including a non-executable hook file.

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
# Create hooks directory (Linux default; on macOS it is
# ~/Library/Application Support/git-hop/hooks unless XDG_CONFIG_HOME is set)
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
| `GIT_HOP_REPO_ID` | Repository identifier, `<host>/<org>/<repo>` with the origin's host ([Repository identifier](#repository-identifier)) | `gitlab.example.com/org/repo` |
| `GIT_HOP_BRANCH` | Branch name | `feature-x` |

Two exceptions:

- **`pre-clone`** receives an empty `GIT_HOP_BRANCH` (the default branch is not known until the remote is queried, which happens later) and a `GIT_HOP_WORKTREE_PATH` naming the project root that is about to be created — a directory that does not exist yet.
- **`pre-repair` / `post-repair`** receive the hub as `GIT_HOP_WORKTREE_PATH` and an empty `GIT_HOP_BRANCH`; `GIT_HOP_REPO_ID` may be empty for `pre-repair`. See [Repair hooks](#repair-hooks).

### Move Variables

`pre-worktree-move` and `post-worktree-move` additionally receive:

| Variable | Description |
|----------|-------------|
| `GIT_HOP_OLD_BRANCH` | Branch name before the move |
| `GIT_HOP_NEW_BRANCH` | Branch name after the move |
| `GIT_HOP_OLD_PATH` | Worktree path before the move |
| `GIT_HOP_NEW_PATH` | Worktree path after the move |

`GIT_HOP_WORKTREE_PATH` and `GIT_HOP_BRANCH` track the *current* state: the old worktree for `pre-worktree-move`, the new one for `post-worktree-move`.

### Add Variables

`pre-worktree-add` and `post-worktree-add` additionally receive
`GIT_HOP_TASK` when the worktree is added with `--task <id>`: the task id,
as recorded in `hop.json` under `branches.<branch>.task`. It is **absent**
(not empty) without `--task`.

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

A missing hook is not a failure: `FindHookFile` returns empty and the run silently succeeds. A hook file that exists but is not executable **is** a failure on non-Windows (checked before execution); for `post-repair` that failure is discarded like any other.

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

# Append: git-hop has already written .env with the allocated ports.
if [ "$GIT_HOP_BRANCH" = "main" ]; then
    cat .env.production >> .env
elif [ "$GIT_HOP_BRANCH" = "staging" ]; then
    cat .env.staging >> .env
else
    cat .env.development >> .env
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

### 5. Per-Worktree Tooling

git-hop installs and links the shared dependencies of every package manager it detects (npm, pnpm, yarn, pip, ...) before `post-worktree-add` fires, so the hook does not install them again (see [Add hooks](#add-hooks)). Use it for the per-worktree setup git-hop does not manage:

```bash
#!/bin/bash
# post-worktree-add

cd "$GIT_HOP_WORKTREE_PATH"

# Dependencies are already linked; tools that need them can run.
if [ -f .pre-commit-config.yaml ]; then
    pre-commit install
fi

if [ -f .envrc ]; then
    direnv allow
fi
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

git-hop has **built-in integration** with [git-flow-next](https://github.com/gittower/git-flow-next). It always **detects** branch types from your git-flow configuration; it **runs git-flow commands** only when you opt in.

#### Built-in Detection (always on)

In a repository where `gitflow.initialized` is `true`, `git hop add`, `remove`, `move` and switching read the `gitflow.branch.<type>.*` config to work out the branch type, and hand it to hooks as the `GIT_HOP_BRANCH_*` variables below. Detection only reads git config; it never changes the repository.

This works with **any branch types configured in git-flow-next**, including custom types:

```bash
# Configure a custom branch type in git-flow
git config gitflow.branch.bugfix.type topic
git config gitflow.branch.bugfix.parent develop
git config gitflow.branch.bugfix.prefix bugfix/

# git-hop detects it: hooks see GIT_HOP_BRANCH_TYPE=bugfix
git hop add bugfix/fix-login
```

#### Running git-flow commands (opt-in: `hop.gitflow.enabled`)

By default git-hop runs **no** `git flow` command. `git flow <type> finish` merges the branch into its parent (for example `develop`), which is not something removing a worktree should do unasked. To have git-hop drive git-flow, turn it on per repository (or with `--global` for all of them):

```bash
git config hop.gitflow.enabled true
```

With it on, for a branch whose prefix matches a git-flow type:

1. `git hop add feature/my-feature`, for a new branch, runs the `pre-worktree-add` hook, creates the worktree on a detached HEAD, then runs `git flow feature start my-feature --no-worktree` in it. git-flow creates the branch (from the type's start point, or from `--from` given as its `[base]`; finish still merges into the type's parent), records its base and checks it out there; git-hop never creates the branch itself. A branch that already exists, locally or on origin, is checked out as usual and not started. If the start fails, the worktree is removed again. Earlier releases ran `git flow start` before the `pre-worktree-add` hook; it now runs after it, so the hook can veto a branch before git-flow creates it, and sees a branch that does not exist yet.
2. `git hop remove feature/my-feature` runs `git flow feature finish my-feature` in the branch's own worktree before the `pre-worktree-remove` hook, then removes the worktree. git-flow merges into the type's parent in the worktree that has it checked out. When no worktree has it, git-hop first detaches the branch's worktree, so git-flow checks the target out there, in the worktree about to be removed, and no other worktree changes branch.

   Because finish merges the branch into its parent, remove's not-merged-into-default check does not apply to it, and it needs no `--force`. Unpushed commits need no `--no-verify` either: the merge keeps them on the parent, which stays local, as after any `git flow finish`. The worktree must be clean: finish runs in it, and the removal would then discard uncommitted or untracked files, so a dirty worktree is refused before finish runs, even with `--no-verify`. To throw those files away and remove the branch without finishing it, run `git -c hop.gitflow.enabled=false hop remove <branch> --force --no-verify`. If finish fails, nothing is removed: the worktree, the branch and the hub's record of it stay.

3. `git hop move feature/a feature/b` runs no `git flow` command, but moves git-flow's per-branch config with the branch: every `gitflow.branch.feature/a.*` key (such as the `base` start records) becomes `gitflow.branch.feature/b.*`, as `git branch -m` does for `branch.feature/a.*`. If `feature/b` already has such keys, move leaves both sets alone and warns.

git-flow-next needs a work tree, and the hub git-hop clones or converts a repository into is a bare repository, so git-hop never runs git-flow in the hub.

A failing git-flow command aborts the add or remove. `--dry-run` shows the `Would run 'git flow ...'` step only when a real run would run it. With the setting off, `--verbose` prints one `hint:` line per command when a git-flow action was skipped.

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

| Command | git-flow action (`hop.gitflow.enabled=true`) | Default (unset / `false`) | Git-Hop Action |
|---------|-----------------|---------|----------------|
| `git hop add feature/my-feature` | `git flow feature start my-feature` | None | Creates worktree |
| `git hop remove feature/my-feature` | `git flow feature finish my-feature` | None | Removes worktree |
| `git hop add release/v1.0.0` | `git flow release start v1.0.0` | None | Creates worktree |
| `git hop remove release/v1.0.0` | `git flow release finish v1.0.0` | None | Removes worktree |
| `git hop move feature/a feature/b` | Moves `gitflow.branch.feature/a.*` to `gitflow.branch.feature/b.*` | None | Renames branch and worktree |

In every row the branch type is detected and passed to hooks as `GIT_HOP_BRANCH_*`.

#### Manual Hook Integration (Optional)

If you want git-flow behavior the built-in actions do not cover, leave `hop.gitflow.enabled` off and drive git-flow from a hook, using the detected type:

```bash
#!/bin/bash
# pre-worktree-add - Custom git-flow logic

# Leave it to git-hop when the built-in actions are on
if [ "$(git config --type=bool hop.gitflow.enabled)" = "true" ]; then
    exit 0
fi

if [ "$GIT_HOP_DETECTOR_SOURCE" = "gitflow-next" ]; then
    : # Your custom logic here, e.g. based on $GIT_HOP_BRANCH_TYPE
fi
```

## Installing Hook Directories

The `.git-hop/hooks` directory is created automatically by `git hop init`.
To skip this, use `--no-hooks`, which also stops init from dispatching
lifecycle hooks (see [Init hooks](#init-hooks)):

```bash
git hop init           # creates .git-hop/hooks/ automatically
git hop init --no-hooks  # no hook directory, no hook runs
```

Re-running `git hop init` on an already-initialized repo also ensures the
hooks directory exists (unless `--no-hooks` is passed).

After creating the directory, `init` prints a `hint:` listing the hooks a
script there can implement. It names only hooks that are dispatched and
resolve at repo level (no `env-*` names, no `pre-clone`), and only those
actually looked up in *that* directory:

- **Hub root** (regular conversion, register-as-is): every repo-level hook
  (`RepoLevelHookNames()`). Repair looks there directly; every other hook
  reaches it through the parent walk from a worktree under the hub.
- **Worktree** (bare conversion: `hops/<branch>/.git-hop/hooks/`): only
  `WorktreeLevelHookNames()`. Four hooks never start their lookup at an
  existing worktree, so a script for them in a worktree's own directory
  never runs: `pre-worktree-add` (the worktree does not exist yet),
  `post-worktree-remove` (it is gone), and `pre-repair` / `post-repair`
  (anchored on the hub). The hint sends these (`HubOnlyHookNames()`) to
  `<hub>/.git-hop/hooks/` instead. Committed and mirrored with `--hooks`,
  they also work from the hopspace.

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

**`pre-repair` / `post-repair` in a worktree's `.git-hop/hooks/` never runs:**
Expected. Repair anchors its repo-level lookup on the hub, not on a worktree. Put the hook in `<hub>/.git-hop/hooks/`, or at hopspace or global level — see [Repair hooks](#repair-hooks).

**`pre-clone` in the current directory never runs:**
Expected. `pre-clone` resolves at hopspace and global level only — see [`pre-clone` has no repo level](#pre-clone-has-no-repo-level).

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

## Lifecycle events

File hooks run a script *around* an operation and can abort it. Alongside
them, git-hop publishes a lifecycle **event** after each successful
mutation. Events cannot veto anything; they exist so other tools can
observe worktree lifecycle without installing hook scripts. Set
[`hop.events.sink`](configuration.md#hopeventssink) to have them appended
to a JSONL file.

| Topic | Published by | Payload keys |
|---|---|---|
| `git.runtime.worktree.created` | `git hop add`; `git hop clone` and `git hop init` (bare or regular conversion, not register-as-is) for the initial worktree, after `hopspace.initialized` | `path`, `branch`, `hopspace_path`, `repo_path` |
| `git.runtime.worktree.removed` | `git hop remove` | `path`, `branch`, `hopspace_path`, `repo_path` |
| `git.runtime.worktree.merged` | `git hop merge` (the source worktree, which merge also removes) | `path`, `branch`, `hopspace_path`, `repo_path` |
| `git.runtime.worktree.moved` | `git hop move` (new path and branch) | `path`, `branch`, `hopspace_path`, `repo_path` |
| `git.runtime.worktree.switched` | `git hop <branch>` | `path`, `branch`, `hopspace_path`, `repo_path` |
| `git.runtime.env.started` / `git.runtime.env.stopped` | `git hop env start` / `stop` | `action`, `root`, `branch` |
| `git.runtime.hopspace.initialized` | `git hop init` and `git hop clone`, for the hub they register (`path` is the hub) | `path`, `org`, `repo` |
| `git.runtime.deps.installed` | `git hop add`, `git hop clone` and `git hop init` (conversions), after dependency install and after `worktree.created` | `worktree_path`, `branch` |

`hopspace_path` is the same for every worktree event of a hub: the hub
itself for a default clone (so it equals `repo_path`), or
`$GIT_HOP_DATA_HOME/<org>/<repo>` for a hub cloned with `--global`
(marked `repo.mode: "global"` in its `hop.json`).

One line per event:

```json
{"topic":"git.runtime.worktree.created","source":"git-hop","timestamp":"2026-09-24T00:05:23.412862-04:00","payload":{"path":"/src/widgets/hops/feat/login","branch":"feat/login","hopspace_path":"/src/widgets","repo_path":"/src/widgets"}}
```

Topics follow the `[source].[category].[object].[action]` grammar of the
hop.top kit event bus. A consumer that maps them onto its own vocabulary
(for example a hook dispatcher's `WorktreeCreate` / `WorktreeRemove`)
should treat `merged` as a removal too, since `git hop merge` deletes the
source worktree without publishing a separate `removed`. Nothing is
published under `--dry-run`, and a failing sink never fails the command.

## Known limitations

- **`pre-env-start` / `post-env-start` / `pre-env-stop` / `post-env-stop` are accepted but never dispatched.** They validate, install, and mirror; nothing fires them. See [Two different hook systems](#two-different-hook-systems).
- **`FindHookFile`'s parent walk climbs to the filesystem root** for every hook except `pre-clone` — it does not stop at the hub or at `$HOME`. See [The parent-walk hazard](#the-parent-walk-hazard).
- **Repo-level `post-worktree-remove` cannot fire from the removed worktree**, because the file lived inside the worktree that was just deleted. A hub-level `.git-hop/hooks/post-worktree-remove` still fires through the parent walk; otherwise install it at hopspace or global level.

Resolved, previously listed here:

- ~~`git hop init` dispatches no lifecycle hooks.~~ A conversion now fires `post-worktree-add` for its initial worktree (`hops/<branch>`, or the repo root with `--regular`), after the committed-hook mirror, as clone does. See [Init hooks](#init-hooks).
- ~~A `.git-hop/hooks/pre-clone` in the directory a clone runs from, or any ancestor of it, fires.~~ `pre-clone` now has no repo level: hopspace and global only.

- ~~`git hop add --dry-run` still creates the worktree.~~ `add --dry-run` now previews the branch, worktree path, hooks it would run, and `hop.json` registration, then exits without writing anything: no worktree, branch, port allocation, or hook run.
- ~~`pre-repair` / `post-repair` resolve at the global level only and receive no `GIT_HOP_*` variables.~~ Repair hooks now go through the shared runner: repo level (anchored on the hub), hopspace, then global, with the standard `GIT_HOP_*` variables. `GIT_HOP_WORKTREE_PATH` is the hub and `GIT_HOP_BRANCH` is empty.
- ~~Repo-level `post-worktree-add` does not fire on the bootstrap worktree.~~ `git hop clone` now mirrors committed `.git-hop/hooks/` into the hopspace *before* dispatching `post-worktree-add`, so a hook carried by the clone applies to the worktree that carried it. The manual symlink is now `--hooks=symlink`. See [Committed-hook mirroring](#committed-hook-mirroring). The chicken-and-egg trap still applies to `git hop add <old-branch>` if you never mirrored — [Choosing a hook level](#choosing-a-hook-level) still stands as guidance.

## Implementation Details

For developers interested in the implementation:

| Concern | Where |
|---|---|
| Hook name list (the authority) | `ValidHookNames`, `internal/hooks/runner.go` |
| Name validation | `ValidateHookName()`, same file |
| Discovery / priority | `FindHookFile()`, plus `findHookInParentDirs()` for the parent walk; `hasRepoLevel()` exempts `pre-clone` from both |
| Dispatched vs reserved names | `IsDispatched()`, `RepoLevelHookNames()`, same file |
| Hooks a worktree's own dir serves | `WorktreeLevelHookNames()` / `HubOnlyHookNames()`, same file; `initHooksHint()` in `cmd/init_dispatch.go` picks by where init created the dir |
| Execution, env, exit-code handling | `Runner.run()`, behind `ExecuteHook` / `ExecuteHookWithDetector` |
| Navigation directive | `ExitNavigationHandled`, `RunResult`, `navigationHandledFor()` |
| Switch env vars | `SwitchEnvVars()` — omits empty fields rather than exporting them empty |
| Hooks-dir creation | `InstallHooks()`; `git hop init` calls it unless `--no-hooks` |
| Committed-hook mirror | `MirrorCommittedHooks()`, `internal/hooks/install.go` |
| Clone dispatch (callback injection) | `HookDispatchOptions`, `internal/hop/clone_worktree.go`; wired by `BuildHookDispatch()` in `internal/cli/root.go` |
| Init dispatch | `dispatchInitWorktreeAdd()` / `previewInitWorktreeAdd()`, `cmd/init_dispatch.go`, via `BuildHookDispatch()` |
| Switch dispatch (`git hop <branch>`) | `internal/cli/root.go` |
| Switch dispatch (plain `cd`) | `cmd/notify_chdir.go` |
| Repair dispatch | `runRepairHook()`, `cmd/repair.go`, via the shared runner |
| Shell wrapper + chdir handler | `internal/shell/wrapper.go`, `internal/shell/chpwd.go` |
| Worktree-roots cache | `internal/shell/roots.go` |
| Config-declared env hooks (the *other* system) | `internal/services/env_hooks.go`, `internal/services/env_managers.go` |

`internal/hop` cannot import `internal/hooks` — `internal/hooks` already imports `internal/hop` for `LooksLikeGitCheckout`, so the reverse edge would be a cycle. That is why clone-time dispatch is injected as callbacks from `internal/cli` rather than called directly.

The hooks system:
- Uses the standard Unix executable model
- Provides environment variables for context
- Follows XDG Base Directory specification
- Supports all scripting languages (bash, python, node, etc.)
