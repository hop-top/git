#!/usr/bin/env bash
#
# Drives the tmux hooks against a REAL tmux server and asserts on what the
# server actually reports.
#
# Runs on a private socket (-L) so it cannot see, disturb, or be disturbed
# by the user's own tmux. The server is killed on exit, including on
# failure, via trap.
#
# Usage: ./test.sh
set -u

HOOKS_DIR="$(cd "$(dirname "$0")/hooks" && pwd)"
# All three sockets carry the PID so concurrent runs cannot collide, and
# all three are named so they could never be the user's own server.
SOCKET="hop-tmux-test-$$"
NOSERVER="hop-tmux-noserver-$$"
COLD="hop-tmux-cold-$$"
REPO_ID="github.com/acme/widgets"
# -P through pwd: on macOS mktemp hands back /var/..., a symlink to
# /private/var/..., and tmux reports the resolved form. Resolving up front
# keeps the dir assertions comparing like with like.
WORKDIR="$(cd "$(mktemp -d)" && pwd -P)"

PASS=0
FAIL=0

# Every server this suite may have started is killed here, including on
# failure. The socket FILES are unlinked too: a killed server leaves its
# socket behind, and since the cold-start sections now legitimately spawn
# servers, letting those accumulate would litter /tmp on every run.
cleanup() {
    local s
    for s in "$SOCKET" "$NOSERVER" "$COLD"; do
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

# --- harness -----------------------------------------------------------

# Runs a hook with a git-hop-shaped environment, against the test socket.
#
# The scripts call bare `tmux`, so the private socket is injected by
# putting a `tmux` shim first on PATH that appends -L. This is exactly how
# the real thing runs -- the hook is not modified or parameterised for the
# test.
SHIMDIR="$WORKDIR/shim"
mkdir -p "$SHIMDIR"
cat >"$SHIMDIR/tmux" <<EOF
#!/usr/bin/env bash
exec $(command -v tmux) -L "$SOCKET" "\$@"
EOF
chmod +x "$SHIMDIR/tmux"

run_hook() {
    local hook="$1"; shift
    env PATH="$SHIMDIR:$PATH" "$@" "$HOOKS_DIR/$hook"
}

# tmux query against the test server.
t() { tmux -L "$SOCKET" "$@"; }

windows_of() { t list-windows -t "=$1" -F '#{window_name}' 2>/dev/null; }

# The session's current window.
#
# NOT `display-message -t <session>`: with no client attached to that
# session tmux resolves the format against the calling client (there is
# none here) and prints an empty string with status 0 -- a silently wrong
# answer. Reading window_active off the window list asks the server about
# server state and works detached, which is how this suite runs.
active_window_of() {
    t list-windows -t "=$1" -F '#{window_active} #{window_name}' 2>/dev/null |
        sed -n 's/^1 //p'
}

# Expected names come from the library itself, not from literals.
#
# The names carry a hash of the branch, so hardcoding them here would mean
# a test suite that has to be edited whenever the transform changes -- and
# a suite that pins today's OUTPUT rather than the required BEHAVIOUR. What
# these sections actually assert is that the same input reaches the same
# window, which is exactly what deriving through the shared helper checks.
# The properties of the names themselves (distinctness, legibility,
# stability, no dots) are asserted directly in section 8.
. "$HOOKS_DIR/_hop-tmux-lib.sh"

SESSION="$(hop_tmux_session_name "$REPO_ID")"
W_MAIN="$(hop_tmux_window_name 'main')"
W_FIXB="$(hop_tmux_window_name 'fix/b')"
W_FEATA="$(hop_tmux_window_name 'feature/a')"
W_RENAMED="$(hop_tmux_window_name 'feature/renamed')"

mkdir -p "$WORKDIR/main" "$WORKDIR/feature-a" "$WORKDIR/fix-b"

printf 'tmux: %s\n' "$(tmux -V)"
printf 'socket: %s\n' "$SOCKET"

# ======================================================================
section "1. post-clone then post-worktree-add produce session + window"

# A scratch session standing in for whatever the user already had open.
# It is here so the sibling-window assertions run against a server that
# holds unrelated sessions too, not because the hooks need one to exist --
# cold start is section 11's subject.
t new-session -d -s scratch -n scratch >/dev/null 2>&1

run_hook post-clone \
    GIT_HOP_HOOK_NAME=post-clone \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/main"
assert_eq "post-clone exits 0" "0" "$?"

if t has-session -t "=$SESSION" 2>/dev/null; then
    ok "post-clone created session '$SESSION'"
else
    bad "post-clone created session '$SESSION'" "session exists" "$(t list-sessions -F '#{session_name}' 2>&1 | tr '\n' ' ')"
fi

run_hook post-worktree-add \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=main \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/main"
assert_eq "post-worktree-add exits 0" "0" "$?"

assert_contains "window for 'main' exists" "$(windows_of "$SESSION")" "$W_MAIN"

got_dir="$(t display-message -p -t "=$SESSION:=$W_MAIN" '#{pane_current_path}' 2>/dev/null)"
assert_eq "window for 'main' starts in its worktree dir" "$WORKDIR/main" "$got_dir"

# ======================================================================
section "2. second branch adds a second window; first survives"

run_hook post-worktree-add \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=fix/b \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/fix-b"

wins="$(windows_of "$SESSION")"
assert_contains "window for 'fix/b' created" "$wins" "$W_FIXB"
assert_contains "window for 'main' survived" "$wins" "$W_MAIN"
assert_eq "session has exactly 2 windows" "2" "$(printf '%s\n' "$wins" | grep -c .)"

# ======================================================================
section "7. slash branch produces a usable, selectable window"

run_hook post-worktree-add \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=feature/a \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a"

assert_contains "branch 'feature/a' got its window" "$(windows_of "$SESSION")" "$W_FEATA"

t select-window -t "=$SESSION:=$W_FEATA" >/dev/null 2>&1
assert_eq "the 'feature/a' window is selectable by exact target" "0" "$?"

assert_eq "the 'feature/a' window became the session's current window" "$W_FEATA" "$(active_window_of "$SESSION")"

# A dot in the repo ID must NOT survive into the session name -- a session
# named with a dot is unreachable by any tmux target at all.
case "$SESSION" in
    *.*) bad "session name contains no '.'" "no dot" "$SESSION" ;;
    *)   ok "session name contains no '.' (dots are unaddressable in targets)" ;;
