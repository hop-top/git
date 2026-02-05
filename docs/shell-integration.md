# Shell Integration

git-hop includes optional shell integration that enables automatic directory switching when you hop between worktrees.

## Overview

Without shell integration, navigating to a worktree requires two steps:
```bash
git hop feature-branch    # Updates git-hop state
cd /path/to/worktree      # Manually navigate
```

With shell integration, it's a single command:
```bash
git-hop feature-branch    # Automatically cd to worktree
```

## Quick Start

### Installation

Install the shell wrapper function:

```bash
git hop install-shell-integration
```

This adds a shell function to your RC file (`~/.bashrc`, `~/.zshrc`, or `~/.config/fish/config.fish`).

**Supported shells:** bash, zsh, fish

After installation, restart your shell or run:
```bash
source ~/.bashrc  # or ~/.zshrc for zsh
```

### Usage

Once installed, use `git-hop` (note: **with hyphen**, not space) for automatic navigation:

```bash
git-hop feature-branch    # Automatically cd to worktree
git-hop main              # Switch back to main
git-hop add new-feature   # Create and navigate to new worktree
```

Regular `git hop` (with space) still works but won't change directories.

## How It Works

### The Current Symlink

git-hop maintains a `current` symlink in each repository hub that always points to the last worktree you navigated to:

```
my-repo/
├── .git/                  (bare repository)
├── hop.json
├── current -> hops/main   (symlink - always points to last hop)
└── hops/
    ├── main/
    ├── feature-a/
    └── feature-b/
```

Every time you run a git-hop command that navigates to a worktree, the `current` symlink updates:

```bash
git-hop feature-a   # current -> hops/feature-a
git-hop main        # current -> hops/main
git-hop add feat-b  # current -> hops/feat-b
```

### The Shell Wrapper

The shell integration installs a `git-hop()` function that:

1. Detects if the command should trigger navigation (add, clone, init, branch names)
2. Calls the real `git hop` binary
3. On success, automatically `cd` to the `current` symlink

```bash
# What the wrapper does:
git-hop feature-branch
# → runs: git hop feature-branch
# → updates: current -> hops/feature-branch
# → runs: cd $(git rev-parse --show-toplevel)/../current
```

## Commands

### install-shell-integration

Installs the shell wrapper function to your RC file.

```bash
git hop install-shell-integration
```

**What it does:**
- Detects your shell (bash/zsh/fish)
- Appends the wrapper function to your RC file
- Updates global config to track installation status
- Preserves existing RC file content

**Options:**
None. The command is fully automatic and idempotent (safe to run multiple times).

**Output:**
```
✓ Shell integration installed!

Installed to: /home/user/.bashrc
Shell: bash

Restart your shell or run: source /home/user/.bashrc

You can now use: git-hop <branch>
And it will automatically cd to the worktree.
```

### uninstall-shell-integration

Removes the shell wrapper function from your RC file.

```bash
git hop uninstall-shell-integration
```

**What it does:**
- Removes the wrapper function from your RC file
- Updates global config status to "declined"
- Preserves other content in your RC file

**After uninstalling:**
You'll need to manually navigate to worktrees:
```bash
git hop feature-branch
cd $(git rev-parse --show-toplevel)/../current
```

## Configuration

Shell integration status is tracked in your global config (`~/.config/git-hop/global.json`):

```json
{
  "shellIntegration": {
    "status": "approved",
    "installedShell": "bash",
    "installedPath": "/home/user/.bashrc",
    "installedAt": "2025-02-05T..."
  }
}
```

**Status values:**
- `unknown` - Never prompted (default for new installs)
- `approved` - User installed shell integration
- `declined` - User declined or uninstalled
- `disabled` - User used `--no-setup` flag (never prompt)

## Multiple Terminals

The `current` symlink is shared across all terminal sessions. The last `git-hop` command in any terminal determines where `current` points:

```bash
# Terminal 1
git-hop feature-a    # current -> hops/feature-a

# Terminal 2 (later)
git-hop feature-b    # current -> hops/feature-b

# Terminal 1 (current now points to feature-b)
cd ../current        # Goes to feature-b, not feature-a
```

**Note:** Each terminal maintains its own `$PWD` (current directory), so this only affects new navigation commands, not your existing shell sessions.

## Troubleshooting

