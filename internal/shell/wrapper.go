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
const wrapperVersion = 2

// GenerateWrapperFunction generates a shell function wrapper for git-hop
// that enables automatic directory switching after successful commands
func GenerateWrapperFunction(shellType string) string {
	switch shellType {
	case "bash", "zsh":
		return generateBashZshWrapper()
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
func generateBashZshWrapper() string {
	return fmt.Sprintf(`
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

    # A post-worktree-switch hook exiting %d means it already moved the
    # user (a tmux window switch, say). The command SUCCEEDED, so report 0
    # to the user's shell -- but do not cd on top of the hook, or this
    # shell lands in the same worktree as the window the hook selected.
    if [[ $exit_code -eq %d ]]; then
        return 0
    fi

    # Only cd if successful and eligible
    if [[ $exit_code -eq 0 ]] && [[ "$should_cd" = true ]]; then
        local hub_root
        hub_root=$(git rev-parse --show-toplevel 2>/dev/null)

        if [[ -n "$hub_root" ]]; then
            # Try to find hub root (might be parent if we're in worktree)
            local current="$hub_root/../current"
            if [[ ! -e "$current" ]]; then
                current="$hub_root/current"
            fi

            if [[ -d "$current" ]]; then
                cd "$current" || true
            fi
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
`, versionedBeginMarker(), hooks.ExitNavigationHandled, hooks.ExitNavigationHandled, wrapperEndMarker)
}

// generateFishWrapper emits the fish integration block.
//
// Behaviourally identical to the bash/zsh block -- exit 0 cds, the
// handled-navigation directive returns 0 without cd'ing, anything else
// propagates untouched. Only the glob and conditional syntax differ.
func generateFishWrapper() string {
	return fmt.Sprintf(`
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

    # A post-worktree-switch hook exiting %d means it already moved the
    # user (a tmux window switch, say). The command SUCCEEDED, so report 0
    # to the user's shell -- but do not cd on top of the hook, or this
    # shell lands in the same worktree as the window the hook selected.
    if test $exit_code -eq %d
        return 0
    end

    # Only cd if successful and eligible
    if test $exit_code -eq 0; and test "$should_cd" = true
        set -l hub_root (git rev-parse --show-toplevel 2>/dev/null)

        if test -n "$hub_root"
            set -l current "$hub_root/../current"
            if not test -e "$current"
                set current "$hub_root/current"
            end

            if test -d "$current"
                cd "$current" 2>/dev/null; or true
            end
        end
    end

    return $exit_code
end

# git-hop tab completion
complete -c git-hop -f -a '(command git-hop __complete (commandline -cop) 2>/dev/null)'
%s
`, versionedBeginMarker(), hooks.ExitNavigationHandled, hooks.ExitNavigationHandled, wrapperEndMarker)
}
