# Hooks System

## Overview

git-hop includes a flexible hooks system that allows you to run custom scripts at specific points in the worktree and environment lifecycle. Hooks can be configured at three different levels with a clear priority system.

## Available Hooks

| Hook Name | When It Runs |
|-----------|-------------|
| `pre-worktree-add` | Before creating a new worktree |
| `post-worktree-add` | After successfully creating a worktree |
| `pre-env-start` | Before starting Docker/environment services |
| `post-env-start` | After successfully starting services |
| `pre-env-stop` | Before stopping environment services |
| `post-env-stop` | After successfully stopping services |

## Hook Priority System

When git-hop looks for a hook to execute, it searches in this order (first found wins):

1. **Repo-level override** - `.git-hop/hooks/<hook-name>` in the worktree
2. **Hopspace-level hook** - `$XDG_DATA_HOME/git-hop/<org>/<repo>/hooks/<hook-name>`
3. **Global hook** - `$XDG_CONFIG_HOME/git-hop/hooks/<hook-name>`

This allows you to:
- Set global defaults for all repositories
- Override for specific repositories (hopspace)
- Override for specific worktrees (repo-level)

### Directory Locations by OS

**Linux/Unix:**
- Global: `~/.config/git-hop/hooks/`
- Hopspace: `~/.local/share/git-hop/<org>/<repo>/hooks/`
- Repo: `<worktree>/.git-hop/hooks/`

**macOS:**
- Global: `~/Library/Preferences/git-hop/hooks/`
- Hopspace: `~/Library/Application Support/git-hop/<org>/<repo>/hooks/`
- Repo: `<worktree>/.git-hop/hooks/`

## Creating Hooks

### 1. Global Hooks

Global hooks apply to all repositories unless overridden:

```bash
# Create hooks directory
mkdir -p ~/.config/git-hop/hooks  # Linux
mkdir -p ~/Library/Preferences/git-hop/hooks  # macOS

# Create a hook
cat > ~/.config/git-hop/hooks/post-env-start << 'EOF'
#!/bin/bash
echo "Environment started for $GIT_HOP_BRANCH in $GIT_HOP_WORKTREE_PATH"
EOF

# Make it executable
chmod +x ~/.config/git-hop/hooks/post-env-start
```

### 2. Hopspace Hooks

Hopspace hooks apply to a specific repository across all worktrees:

```bash
# Example for github.com/org/repo
mkdir -p ~/.local/share/git-hop/github.com/org/repo/hooks

cat > ~/.local/share/git-hop/github.com/org/repo/hooks/post-worktree-add << 'EOF'
#!/bin/bash
# Run database migrations after creating a new worktree
cd "$GIT_HOP_WORKTREE_PATH"
npm run db:migrate
EOF

chmod +x ~/.local/share/git-hop/github.com/org/repo/hooks/post-worktree-add
```

### 3. Repo-Level Overrides

Repo-level hooks are checked into version control and override all others:

```bash
# From within a worktree
mkdir -p .git-hop/hooks

cat > .git-hop/hooks/pre-env-start << 'EOF'
#!/bin/bash
# Load secrets before starting services
./scripts/load-secrets.sh
EOF

chmod +x .git-hop/hooks/pre-env-start

# Commit to version control
git add .git-hop/hooks/pre-env-start
git commit -m "Add pre-env-start hook"
```

**Note:** Repo-level hooks in `.git-hop/hooks/` can be committed to version control, making them available to all team members.

## Hook Environment Variables

All hooks receive these environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `GIT_HOP_HOOK_NAME` | Name of the hook being executed | `post-env-start` |
| `GIT_HOP_WORKTREE_PATH` | Absolute path to the worktree | `/home/user/projects/org/repo/feature-x` |
| `GIT_HOP_REPO_ID` | Repository identifier | `github.com/org/repo` |
| `GIT_HOP_BRANCH` | Branch name | `feature-x` |

Example hook using these variables:

```bash
#!/bin/bash
echo "Hook: $GIT_HOP_HOOK_NAME"
echo "Repo: $GIT_HOP_REPO_ID"
echo "Branch: $GIT_HOP_BRANCH"
echo "Path: $GIT_HOP_WORKTREE_PATH"

# Change to worktree directory
cd "$GIT_HOP_WORKTREE_PATH"

# Branch-specific logic
if [ "$GIT_HOP_BRANCH" = "main" ]; then
    echo "Running production setup..."
else
    echo "Running development setup..."
fi
```

## Hook Execution

### Success and Failure

- **Exit code 0**: Hook succeeded, operation continues
- **Non-zero exit code**: Hook failed, operation is aborted

Example blocking hook:

```bash
#!/bin/bash
# Block worktree creation if branch name doesn't follow convention

if [[ ! "$GIT_HOP_BRANCH" =~ ^(feature|bugfix|hotfix)/ ]]; then
    echo "Error: Branch name must start with feature/, bugfix/, or hotfix/"
    exit 1
fi

exit 0
```

### Hook Output

- `stdout` and `stderr` from hooks are displayed to the user
- Use this to provide feedback about what the hook is doing

