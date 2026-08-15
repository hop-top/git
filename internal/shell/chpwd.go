package shell

import (
	"fmt"
	"strings"
)

// The chdir handler runs on EVERY shell prompt. Everything below is
// written to that budget.
//
// The problem it solves: a plain `cd` into a registered worktree never
// reaches the git-hop binary, so an integration that tracks "which worktree
// is the user in" (a tmux session, say) desyncs the moment somebody
// navigates by hand. To such an integration a manual cd is the same event
// as `git hop <branch>`; it just has no way to hear about it.
//
// The problem that design creates: the handler fires on every prompt, and
// the overwhelming majority of cds are NOT into a worktree. Forking a
// process to answer "is this a worktree?" would tax every command the user
// runs -- and they would blame their shell, not git-hop. So the negative
// answer must be reachable with shell builtins only:
//
//  1. At shell startup, slurp the cached worktree paths into an array.
//     One file read per SESSION, not per prompt.
//  2. On each PWD change, string-compare $PWD against that array. Pure
//     parameter expansion: no stat, no subshell, no fork.
//  3. Only a match reaches the binary, via the hidden __notify-chdir
//     subcommand, which re-verifies against hop.json before firing
//     anything -- the shell's job is to be cheap, the binary's is to be
//     right.
//
// The obvious cheap test does not work, which is why the cache exists at
// all: worktrees do NOT live under the hopspace root. The hopspace is
// ~/.local/share/git-hop/, while a hub's worktrees sit next to the hub
// wherever the user put it. A single prefix test against a fixed root
// therefore misses essentially every real worktree. A marker-file probe
// ([[ -e $PWD/../../hop.json ]]) fails for a different reason: branch names
// nest to arbitrary depth ("feat/chpwd-switch" is two levels below hops/,
// "main" is one), so no fixed depth is correct and a walk-up loop pays a
// stat per parent on every prompt.

// Opt-in or unconditional?
//
// Unconditional within the wrapper install, with a runtime escape hatch.
//
// A separate consent prompt would be asking the same question twice. The
// wrapper the user already approved exists to make `git hop <branch>` move
// their shell; this makes a manual cd report the same event. Both serve one
// promise -- integrations know which worktree you are in -- and a user who
// wanted that for the hop path and not the cd path is a hypothesis with no
// evidence behind it. Splitting the consent would also leave the default
// install silently half-working for exactly the tmux case that motivated
// the feature.
//
// What does justify an escape hatch is the cost profile: this runs on every
// prompt, and someone debugging a slow shell needs to rule it out without
// editing their rc file. Hence disableEnvVar -- set it and the handler
// returns before doing anything, no reinstall required.
//
// The version bump is what carries this to existing installs: their block
// reads as stale and gets rewritten, so approving the wrapper once keeps
// meaning "keep this working" rather than freezing whichever generation
// happened to be current that day.
const (
	// disableEnvVar switches the handler off for a session without
	// touching the rc file. First thing checked, so an operator ruling out
	// a slow prompt pays one test rather than a reinstall.
	disableEnvVar = "GIT_HOP_NO_CHDIR_HOOK"

	// rootsVar holds the cached worktree paths for the session.
	rootsVar = "__git_hop_roots"

	// lastPwdVar caches the PWD the handler last acted on. bash has no
	// chpwd hook, so its PROMPT_COMMAND fires on every prompt including
	// ones where nothing moved; this is what makes the repeat case free.
	lastPwdVar = "__git_hop_last_pwd"

	// busyVar is the re-entrancy guard. A post-worktree-switch hook is
	// free to cd (that is much of the point of hooks), and in zsh/fish
	// that cd re-enters the very handler that invoked it -- unbounded
	// recursion, one hook run per level. The guard makes the handler a
	// no-op while it is already running.
	busyVar = "__git_hop_in_chdir"
)

// NotifyChdirCommand is the hidden subcommand the shell handler invokes on
// a cache hit. Hidden because this is plumbing: it exists for the installed
// integration to call, never for a user to type.
const NotifyChdirCommand = "__notify-chdir"

