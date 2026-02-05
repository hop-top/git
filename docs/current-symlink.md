# Current Symlink

The `current` symlink is a convenience feature that makes it easy to navigate to your most recently accessed worktree.

## What is it?

Every git-hop repository maintains a `current` symlink in the hub root that points to the last worktree you navigated to:

```
my-repo/
├── .git/                  (bare repository)
├── hop.json
├── current -> hops/main   (← symlink)
└── hops/
    ├── main/
    ├── feature-a/
    └── feature-b/
```

## When does it update?

The `current` symlink updates automatically whenever you run:

- `git hop <branch>` - Navigate to existing worktree
- `git hop add <branch>` - Create new worktree
- `git hop clone <uri>` - Clone repository
- `git hop init` - Initialize repository

## Why is it useful?

### 1. Quick Navigation

Instead of remembering paths:
```bash
cd /long/path/to/repo/hops/feature-branch
```

Use the symlink:
```bash
cd $(git rev-parse --show-toplevel)/../current
```

### 2. Shell Integration

Combined with [shell integration](./shell-integration.md), it enables automatic directory switching:

```bash
git-hop feature-branch  # Automatically: cd to current symlink
```

### 3. Scripts and Tools

Scripts can always reference the last-used worktree:

```bash
#!/bin/bash
# Deploy the currently active worktree
CURRENT_WORKTREE="$(git rev-parse --show-toplevel)/../current"
cd "$CURRENT_WORKTREE"
npm run deploy
```

### 4. Editor Integration

Configure your editor to open the current worktree:

```bash
# VS Code
code $(git rev-parse --show-toplevel)/../current

# Vim
vim $(git rev-parse --show-toplevel)/../current
```

## How it works

### Relative Paths

The symlink uses **relative paths** for portability:

```bash
# Good (relative)
current -> hops/feature-branch

# Bad (absolute - not portable)
current -> /home/user/repos/my-repo/hops/feature-branch
```

This means you can move the entire repository and the symlink still works.

### Branch Names with Slashes

Supports branch names with slashes:

```bash
git hop add feat/awesome-feature
# current -> hops/feat/awesome-feature
```

### Multiple Terminals

The `current` symlink is **shared** across all terminals:

```bash
# Terminal 1
git hop feature-a    # current -> hops/feature-a

# Terminal 2
git hop feature-b    # current -> hops/feature-b

# Back in Terminal 1
cd ../current        # Now points to feature-b (not feature-a)
```

Each terminal maintains its own `$PWD`, so this only affects new navigation.

## Manual Management

### Create Symlink

The symlink is created automatically, but you can create it manually:

```bash
cd /path/to/repo
ln -s hops/main current
```

### Update Symlink

To manually update (without using git-hop commands):

```bash
cd /path/to/repo
rm current
ln -s hops/other-branch current
```

### Remove Symlink

```bash
cd /path/to/repo
rm current
```

The symlink will be recreated next time you run a git-hop navigation command.

## Under the Hood

The symlink is managed by the `internal/hop/current.go` module:

```go
// Create or update the current symlink
hop.UpdateCurrentSymlink(fs, hubPath, worktreePath)

// Read where current points
target, err := hop.GetCurrentSymlink(fs, hubPath)

// Remove the symlink
hop.RemoveCurrentSymlink(fs, hubPath)
```

### Commands that update current

| Command | Updates current? |
|---------|------------------|
| `git hop <branch>` | ✅ Yes |
| `git hop add <branch>` | ✅ Yes |
| `git hop clone <uri>` | ✅ Yes |
| `git hop init` | ✅ Yes |
| `git hop list` | ❌ No |
| `git hop status` | ❌ No |
| `git hop remove` | ❌ No |
| `git hop prune` | ❌ No |
| `git hop doctor` | ❌ No |
| `git hop env` | ❌ No |

## Git Ignore

The `current` symlink is automatically ignored by git. It's a local navigation aid and doesn't need to be tracked.

If you see it in `git status`, add it to `.gitignore`:

```bash
echo "current" >> .gitignore
```

(This should not be necessary as git-hop manages this automatically)

## Limitations

### Windows Support

Symlinks work on:
- ✅ macOS
- ✅ Linux
- ✅ Windows (WSL)
- ⚠️ Windows (native) - Limited symlink support, requires developer mode or admin rights

### Network Filesystems

Some network filesystems don't support symlinks:
- ✅ NFS - Usually supported
- ⚠️ CIFS/SMB - May have issues
- ⚠️ VirtualBox shared folders - Often disabled

If symlinks don't work, git-hop will warn but continue to function. You'll just need to navigate manually.

## FAQ

**Q: What happens if I delete the symlink?**
A: It will be recreated the next time you run a git-hop navigation command.

**Q: Can I rename it to something else?**
A: The symlink name is hardcoded to `current`. If you rename it, git-hop will create a new one named `current`.

**Q: Does it affect git operations?**
A: No, git ignores symlinks in the repository root. It won't interfere with git commands.

**Q: What if two people share the same repository?**
A: Each person should have their own clone. The `current` symlink is personal to each clone.

**Q: Can I use it in scripts?**
A: Yes! It's designed for scripting:
```bash
cd "$(git rev-parse --show-toplevel)/../current"
```

**Q: What if the symlink points to a deleted worktree?**
A: The symlink becomes broken. git-hop will detect this and update it the next time you navigate.

## Examples

### Bash Alias

Create an alias to quickly navigate to current:

```bash
# Add to ~/.bashrc
alias hopc='cd $(git rev-parse --show-toplevel 2>/dev/null)/../current'

# Usage
hopc  # Jump to current worktree from anywhere in the repo
```

### Find Current Branch

```bash
# Get the branch name of the current worktree
cd $(git rev-parse --show-toplevel)/../current
git branch --show-current
```

### Deploy Script

```bash
#!/bin/bash
# deploy-current.sh - Deploy whatever worktree is currently active

set -e

# Navigate to hub root
HUB_ROOT=$(git rev-parse --show-toplevel)
cd "$HUB_ROOT/.."

# Check if current exists
if [[ ! -L "current" ]]; then
    echo "Error: No current worktree"
    exit 1
fi

# Navigate to current and deploy
cd current
echo "Deploying branch: $(git branch --show-current)"
npm run build
npm run deploy
```

## See Also

- [Shell Integration Guide](./shell-integration.md) - Automatic directory switching
- [Command Reference](./commands.md) - All git-hop commands
- [Configuration Guide](./configuration.md) - Customize git-hop behavior
