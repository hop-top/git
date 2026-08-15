# tmux integration

A worked example of driving a window manager entirely from git-hop's
lifecycle hooks. No git-hop code is involved — these are five shell
scripts and a sourced library.

The model:

```
repository        -> tmux session
  main worktree   -> main-<hash> window
  feature/a       -> feature+a-<hash> window
  fix/b           -> fix+b-<hash> window
```

One session per repository, one window per worktree. Creating a worktree
adds its window; removing it kills that window; renaming a branch renames
the window in place; hopping to a branch selects its window.

## Install

Copy the `hooks/` contents into a hook directory. Everything in `hooks/`
must go together — the scripts source `_hop-tmux-lib.sh` from their own
directory.

**Install at the hopspace level.** For `github.com/acme/widgets`:

```bash
dest=~/.local/share/git-hop/github.com/acme/widgets/hooks
mkdir -p "$dest"
cp hooks/* "$dest"/
chmod +x "$dest"/post-*
```

For every repo on the machine, use the global level instead
(`~/.config/git-hop/hooks/`). The scripts read the repo ID from the
environment, so the same copy serves any number of repositories.

### Why not repo level

Repo-level hooks live at `<worktree>/.git-hop/hooks/`, inside the
worktree — which creates a chicken-and-egg problem for
`post-worktree-add` specifically. `git hop add` looks the hook up in the
worktree it just created, so when you add a branch that predates the
commit introducing the hook, the file is not in that checkout and the
hook silently does not fire. You get a worktree with no window, and no
error explaining why.

The hopspace path is resolved from the repo ID before the worktree
exists, so it fires on the very first add and on every add after,
whatever branch you start from. See "Choosing a hook level" in
`docs/hooks.md`.

If you want the scripts committed and shared with a team, keep the
canonical copy in the repo and symlink the hopspace path at it, so the
committed file stays the single source of truth without the lookup gap:

```bash
ln -s "$(pwd)/examples/tmux/hooks/post-worktree-add" \
  ~/.local/share/git-hop/github.com/acme/widgets/hooks/post-worktree-add
```

## Naming scheme

Names are computed from `GIT_HOP_REPO_ID` and `GIT_HOP_BRANCH` alone, by
a pure function in `_hop-tmux-lib.sh`. No shared state on disk: each hook
runs in its own process and derives the same target independently.

A name is a readable **stem** plus a short **hash** of the original,
unsanitized string:

| Source | Becomes |
|---|---|
| `github.com/acme/widgets` | session `hop+github_com+acme+widgets-445fca` |
| `main` | window `main-8bfbc8` |
| `feature/a` | window `feature+a-9805bc` |
| `fix/b` | window `fix+b-...` |

The stem transform is `/` and `:` → `+`, and `.` → `_`. The suffix is six
hex digits from `cksum` (POSIX CRC-32) over the input before any folding.

### Why those characters

tmux target syntax gives three characters structural meaning: `:` splits
session from window, `.` splits window from pane, and `$@%` are id
sigils. Branch names routinely contain `/`, and repo IDs always contain
`.` — so both need handling.

Prefixing a target with `=` forces exact-name matching instead of the
default prefix search. That is enough to make `:` safe: a window named
`a:b` is reachable as `=sess:=a:b`. It is **not** enough for `.` — tmux
splits on `.` before any name matching happens, so `=sess:=a.b` fails
with `can't find window: a`, and a *session* containing a dot cannot be
addressed at all. That is why `github.com/...` cannot be used as a
session name directly, and why `.` is the one character that genuinely
must be replaced.

`+` and `_` are legal in tmux names, legal inside `=` targets, and rare
in branch names — so the result stays readable. `feature+a` is
recognisably `feature/a` when you are scanning a status bar for the right
window, which is the whole point of naming windows after branches.

### Why the hash suffix

The stem alone is **not injective**, and that is a correctness bug rather
than a cosmetic one. `/` and a literal `+` both fold to `+`, so
`feature/a` and `feature+a` produce an identical stem.

`post-worktree-remove` kills a window **by name**. Under a colliding
stem, `git hop remove feature/a` kills `feature+a`'s window and every
process running in it — the dev server, the REPL, the editor with unsaved
buffers — silently, with no error, because from tmux's side the kill did
exactly what it was told. "Branch pairs like that are rare" is no defence
when the failure destroys work the user never asked to close.

