#!/usr/bin/env bash
#
# Drives the tmux hooks through the REAL git-hop binary against a REAL
# tmux server, with the hooks installed as REAL hooks.
#
# test.sh covers the hooks in isolation: it invokes the scripts directly
# with a hand-built GIT_HOP_* environment. That answers "given this
# environment, does the hook do the right thing to tmux" and nothing more.
# It cannot answer whether git-hop actually hands the hooks that
# environment, whether hook resolution finds the files where the README
# says to install them, or whether the exit code the switch hook returns
# survives the trip back out through the binary's own process status.
#
# So this suite composes the layers instead of stubbing them:
#
#   real `git hop` binary  ->  real hook dispatch  ->  real hook scripts
#                          ->  real tmux server
#
# Nothing about the branch names, worktree paths, or repo ID is synthetic
# here -- they are whatever git-hop computes, which is the point. A test
# that hardcodes the environment cannot catch git-hop changing it.
#
# Kept separate from test.sh rather than folded in as more sections,
# because the two have different costs and different prerequisites.
# test.sh needs bash, git-hop's hook scripts, and tmux; it runs in a couple
# of seconds and is the fast feedback loop while editing a hook. This one
# additionally needs a Go toolchain, links a ~25MB binary, and does a full
# clone-plus-hub setup per case. Making the quick suite pay that cost every
# time would discourage running it, and a suite people skip protects
# nothing.
#
# Usage: ./test-binary.sh
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
HOOKS_SRC="$HERE/hooks"
# examples/tmux -> examples -> project root
PROJECT_ROOT="$(cd "$HERE/../.." && pwd)"

# Sockets carry the PID so concurrent runs cannot collide, and are named so
# they could never be the user's own server. Every tmux invocation in this
# file goes through -L; a bare `tmux` would touch the default socket and
# the user's live sessions.
SOCKET="hop-binary-test-$$"
COLD="hop-binary-cold-$$"

# -P through pwd: on macOS mktemp hands back /var/..., a symlink to
# /private/var/..., and both tmux and git-hop report the resolved form.
# Resolving up front keeps the path assertions comparing like with like.
WORKDIR="$(cd "$(mktemp -d)" && pwd -P)"

# The version string this run's binary is stamped with. Nothing else on the
# machine can be reporting it, which is what makes the identity check in
# section 0 conclusive rather than suggestive.
BUILD_STAMP="hop-tmux-e2e-$$-$(date +%s)"

PASS=0
FAIL=0

# Kills every server this suite may have started, including on failure, and
# unlinks the socket files a killed server leaves behind. The cold-start
# section legitimately spawns a server, so letting those accumulate would
# litter /tmp on every run.
cleanup() {
    local s
    for s in "$SOCKET" "$COLD"; do
        tmux -L "$s" kill-server >/dev/null 2>&1
        rm -f "${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)/$s"
    done
    rm -rf "$WORKDIR"
}
trap cleanup EXIT

# --- assertions --------------------------------------------------------

ok() {
    PASS=$((PASS + 1))
    printf '  PASS  %s\n' "$1"
}