esac

# ======================================================================
section "3. post-worktree-switch selects the right window and exits 93"

t select-window -t "=$SESSION:=$W_MAIN" >/dev/null 2>&1

# TMUX must be set for the hook to take the "inside tmux" path. Its value
# is only tested for emptiness by the hook, so a marker string is enough.
run_hook post-worktree-switch \
    TMUX="/tmp/fake,0,0" \
    GIT_HOP_HOOK_NAME=post-worktree-switch \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=feature/a \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a" \
    GIT_HOP_FROM_BRANCH=main \
    GIT_HOP_FROM_WORKTREE_PATH="$WORKDIR/main" \
    GIT_HOP_TRIGGER=hop
switch_rc=$?

# THE load-bearing assertion. Exit 0 here means the shell wrapper also cds
# the originating window into the target worktree -- two windows in one
# tree, source window silently contaminated.
assert_eq "post-worktree-switch exits 93 (ExitNavigationHandled)" "93" "$switch_rc"

assert_eq "switch selected the 'feature/a' window" "$W_FEATA" "$(active_window_of "$SESSION")"

# Killed by hand, then resumed: must recreate, not error.
t kill-window -t "=$SESSION:=$W_FIXB" >/dev/null 2>&1
run_hook post-worktree-switch \
    TMUX="/tmp/fake,0,0" \
    GIT_HOP_HOOK_NAME=post-worktree-switch \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=fix/b \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/fix-b" \
    GIT_HOP_TRIGGER=hop
assert_eq "switch to a hand-killed window still exits 93" "93" "$?"
assert_contains "switch recreated the missing window" "$(windows_of "$SESSION")" "$W_FIXB"

# ======================================================================
section "4. post-worktree-remove kills only the matching window"

run_hook post-worktree-remove \
    GIT_HOP_HOOK_NAME=post-worktree-remove \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=fix/b \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/fix-b"
assert_eq "post-worktree-remove exits 0" "0" "$?"

