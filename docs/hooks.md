# Hooks System

## Overview

git-hop includes a flexible hooks system that allows you to run custom scripts at specific points in the worktree and environment lifecycle. Hooks can be configured at three different levels with a clear priority system.

## Available Hooks

| Hook Name | When It Runs |
|-----------|-------------|
| `pre-worktree-add` | Before creating a new worktree |
| `post-worktree-add` | After successfully creating a worktree |
| `pre-worktree-remove` | Before removing a worktree |
| `post-worktree-remove` | After successfully removing a worktree |
| `pre-env-start` | Before starting Docker/environment services |
| `post-env-start` | After successfully starting services |
| `pre-env-stop` | Before stopping environment services |
| `post-env-stop` | After successfully stopping services |

## Hook Priority System

When git-hop looks for a hook to execute, it searches in this order (first found wins):

1. **Repo-level override** — `.git-hop/hooks/<hook-name>` inside the worktree (the runner also walks parent directories so a hub-level `.git-hop/hooks/` is picked up)
2. **Hopspace-level hook** — `$XDG_DATA_HOME/git-hop/<host>/<org>/<repo>/hooks/<hook-name>` (only matches when the repoID has 3 slash-separated parts; see [Repository identifier](#repository-identifier))
3. **Global hook** — `$XDG_CONFIG_HOME/git-hop/hooks/<hook-name>`

This allows you to:
- Set global defaults for all repositories
- Override for specific repositories (hopspace)
- Override for specific worktrees (repo-level)

### Directory Locations by OS

git-hop resolves all paths through the XDG Base Directory specification. Linux and macOS share the same layout because the underlying `hop.top/kit/xdg` package follows XDG everywhere, including macOS — it does **not** use `~/Library/Preferences` or `~/Library/Application Support`.

**Linux and macOS:**

| Level | Default path |
|-------|--------------|
| Global | `~/.config/git-hop/hooks/` |
| Hopspace | `~/.local/share/git-hop/<host>/<org>/<repo>/hooks/` |
| Repo | `<worktree>/.git-hop/hooks/` |

Override with the standard XDG environment variables:

- `XDG_CONFIG_HOME` — relocates the global hooks dir (e.g. `$XDG_CONFIG_HOME/git-hop/hooks/`)
- `XDG_DATA_HOME` — relocates the hopspace base
- `GIT_HOP_DATA_HOME` — git-hop-specific override that wins over `XDG_DATA_HOME` for hopspace lookup

**Windows:**

The XDG kit maps to platform-native locations under the hood (typically `%APPDATA%` for config and `%LOCALAPPDATA%` for data). For the canonical resolution see `internal/hop/paths.go`. The repo-level path (`<worktree>/.git-hop/hooks/`) is the same on every platform.

### Repository identifier

The hopspace-level lookup keys off a 3-part repository identifier of the shape `<host>/<org>/<repo>` — for example `github.com/acme/widgets`. The runner splits the ID on `/` and only resolves a hopspace hook when there are at least three parts. So a hook for `github.com/acme/widgets` is looked up at:

```
~/.local/share/git-hop/github.com/acme/widgets/hooks/<hook-name>
```

A 2-part identifier such as `acme/widgets` **silently skips the hopspace lookup** — `FindHookFile` falls through to the global hook with no warning. For that reason, callers inside git-hop (e.g. `cmd/add.go`) always construct the repoID as `github.com/<org>/<repo>` so the hopspace lookup actually fires. If you are creating hopspace hooks by hand, mirror that 3-part shape on disk.

## Choosing a hook level

The three levels look interchangeable in the priority list, but they answer different questions. Pick the level that matches who needs the hook and when it must fire.

| Level | Storage | Versioned? | Best for |
|-------|---------|------------|----------|
| Repo | `<worktree>/.git-hop/hooks/` | Yes — committed in the repo | Team-shared hooks that travel with the codebase |
| Hopspace | `~/.local/share/git-hop/<host>/<org>/<repo>/hooks/` | No — local to your machine | Per-repo hooks that must fire on every `git hop add`, including the very first worktree |
| Global | `~/.config/git-hop/hooks/` | No — local to your machine | Defaults that apply to every repo on this machine unless overridden |

### The `post-worktree-add` chicken-and-egg trap

Repo-level hooks have a sharp edge for `post-worktree-add`. The hook file lives inside the worktree at `.git-hop/hooks/post-worktree-add`. When `git hop add` creates a fresh worktree from a branch that pre-dates the commit introducing the hook, that file is **not present** in the just-created worktree, so `FindHookFile` does not see it and the hook never fires. The first `git hop add` after introducing the hook silently skips it.

The runner does walk parent directories from the worktree path looking for a `.git-hop/hooks/` dir, so a hub-level repo hook can paper over the gap if you maintain one. But the canonical fix is to put the hook somewhere that does not depend on the worktree's content existing first — i.e. at the hopspace level.

### Recommendation

For any hook that **must** fire on every `git hop add` — bootstrap scripts, dependency installers, env-file copiers — install it at the hopspace level. The hopspace path is resolved from the repoID before the worktree is created, so it works on the very first `add` and on every `add` thereafter, regardless of which branch you start from.

For hooks that should travel with the repository so the whole team gets them, commit the canonical script at `<worktree>/.git-hop/hooks/<name>`. To get the best of both worlds, symlink the hopspace path to the committed file:

```bash
# One-time setup per machine, after cloning
mkdir -p ~/.local/share/git-hop/github.com/acme/widgets/hooks
ln -s "$(pwd)/.git-hop/hooks/post-worktree-add" \
  ~/.local/share/git-hop/github.com/acme/widgets/hooks/post-worktree-add
```

That way the committed hook is the single source of truth, and the hopspace symlink covers the bootstrap-time chicken-and-egg gap as well as the case where a teammate runs `git hop add <existing-old-branch>`.

> **Future work**: T-0217 will auto-mirror committed `.git-hop/hooks/` into the hopspace at clone/init time, removing the manual symlink step. Until that lands, the symlink pattern above is the recommended workaround.

## Creating Hooks

### 1. Global Hooks

Global hooks apply to all repositories unless overridden:

```bash
# Create hooks directory (same path on Linux and macOS)
mkdir -p ~/.config/git-hop/hooks

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

### Branch Type Detection Variables

When a branch type is detected (via git-flow-next or custom prefixes), these additional variables are available:

| Variable | Description | Example |
|----------|-------------|---------|
| `GIT_HOP_BRANCH_TYPE` | Detected branch type | `feature` |
| `GIT_HOP_BRANCH_NAME` | Branch name without prefix | `my-feature` |
| `GIT_HOP_BRANCH_PREFIX` | Matched prefix | `feature/` |
| `GIT_HOP_BRANCH_PARENT` | Parent branch for this type | `develop` |
| `GIT_HOP_BRANCH_START_POINT` | Branch to start from | `develop` |
| `GIT_HOP_DETECTOR_SOURCE` | Which detector matched | `gitflow-next` |

Example hook using these variables:

```bash
#!/bin/bash
echo "Hook: $GIT_HOP_HOOK_NAME"
echo "Repo: $GIT_HOP_REPO_ID"
echo "Branch: $GIT_HOP_BRANCH"
echo "Path: $GIT_HOP_WORKTREE_PATH"

# Branch type detection (if available)
if [ -n "$GIT_HOP_BRANCH_TYPE" ]; then
    echo "Branch Type: $GIT_HOP_BRANCH_TYPE"
    echo "Branch Name: $GIT_HOP_BRANCH_NAME"
    echo "Parent Branch: $GIT_HOP_BRANCH_PARENT"
    echo "Detected by: $GIT_HOP_DETECTOR_SOURCE"
fi

# Change to worktree directory
cd "$GIT_HOP_WORKTREE_PATH"

# Branch-specific logic
if [ "$GIT_HOP_BRANCH" = "main" ]; then
    echo "Running production setup..."
elif [ "$GIT_HOP_BRANCH_TYPE" = "feature" ]; then
    echo "Running feature setup..."
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

### 7. Git-Flow Integration

git-hop has **built-in integration** with [git-flow-next](https://github.com/gittower/git-flow-next) that automatically detects branch types and runs appropriate git-flow commands.

#### Built-in Detection

When you run `git hop add feature/my-feature`, git-hop:

1. **Detects the branch type** by reading your git-flow configuration
2. **Runs `git flow feature start my-feature`** automatically
3. **Creates the worktree**
4. **Sets environment variables** for hooks to use

Similarly, `git hop remove feature/my-feature` will run `git flow feature finish my-feature` before removing the worktree.

This works with **any branch types configured in git-flow-next**, including custom types:

```bash
# Configure a custom branch type in git-flow
git config gitflow.branch.bugfix.type topic
git config gitflow.branch.bugfix.parent develop
git config gitflow.branch.bugfix.prefix bugfix/

# git-hop automatically detects it
git hop add bugfix/fix-login  # Runs: git flow bugfix start fix-login
```

#### Environment Variables

When a branch type is detected, hooks receive these additional variables:

| Variable | Description |
|----------|-------------|
| `GIT_HOP_BRANCH_TYPE` | The detected branch type (feature, release, etc.) |
| `GIT_HOP_BRANCH_NAME` | Branch name without prefix |
| `GIT_HOP_BRANCH_PARENT` | Parent branch from git-flow config |
| `GIT_HOP_DETECTOR_SOURCE` | Which detector matched (`gitflow-next` or `generic`) |

#### Example: Extend Git-Flow Behavior

Hooks can extend the built-in git-flow integration:

```bash
#!/bin/bash
# post-worktree-add - Run tests after feature branch starts

# Only for feature branches
if [ "$GIT_HOP_BRANCH_TYPE" = "feature" ]; then
    cd "$GIT_HOP_WORKTREE_PATH"
    
    echo "Running initial tests for $GIT_HOP_BRANCH_NAME..."
    npm test
fi
```

#### Example: Custom Validation

```bash
#!/bin/bash
# pre-worktree-add - Validate branch names

# Use detected branch type info
if [ -n "$GIT_HOP_BRANCH_TYPE" ]; then
    echo "Detected $GIT_HOP_BRANCH_TYPE branch: $GIT_HOP_BRANCH_NAME"
    
    # Ensure release branches follow semver
    if [ "$GIT_HOP_BRANCH_TYPE" = "release" ]; then
        if [[ ! "$GIT_HOP_BRANCH_NAME" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            echo "Error: Release must use semver (e.g., v1.2.3)"
            exit 1
        fi
    fi
fi

exit 0
```

#### Workflow

| Command | Built-in Action | Git-Hop Action |
|---------|-----------------|----------------|
| `git hop add feature/my-feature` | `git flow feature start my-feature` | Creates worktree |
| `git hop remove feature/my-feature` | `git flow feature finish my-feature` | Removes worktree |
| `git hop add release/v1.0.0` | `git flow release start v1.0.0` | Creates worktree |
| `git hop remove release/v1.0.0` | `git flow release finish v1.0.0` | Removes worktree |

#### Manual Hook Integration (Optional)

If you need custom git-flow behavior not handled by the built-in detector, you can still use hooks:

```bash
#!/bin/bash
# pre-worktree-add - Custom git-flow logic

# Skip if git-flow-next already handled it
if [ "$GIT_HOP_DETECTOR_SOURCE" = "gitflow-next" ]; then
    exit 0  # Already handled by built-in detector
fi

# Your custom logic here
```

## Installing Hook Directories

The `.git-hop/hooks` directory is created automatically by `git hop init`.
To skip this, use `--no-hooks`:

```bash
git hop init           # creates .git-hop/hooks/ automatically
git hop init --no-hooks  # skip hook directory creation
```

Re-running `git hop init` on an already-initialized repo also ensures the
hooks directory exists (unless `--no-hooks` is passed).

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

## Known limitations

- **`git hop add --dry-run` still creates the worktree.** The flag does not currently preview the operation — the worktree, port allocation, and any `post-worktree-add` hooks all run as if `--dry-run` were not passed. Treat the flag as a no-op for now. Tracked separately.
- **Repo-level `post-worktree-add` does not fire on the bootstrap worktree** when the branch pre-dates the hook commit. See [Choosing a hook level](#choosing-a-hook-level) for the workaround and the planned fix in T-0217.

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