Hashing the original string before folding makes distinct inputs produce
distinct names, so the collision is unreachable. Six characters buys that,
and the stem keeps the status bar scannable.

`cksum` is the hash because POSIX specifies it and it is present
everywhere. `shasum` / `sha1sum` / `md5` / `md5sum` are none of them
universally available — the names differ between macOS and Linux, and any
of them can be missing from a stripped `PATH`. A naming function that
silently changed its answer depending on which tool it found would break
the one invariant the library exists to hold: every hook, in its own
process, deriving the identical target from the identical input with no
shared state on disk. The arithmetic and hex formatting are shell
builtins, so `cksum` is the only external command involved. If it is
missing, the bare stem is emitted — degrading to the old behaviour rather
than to names two hooks disagree about.

### Worktree identity (`@hop-worktree`)

The window name is a *derived* string; the worktree path is the fact.
Even with a collision-free transform a name can end up on the wrong
window — renamed by hand, created by another tool, left over from a
worktree deleted outside git-hop. The operation that pays for that is
`kill-window`.

So every window these hooks create records its worktree in a tmux user
option:

```bash
tmux set-option -w -t '=sess:=win' @hop-worktree /path/to/worktree
```

`post-worktree-remove` reads it back and refuses to kill a window whose
recorded worktree disagrees with the one being removed. That turns "kill
whatever answers to this name" into "kill the window for the worktree
actually being removed".

A window carrying **no** record is still killable: windows predating the
option, and hand-made windows that happen to match, would otherwise become
permanently unremovable, and a hook that refuses to clean up is its own
bug. Absent means unknown and falls back to the name match. Only a
recorded identity that actively disagrees blocks the kill.

`post-worktree-move` re-stamps the option at the new name, so the safety
check does not turn into a leak after a rename.

### The `=` prefix is load-bearing

All targets are built by `hop_tmux_window_target` / `hop_tmux_session_target`,
never by hand. Without the `=`, tmux falls back to a prefix match, and a
prefix match silently resolves to the **wrong** window whenever one branch
name is a prefix of another (`feature/a` vs `feature/ab`). In
`post-worktree-remove` that means killing a window the user never asked to
close, along with whatever was running in it.

## Behaviour without tmux

Every script is a silent no-op — exit 0, nothing on stderr — when tmux is
not installed. Installing these hooks on a machine without tmux changes
nothing observable.

Note what is *not* required: `$TMUX` being set. `$TMUX` is only set for
processes running inside a tmux pane, and a user with a session attached
in another terminal still wants `git hop add` typed at a plain shell to
create the window. "Am I inside tmux" is the wrong question.

### Cold start

Also not required: a server already running. `tmux new-session` starts
one, and here that is correct rather than a surprise.

Copying these scripts into a hook directory is a deliberate act meaning
"manage my worktrees as tmux windows" — there is no way to install them by
accident, so the consent is explicit. Gating on a live server would make
the *create* half of `hop_tmux_ensure_session` unreachable from cold: the
first `git hop add` after a reboot would find no server, decline to start
one, and silently produce no window, while the second — run after the user
happened to open tmux by hand — would work. A hook whose effect depends on
whether some unrelated terminal is open is worse than either consistent
behaviour.

The read-only hooks stay cheap regardless. `post-worktree-remove` and the
rename path of `post-worktree-move` do their existence checks first, which
fail against a dead server, so they exit without creating anything — a
`git hop remove` never spawns a server just to discover there is nothing
to kill.

`post-worktree-switch` still exits **0, not 93**, whenever it did not
actually navigate (see below).

## The navigation directive (exit 93)

`post-worktree-switch` exits **93** after it selects the target window.
Every other hook exits 0.

git-hop's shell wrapper cannot read a hook's output; its only channel back
is the exit status. On a normal exit 0 the wrapper resolves the hub's
`current` symlink and `cd`s the calling shell into the target worktree.
That is right with no window manager in play, and wrong here: tmux has
already put the user in the target worktree's own window, so the wrapper's
`cd` would *also* drag the originating window's shell there. Two windows
in one worktree, and the window you hopped away from is silently pointed
somewhere it never agreed to go.

Exit 93 (`hooks.ExitNavigationHandled`) is how a hook says "navigation is
done, stand down". The wrapper reports 0 to the user's shell and skips the
`cd`. It is honoured for `post-worktree-switch` only — elsewhere a 93
would just turn a genuine failure into a silent success.