wins="$(windows_of "$SESSION")"
assert_not_contains "window for 'fix/b' killed" "$wins" "$W_FIXB"
assert_contains "window for 'main' untouched" "$wins" "$W_MAIN"
assert_contains "window for 'feature/a' untouched" "$wins" "$W_FEATA"

# Removing an already-absent window is a no-op, not an error.
run_hook post-worktree-remove \
    GIT_HOP_HOOK_NAME=post-worktree-remove \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=fix/b \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/fix-b"
assert_eq "removing an absent window exits 0" "0" "$?"

# ======================================================================
section "5. post-worktree-move renames IN PLACE (pane survives)"

# Start a marker process in the window and record the pane's PID. If the
# hook kills and recreates the window, the pane is a different process and
# the marker is gone -- which is precisely the user losing their dev
# server to a branch rename.
t send-keys -t "=$SESSION:=$W_FEATA" \
    "echo HOP_MARKER_ALIVE; exec sleep 600" Enter >/dev/null 2>&1
sleep 0.5

pid_before="$(t display-message -p -t "=$SESSION:=$W_FEATA" '#{pane_pid}' 2>/dev/null)"
id_before="$(t display-message -p -t "=$SESSION:=$W_FEATA" '#{window_id}' 2>/dev/null)"
printf '  (pane_pid before=%s window_id before=%s)\n' "$pid_before" "$id_before"

run_hook post-worktree-move \
    GIT_HOP_HOOK_NAME=post-worktree-move \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_OLD_BRANCH=feature/a \
    GIT_HOP_NEW_BRANCH=feature/renamed \
    GIT_HOP_OLD_PATH="$WORKDIR/feature-a" \
    GIT_HOP_NEW_PATH="$WORKDIR/feature-a" \
    GIT_HOP_BRANCH=feature/renamed \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a"
assert_eq "post-worktree-move exits 0" "0" "$?"

wins="$(windows_of "$SESSION")"
assert_contains "window renamed for 'feature/renamed'" "$wins" "$W_RENAMED"
assert_not_contains "old 'feature/a' name gone" "$wins" "$W_FEATA"
assert_eq "no window was added (rename, not create)" "2" "$(printf '%s\n' "$wins" | grep -c .)"

pid_after="$(t display-message -p -t "=$SESSION:=$W_RENAMED" '#{pane_pid}' 2>/dev/null)"
id_after="$(t display-message -p -t "=$SESSION:=$W_RENAMED" '#{window_id}' 2>/dev/null)"
printf '  (pane_pid after=%s  window_id after=%s)\n' "$pid_after" "$id_after"

# THE assertion that distinguishes rename from kill-and-recreate.
assert_eq "pane PID survived the rename" "$pid_before" "$pid_after"
assert_eq "window ID survived the rename" "$id_before" "$id_after"

# The ORIGINAL process must still be running.
#
# Deliberately $pid_before, not $pid_after: after a kill-and-recreate the
# new pane's PID is trivially alive, so asserting on it is a tautology
# that passes no matter what the hook did. The question is whether the
# user's process survived, and only the pre-rename PID can answer it.
if ps -p "$pid_before" >/dev/null 2>&1; then
    ok "the original pane process is still running"
else
    bad "the original pane process is still running" "pid $pid_before alive" "killed"
fi

# ======================================================================
section "6. no tmux -> every script exits 0, silently"

# 6a. tmux absent from PATH entirely.
#
# NOT an empty PATH: these scripts are `#!/usr/bin/env bash`, so an empty
# PATH means env cannot find bash and the hook exits 127 without ever
# running -- which would "pass" a naive rc check for the wrong reason
# while proving nothing about the hooks. Instead: a dir symlinking in the
# interpreters and coreutils a hook legitimately needs, and pointedly no
# tmux. That is a user who does not have tmux installed.
NOTMUX="$WORKDIR/notmux"
mkdir -p "$NOTMUX"
for bin in sh bash env dirname tr printf grep sed; do
    src="$(command -v "$bin" 2>/dev/null)" && ln -sf "$src" "$NOTMUX/$bin"
