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
SOCKET="hop-tmux-test-$$"
REPO_ID="github.com/acme/widgets"
# -P through pwd: on macOS mktemp hands back /var/..., a symlink to
# /private/var/..., and tmux reports the resolved form. Resolving up front
# keeps the dir assertions comparing like with like.
WORKDIR="$(cd "$(mktemp -d)" && pwd -P)"

PASS=0
FAIL=0

cleanup() {
    tmux -L "$SOCKET" kill-server >/dev/null 2>&1
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

SESSION="hop+github_com+acme+widgets"

mkdir -p "$WORKDIR/main" "$WORKDIR/feature-a" "$WORKDIR/fix-b"

printf 'tmux: %s\n' "$(tmux -V)"
printf 'socket: %s\n' "$SOCKET"

# ======================================================================
section "1. post-clone then post-worktree-add produce session + window"

# post-clone on a dead server is a no-op by design (a hook must not spawn
# a server nobody asked for), so bring one up the way a real user's
# already-running tmux would be up.
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

assert_contains "window 'main' exists" "$(windows_of "$SESSION")" "main"

got_dir="$(t display-message -p -t "=$SESSION:=main" '#{pane_current_path}' 2>/dev/null)"
assert_eq "window 'main' starts in its worktree dir" "$WORKDIR/main" "$got_dir"

# ======================================================================
section "2. second branch adds a second window; first survives"

run_hook post-worktree-add \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=fix/b \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/fix-b"

wins="$(windows_of "$SESSION")"
assert_contains "window 'fix+b' created" "$wins" "fix+b"
assert_contains "window 'main' survived" "$wins" "main"
assert_eq "session has exactly 2 windows" "2" "$(printf '%s\n' "$wins" | grep -c .)"

# ======================================================================
section "7. slash branch produces a usable, selectable window"

run_hook post-worktree-add \
    GIT_HOP_HOOK_NAME=post-worktree-add \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=feature/a \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a"

assert_contains "branch 'feature/a' -> window 'feature+a'" "$(windows_of "$SESSION")" "feature+a"

t select-window -t "=$SESSION:=feature+a" >/dev/null 2>&1
assert_eq "'feature+a' is selectable by exact target" "0" "$?"

assert_eq "'feature+a' became the session's current window" "feature+a" "$(active_window_of "$SESSION")"

# A dot in the repo ID must NOT survive into the session name -- a session
# named with a dot is unreachable by any tmux target at all.
case "$SESSION" in
    *.*) bad "session name contains no '.'" "no dot" "$SESSION" ;;
    *)   ok "session name contains no '.' (dots are unaddressable in targets)" ;;
esac

# ======================================================================
section "3. post-worktree-switch selects the right window and exits 93"

t select-window -t "=$SESSION:=main" >/dev/null 2>&1

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

assert_eq "switch selected window 'feature+a'" "feature+a" "$(active_window_of "$SESSION")"

# Killed by hand, then resumed: must recreate, not error.
t kill-window -t "=$SESSION:=fix+b" >/dev/null 2>&1
run_hook post-worktree-switch \
    TMUX="/tmp/fake,0,0" \
    GIT_HOP_HOOK_NAME=post-worktree-switch \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=fix/b \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/fix-b" \
    GIT_HOP_TRIGGER=hop
assert_eq "switch to a hand-killed window still exits 93" "93" "$?"
assert_contains "switch recreated the missing window" "$(windows_of "$SESSION")" "fix+b"

# ======================================================================
section "4. post-worktree-remove kills only the matching window"

run_hook post-worktree-remove \
    GIT_HOP_HOOK_NAME=post-worktree-remove \
    GIT_HOP_REPO_ID="$REPO_ID" \
    GIT_HOP_BRANCH=fix/b \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/fix-b"
assert_eq "post-worktree-remove exits 0" "0" "$?"

wins="$(windows_of "$SESSION")"
assert_not_contains "window 'fix+b' killed" "$wins" "fix+b"
assert_contains "window 'main' untouched" "$wins" "main"
assert_contains "window 'feature+a' untouched" "$wins" "feature+a"

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
t send-keys -t "=$SESSION:=feature+a" \
    "echo HOP_MARKER_ALIVE; exec sleep 600" Enter >/dev/null 2>&1
sleep 0.5

pid_before="$(t display-message -p -t "=$SESSION:=feature+a" '#{pane_pid}' 2>/dev/null)"
id_before="$(t display-message -p -t "=$SESSION:=feature+a" '#{window_id}' 2>/dev/null)"
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
assert_contains "window renamed to 'feature+renamed'" "$wins" "feature+renamed"
assert_not_contains "old name 'feature+a' gone" "$wins" "feature+a"
assert_eq "no window was added (rename, not create)" "2" "$(printf '%s\n' "$wins" | grep -c .)"

pid_after="$(t display-message -p -t "=$SESSION:=feature+renamed" '#{pane_pid}' 2>/dev/null)"
id_after="$(t display-message -p -t "=$SESSION:=feature+renamed" '#{window_id}' 2>/dev/null)"
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

# 6b. tmux installed, but no server running and $TMUX unset. Must not
# spawn a server for a user who never asked for one.
NOSERVER="hop-tmux-noserver-$$"
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

if tmux -L "$NOSERVER" has-session 2>/dev/null; then
    bad "hooks did not spawn a tmux server" "no server on $NOSERVER" "server running"
    tmux -L "$NOSERVER" kill-server >/dev/null 2>&1
else
    ok "hooks did not spawn a tmux server for a non-tmux user"
fi

# post-worktree-switch specifically must exit 0 and NOT 93 without tmux --
# 93 would suppress the wrapper's cd and strand the user.
env PATH="$NOTMUX" GIT_HOP_HOOK_NAME=post-worktree-switch \
    GIT_HOP_REPO_ID="$REPO_ID" GIT_HOP_BRANCH=feature/a \
    GIT_HOP_WORKTREE_PATH="$WORKDIR/feature-a" \
    "$HOOKS_DIR/post-worktree-switch" >/dev/null 2>&1
assert_eq "switch exits 0 (NOT 93) without tmux, so wrapper still cds" "0" "$?"

# ======================================================================
printf '\n== summary\n  passed: %d\n  failed: %d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
printf '  ALL GREEN\n'