### Shell wrapper not found

**Problem:** Running `git-hop` says "command not found"

**Solution:**
1. Verify installation: `git hop install-shell-integration`
2. Restart your shell: `exec $SHELL`
3. Check your RC file has the wrapper:
   ```bash
   grep "git-hop shell integration" ~/.bashrc
   ```

### Automatic cd not working

**Problem:** `git-hop <branch>` doesn't change directory

**Solutions:**

1. **Using wrong command:** Use `git-hop` (hyphen), not `git hop` (space)
   ```bash
   git-hop feature-branch  ✓ Correct
   git hop feature-branch  ✗ Won't auto-cd
   ```

2. **Not in a git-hop repository:**
   ```bash
   git hop init  # Initialize git-hop first
   ```

3. **Symlink doesn't exist:** The current symlink should be created automatically. Verify:
   ```bash
   ls -la $(git rev-parse --show-toplevel)/../current
   ```

### Wrapper installed twice

**Problem:** Wrapper function appears multiple times in RC file

**Solution:** The installation is idempotent (safe to run multiple times). If you see duplicates:
1. Manually remove duplicate entries from RC file
2. Or uninstall and reinstall:
   ```bash
   git hop uninstall-shell-integration
   git hop install-shell-integration
   ```

### CI/Scripts

**Problem:** Shell integration prompts/interferes in CI/scripts

**Solution:** The wrapper automatically detects non-interactive environments:
- Checks for `CI` environment variable
- Checks for `HOP_NO_SHELL_INTEGRATION` flag
- Only installs in interactive terminals

To explicitly disable:
```bash
export HOP_NO_SHELL_INTEGRATION=1
git hop <command>
```

## Advanced Usage

### Manual Navigation (without shell integration)

If you prefer not to use shell integration, you can still navigate manually:

```bash
git hop feature-branch
cd $(git rev-parse --show-toplevel)/../current
```

Or create your own alias:
```bash
alias hopc='git hop "$@" && cd $(git rev-parse --show-toplevel)/../current'
```

### Custom Shell Function

You can customize the wrapper function by editing your RC file. The installed function looks like:

```bash
# git-hop shell integration (installed by git-hop)
git-hop() {
    local should_cd=false
    local first_arg="$1"

    # Determine if this command should trigger cd
    case "$first_arg" in
        add|init|clone|''|[!-]*)
            should_cd=true
            ;;
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
```

**Customization ideas:**
- Change which commands trigger cd
- Add pre/post-hop hooks
- Customize error handling

### Fish Shell

For fish users, the wrapper uses fish syntax:

```fish
# git-hop shell integration (installed by git-hop)
function git-hop
    set -l should_cd false
    set -l first_arg $argv[1]

    switch "$first_arg"
        case add init clone '' '[!-]*'
            set should_cd true
        case list status doctor prune env --help -h --version -v
            set should_cd false
    end

    env HOP_WRAPPER_ACTIVE=1 command git hop $argv
    set -l exit_code $status

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
```

## FAQ

**Q: Do I need shell integration?**
A: No, it's optional. git-hop works perfectly fine without it, you'll just need to manually `cd` to worktrees.

**Q: What's the difference between `git hop` and `git-hop`?**
A:
- `git hop` (space) - The actual binary, doesn't change directories
- `git-hop` (hyphen) - The shell wrapper function, auto-cd enabled

**Q: Can I use both `git hop` and `git-hop`?**
A: Yes! Use `git-hop` when you want auto-cd, use `git hop` when you don't.

**Q: Does this work with oh-my-zsh/prezto/other frameworks?**
A: Yes, the wrapper function is compatible with all shell frameworks. It's just a regular function appended to your RC file.

**Q: Is the current symlink safe with git operations?**
A: Yes, git ignores symlinks in the root directory. The `current` symlink won't interfere with git operations.

**Q: What if I delete the current symlink?**
A: It will be recreated the next time you run a git-hop navigation command (hop, add, clone, init).

**Q: Does this work on Windows?**
A: The current symlink works on Windows with WSL. Native Windows (without WSL) has limited symlink support and is not currently tested.

## See Also

- [Main README](../README.md) - Full git-hop documentation
- [Configuration Guide](./configuration.md) - Global and local config options
- [Command Reference](./commands.md) - All git-hop commands