done
# Guard the guard: if tmux leaked into this PATH the whole section is
# vacuous, and if bash did not make it in every case is a false 127.
# Probed in a child `env`, not with `command -v` in this shell: the
# caller's shell may carry a `tmux` alias or function that command -v
# happily resolves regardless of PATH, reporting "found" for something the
# hooks could never reach.
if env PATH="$NOTMUX" sh -c 'command -v tmux' >/dev/null 2>&1; then
    bad "tmux really is absent from the stripped PATH" "no tmux" "tmux found"
else
    ok "tmux really is absent from the stripped PATH"
fi
if env PATH="$NOTMUX" sh -c 'command -v bash' >/dev/null 2>&1; then
    ok "bash is present in the stripped PATH (so hooks actually run)"
else
    bad "bash is present in the stripped PATH" "bash found" "missing"
fi
for hook in post-clone post-worktree-add post-worktree-switch \
            post-worktree-remove post-worktree-move; do
    err="$(env -i PATH="$NOTMUX" HOME="$HOME" \
        GIT_HOP_HOOK_NAME="$hook" \
        GIT_HOP_REPO_ID="$REPO_ID" \
        GIT_HOP_BRANCH=feature/a \
        GIT_HOP_OLD_BRANCH=feature/a \
        GIT_HOP_NEW_BRANCH=feature/c \
        GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a" \
        "$HOOKS_DIR/$hook" 2>&1 >/dev/null)"
    rc=$?
    assert_eq "$hook exits 0 with tmux absent from PATH" "0" "$rc"
    assert_eq "$hook is silent on stderr with tmux absent" "" "$err"
done

# 6b. tmux installed, but no server running and $TMUX unset.
#
# Every hook must still exit 0 and stay silent. Whether a server gets
# spawned is section 11's subject, not this one's -- here the contract is
# only that a cold server is never an ERROR. The read-only hooks (remove,
# move against an absent window) have nothing to do and must not complain
# about it; the creating hooks bring the server up.
mkdir -p "$WORKDIR/shim2"
cat >"$WORKDIR/shim2/tmux" <<EOF
#!/usr/bin/env bash
exec $(command -v tmux) -L "$NOSERVER" "\$@"
EOF
chmod +x "$WORKDIR/shim2/tmux"

for hook in post-clone post-worktree-add post-worktree-switch \
            post-worktree-remove post-worktree-move; do
    err="$(env PATH="$WORKDIR/shim2:$PATH" \
        TMUX= \
        GIT_HOP_HOOK_NAME="$hook" \
        GIT_HOP_REPO_ID="$REPO_ID" \
        GIT_HOP_BRANCH=feature/a \
        GIT_HOP_OLD_BRANCH=feature/a \
        GIT_HOP_NEW_BRANCH=feature/c \
        GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a" \
        "$HOOKS_DIR/$hook" 2>&1 >/dev/null)"
    rc=$?
    assert_eq "$hook exits 0 with no tmux server running" "0" "$rc"
    assert_eq "$hook is silent on stderr with no server" "" "$err"
done

tmux -L "$NOSERVER" kill-server >/dev/null 2>&1

# post-worktree-switch specifically must exit 0 and NOT 93 without tmux --
# 93 would suppress the wrapper's cd and strand the user.
env PATH="$NOTMUX" GIT_HOP_HOOK_NAME=post-worktree-switch \
    GIT_HOP_REPO_ID="$REPO_ID" GIT_HOP_BRANCH=feature/a \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a" \
    "$HOOKS_DIR/post-worktree-switch" >/dev/null 2>&1
assert_eq "switch exits 0 (NOT 93) without tmux, so wrapper still cds" "0" "$?"

# ======================================================================
section "8. distinct branches never share a window name"

# The collision that matters: '/' and a literal '+' both used to fold to
# '+', so feature/a and feature+a produced the SAME window name. Since
# post-worktree-remove kills by name, removing one destroyed the other's
# window and everything running in it.
#
# These call the naming helpers directly (already sourced above) rather
# than going through the server: the transform is a pure function of its
# input, and the properties asserted here -- distinctness, legibility,
# absence of dots -- are properties of the string, not of tmux.
n_slash="$(hop_tmux_window_name 'feature/a')"
n_plus="$(hop_tmux_window_name 'feature+a')"
printf '  (feature/a -> %s   feature+a -> %s)\n' "$n_slash" "$n_plus"

