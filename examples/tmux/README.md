# tmux integration

A worked example of driving a window manager entirely from git-hop's
lifecycle hooks. No git-hop code is involved — these are five shell
scripts and a sourced library.

The model:

```
repository        -> tmux session
  main worktree   -> main window
  feature/a       -> feature+a window
  fix/b           -> fix+b window
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

| Source | Becomes |
|---|---|
| `github.com/acme/widgets` | session `hop+github_com+acme+widgets` |
| `main` | window `main` |
| `feature/a` | window `feature+a` |
| `fix/b` | window `fix+b` |

The transform is `/` and `:` → `+`, and `.` → `_`.

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

The mapping is not strictly injective: a branch containing a literal `+`
collides with one containing `/`. That is a deliberate trade — legibility
matters more than bijectivity for a string a human reads, and branches
differing only that way do not occur in practice.

### The `=` prefix is load-bearing

All targets are built by `hop_tmux_window_target` / `hop_tmux_session_target`,
never by hand. Without the `=`, tmux falls back to a prefix match, and a
prefix match silently resolves to the **wrong** window whenever one branch
name is a prefix of another (`feature/a` vs `feature/ab`). In
`post-worktree-remove` that means killing a window the user never asked to
close, along with whatever was running in it.

## Behaviour without tmux

Every script is a silent no-op — exit 0, nothing on stderr — when either:

- tmux is not installed, or
- no tmux server is running.

The second condition matters as much as the first. `tmux new-session`
*starts* a server if none is running, so a hook that skipped the check
would spawn a background tmux for a user who has never opened one. The
guard is "is there a server to talk to", asked by querying it, not "am I
inside tmux" — a user with tmux running in another terminal still wants
`git hop add` typed at a plain shell to create the window.

Installing these hooks on a machine without tmux changes nothing
observable.

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

```bash
./test.sh
```

Drives the real hooks against a real tmux server on a private socket
(`tmux -L`), so it cannot disturb your own sessions; the server is killed
on exit. It covers session/window creation, sibling windows surviving,
the exit-93 contract, selective removal, in-place rename with pane-PID
survival, slash-bearing branch names, and silent no-op with tmux absent
and with no server running.

The suite is mutation-tested: making the switch hook exit 0, or making the
move hook kill-and-recreate, both turn it red.

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
