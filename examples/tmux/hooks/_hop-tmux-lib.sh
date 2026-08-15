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
# Every hook has to be a silent no-op for a user who does not run tmux.
# Two distinct conditions, both of which mean "do nothing":
#
#   1. tmux is not installed  -> nothing to talk to.
#   2. No server is running   -> `tmux new-session` would START one, and
#      spawning a background tmux server for someone who never asked for
#      it is exactly the surprise this must avoid.
#
# Note what is deliberately NOT required: $TMUX being set. $TMUX is only
# set for processes running INSIDE a tmux pane. A user with an attached
# tmux session in another terminal still wants `git hop add` typed at a
# plain shell to create the window. So the gate is "is there a server",
# not "am I inside it" -- checked by asking the server something harmless.
hop_tmux_available() {
    command -v tmux >/dev/null 2>&1 || return 1
    tmux list-sessions >/dev/null 2>&1 || return 1
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
# The transform:
#
#   '/' -> '+'    branch and repo path separator
#   '.' -> '_'    must go; unaddressable otherwise
#   ':' -> '+'    legal with '=' targets, but folded anyway so the
#                 encoding stays unambiguous to read
#
# Chosen because both replacements are (a) legal in tmux names, (b) legal
# in tmux targets under '=', and (c) rare in branch names, which keeps the
# result readable -- "feature+a" is obviously feature/a at a glance, which
# matters when you are staring at a status bar trying to work out which
# window is which. Not fully injective (a branch containing a literal '+'
# collides with one containing '/'), and that is an accepted trade: legible
# beats bijective for a name a human reads, and worktrees whose names
# differ only in that way do not occur in practice.
hop_tmux_sanitize() {
    printf '%s' "$1" | tr '/:' '++' | tr '.' '_'
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
    local session="$1" window="$2" dir="$3"

    # Create the session with the window already correctly named, rather
    # than creating it and then adding a window -- that would leave the
    # placeholder behind (see HOP_PLACEHOLDER_WINDOW).
    if ! hop_tmux_has_session "$session"; then
        tmux new-session -d -s "$session" -n "$window" -c "$dir" >/dev/null 2>&1
        return $?
    fi

    if hop_tmux_has_window "$session" "$window"; then
        return 0
    fi

    # Session exists but was created by post-clone, which had no branch to
    # name a window after. Claim the placeholder instead of adding beside
    # it, so the common clone-then-add sequence yields one window, not two.
    # Its cwd is already the repo path post-clone passed.
    if hop_tmux_has_window "$session" "$HOP_PLACEHOLDER_WINDOW"; then
        tmux rename-window \
            -t "$(hop_tmux_window_target "$session" "$HOP_PLACEHOLDER_WINDOW")" \
            "$window" >/dev/null 2>&1 && return 0
    fi

    tmux new-window -d -t "$(hop_tmux_session_target "$session"):" \
        -n "$window" -c "$dir" >/dev/null 2>&1
}