if [ "$n_slash" = "$n_plus" ]; then
    bad "'feature/a' and 'feature+a' get distinct window names" \
        "two different names" "both '$n_slash'"
else
    ok "'feature/a' and 'feature+a' get distinct window names"
fi

# Legibility is still a requirement: the readable stem must survive so a
# status bar remains scannable.
case "$n_slash" in
    feature+a*) ok "window name keeps the readable 'feature+a' stem" ;;
    *) bad "window name keeps the readable 'feature+a' stem" "feature+a*" "$n_slash" ;;
esac

# Session names carry the same transform and the same collision.
s_slash="$(hop_tmux_session_name 'github.com/acme/widgets')"
s_plus="$(hop_tmux_session_name 'github.com+acme+widgets')"
if [ "$s_slash" = "$s_plus" ]; then
    bad "distinct repo IDs get distinct session names" \
        "two different names" "both '$s_slash'"
else
    ok "distinct repo IDs get distinct session names"
fi

# A dot anywhere in the result is fatal: dotted sessions are unaddressable.
case "$s_slash" in
    *.*) bad "session name still contains no '.'" "no dot" "$s_slash" ;;
    *)   ok "session name still contains no '.'" ;;
esac
case "$n_slash" in
    *.*) bad "window name contains no '.'" "no dot" "$n_slash" ;;
    *)   ok "window name contains no '.'" ;;
esac

# Stability is the load-bearing invariant: hooks share no state on disk, so
# every process must derive the identical name from the identical input.
# Computed in a FRESH shell, not this one, so a cached variable cannot make
# it pass.
again="$(env PATH="$SHIMDIR:$PATH" bash -c \
    '. "$1/_hop-tmux-lib.sh"; hop_tmux_window_name "feature/a"' _ "$HOOKS_DIR")"
assert_eq "window name is stable across separate processes" "$n_slash" "$again"

again_s="$(env PATH="$SHIMDIR:$PATH" bash -c \
    '. "$1/_hop-tmux-lib.sh"; hop_tmux_session_name "github.com/acme/widgets"' _ "$HOOKS_DIR")"
assert_eq "session name is stable across separate processes" "$s_slash" "$again_s"

# ======================================================================
section "9. removing one branch does not kill a colliding sibling"

# The actual data-loss scenario, driven against the real server: two
# branches whose sanitized stems used to be identical, both with windows,
# both with a live process. Remove one; the other must survive with its
# process intact.
COLL_REPO="github.com/acme/collide"
COLL_SESSION="$(hop_tmux_session_name "$COLL_REPO")"
mkdir -p "$WORKDIR/coll-slash" "$WORKDIR/coll-plus"

run_hook post-worktree-add \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$COLL_REPO" \
    GIT_HOP_BRANCH='feature/a' \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/coll-slash"

run_hook post-worktree-add \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$COLL_REPO" \
    GIT_HOP_BRANCH='feature+a' \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/coll-plus"

coll_wins="$(windows_of "$COLL_SESSION")"
printf '  (windows: %s)\n' "$(printf '%s' "$coll_wins" | tr '\n' ' ')"
assert_eq "both colliding branches got their own window" "2" \
    "$(printf '%s\n' "$coll_wins" | grep -c .)"

# Put a live marker process in the window that must SURVIVE, so the
# assertion is about the user's work and not just about a name in a list.
w_plus="$(hop_tmux_window_name 'feature+a')"
w_slash="$(hop_tmux_window_name 'feature/a')"
t send-keys -t "=$COLL_SESSION:=$w_plus" 'exec sleep 600' Enter >/dev/null 2>&1
sleep 0.5
survivor_pid="$(t display-message -p -t "=$COLL_SESSION:=$w_plus" '#{pane_pid}' 2>/dev/null)"
printf '  (survivor pane_pid=%s)\n' "$survivor_pid"

run_hook post-worktree-remove \
    GIT_HOP_HOOK_NAME=post-worktree-remove \
    GIT_HOP_REPO_ID="$COLL_REPO" \
    GIT_HOP_BRANCH='feature/a' \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/coll-slash"
assert_eq "removing 'feature/a' exits 0" "0" "$?"