// generateBashChdirHandler emits the bash chdir integration.
//
// bash has no chpwd hook, so PROMPT_COMMAND is the only place to run this
// -- and PROMPT_COMMAND fires on every prompt, not only when the directory
// changed. The $PWD-vs-cached comparison at the top is therefore doing two
// jobs: it satisfies "a no-op cd must not re-fire", and it is what keeps
// the steady-state cost (same directory, prompt after prompt) down to one
// string compare.
//
// Written for bash 3.2, which is what macOS still ships: no associative
// arrays, no ${var,,}, no readarray.
func generateBashChdirHandler() string {
	return fmt.Sprintf(`
# Cached registered-worktree paths, read ONCE per shell session. Re-read
# only when git-hop itself runs (it rewrites the cache), which is rare
# compared to the prompt rate this handler runs at.
%s=()
if [[ -r "%s" ]]; then
    while IFS= read -r __git_hop_line; do
        [[ -n "$__git_hop_line" ]] && %s+=("$__git_hop_line")
    done < "%s"
    unset __git_hop_line
fi

%s="$PWD"

__git_hop_chdir() {
    # Escape hatch, checked first so ruling this out costs one test.
    [[ -n "$%s" ]] && return 0

    # Cheapest possible exit, in order of how often it is taken:
    #   1. nothing moved (every prompt where the user ran a command)
    #   2. we are inside our own hook (re-entrancy)
    #   3. $PWD is not under any worktree (nearly every actual cd)
    # None of these forks. Only a real hit gets past all three.
    [[ "$PWD" == "$%s" ]] && return 0
    [[ -n "$%s" ]] && return 0

    local __prev="$%s"
    %s="$PWD"

    [[ ${#%s[@]} -eq 0 ]] && return 0

    local __root __hit="" __prev_root=""
    for __root in "${%s[@]}"; do
        # Pure parameter expansion -- no stat, no subshell, no fork.
        if [[ "$PWD" == "$__root" || "$PWD" == "$__root"/* ]]; then
            # Longest match wins so nested worktrees resolve inward.
            if [[ ${#__root} -gt ${#__hit} ]]; then
                __hit="$__root"
            fi
        fi
    done

    [[ -z "$__hit" ]] && return 0

    # Same worktree as before (moved between its subdirectories): the
    # user did not switch worktrees, so there is nothing to report.
    if [[ -n "$__prev" ]]; then
        for __root in "${%s[@]}"; do
            if [[ "$__prev" == "$__root" || "$__prev" == "$__root"/* ]]; then
                if [[ ${#__root} -gt ${#__prev_root} ]]; then
                    __prev_root="$__root"
                fi
            fi
        done
        [[ "$__prev_root" == "$__hit" ]] && return 0
    fi

    # Confirmed transition into a different registered worktree. Now --
    # and only now -- is a fork justified.
    %s=1
    GIT_HOP_CHDIR_FROM="$__prev_root" command git hop %s "$__hit" >/dev/null 2>&1
    %s=""
    return 0
}

case "$PROMPT_COMMAND" in
    *__git_hop_chdir*) ;;
    "") PROMPT_COMMAND="__git_hop_chdir" ;;
    *) PROMPT_COMMAND="__git_hop_chdir;$PROMPT_COMMAND" ;;
esac
`,
		rootsVar, RootsCachePath(), rootsVar, RootsCachePath(),
		lastPwdVar,
		disableEnvVar,
		lastPwdVar,
		busyVar,
		lastPwdVar, lastPwdVar,
		rootsVar,
		rootsVar,
		rootsVar,
		busyVar, NotifyChdirCommand, busyVar,
	)
}

// generateZshChdirHandler emits the zsh chdir integration.
//
// zsh has a real chpwd hook, so unlike bash this only runs when the
// directory actually changed -- the per-prompt no-op case costs nothing at
// all. The prev-PWD bookkeeping is still needed, but to answer "which
// worktree did we come FROM", not to suppress repeats.
func generateZshChdirHandler() string {
	return fmt.Sprintf(`
# Cached registered-worktree paths, read ONCE per shell session.
%s=()
if [[ -r "%s" ]]; then
    while IFS= read -r __git_hop_line; do
        [[ -n "$__git_hop_line" ]] && %s+=("$__git_hop_line")
    done < "%s"
    unset __git_hop_line
fi

%s="$PWD"

__git_hop_chdir() {
    # Escape hatch, checked first so ruling this out costs one test.
    [[ -n "$%s" ]] && return 0

    # zsh fires chpwd only on a real directory change, so the repeat case
    # is free. Re-entrancy still needs guarding: a hook that cds lands
    # back here.
    [[ -n "$%s" ]] && return 0
    [[ "$PWD" == "$%s" ]] && return 0

    local __prev="$%s"
    %s="$PWD"

    (( ${#%s[@]} == 0 )) && return 0

    local __root __hit="" __prev_root=""
    for __root in "${%s[@]}"; do
        # Pure parameter expansion -- no stat, no subshell, no fork.
        if [[ "$PWD" == "$__root" || "$PWD" == "$__root"/* ]]; then
            if [[ ${#__root} -gt ${#__hit} ]]; then
                __hit="$__root"
            fi
        fi
    done

    [[ -z "$__hit" ]] && return 0

    if [[ -n "$__prev" ]]; then
        for __root in "${%s[@]}"; do
            if [[ "$__prev" == "$__root" || "$__prev" == "$__root"/* ]]; then
                if [[ ${#__root} -gt ${#__prev_root} ]]; then
                    __prev_root="$__root"
                fi
            fi
        done
        [[ "$__prev_root" == "$__hit" ]] && return 0
    fi

    %s=1
    GIT_HOP_CHDIR_FROM="$__prev_root" command git hop %s "$__hit" >/dev/null 2>&1
    %s=""
    return 0
}

autoload -U add-zsh-hook
add-zsh-hook chpwd __git_hop_chdir
`,
		rootsVar, RootsCachePath(), rootsVar, RootsCachePath(),
		lastPwdVar,
		disableEnvVar,
		busyVar,
		lastPwdVar,
		lastPwdVar, lastPwdVar,
		rootsVar,
		rootsVar,
		rootsVar,
		busyVar, NotifyChdirCommand, busyVar,
	)
}

