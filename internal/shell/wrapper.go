package shell

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

func generateBashZshWrapper() string {
	return `
# git-hop shell integration (installed by git-hop)
git-hop() {
    local should_cd=false
    local first_arg="$1"

    # Determine if this command should trigger cd
    case "$first_arg" in
        # Branch names or commands that navigate
        add|init|clone|''|[!-]*)
            should_cd=true
            ;;
        # Read-only commands
        list|status|doctor|prune|env|--help|-h|--version|-v)
            should_cd=false
            ;;
    esac

    # Call the real binary with wrapper marker
    HOP_WRAPPER_ACTIVE=1 command git hop "$@"
    local exit_code=$?

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
`
}

func generateFishWrapper() string {
	return `
# git-hop shell integration (installed by git-hop)
function git-hop
    set -l should_cd false
    set -l first_arg $argv[1]

    # Determine if this command should trigger cd
    switch "$first_arg"
        case add init clone '' '[!-]*'
            set should_cd true
        case list status doctor prune env --help -h --version -v
            set should_cd false
    end

    # Call the real binary
    env HOP_WRAPPER_ACTIVE=1 command git hop $argv
    set -l exit_code $status

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
`
}
