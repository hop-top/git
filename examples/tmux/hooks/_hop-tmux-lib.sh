#!/usr/bin/env bash
#
# Shared helpers for the git-hop tmux hooks.
#
# Sourced, never executed directly. Every hook in this directory sources
# this file, so the session/window naming transform has exactly one
# definition: each hook must compute the same target from the same
# GIT_HOP_* environment with no shared state on disk, and the only way to
# guarantee that is to share the code.

# --- tmux availability -------------------------------------------------
#
# One condition, and only one: is tmux installed. Nothing to talk to
# otherwise, and every hook must be a silent no-op for a user who does not
# run tmux at all.
#
# Note what is deliberately NOT required: $TMUX being set. $TMUX is only
# set for processes running INSIDE a tmux pane. A user with an attached
# tmux session in another terminal still wants `git hop add` typed at a
# plain shell to create the window, so "am I inside it" is the wrong
# question.
#
# Also NOT required: a server already running. `tmux new-session` starts
# one, and that is correct here rather than a surprise. Copying these
# scripts into a hook directory is an explicit, deliberate act that says
# "manage my worktrees as tmux windows" -- there is no way to install them
# by accident. Gating on a live server would make the create half of
# hop_tmux_ensure_session unreachable from cold: the first `git hop add`
# after a reboot would find no server, decline to start one, and silently
# produce no window, while the second one -- run after the user happened
# to open tmux by hand -- would work. A hook whose effect depends on
# whether some unrelated terminal is open is worse than either consistent
# behaviour.
#
# The read-only hooks stay cheap regardless: their existence checks fail
# against a dead server and they exit without creating anything, so a
# `git hop remove` never spawns a server just to find nothing to kill.
hop_tmux_available() {
    command -v tmux >/dev/null 2>&1 || return 1
    return 0
}

# hop_tmux_inside reports whether the calling process is itself inside a
# tmux pane. Used only to decide between select-window (we are inside, so
# switching is meaningful) and switch-client (attached elsewhere).
hop_tmux_inside() {
    [ -n "${TMUX:-}" ]
}

# --- naming transform --------------------------------------------------
#
# tmux target syntax gives three characters structural meaning:
#
#   ':'  separates session from window       (session:window)
#   '.'  separates window from pane          (window.pane)
#   '$@%' sigils for session/window/pane ids ($0, @1, %2)
#
# A '=' prefix on a target forces exact-name matching rather than the
# default prefix/fnmatch search, which neutralises ':' -- verified: a
# window literally named "a:b" is selectable as "=sess:=a:b". It does NOT
# neutralise '.': a window named "a.b" targeted as "=sess:=a.b" fails with
# "can't find window: a", because tmux splits on '.' before name matching
# ever happens. Session names behave the same way: a session named
# "has.dot" cannot be reached by any target at all.
#
# That is the binding constraint. Repo IDs are full of dots
# ("github.com/org/repo") and branch names are full of slashes
# ("feature/a"), so both need a transform.
#
# The transform is a readable stem plus a hash of the original:
#
#   stem:  '/' -> '+'    branch and repo path separator
#          '.' -> '_'    must go; unaddressable otherwise
#          ':' -> '+'    legal with '=' targets, but folded anyway so the
#                        encoding stays unambiguous to read
#   then:  '-' + 6 hex digits derived from the UNSANITIZED input
#
# The stem exists for legibility: "feature+a" is obviously feature/a at a
# glance, which matters when you are staring at a status bar working out
# which window is which. Both replacements are legal in tmux names and
# legal inside '=' targets, and both are rare in branch names.
#
# The suffix exists because the stem alone is not injective, and here that
# is a correctness bug rather than a cosmetic one. '/' and a literal '+'
# both fold to '+', so "feature/a" and "feature+a" produce an identical
# stem. post-worktree-remove kills a window BY NAME: under a colliding
# stem, removing one of those branches kills the other's window and every
# process running in it -- the dev server, the REPL, the editor with
# unsaved buffers. Silently, with no error, because from tmux's side the
# kill did exactly what it was told. "Such branch pairs are rare" is not a
# defence when the failure mode is destroying work the user never asked to
# close; rare and catastrophic still has to be impossible, and a suffix
# costs six characters.
#
# Distinct inputs get distinct names because the hash is taken over the
# original string, before any folding. Equal names therefore imply equal
# inputs up to a CRC-32 collision, which is not reachable by the handful of
# branch names one repository has.
#
# cksum is the hash because it is specified by POSIX and present
# everywhere. shasum/sha1sum/md5/md5sum are none of them universally
# available -- the names differ across macOS and Linux and any of them can
# be absent from a stripped PATH -- and a naming function that silently
# changes its answer depending on which tool it finds would break the one
# invariant this file exists to hold. The arithmetic and the hex formatting
# are shell builtins, so the only external command is cksum itself.
#
# If cksum is somehow missing, the stem is emitted alone. That degrades to
# the old colliding behaviour rather than producing names that disagree
# between two hooks on the same machine -- inconsistent names would break
# every hook, whereas the bare stem still addresses the right window in the
# overwhelming majority of cases.
hop_tmux_hash() {
    local sum rest
    command -v cksum >/dev/null 2>&1 || return 1
    read -r sum rest <<EOF
$(printf '%s' "$1" | cksum)
EOF
    [ -n "${sum:-}" ] || return 1
    printf '%06x' "$((sum % 16777216))"
}