bad() {
    FAIL=$((FAIL + 1))
    printf '  FAIL  %s\n' "$1"
    [ $# -gt 1 ] && printf '        expected: %s\n' "$2"
    [ $# -gt 2 ] && printf '        actual:   %s\n' "$3"
    return 0
}

assert_eq() {
    if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "$2" "$3"; fi
}

assert_contains() {
    if printf '%s\n' "$2" | grep -Fxq -- "$3"; then
        ok "$1"
    else
        bad "$1" "list containing '$3'" "$(printf '%s' "$2" | tr '\n' ' ')"
    fi
}

assert_not_contains() {
    if printf '%s\n' "$2" | grep -Fxq -- "$3"; then
        bad "$1" "list WITHOUT '$3'" "$(printf '%s' "$2" | tr '\n' ' ')"
    else
        ok "$1"
    fi
}

section() { printf '\n== %s\n' "$1"; }

fatal() {
    printf '\nFATAL: %s\n' "$1" >&2
    exit 1
}

# --- prerequisites -----------------------------------------------------

command -v tmux >/dev/null 2>&1 || fatal "tmux not installed"
command -v go >/dev/null 2>&1 || fatal "go toolchain not installed (this suite builds the binary)"
command -v git >/dev/null 2>&1 || fatal "git not installed"

# The real tmux, resolved once, before any PATH shimming. Every direct
# query in this file uses it so a shim can never redirect an assertion.
#
# Resolved in a child `sh`, not with `command -v` in this shell: an
# interactive user's rc may define a `tmux` alias or function, which
# `command -v` happily reports in preference to the binary.
TMUX_BIN="$(sh -c 'command -v tmux')"
[ -x "$TMUX_BIN" ] || fatal "could not resolve the tmux binary"

# ======================================================================
section "0. the binary under test is the one we just built"

# A suite that silently exercised a system-installed git-hop would prove
# nothing about this worktree, and would do it while looking green. So the
# binary is stamped with a value unique to this process and the identity is
# asserted through the exact invocation path the rest of the suite uses --
# `git hop`, resolved by git's own subcommand lookup, not by calling the
# file directly.

BINDIR="$WORKDIR/bin"
mkdir -p "$BINDIR"

# Built from PROJECT_ROOT, output OUTSIDE it: the worktree must be left
# byte-identical, and dropping a 25MB artifact in it would show up in
# `git status`.
if ! (cd "$PROJECT_ROOT" && go build -buildvcs=false \
        -ldflags "-X main.version=$BUILD_STAMP" \
        -o "$BINDIR/git-hop" . ) >"$WORKDIR/build.log" 2>&1; then
    printf '%s\n' "$(cat "$WORKDIR/build.log")" >&2
    fatal "go build failed"
fi
ok "built git-hop from $PROJECT_ROOT"

# git finds `git hop` by looking for a `git-hop` executable on PATH, so
# putting the fresh build first is what makes `git hop` mean this binary.
export PATH="$BINDIR:$PATH"

resolved="$(sh -c 'command -v git-hop')"
assert_eq "'git-hop' on PATH resolves to the freshly built binary" \
    "$BINDIR/git-hop" "$resolved"

# THE identity assertion. Goes through `git hop`, the same path every case
# below uses, and reads back a version string that only this run's build
# carries. A system-installed git-hop answering here would report some
# other version and fail.
reported="$(git hop --version 2>&1)"
printf '  (git hop --version -> %s)\n' "$reported"
case "$reported" in
    *"$BUILD_STAMP"*) ok "'git hop' dispatches to THIS build (stamp present in --version)" ;;
    *) bad "'git hop' dispatches to THIS build" "version containing $BUILD_STAMP" "$reported" ;;
esac

printf '  (tmux: %s)\n' "$($TMUX_BIN -V)"
printf '  (socket: %s)\n' "$SOCKET"

# --- harness -----------------------------------------------------------

# The hooks call bare `tmux`. The private socket is injected by putting a
# `tmux` shim first on PATH that appends -L, exactly as test.sh does: the
# hooks are not modified or parameterised for the test, and they run under
# the same code path a user gets.
#
# The shim is placed AFTER the identity check above so nothing about
# resolving git-hop depends on it.
SHIMDIR="$WORKDIR/shim"
mkdir -p "$SHIMDIR"
cat >"$SHIMDIR/tmux" <<EOF
#!/usr/bin/env bash
exec $TMUX_BIN -L "$SOCKET" "\$@"
EOF
chmod +x "$SHIMDIR/tmux"