coll_wins="$(windows_of "$COLL_SESSION")"
assert_not_contains "'feature/a' window was killed" "$coll_wins" "$w_slash"
assert_contains "'feature+a' window SURVIVED the removal" "$coll_wins" "$w_plus"

# THE assertion. A name surviving in a listing proves nothing if the pane
# behind it was recreated or the process was killed.
if [ -n "$survivor_pid" ] && ps -p "$survivor_pid" >/dev/null 2>&1; then
    ok "the sibling's running process survived the removal"
else
    bad "the sibling's running process survived the removal" \
        "pid $survivor_pid alive" "killed"
fi

# ======================================================================
section "10. remove verifies worktree identity before killing"

# The name is a derived string; the worktree path is the truth. Hooks
# record it on the window as a user option so a kill can confirm it is
# addressing the right worktree rather than trusting a name match.
opt="$(t show-options -w -v -t "=$COLL_SESSION:=$w_plus" @hop-worktree 2>/dev/null)"
assert_eq "window records its canonical worktree path" "$WORKDIR/coll-plus" "$opt"

# A window whose recorded identity is some OTHER worktree must not be
# killed, even when the name matches exactly.
mkdir -p "$WORKDIR/imposter"
t set-option -w -t "=$COLL_SESSION:=$w_plus" @hop-worktree "$WORKDIR/imposter" >/dev/null 2>&1

run_hook post-worktree-remove \
    GIT_HOP_HOOK_NAME=post-worktree-remove \
    GIT_HOP_REPO_ID="$COLL_REPO" \
    GIT_HOP_BRANCH='feature+a' \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/coll-plus"
assert_eq "remove with mismatched identity exits 0" "0" "$?"

assert_contains "window with a mismatched identity was NOT killed" \
    "$(windows_of "$COLL_SESSION")" "$w_plus"

# Restore the true identity; now the kill must go through.
t set-option -w -t "=$COLL_SESSION:=$w_plus" @hop-worktree "$WORKDIR/coll-plus" >/dev/null 2>&1
run_hook post-worktree-remove \
    GIT_HOP_HOOK_NAME=post-worktree-remove \
    GIT_HOP_REPO_ID="$COLL_REPO" \
    GIT_HOP_BRANCH='feature+a' \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/coll-plus"
assert_not_contains "window with a matching identity WAS killed" \
    "$(windows_of "$COLL_SESSION")" "$w_plus"

# ======================================================================
section "11. cold start: no server running, hooks bring one up"

# Installing these hooks is explicit consent to have tmux driven. A user
# who has not opened tmux yet still asked for a window per worktree, so a
# hook firing against a dead server must START one -- otherwise the
# "find or create" half of ensure_session is unreachable from cold and the
# very first `git hop add` after a reboot silently does nothing.
mkdir -p "$WORKDIR/shim3" "$WORKDIR/cold"
cat >"$WORKDIR/shim3/tmux" <<EOF
#!/usr/bin/env bash
exec $(command -v tmux) -L "$COLD" "\$@"
EOF
chmod +x "$WORKDIR/shim3/tmux"
if tmux -L "$COLD" has-session 2>/dev/null; then
    bad "cold socket really has no server yet" "no server" "server running"
else
    ok "cold socket really has no server yet"
fi

err="$(env PATH="$WORKDIR/shim3:$PATH" TMUX= \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=cold/start \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/cold" \
    "$HOOKS_DIR/post-worktree-add" 2>&1 >/dev/null)"
assert_eq "cold post-worktree-add exits 0" "0" "$?"
assert_eq "cold post-worktree-add is silent on stderr" "" "$err"

if tmux -L "$COLD" has-session 2>/dev/null; then
    ok "cold start spawned a tmux server"
else
    bad "cold start spawned a tmux server" "server running" "no server"
fi

cold_wins="$(tmux -L "$COLD" list-windows -t "=$SESSION" -F '#{window_name}' 2>/dev/null)"
assert_contains "cold start created the worktree's window" \
    "$cold_wins" "$(hop_tmux_window_name 'cold/start')"

tmux -L "$COLD" kill-server >/dev/null 2>&1

# ======================================================================
printf '\n== summary\n  passed: %d\n  failed: %d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
printf '  ALL GREEN\n'