hop_tmux_stem() {
    printf '%s' "$1" | tr '/:' '++' | tr '.' '_'
}

hop_tmux_sanitize() {
    local stem hash
    stem="$(hop_tmux_stem "$1")"
    if hash="$(hop_tmux_hash "$1")" && [ -n "$hash" ]; then
        printf '%s-%s' "$stem" "$hash"
    else
        printf '%s' "$stem"
    fi
}

# hop_tmux_session_name maps a repo ID to its session name.
#
# The "hop+" prefix namespaces these sessions so they are recognisable
# among a user's hand-made ones, and guarantees the name never starts with
# a character tmux might read as a sigil.
hop_tmux_session_name() {
    printf 'hop+%s' "$(hop_tmux_sanitize "$1")"
}

# hop_tmux_window_name maps a branch to its window name.
hop_tmux_window_name() {
    hop_tmux_sanitize "$1"
}

# --- targets -----------------------------------------------------------
#
# Always build targets through these, never by hand: forgetting a single
# '=' turns an exact lookup into a prefix search, and a prefix search
# silently matches the WRONG window whenever one branch name is a prefix
# of another (feature/a vs feature/ab). That failure is invisible until a
# user loses work in the wrong window.
hop_tmux_session_target() {
    printf '=%s' "$1"
}

hop_tmux_window_target() {
    printf '=%s:=%s' "$1" "$2"
}

# --- existence checks --------------------------------------------------
hop_tmux_has_session() {
    tmux has-session -t "$(hop_tmux_session_target "$1")" 2>/dev/null
}

hop_tmux_has_window() {
    tmux list-windows -t "$(hop_tmux_session_target "$1")" -F '#{window_name}' 2>/dev/null |
        grep -Fxq -- "$2"
}

# --- worktree identity -------------------------------------------------
#
# The window name is a DERIVED string; the worktree path is the fact. Even
# with a collision-free transform, a name can end up attached to the wrong
# window by means outside this code: the user renames a window by hand, a
# second tool creates one that happens to match, a stale window survives a
# worktree that was deleted outside git-hop. None of those are exotic, and
# the operation that pays for them is kill-window.
#
# So every window this library creates records the worktree it belongs to
# in a tmux user option, and destructive operations check it before acting.
# That turns "the name matches" into "this is provably the window for the
# worktree being removed" -- which is the question actually being asked.
#
# @hop-worktree is a window option (-w). tmux stores user options, which
# must begin with '@', verbatim and hands them back unchanged, so no
# escaping of the path is needed.
HOP_WORKTREE_OPTION='@hop-worktree'

