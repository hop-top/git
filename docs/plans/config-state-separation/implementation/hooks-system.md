# Hooks System

## Overview

Hooks allow customization of git-hop behavior without modifying core code. Follow priority system for fine-grained control.

## Classification

### Global Hooks (CONFIG)

Location: `$XDG_CONFIG_HOME/git-hop/hooks/`

Purpose: User preferences, portable across machines.

Use cases:
- User-wide notifications
- Global docker-compose wrappers
- Cross-project validation

### Project Hooks (DATA)

Location: `$XDG_DATA_HOME/git-hop/{org}/{repo}/hooks/`

Purpose: Project/machine-specific behavior.

Use cases:
- Project-specific environment setup
- Machine-specific tool paths
- Team-wide project conventions

## Available Hooks

### Worktree Lifecycle

- pre-worktree-add: Before worktree creation
- post-worktree-add: After worktree creation

### Environment Lifecycle

- pre-env-start: Before Docker start
- post-env-start: After Docker start
- pre-env-stop: Before Docker stop
- post-env-stop: After Docker stop

## Priority System

See [Hook Priority Flow](../diagrams/hook-priority-system.mmd)

1. Git native hook
   - .git/hooks/<hook-name>
   - Git's own hooks (pre-commit, pre-push)

2. Hop wrapper
   - Wrapper script in .git/hooks/
   - Dispatches to git-hop hooks

3. Repo override
   - worktree/.git-hop/hooks/<hook-name>
   - Worktree-specific override

4. Hopspace hook
   - $XDG_DATA_HOME/git-hop/{org}/{repo}/hooks/<hook-name>
   - Project-specific hook

5. Global hook
   - $XDG_CONFIG_HOME/git-hop/hooks/<hook-name>
   - User preference hook

6. Built-in default
   - git-hop internal behavior
   - Fallback when no hooks found

## Hook Runner Implementation

### ExecuteHook Function

```go
func (r *Runner) ExecuteHook(
    hookName string,
    worktreePath string,
    args ...string,
) error
```

**Location:** [internal/hooks/runner.go](../../../../internal/hooks/runner.go)

### findHookFile Function

```go
func (r *Runner) findHookFile(
    hookName string,
    worktreePath string,
) string
```

Search order:
1. Repo-level override
2. Hopspace-level hook
3. Global hook
4. Return empty if none found

## Environment Variables

Available in hook scripts:

- $GIT_HOP_WORKTREE_PATH: Absolute path to worktree
- $GIT_HOP_HOOK_NAME: Name of hook being run
- $GIT_HOP_REPO_ID: Repository ID (org/repo)
- $GIT_HOP_BRANCH: Current branch (env hooks)

## Best Practices

### Shebang & Permissions

```bash
#!/usr/bin/env bash
# chmod +x required
```

### Error Handling

Pre-hooks (block operations):
```bash
set -e
```

Post-hooks (log errors, don't block):
```bash
set +e
```

### Security

```bash
# Check not running as root
if [ -n "$SUDO_USER" ]; then
    echo "Cannot run hooks as root" >&2
    exit 1
fi

# Ensure not world-writable
chmod o-w /path/to/hook
```

## Hook Installation

### Auto-Install

Operations that auto-install:
- git hop init
- git hop add
- git hop clone

See [Command Integration](command-integration.md)

### Manual Install

Command: `git hop install-hooks`

Installs hooks in current worktree:
1. Create .git-hop/hooks/ directory
2. Install wrapper scripts in .git/hooks/
3. Copy existing hooks if found

## Hook Cleanup

See [Migration Guide](migration-guide.md) for hook migration details.

## Config-Based Control

Hooks check `doctor.checksEnabled` in config.json:

```json
{
  "doctor": {
    "checksEnabled": [
      "worktreeState",
      "configConsistency",
      "orphanedDirectories",
      "gitMetadata"
    ]
  }
}
```

Hook can be enabled/disabled per-check.

## Native Git Hooks

Git's own hooks stored in bare repo `.git/hooks/`:

- pre-commit
- pre-push
- post-receive

Separate from git-hop custom hooks.
Continue to work independently.

## Diagrams

- [Hook Priority System](../diagrams/hook-priority-system.mmd)
- [Directory Structure](../diagrams/directory-structure.mmd)

## Related

- [Worktree Verification](worktree-verification.md)
- [Command Integration](command-integration.md)
- [Config Schema](../schemas/config-json.md)