Note the deliberate asymmetry: when tmux is unavailable, the switch hook
exits **0, not 93**. It did not navigate anything, so the wrapper's `cd`
is the correct fallback; claiming 93 there would strand the user in their
original directory.

## Rename, never recreate

`post-worktree-move` uses `rename-window`. That window very likely holds a
dev server, a REPL, a long test run, an editor with unsaved buffers —
renaming a branch must not cost the user any of it.

Kill-and-recreate would produce a window with the right name and none of
the work in it: identical in a `list-windows` listing, total loss in
practice. This is why `test.sh` asserts on `pane_pid` and `window_id`
rather than on the window name — the name-based assertions pass under both
implementations and prove nothing.

## Tests

Two suites, both driving a real tmux server on a private socket
(`tmux -L`) so neither can disturb your own sessions; both kill their
server on exit, including on failure.

### `./test.sh` — the hooks in isolation

```bash
./test.sh
```

Invokes the hook scripts directly with a hand-built `GIT_HOP_*`
environment. Needs only bash, the hooks, and tmux, so it runs in a couple
of seconds — the fast feedback loop while editing a hook.

It covers session/window creation, sibling windows surviving, the exit-93
contract, selective removal, in-place rename with pane-PID survival,
slash-bearing branch names, name distinctness and stability,
identity-checked removal, cold start against a dead server, and silent
no-op with tmux absent.

Expected names are derived through the library rather than hardcoded, so
the suite pins the required *behaviour* — the same input reaching the same
window — instead of today's output format. The properties of the names
themselves are asserted directly.

### `./test-binary.sh` — the hooks through the real binary

```bash
./test-binary.sh
```

Composes the layers `test.sh` stubs: a real `git hop` binary, real hook
dispatch, these hooks installed at the hopspace level exactly as the
install instructions above describe, and a real tmux server.

`test.sh` answers "given this environment, does the hook do the right
thing to tmux". It cannot answer whether git-hop actually *hands* the
hooks that environment, whether hook resolution finds the files where this
README says to put them, or whether the switch hook's exit code survives
the trip back out through the binary's own process status. Those are the
questions this suite exists for, and every branch name, worktree path and
repo ID in it is whatever git-hop computes rather than something the test
made up.

It additionally needs a Go toolchain and git: it builds the binary from
the project root, stamped with a version unique to that run, and asserts
`git hop --version` reports the stamp — so a system-installed `git-hop`
answering instead would fail the suite rather than silently make it
vacuous. The build lands outside the worktree.

Covered: `git hop add` creating the window in the right worktree,
`git hop <branch>` both selecting the window and exiting 93 (and exiting 0
when tmux is unavailable), `git hop remove` killing only its own window
with a colliding sibling's process left running, `git hop move` preserving
pane PID and window ID while `@hop-worktree` follows the rename, and cold
start against a socket that has never had a server.

### Mutation testing

Both suites are mutation-tested. Each of these turns `test.sh` red:

- making the switch hook exit 0 instead of 93
- making the move hook kill-and-recreate instead of rename
- restoring the colliding sanitize (drops the hash suffix) — fails the
  distinctness assertions *and* the one proving a sibling's running
  process survives a removal
- restoring the `list-sessions` gate in `hop_tmux_available` — fails cold
  start
- dropping the `@hop-worktree` check from `post-worktree-remove` — fails
  the mismatched-identity assertion

And each of these turns `test-binary.sh` red:

- restoring the colliding sanitize — the two branches collapse onto one
  window, failing the distinctness guards *and* the assertion that the
  removed branch's window and process are actually gone
- restoring the `list-sessions` gate — cold start creates no server and no
  window

## Known gaps

**A worktree move does not update the pane's working directory.** tmux
has no command to change an existing pane's cwd — `rename-window` moves
the name, and the shell already running in the pane keeps its old `$PWD`.
When `git hop move` relocates a worktree on disk, the pane is left in the
old path. Fixing it means either interrupting whatever is running in the
pane (`send-keys cd`, which would corrupt any foreground process) or
recreating the window (which is the thing this hook exists to avoid). The
name is corrected; the shell's cwd is the user's to fix. Only affects
moves that change the path, not plain branch renames.