hop_tmux_set_identity() {
    local target="$1" dir="$2"
    [ -n "$dir" ] || return 0
    tmux set-option -w -t "$target" "$HOP_WORKTREE_OPTION" "$dir" >/dev/null 2>&1
}

hop_tmux_get_identity() {
    tmux show-options -w -v -t "$1" "$HOP_WORKTREE_OPTION" 2>/dev/null
}

# hop_tmux_identity_matches answers "is it safe to act destructively on
# this window".
#
# Deliberately true when the window carries NO recorded identity. Windows
# predating this option, and windows the user made by hand and named to
# match, would otherwise become permanently unremovable -- a hook that
# refuses to clean up is its own bug. Absent means unknown, and unknown
# falls back to the name match that was the whole basis before. Only a
# RECORDED identity that disagrees is treated as proof of a different
# worktree, because only then is there something concrete to disagree with.
hop_tmux_identity_matches() {
    local target="$1" want="$2" got
    got="$(hop_tmux_get_identity "$target")"
    [ -n "$got" ] || return 0
    [ -n "$want" ] || return 0
    [ "$got" = "$want" ]
}

# --- ensure -----------------------------------------------------------
#
# Both ensure_* helpers are idempotent and tolerate the session or window
# having been killed by hand since the last hook ran. That is the normal
# case, not an edge case: users kill tmux windows constantly, and a hook
# that errored because its window was gone would be worse than no hook.
# hop_tmux_ensure_session creates the session if absent.
#
# A tmux session cannot exist with zero windows, so creating one always
# creates a window too. Left to itself tmux names that window after
# whatever it runs ("tmux", "bash", "zsh"), and since no worktree ever
# claims that name, it lingers in the session forever as a junk entry
# beside the real ones. So the placeholder is named explicitly, and every
# other helper treats HOP_PLACEHOLDER_WINDOW as free real estate to be
# renamed into the first real worktree window rather than added beside.
HOP_PLACEHOLDER_WINDOW='hop+scratch'

hop_tmux_ensure_session() {
    local session="$1" dir="$2"
    if hop_tmux_has_session "$session"; then
        return 0
    fi
    # -d so the new session is detached: a hook must never steal the
    # terminal out from under the command that triggered it.
    tmux new-session -d -s "$session" -n "$HOP_PLACEHOLDER_WINDOW" \
        -c "$dir" >/dev/null 2>&1
}

hop_tmux_ensure_window() {
    local session="$1" window="$2" dir="$3" target
    target="$(hop_tmux_window_target "$session" "$window")"

    # Create the session with the window already correctly named, rather
    # than creating it and then adding a window -- that would leave the
    # placeholder behind (see HOP_PLACEHOLDER_WINDOW).
    if ! hop_tmux_has_session "$session"; then
        tmux new-session -d -s "$session" -n "$window" -c "$dir" >/dev/null 2>&1 || return 1
        hop_tmux_set_identity "$target" "$dir"
        return 0
    fi

    if hop_tmux_has_window "$session" "$window"; then
        # Refresh the recorded identity on every pass. A window can outlive
        # the path it was created for -- `git hop move` renames it in place
        # and hands it a new path -- and a stale record would later block
        # the very removal it exists to make safe.
        hop_tmux_set_identity "$target" "$dir"
        return 0
    fi

    # Session exists but was created by post-clone, which had no branch to
    # name a window after. Claim the placeholder instead of adding beside
    # it, so the common clone-then-add sequence yields one window, not two.
    # Its cwd is already the repo path post-clone passed.
    if hop_tmux_has_window "$session" "$HOP_PLACEHOLDER_WINDOW"; then
        if tmux rename-window \
            -t "$(hop_tmux_window_target "$session" "$HOP_PLACEHOLDER_WINDOW")" \
            "$window" >/dev/null 2>&1; then
            hop_tmux_set_identity "$target" "$dir"
            return 0
        fi
    fi

    tmux new-window -d -t "$(hop_tmux_session_target "$session"):" \
        -n "$window" -c "$dir" >/dev/null 2>&1 || return 1
    hop_tmux_set_identity "$target" "$dir"
}