// generateFishChdirHandler emits the fish chdir integration.
//
// fish signals directory changes as a variable event on PWD, which is the
// direct analogue of zsh's chpwd. `string match` and `test` are both fish
// builtins, so the miss path stays fork-free the same way the others do --
// worth stating explicitly because fish's `string` reads like an external
// command and is not one.
//
// fish's switch has no bracket classes and its `test` has no [[ ]], so the
// comparisons are spelled differently here even though the logic is
// identical to the bash/zsh blocks.
func generateFishChdirHandler() string {
	return fmt.Sprintf(`
# Cached registered-worktree paths, read ONCE per shell session.
set -g %s
if test -r "%s"
    while read -l __git_hop_line
        if test -n "$__git_hop_line"
            set -g -a %s $__git_hop_line
        end
    end < "%s"
end

set -g %s $PWD
set -g %s ""

function __git_hop_chdir --on-variable PWD
    # Escape hatch, checked first so ruling this out costs one test.
    if test -n "$%s"
        return 0
    end

    # Re-entrancy first: a hook that cds re-enters this handler.
    if test -n "$%s"
        return 0
    end
    if test "$PWD" = "$%s"
        return 0
    end

    set -l __prev $%s
    set -g %s $PWD

    if test (count $%s) -eq 0
        return 0
    end

    set -l __hit ""
    set -l __prev_root ""
    for __root in $%s
        # string and test are fish builtins -- no fork on the miss path.
        if test "$PWD" = "$__root"; or string match -q -- "$__root/*" "$PWD"
            if test (string length -- "$__root") -gt (string length -- "$__hit")
                set __hit "$__root"
            end
        end
    end

    if test -z "$__hit"
        return 0
    end

    if test -n "$__prev"
        for __root in $%s
            if test "$__prev" = "$__root"; or string match -q -- "$__root/*" "$__prev"
                if test (string length -- "$__root") -gt (string length -- "$__prev_root")
                    set __prev_root "$__root"
                end
            end
        end
        if test "$__prev_root" = "$__hit"
            return 0
        end
    end

    set -g %s 1
    GIT_HOP_CHDIR_FROM="$__prev_root" command git hop %s "$__hit" >/dev/null 2>&1
    set -g %s ""
    return 0
end
`,
		rootsVar, RootsCachePath(), rootsVar, RootsCachePath(),
		lastPwdVar,
		busyVar,
		disableEnvVar,
		busyVar,
		lastPwdVar,
		lastPwdVar, lastPwdVar,
		rootsVar,
		rootsVar,
		rootsVar,
		busyVar, NotifyChdirCommand, busyVar,
	)
}

// GenerateChdirHandler returns the chdir integration for a shell type, or
// "" for an unrecognised one.
func GenerateChdirHandler(shellType string) string {
	switch shellType {
	case "bash":
		return generateBashChdirHandler()
	case "zsh":
		return generateZshChdirHandler()
	case "fish":
		return generateFishChdirHandler()
	default:
		return ""
	}
}

// chdirHandlerFor is the generator the wrapper block embeds. Split out so
// the emitted block is assembled in one place and the handler stays
// independently testable.
func chdirHandlerFor(shellType string) string {
	return strings.TrimRight(GenerateChdirHandler(shellType), "\n")
}