# Isolated HOME and XDG roots. git-hop reads its global config, global
# hooks, and state from these, so pointing them into WORKDIR keeps the run
# from reading -- or writing -- anything belonging to the user.
export HOME="$WORKDIR/home"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_DATA_HOME="$HOME/.local/share"
export XDG_STATE_HOME="$HOME/.local/state"
export GIT_HOP_DATA_HOME="$WORKDIR/data"
export GIT_CONFIG_GLOBAL="$WORKDIR/gitconfig"
mkdir -p "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME"
printf '[user]\n\tname = Hop Test\n\temail = test@example.com\n[init]\n\tdefaultBranch = main\n' \
    >"$GIT_CONFIG_GLOBAL"

# TMUX is unset by default so a run started from inside the user's own tmux
# does not leak its value into the hooks and change which branch of
# post-worktree-switch executes. Cases that need "inside tmux" set it
# explicitly, per invocation.
unset TMUX

# Runs `git hop ...` with the tmux shim on PATH, from the hub, and returns
# git-hop's exit status. Output is captured, not printed, so a passing run
# stays readable; failing cases print it.
LASTOUT=""
hop() {
    LASTOUT="$(env PATH="$SHIMDIR:$PATH" git hop "$@" 2>&1)"
    return $?
}

# Same, for the cases that must run as if inside a tmux pane. The hook only
# tests $TMUX for emptiness, so a marker string is enough.
hop_inside() {
    LASTOUT="$(env PATH="$SHIMDIR:$PATH" TMUX="/tmp/fake,0,0" git hop "$@" 2>&1)"
    return $?
}

# tmux queries go to the real binary with an explicit -L, never through the
# shim: an assertion that read the server through the same shim the hooks
# use could be fooled by a broken shim into agreeing with itself.
t() { "$TMUX_BIN" -L "$SOCKET" "$@"; }

windows_of() { t list-windows -t "=$1" -F '#{window_name}' 2>/dev/null; }

# Expected names are derived through the library, never hardcoded. Same
# reasoning as test.sh: hardcoded names would pin today's output format
# instead of the required behaviour, which is that the same branch reaches
# the same window from every process independently. The properties of the
# names themselves are test.sh section 8's subject.
# shellcheck source=./hooks/_hop-tmux-lib.sh
. "$HOOKS_SRC/_hop-tmux-lib.sh"

# ======================================================================
section "1. hooks installed at the hopspace level, as the README says"

# A hub built the way a user's is: a bare origin, a seed clone with one
# commit, then `git hop <uri> hub`.
git init --bare "$WORKDIR/origin.git" >/dev/null 2>&1 ||
    fatal "git init --bare failed"
git clone "$WORKDIR/origin.git" "$WORKDIR/seed" >/dev/null 2>&1 ||
    fatal "git clone failed"
git -C "$WORKDIR/seed" commit --allow-empty -m "initial" >/dev/null 2>&1 ||
    fatal "seed commit failed"
git -C "$WORKDIR/seed" push origin main >/dev/null 2>&1 ||
    fatal "seed push failed"

HUB="$WORKDIR/hub"
if ! (cd "$WORKDIR" && env PATH="$SHIMDIR:$PATH" git hop "$WORKDIR/origin.git" hub) \
        >"$WORKDIR/hub.log" 2>&1; then
    printf '%s\n' "$(cat "$WORKDIR/hub.log")" >&2
    fatal "git hop <uri> hub failed"
fi
[ -d "$HUB" ] || fatal "hub was not created at $HUB"
ok "hub created by the real binary"

cd "$HUB" || fatal "cannot cd into the hub"

# The repo ID git-hop will hand the hooks. Read out of the hub's own
# hop.json rather than assumed: the hooks derive every tmux name from it,
# so a test that guessed it could pass while the real names differed.
ORG="$(sed -n 's/.*"org"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$HUB/hop.json" | head -1)"
REPO="$(sed -n 's/.*"repo"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$HUB/hop.json" | head -1)"
[ -n "$ORG" ] && [ -n "$REPO" ] || fatal "could not read org/repo from $HUB/hop.json"
REPO_ID="github.com/$ORG/$REPO"
SESSION="$(hop_tmux_session_name "$REPO_ID")"
printf '  (repo id: %s)\n' "$REPO_ID"
printf '  (session: %s)\n' "$SESSION"