### Execution Permissions

Hooks must be executable:

```bash
chmod +x path/to/hook
```

On Unix-like systems, git-hop verifies the executable bit before running a hook. On Windows, this check is skipped.

## Example Use Cases

### 1. Database Seeding

Automatically seed the database after environment starts:

```bash
#!/bin/bash
# post-env-start

cd "$GIT_HOP_WORKTREE_PATH"

echo "Waiting for database..."
sleep 2

echo "Seeding database for $GIT_HOP_BRANCH..."
npm run db:seed
```

### 2. Cleanup Before Stop

Clean up temporary files before stopping services:

```bash
#!/bin/bash
# pre-env-stop

cd "$GIT_HOP_WORKTREE_PATH"

echo "Cleaning up temporary files..."
rm -rf tmp/* logs/*.log
```

### 3. Environment-Specific Setup

Load different configurations per branch:

```bash
#!/bin/bash
# post-worktree-add

cd "$GIT_HOP_WORKTREE_PATH"

if [ "$GIT_HOP_BRANCH" = "main" ]; then
    cp .env.production .env
elif [ "$GIT_HOP_BRANCH" = "staging" ]; then
    cp .env.staging .env
else
    cp .env.development .env
fi

echo "Environment configured for $GIT_HOP_BRANCH"
```

### 4. Notification on Environment Start

Send a notification when services start:

```bash
#!/bin/bash
# post-env-start

# macOS notification
osascript -e "display notification \"Services started for $GIT_HOP_BRANCH\" with title \"git-hop\""

# Linux notification (requires notify-send)
# notify-send "git-hop" "Services started for $GIT_HOP_BRANCH"
```

### 5. Dependency Installation

Install dependencies after creating a worktree:

```bash
#!/bin/bash
# post-worktree-add

cd "$GIT_HOP_WORKTREE_PATH"

echo "Installing dependencies for $GIT_HOP_BRANCH..."

# Check for package.json
if [ -f package.json ]; then
    npm ci
fi

# Check for go.mod
if [ -f go.mod ]; then
    go mod download
fi

echo "Dependencies installed"
```

### 6. Branch Name Validation

Enforce branch naming conventions:

```bash
#!/bin/bash
# pre-worktree-add

VALID_PREFIXES="^(feature|bugfix|hotfix|release)/"

if [[ ! "$GIT_HOP_BRANCH" =~ $VALID_PREFIXES ]]; then
    echo "❌ Invalid branch name: $GIT_HOP_BRANCH"
    echo "Branch must start with: feature/, bugfix/, hotfix/, or release/"
    exit 1
fi

echo "✓ Branch name is valid"
exit 0
```

## Installing Hook Directories

To set up the `.git-hop/hooks` directory in a worktree:

```bash
git hop install-hooks
```

This creates the necessary directory structure for repo-level hook overrides.

## Debugging Hooks

### Verbose Output

Add debugging to your hooks:

```bash
#!/bin/bash
set -x  # Print each command before executing

echo "Starting hook: $GIT_HOP_HOOK_NAME"
# ... rest of hook
```

### Testing Hooks Manually

You can test hooks manually by setting the environment variables:

```bash
export GIT_HOP_HOOK_NAME="post-env-start"
export GIT_HOP_WORKTREE_PATH="/path/to/worktree"
export GIT_HOP_REPO_ID="github.com/org/repo"
export GIT_HOP_BRANCH="feature-x"

# Run the hook
~/.config/git-hop/hooks/post-env-start
```

### Common Issues

**Hook not executing:**
- Check that the hook file exists in one of the priority locations
- Verify the hook is executable: `ls -l path/to/hook`
- Ensure the hook name is spelled correctly
- Check for syntax errors in the script

**Permission denied:**
```bash
chmod +x path/to/hook
```

**Wrong hook directory:**
- Verify you're using the correct XDG directory for your OS
- Check `echo $XDG_CONFIG_HOME` and `echo $XDG_DATA_HOME`

## Security Considerations

### Repo-Level Hooks and Version Control

Repo-level hooks in `.git-hop/hooks/` can be committed to version control. This is convenient for sharing hooks with your team, but consider:

- **Code review:** Review hook scripts carefully before merging
- **Trust:** Only commit hooks from trusted sources
- **Permissions:** Users must explicitly make hooks executable on their machine

### Global and Hopspace Hooks

Global and hopspace hooks are stored locally and never committed to version control:

- Safe to include sensitive operations (API keys, credentials)
- Use environment variables for secrets, not hardcoded values
- Consider using dedicated secret management tools

## Implementation Details

For developers interested in the implementation:

- **Hook runner**: `internal/hooks/runner.go`
- **Hook validation**: `ValidateHookName()` function
- **Hook discovery**: `FindHookFile()` follows the priority system
- **Hook execution**: `ExecuteHook()` handles environment and execution
- **Installation**: `InstallHooks()` creates the `.git-hop/hooks` directory

The hooks system:
- Uses the standard Unix executable model
- Provides environment variables for context
- Follows XDG Base Directory specification
- Supports all scripting languages (bash, python, node, etc.)
