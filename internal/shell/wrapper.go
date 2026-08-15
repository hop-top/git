package shell

import (
	"fmt"

	"hop.top/git/internal/hooks"
)

// wrapperVersion identifies the generation of the emitted shell block.
//
// The install path compares this against the version recorded in an
// already-installed block; a mismatch means the user is carrying an
// outdated wrapper and it gets rewritten. Bump it whenever the emitted
// shell changes behaviour, otherwise every existing installation keeps
// the old code forever while reporting itself correctly configured.
const wrapperVersion = 4

// CurrentPathCommand is the hidden subcommand the wrapper asks for its cd
// target. Hidden for the same reason NotifyChdirCommand is: it exists for
// the installed integration to call, never for a user to type.
const CurrentPathCommand = "__current-path"

// GenerateWrapperFunction generates a shell function wrapper for git-hop
// that enables automatic directory switching after successful commands
func GenerateWrapperFunction(shellType string) string {
	switch shellType {
	case "bash", "zsh":
		return generateBashZshWrapper(shellType)
	case "fish":
		return generateFishWrapper()
	default:
		return ""
	}
}

// generateBashZshWrapper emits the bash/zsh integration block.
//
// The block is delimited by wrapperBeginMarker / wrapperEndMarker so the
// uninstall path can excise it by exact bounds instead of guessing where
// the function ends -- brace-matching stopped at the wrapper function and
// orphaned the completion block that follows it.
//
// The wrapper function itself is identical for bash and zsh, but the chdir
// handler appended to it is not: zsh has a real chpwd hook while bash has
// to approximate one on PROMPT_COMMAND. Hence the shellType parameter.
func generateBashZshWrapper(shellType string) string {
	return fmt.Sprintf(`
%s
%s
git-hop() {
    local should_cd=false
    local first_arg="$1"

    # Determine if this command should trigger cd.
    #
    # Read-only verbs MUST come first: case takes the first matching arm,
    # and the catch-all below matches any non-flag word -- including
    # "list" and "status". With the arms the other way round the
    # read-only list was dead code and every read-only command cd'd.
    case "$first_arg" in
        # Read-only commands
        list|status|doctor|prune|env|--help|-h|--version|-v)
            should_cd=false
            ;;
        # Any other flag
        -*)
            should_cd=false
            ;;
        # Branch names or commands that navigate
        add|init|clone|''|*)
            should_cd=true
            ;;
    esac

    # Call the real binary with wrapper marker
    HOP_WRAPPER_ACTIVE=1 command git hop "$@"
    local exit_code=$?

    # The binary may have changed which worktrees exist (add, remove,
    # move, clone) and rewritten the roots cache. Re-slurp it so this
    # session's chdir handler sees the new set. Unconditional on purpose:
    # deciding whether this particular invocation mutated anything costs
    # more shell than just reading the file, and this runs at the rate the
    # user types "git hop", not at the rate their prompt draws.
    %s

    # A post-worktree-switch hook exiting %d means it already moved the
    # user (a tmux window switch, say). The command SUCCEEDED, so report 0
    # to the user's shell -- but do not cd on top of the hook, or this
    # shell lands in the same worktree as the window the hook selected.
    if [[ $exit_code -eq %d ]]; then
        return 0
    fi

    # Only cd if successful and eligible.
    #
    # The destination comes from the binary, not from path arithmetic here.
    # This block used to resolve it as "$(git rev-parse
    # --show-toplevel)/../current", falling back to ".../current", and both
    # candidates miss in the layout git-hop actually creates: inside a
    # worktree the toplevel IS the worktree, and the hub sits however many
    # levels above it the branch name nests -- one for "main", three for
    # "a/b/c" -- so no fixed number of ".." is right. Run from the bare hub
    # the toplevel does not exist at all, rev-parse exits 128, and the cd was
    # skipped without a word. The binary walks up for hop.json, which is the
    # answer at every depth and from the hub itself, and prints nothing when
    # there is no hub above the caller -- so an unrelated repository yields
    # an empty string and no cd.
    if [[ $exit_code -eq 0 ]] && [[ "$should_cd" = true ]]; then
        local current
        current=$(command git hop %s 2>/dev/null)

        if [[ -n "$current" ]] && [[ -d "$current" ]]; then
            cd "$current" || true
        fi
    fi

    return $exit_code
}

# git-hop tab completion
_git_hop() {
    local cur prev words cword
    _init_completion -n : || return

    local completions
    completions=$(command git-hop __complete "${words[@]:1}" 2>/dev/null)
    if [[ $? -eq 0 ]]; then
        COMPREPLY=($(compgen -W "$completions" -- "$cur"))
        __ltrim_colon_completions "$cur"
    fi
}
complete -o default -F _git_hop git-hop
%s
%s
`, versionedBeginMarker(), rootsReloadFor(shellType), reloadFunc,
		hooks.ExitNavigationHandled, hooks.ExitNavigationHandled,
		CurrentPathCommand,
		chdirHandlerFor(shellType), wrapperEndMarker)
}

// generateFishWrapper emits the fish integration block.
//
// Behaviourally identical to the bash/zsh block -- exit 0 cds, the
// handled-navigation directive returns 0 without cd'ing, anything else
// propagates untouched. Only the glob and conditional syntax differ.
func generateFishWrapper() string {
	return fmt.Sprintf(`
%s
%s
function git-hop
    set -l should_cd false
    set -l first_arg $argv[1]

    # Determine if this command should trigger cd.
    #
    # fish's switch matches with wildcards only -- it has no bracket
    # classes, so bash's '[!-]*' ("anything not starting with a dash")
    # matches nothing at all here and every branch name fell through
    # with should_cd unset. Same intent, expressed the way fish can
    # actually evaluate: read-only verbs first, then any flag, then
    # everything else is a branch to navigate to.
    switch "$first_arg"
        case list status doctor prune env --help -h --version -v
            set should_cd false
        case '-*'
            set should_cd false
        case add init clone '' '*'
            set should_cd true
    end

    # Call the real binary
    env HOP_WRAPPER_ACTIVE=1 command git hop $argv
    set -l exit_code $status

    # The binary may have changed which worktrees exist and rewritten the
    # roots cache. Re-slurp it so this session's chdir handler sees the new
    # set. See the bash/zsh block for why this lives here and not on the
    # prompt path.
    %s

    # A post-worktree-switch hook exiting %d means it already moved the
    # user (a tmux window switch, say). The command SUCCEEDED, so report 0
    # to the user's shell -- but do not cd on top of the hook, or this
    # shell lands in the same worktree as the window the hook selected.
    if test $exit_code -eq %d
        return 0
    end

    # Only cd if successful and eligible. The destination comes from the
    # binary rather than from ".." arithmetic here -- see the bash/zsh block
    # for why every path-relative candidate misses the real layout.
    if test $exit_code -eq 0; and test "$should_cd" = true
        set -l current (command git hop %s 2>/dev/null)

        if test -n "$current"; and test -d "$current"
            cd "$current" 2>/dev/null; or true
        end
    end

    return $exit_code
end

# git-hop tab completion
complete -c git-hop -f -a '(command git-hop __complete (commandline -cop) 2>/dev/null)'
%s
%s
`, versionedBeginMarker(), rootsReloadFor("fish"), reloadFunc,
		hooks.ExitNavigationHandled, hooks.ExitNavigationHandled,
		CurrentPathCommand,
		chdirHandlerFor("fish"), wrapperEndMarker)
}