# Installed exactly where README.md tells users to put them:
# $GIT_HOP_DATA_HOME/<host>/<org>/<repo>/hooks/. Not a copy edited for the
# test, and not a stub -- the same files test.sh drives directly.
HOPSPACE_HOOKS="$GIT_HOP_DATA_HOME/github.com/$ORG/$REPO/hooks"
mkdir -p "$HOPSPACE_HOOKS"
cp "$HOOKS_SRC"/* "$HOPSPACE_HOOKS"/ || fatal "failed to install hooks"
chmod +x "$HOPSPACE_HOOKS"/post-* || fatal "failed to chmod hooks"
ok "tmux hooks installed at the hopspace level"

# Guard the guard: if the binary cannot find these files, every case below
# would pass vacuously by asserting on windows that were never asked for.
# A canary hook at the same level, fired by the same lookup, proves the
# resolution path reaches this directory before anything depends on it.
cp "$HOPSPACE_HOOKS/post-worktree-add" "$WORKDIR/real-post-add"
cat >"$HOPSPACE_HOOKS/post-worktree-add" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$GIT_HOP_REPO_ID" >"$WORKDIR/canary"
exit 0
EOF
chmod +x "$HOPSPACE_HOOKS/post-worktree-add"
hop add canary/probe >/dev/null 2>&1
if [ -f "$WORKDIR/canary" ]; then
    ok "git-hop really resolves hooks from the hopspace dir"
else
    bad "git-hop really resolves hooks from the hopspace dir" \
        "canary hook fired" "no hook ran"
fi
# The repo ID the binary hands a hook must be the one the names are derived
# from, or the hooks address a different session than this suite queries.
assert_eq "the repo ID git-hop passes hooks matches the one under test" \
    "$REPO_ID" "$(cat "$WORKDIR/canary" 2>/dev/null)"
cp "$WORKDIR/real-post-add" "$HOPSPACE_HOOKS/post-worktree-add"
chmod +x "$HOPSPACE_HOOKS/post-worktree-add"
hop remove canary/probe >/dev/null 2>&1

# ======================================================================
section "2. 'git hop add' creates the window"

W_FEATA="$(hop_tmux_window_name 'feature/a')"

hop add feature/a
add_rc=$?
[ "$add_rc" -eq 0 ] || printf '%s\n' "$LASTOUT" >&2
assert_eq "git hop add exits 0" "0" "$add_rc"

assert_contains "add created the window for 'feature/a'" \
    "$(windows_of "$SESSION")" "$W_FEATA"

# The window must open IN the worktree git-hop just made, not in whatever
# directory the command happened to run from -- a window in the wrong tree
# is worse than no window.
wt_a="$(t display-message -p -t "=$SESSION:=$W_FEATA" '#{pane_current_path}' 2>/dev/null)"
assert_eq "the window opens in the worktree git-hop created" \
    "$HUB/hops/feature/a" "$wt_a"

# And it records that worktree as its identity, which is what makes the
# removal in section 4 provably about this worktree rather than this name.
assert_eq "the window records the worktree git-hop created" \
    "$HUB/hops/feature/a" \
    "$(t show-options -w -v -t "=$SESSION:=$W_FEATA" @hop-worktree 2>/dev/null)"

# ======================================================================
section "3. 'git hop <branch>' selects the window AND exits 93"

W_FIXB="$(hop_tmux_window_name 'fix/b')"
hop add fix/b >/dev/null 2>&1
assert_contains "second branch got its own window" \
    "$(windows_of "$SESSION")" "$W_FIXB"

# Point the session somewhere else first, so "selected" means the switch
# moved it rather than it already happening to be there.
t select-window -t "=$SESSION:=$W_FIXB" >/dev/null 2>&1

hop_inside feature/a
switch_rc=$?

# THE load-bearing assertion, and the reason this case exists on top of the
# e2e Go test and test.sh. The Go test proves 93 survives the binary when a
# stub hook emits it; test.sh proves the tmux hook emits 93 when driven by
# hand. Neither proves the composition -- that the REAL hook, dispatched by
# the REAL binary, produces a process exit status of 93. That status is the
# shell wrapper's only channel: without it the wrapper also cds the
# ORIGINATING window into the target worktree, leaving two windows in one
# tree with the source window silently pointed somewhere it never agreed
# to go.
assert_eq "git hop <branch> exits 93 through the real binary" "93" "$switch_rc"

active="$(t list-windows -t "=$SESSION" -F '#{window_active} #{window_name}' 2>/dev/null |
    sed -n 's/^1 //p')"
assert_eq "the switch selected the target branch's window" "$W_FEATA" "$active"

# The asymmetry that keeps a non-tmux user from being stranded: with no
# tmux to navigate, the hook must NOT claim 93, or the wrapper skips the cd
# and the user stays put. Driven here through the binary, with tmux absent
# from the hook's PATH rather than from the hook's arguments.
NOTMUX="$WORKDIR/notmux"
mkdir -p "$NOTMUX"
for bin in sh bash env dirname tr printf grep sed cksum git; do
    src="$(sh -c "command -v $bin" 2>/dev/null)" && ln -sf "$src" "$NOTMUX/$bin"
done
ln -sf "$BINDIR/git-hop" "$NOTMUX/git-hop"
if env PATH="$NOTMUX" sh -c 'command -v tmux' >/dev/null 2>&1; then
    bad "tmux really is absent from the stripped PATH" "no tmux" "tmux found"
else
    ok "tmux really is absent from the stripped PATH"
fi
env PATH="$NOTMUX" TMUX="/tmp/fake,0,0" git hop fix/b >/dev/null 2>&1
assert_eq "git hop <branch> exits 0 (NOT 93) when tmux is unavailable" "0" "$?"

# ======================================================================
section "4. 'git hop remove' kills only that branch's window"

# The composed version of the data-loss scenario. Two branches whose
# sanitized stems used to be IDENTICAL -- '/' and a literal '+' both folded
# to '+', so 'ship/it' and 'ship+it' produced the same window name, and
# post-worktree-remove kills BY NAME. Removing one destroyed the other's
# window and every process in it: the dev server, the REPL, the editor with
# unsaved buffers. Silently, because from tmux's side the kill did exactly
# what it was told.
#
# test.sh section 9 covers this by hand-invoking the hooks. What it cannot
# cover is git-hop passing the branch through unmangled: if the binary
# normalised '+' or '/' anywhere on the way to the hook environment, both
# removals would address one window again and the hand-driven test would
# still be green.
W_SLASH="$(hop_tmux_window_name 'ship/it')"
W_PLUS="$(hop_tmux_window_name 'ship+it')"

hop add ship/it >/dev/null 2>&1
hop add ship+it >/dev/null 2>&1

wins="$(windows_of "$SESSION")"
printf '  (ship/it -> %s   ship+it -> %s)\n' "$W_SLASH" "$W_PLUS"
assert_contains "'ship/it' got its own window" "$wins" "$W_SLASH"
assert_contains "'ship+it' got its own window" "$wins" "$W_PLUS"

# Asserted BEFORE anything about survival, because every later assertion in
# this section is vacuous without it. Under a colliding transform both
# branches address one single window: "the sibling survived" then passes by
# inspecting the very window the removal was aimed at, and the case reports
# green while describing a scenario that never happened. Two branches, two
# windows, or there is nothing here to test.
if [ "$W_SLASH" = "$W_PLUS" ]; then
    bad "the two branches address DIFFERENT windows" \
        "two distinct window names" "both '$W_SLASH'"
else
    ok "the two branches address DIFFERENT windows"
fi
assert_eq "the collision pair really produced two windows on the server" "2" \
    "$(printf '%s\n' "$wins" | grep -Fx -e "$W_SLASH" -e "$W_PLUS" | sort -u | grep -c .)"

# Real long-lived processes in BOTH windows. A window name surviving in a
# listing proves nothing if the pane behind it was recreated or the process
# in it was killed -- what the user loses is the process, not the name.
t send-keys -t "=$SESSION:=$W_SLASH" 'exec sleep 600' Enter >/dev/null 2>&1
t send-keys -t "=$SESSION:=$W_PLUS" 'exec sleep 600' Enter >/dev/null 2>&1
sleep 0.6
doomed_pid="$(t display-message -p -t "=$SESSION:=$W_SLASH" '#{pane_pid}' 2>/dev/null)"
survivor_pid="$(t display-message -p -t "=$SESSION:=$W_PLUS" '#{pane_pid}' 2>/dev/null)"
printf '  (doomed pane_pid=%s  survivor pane_pid=%s)\n' "$doomed_pid" "$survivor_pid"

# Same guard one level down: if both names resolved to one window, both
# lookups return the same PID and "the survivor is still alive" becomes a
# statement about the process that was supposed to die.
if [ -n "$doomed_pid" ] && [ -n "$survivor_pid" ] && [ "$doomed_pid" != "$survivor_pid" ]; then
    ok "the two windows hold two DIFFERENT processes"
else
    bad "the two windows hold two DIFFERENT processes" \
        "distinct pids" "doomed=$doomed_pid survivor=$survivor_pid"
fi

if [ -n "$survivor_pid" ] && ps -p "$survivor_pid" >/dev/null 2>&1; then
    ok "the sibling's process is running before the removal"
else
    bad "the sibling's process is running before the removal" \
        "pid $survivor_pid alive" "not running"
fi

hop remove ship/it
rm_rc=$?
[ "$rm_rc" -eq 0 ] || printf '%s\n' "$LASTOUT" >&2
assert_eq "git hop remove exits 0" "0" "$rm_rc"

wins="$(windows_of "$SESSION")"
assert_not_contains "the removed branch's window is gone" "$wins" "$W_SLASH"
assert_contains "the colliding sibling's window SURVIVED" "$wins" "$W_PLUS"

# THE assertion. This is the data loss, stated in the only terms that
# matter: is the user's process still running.
if [ -n "$survivor_pid" ] && ps -p "$survivor_pid" >/dev/null 2>&1; then
    ok "the colliding sibling's RUNNING PROCESS survived the removal"
else
    bad "the colliding sibling's RUNNING PROCESS survived the removal" \
        "pid $survivor_pid alive" "killed"
fi

# The removal did do its job, so the case cannot pass by the hook having
# been a no-op for both branches.
if ps -p "$doomed_pid" >/dev/null 2>&1; then
    bad "the removed branch's process was cleaned up" \
        "pid $doomed_pid gone" "still running"
else
    ok "the removed branch's process was cleaned up"
fi

# Untouched bystanders on the same session.
assert_contains "'feature/a' window untouched by the removal" "$wins" "$W_FEATA"
assert_contains "'fix/b' window untouched by the removal" "$wins" "$W_FIXB"

# ======================================================================
section "5. 'git hop move' preserves window identity end to end"

W_MOVED="$(hop_tmux_window_name 'ship+moved')"

# A marker process again, for the same reason: kill-and-recreate produces a
# window with the right NAME and none of the user's work in it. Identical
# in a listing, total loss in practice.
t send-keys -t "=$SESSION:=$W_PLUS" 'exec sleep 600' Enter >/dev/null 2>&1
sleep 0.6
pid_before="$(t display-message -p -t "=$SESSION:=$W_PLUS" '#{pane_pid}' 2>/dev/null)"
id_before="$(t display-message -p -t "=$SESSION:=$W_PLUS" '#{window_id}' 2>/dev/null)"
printf '  (pane_pid before=%s  window_id before=%s)\n' "$pid_before" "$id_before"

hop move ship+it ship+moved
mv_rc=$?
[ "$mv_rc" -eq 0 ] || printf '%s\n' "$LASTOUT" >&2
assert_eq "git hop move exits 0" "0" "$mv_rc"

wins="$(windows_of "$SESSION")"
assert_contains "the window carries the new branch's name" "$wins" "$W_MOVED"
assert_not_contains "the old name is gone" "$wins" "$W_PLUS"

pid_after="$(t display-message -p -t "=$SESSION:=$W_MOVED" '#{pane_pid}' 2>/dev/null)"
id_after="$(t display-message -p -t "=$SESSION:=$W_MOVED" '#{window_id}' 2>/dev/null)"
printf '  (pane_pid after=%s   window_id after=%s)\n' "$pid_after" "$id_after"

# THE assertions distinguishing rename from kill-and-recreate.
assert_eq "the pane PID survived the move" "$pid_before" "$pid_after"
assert_eq "the window ID survived the move" "$id_before" "$id_after"

# Deliberately $pid_before, not $pid_after: after a kill-and-recreate the
# new pane's PID is trivially alive, so asserting on it is a tautology that
# passes whatever the hook did.
if ps -p "$pid_before" >/dev/null 2>&1; then
    ok "the original process is still running after the move"
else
    bad "the original process is still running after the move" \
        "pid $pid_before alive" "killed"
fi

# The identity must FOLLOW the rename. A record left pointing at the old
# worktree would make the eventual removal decline to kill the window --
# the safety check turning into a leak -- so this is asserted by actually
# removing the branch and requiring the window to go.
moved_id="$(t show-options -w -v -t "=$SESSION:=$W_MOVED" @hop-worktree 2>/dev/null)"
printf '  (@hop-worktree after move=%s)\n' "$moved_id"
assert_eq "@hop-worktree followed the rename to the new worktree path" \
    "$HUB/hops/ship+moved" "$moved_id"

hop remove ship+moved >/dev/null 2>&1
assert_not_contains "the moved window is still removable afterwards" \
    "$(windows_of "$SESSION")" "$W_MOVED"

# ======================================================================
section "6. cold start: no server at all, then 'git hop add'"

# Installing these hooks is explicit consent to have tmux driven, so the
# first `git hop add` after a reboot must produce a window rather than
# silently doing nothing until the user happens to open tmux by hand.
#
# Driven through the binary on a socket that has never had a server: the
# create half of hop_tmux_ensure_session is only reachable from cold, and
# only reachable at all if git-hop dispatches the hook in the first place.
mkdir -p "$WORKDIR/shim-cold"
cat >"$WORKDIR/shim-cold/tmux" <<EOF
#!/usr/bin/env bash
exec $TMUX_BIN -L "$COLD" "\$@"
EOF
chmod +x "$WORKDIR/shim-cold/tmux"

if "$TMUX_BIN" -L "$COLD" has-session >/dev/null 2>&1; then
    bad "the cold socket really has no server yet" "no server" "server running"
else
    ok "the cold socket really has no server yet"
fi

cold_err="$(env PATH="$WORKDIR/shim-cold:$PATH" git hop add cold/start 2>&1 >/dev/null)"
cold_rc=$?
assert_eq "git hop add exits 0 against a dead tmux server" "0" "$cold_rc"

if "$TMUX_BIN" -L "$COLD" has-session >/dev/null 2>&1; then
    ok "the add spawned a tmux server from cold"
else
    bad "the add spawned a tmux server from cold" "server running" "no server"
    printf '        stderr: %s\n' "$cold_err"
fi

cold_wins="$("$TMUX_BIN" -L "$COLD" list-windows -t "=$SESSION" -F '#{window_name}' 2>/dev/null)"
assert_contains "cold start created the branch's window" \
    "$cold_wins" "$(hop_tmux_window_name 'cold/start')"

"$TMUX_BIN" -L "$COLD" kill-server >/dev/null 2>&1

# ======================================================================
printf '\n== summary\n  passed: %d\n  failed: %d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
printf '  ALL GREEN\n'
