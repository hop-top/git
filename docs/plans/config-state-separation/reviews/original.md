# Configuration and State Separation Plan

## Overview

Separate git-hop's configuration (user preferences) from state (repository tracking) following XDG Base Directory specification.

## Goals

1. **Clean separation of concerns**: Configuration (what the user wants) vs State (what repos exist)
2. **XDG compliance**: Use proper directories for config, state, and data
3. **Portability**: Config can be shared/versioned; state is machine-specific
4. **Efficient hub discovery**: Find hubs without walking entire filesystem

## Directory Structure

```
$XDG_CONFIG_HOME/git-hop/
  ├── config.json          # User preferences and tool settings
  └── hooks/              # Global user hooks (portable)
      ├── pre-worktree-add
      ├── post-worktree-add
      ├── pre-env-start
      ├── post-env-start
      ├── pre-env-stop
      └── post-env-stop

$XDG_STATE_HOME/git-hop/
  └── state.json           # Repository state and hub locations

$XDG_DATA_HOME/git-hop/    # Global hopspaces and project hooks
  ├── {org}/
  │   └── {repo}/
  │       ├── hop.json         # Global hopspace config
  │       └── hooks/          # Project-specific hooks (machine-specific)
  │           ├── pre-worktree-add
  │           ├── post-worktree-add
  │           ├── pre-env-start
  │           ├── post-env-start
  │           ├── pre-env-stop
  │           └── post-env-stop
  └── volumes/
      └── {org}/
          └── {repo}/
              └── {branch}/
```

## Schema: `config.json`

**Location**: `$XDG_CONFIG_HOME/git-hop/config.json`

**Purpose**: Tool configuration - user preferences and global settings

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "git-hop global configuration - user preferences and tool settings",

  "defaults": {
    "gitDomain": "github.com",
    "bareRepo": true,
    "autoEnvStart": false,
    "editor": "${EDITOR}",
    "shell": "${SHELL}"
  },

  "output": {
    "format": "human",
    "colorScheme": "auto",
    "verbose": false,
    "quiet": false
  },

  "ports": {
    "allocationMode": "hash",
    "baseRange": {
      "start": 10000,
      "end": 15000
    }
  },

  "volumes": {
    "basePath": "${XDG_DATA_HOME}/git-hop/volumes",
    "cleanup": {
      "onRemove": true,
      "orphanedAfterDays": 30
    }
  },

  "hooks": {
    "preWorktreeAdd": null,
    "postWorktreeAdd": null,
    "preEnvStart": null,
    "postEnvStart": null,
    "preEnvStop": null,
    "postEnvStop": null
  },

  "doctor": {
    "autoFix": false,
    "checksEnabled": [
      "worktreeState",
      "configConsistency",
      "orphanedDirectories",
      "gitMetadata"
    ]
  }
}
```

### Fields

#### `defaults`
- **`gitDomain`**: Default Git hosting domain for shorthand notation (e.g., `org/repo` → `git@github.com:org/repo.git`)
- **`bareRepo`**: Whether to use bare repository structure by default for clones
- **`autoEnvStart`**: Automatically start Docker environments when entering worktrees
- **`editor`**: Preferred editor for interactive commands
- **`shell`**: Preferred shell for spawned processes

#### `output`
- **`format`**: Output format (`human`, `json`, `porcelain`)
- **`colorScheme`**: Color output (`auto`, `always`, `never`)
- **`verbose`**: Enable verbose logging
- **`quiet`**: Suppress non-essential output

#### `ports`
- **`allocationMode`**: How to allocate ports (`hash`, `sequential`, `random`)
- **`baseRange`**: Port range for allocation

#### `volumes`
- **`basePath`**: Root directory for Docker volumes
- **`cleanup.onRemove`**: Remove volumes when removing worktrees
- **`cleanup.orphanedAfterDays`**: Clean up orphaned volumes after N days

#### `hooks`
- Paths to hook scripts for lifecycle events
- `null` means no hook configured

#### `doctor`
- **`autoFix`**: Automatically fix issues when running `git hop doctor`
- **`checksEnabled`**: Which health checks to run

## Schema: `state.json`

**Location**: `$XDG_STATE_HOME/git-hop/state.json`

**Purpose**: Repository state - tracks repositories and their hub locations

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "git-hop state - tracks repositories and their locations",

  "version": "1.0.0",
  "lastUpdated": "2026-02-02T19:45:00Z",

  "repositories": {
    "github.com/jadb/git-hop": {
      "uri": "git@github.com:jadb/git-hop.git",
      "org": "jadb",
      "repo": "git-hop",
      "defaultBranch": "main",

      "worktrees": {
        "main": {
          "path": "/Users/jadb/code/git-hop",
          "type": "bare",
          "hubPath": "/Users/jadb/code/git-hop",
          "createdAt": "2026-01-15T10:30:00Z",
          "lastAccessed": "2026-02-02T19:45:00Z"
        },
        "feature-x": {
          "path": "/Users/jadb/code/git-hop/hops/feature-x",
          "type": "linked",
          "hubPath": "/Users/jadb/code/git-hop",
          "createdAt": "2026-01-20T14:15:00Z",
          "lastAccessed": "2026-02-02T18:20:00Z"
        }
      },

      "hubs": [
        {
          "path": "/Users/jadb/code/git-hop",
          "mode": "local",
          "createdAt": "2026-01-15T10:30:00Z",
          "lastAccessed": "2026-02-02T19:45:00Z"
        },
        {
          "path": "/Users/jadb/work/git-hop-fork",
          "mode": "local",
          "createdAt": "2026-02-01T14:20:00Z",
          "lastAccessed": "2026-02-02T15:30:00Z"
        }
      ],

      "globalHopspace": {
        "enabled": false,
        "path": null
      }
    },

    "github.com/user/project": {
      "uri": "git@github.com:user/project.git",
      "org": "user",
      "repo": "project",
      "defaultBranch": "main",

      "worktrees": {},

      "hubs": [],

      "globalHopspace": {
        "enabled": true,
        "path": "${XDG_DATA_HOME}/git-hop/user/project"
      }
    }
  },

  "orphaned": [
    {
      "path": "/tmp/old-project",
      "detectedAt": "2026-01-20T08:00:00Z",
      "reason": "hub directory no longer exists"
    }
  ]
}
```

### Fields

#### `version`
- Schema version for migration compatibility

#### `lastUpdated`
- Timestamp of last state modification

#### `repositories`
- Map of repository ID to repository state
- **Repository ID format**: `{domain}/{org}/{repo}` (e.g., `github.com/jadb/git-hop`)

##### Per-repository fields:
- **`uri`**: Full Git URI
- **`org`**: Organization/user name
- **`repo`**: Repository name
- **`defaultBranch`**: Default branch (usually `main` or `master`)
- **`worktrees`**: Map of branch name to worktree state
  - **`path`**: Absolute path to worktree directory
  - **`type`**: `"bare"` (main repo) or `"linked"` (worktree)
  - **`hubPath`**: Path to hub containing this worktree
  - **`createdAt`**: When this worktree was created
  - **`lastAccessed`**: When this worktree was last used
- **`hubs[]`**: Array of hub locations for this repository
  - **`path`**: Absolute path to hub directory
  - **`mode`**: `"local"` (hopspace in hub) or `"global"` (hopspace in XDG_DATA_HOME)
  - **`createdAt`**: When this hub was created
  - **`lastAccessed`**: When this hub was last used
- **`globalHopspace`**: Global hopspace configuration
  - **`enabled`**: Whether global hopspace is enabled for this repo
  - **`path`**: Path to global hopspace (if enabled)

#### `orphaned`
- Array of detected orphaned entries
- Cleaned up by `git hop prune` or `git hop doctor --fix`

## FindHub Logic

Updated `FindHub` algorithm:

```go
func FindHub(fs afero.Fs, g GitInterface, startPath string) (string, error) {
    // 1. Search up directory tree for .git
    gitDir := findGitDirectory(startPath)
    if gitDir == "" {
        return "", fmt.Errorf("not in a git repository")
    }

    // 2. Check same level for hop.json (local hub)
    gitParent := filepath.Dir(gitDir)
    if exists, _ := afero.Exists(fs, filepath.Join(gitParent, "hop.json")); exists {
        return gitParent, nil
    }

    // 3. Get repository ID from git remote or PWD
    repoID := getRepositoryID(g, gitParent, startPath)

    // 4. Look up in state.json
    state := loadState(fs)
    repoState, ok := state.Repositories[repoID]
    if !ok {
        return "", fmt.Errorf("repository not registered with git-hop: %s", repoID)
    }

    // 5. Find matching hub
    for _, hub := range repoState.Hubs {
        if isAncestor(startPath, hub.Path) {
            // Update lastAccessed
            hub.LastAccessed = time.Now()
            saveState(fs, state)
            return hub.Path, nil
        }
    }

    return "", fmt.Errorf("no hub found for repository %s", repoID)
}

func getRepositoryID(g GitInterface, gitDir, fallbackPath string) string {
    // Try git remote
    remote, err := g.GetRemoteURL(gitDir, "origin")
    if err == nil {
        return parseRepoID(remote) // github.com/org/repo
    }

    // Fallback: derive from directory structure
    // /path/to/org/repo -> github.com/org/repo (using default domain)
    parts := strings.Split(fallbackPath, string(filepath.Separator))
    if len(parts) >= 2 {
        org := parts[len(parts)-2]
        repo := parts[len(parts)-1]
        domain := getDefaultDomain() // from config.json
        return fmt.Sprintf("%s/%s/%s", domain, org, repo)
    }

    return ""
}

func VerifyWorktree(state *State, repoID, branch string, g GitInterface) (string, error) {
    // 1. Look up in state.json
    repoState, ok := state.Repositories[repoID]
    if !ok {
        return "", fmt.Errorf("repository not registered: %s", repoID)
    }

    worktree, ok := repoState.Worktrees[branch]
    if !ok {
        return "", fmt.Errorf("branch not registered: %s", branch)
    }

    // 2. Verify path exists
    if _, err := os.Stat(worktree.Path); err != nil {
        // Path missing, re-scan
        return rescanAndUpdateWorktree(state, repoID, branch, g)
    }

    // 3. Verify it's still a valid worktree
    hub := repoState.Hubs[0]
    worktrees, err := g.WorktreeList(hub.Path)
    if err != nil {
        return "", fmt.Errorf("failed to list worktrees: %w", err)
    }

    for _, wt := range worktrees {
        if wt.Branch == branch && wt.Path == worktree.Path {
            // Valid, update lastAccessed
            worktree.LastAccessed = time.Now()
            SaveState(state)
            return worktree.Path, nil
        }
    }

    // Invalid, re-scan
    return rescanAndUpdateWorktree(state, repoID, branch, g)
}

func rescanAndUpdateWorktree(state *State, repoID, branch string, g GitInterface) (string, error) {
    repoState, ok := state.Repositories[repoID]
    if !ok {
        return "", fmt.Errorf("repository not registered: %s", repoID)
    }

    hub := repoState.Hubs[0]
    worktrees, err := g.WorktreeList(hub.Path)
    if err != nil {
        return "", fmt.Errorf("failed to list worktrees: %w", err)
    }

    for _, wt := range worktrees {
        if wt.Branch == branch {
            // Found it, update state
            repoState.Worktrees[branch] = WorktreeState{
                Path:       wt.Path,
                Type:       "linked",
                HubPath:    hub.Path,
                CreatedAt:  time.Now(),
                LastAccessed: time.Now(),
            }
            SaveState(state)
            return wt.Path, nil
        }
    }

    return "", fmt.Errorf("worktree not found: %s", branch)
}
```

## Hooks Implementation

### Hook Classification

**Global Hooks** (`$XDG_CONFIG_HOME/git-hop/hooks/`) - CONFIG
- User preferences and behavior customization
- Portable across machines
- Example: Global notification script for all projects

**Project Hooks** (`$XDG_DATA_HOME/git-hop/{org}/{repo}/hooks/`) - DATA
- Project/machine-specific behavior
- Not meant to be portable
- Example: Project-specific docker-compose wrapper

### Available Hooks

#### Worktree Lifecycle
- `pre-worktree-add` / `post-worktree-add` - Called when creating/removing worktrees

#### Environment Lifecycle
- `pre-env-start` / `post-env-start` - Called when starting Docker environments
- `pre-env-stop` / `post-env-stop` - Called when stopping Docker environments

### Hook Priority System

When hooks are executed, git-hop checks locations in order:

1. **Git native hook** → `.git/hooks/<hook-name>` (for git's own hooks)
2. **git-hop wrapper** → Calls git-hop's hook dispatcher
3. **Repo-level override** → `.git-hop/hooks/<hook-name>` (worktree-specific)
4. **Hopspace-level hook** → `$XDG_DATA_HOME/git-hop/{org}/{repo}/hooks/<hook-name>`
5. **Global hook** → `$XDG_CONFIG_HOME/git-hop/hooks/<hook-name>`
6. **Built-in default** → git-hop's internal behavior

### Hook Wrapper Pattern

Instead of replacing git hooks, install wrapper scripts that dispatch to multiple handlers:

```bash
#!/usr/bin/env bash
# .git/hooks/pre-worktree-add (wrapper installed by git-hop)

set -e

# Run git-hop hook if available
if command -v git-hop &> /dev/null; then
    git-hop hook run pre-worktree-add "$@"
fi

exit $?
```

### Go Hook Runner

```go
func ExecuteHook(hookName string, worktreePath string, args ...string) error {
    // Find hook file by priority
    hookPath := findHookFile(hookName, worktreePath)

    if hookPath == "" {
        return nil // No hook found
    }

    // Prepare command with environment
    cmd := exec.Command(hookPath, args...)
    cmd.Dir = worktreePath
    cmd.Env = append(os.Environ(),
        "GIT_HOP_WORKTREE_PATH="+worktreePath,
        "GIT_HOP_HOOK_NAME="+hookName,
        "GIT_HOP_REPO_ID="+repoID,
    )

    // Run and capture output
    output, err := cmd.CombinedOutput()
    if err != nil {
        output.Error("Hook %s failed: %s", hookName, string(output))
        return fmt.Errorf("hook failed: %w", err)
    }

    if len(output) > 0 {
        output.Info("Hook output: %s", string(output))
    }

    return nil
}

func findHookFile(hookName string, worktreePath string) string {
    // Priority 1: Repo-level override (worktree-specific)
    repoHook := filepath.Join(worktreePath, ".git-hop", "hooks", hookName)
    if _, err := os.Stat(repoHook); err == nil {
        return repoHook
    }

    // Priority 2: Hopspace-level hook (project-specific)
    org, repo := getRepoFromPath(worktreePath)
    hopspaceHook := filepath.Join(GetDataHome(), "git-hop", org, repo, "hooks", hookName)
    if _, err := os.Stat(hopspaceHook); err == nil {
        return hopspaceHook
    }

    // Priority 3: Global hook (user preferences)
    globalHook := filepath.Join(GetConfigHome(), "git-hop", "hooks", hookName)
    if _, err := os.Stat(globalHook); err == nil {
        return globalHook
    }

    return ""
}
```

### Hook Best Practices

#### Shebang & Permissions
```bash
#!/usr/bin/env bash  # Portable across systems
# chmod +x required for all hook scripts
```

#### Error Handling
```bash
# Pre-* hooks: non-zero exit blocks operation
set -e  # Exit on error

# Post-* hooks: non-zero exit doesn't block, but logs warnings
set +e  # Continue on error
```

#### Environment Variables
```bash
# Available in hook scripts:
# - $GIT_HOP_WORKTREE_PATH  # Absolute path to worktree
# - $GIT_HOP_HOOK_NAME     # Name of hook being run
# - $GIT_HOP_REPO_ID       # Repository ID (org/repo)
# - $GIT_HOP_BRANCH         # Current branch (for env hooks)
```

#### Security
```bash
# Hook must be owned by user
if [ -n "$SUDO_USER" ]; then
    echo "Cannot run hooks as root" >&2
    exit 1
fi

# Hook must not be world-writable
if [ -O /path/to/hook ] && [ -w /path/to/hook ]; then
    chmod o-w /path/to/hook
fi
```

### Hook Installation

#### Auto-Install on Operations
- `git hop init` → Install hooks in newly created worktrees
- `git hop add` → Install hooks in new worktree
- `git hop clone` → Install hooks in cloned repository

#### Manual Installation
```bash
# User runs to install hooks in existing repos
git-hop install-hooks
```

### Hook Cleanup

When removing hooks or migrating:
```go
func UninstallHooks(worktreePath string) error {
    hooksDir := filepath.Join(worktreePath, ".git-hop", "hooks")

    // Remove only git-hop hooks (preserve user's hooks)
    gitHopHooks := []string{
        "pre-worktree-add",
        "post-worktree-add",
        "pre-env-start",
        "post-env-start",
        "pre-env-stop",
        "post-env-stop",
    }

    for _, hook := range gitHopHooks {
        hookPath := filepath.Join(hooksDir, hook)
        if isGitHopHook(hookPath) {
            os.Remove(hookPath)
        }
    }
}
```

### Config-Based Hook Control

Hooks can be enabled/disabled via `config.json`:

```json
{
  "doctor": {
    "autoFix": false,
    "checksEnabled": [
      "worktreeState",
      "configConsistency",
      "orphanedDirectories",
      "gitMetadata"
    ]
  }
}
```

Hooks check `checksEnabled` to determine if they should run:

```go
func isHookEnabled(hookName string, config *GlobalConfig) bool {
    for _, check := range config.Doctor.ChecksEnabled {
        if check == hookName {
            return true
        }
    }
    return false
}
```

### Integration Points

#### In `cmd/add.go` (after worktree creation)
```go
// Create worktree
worktreePath, err := wm.CreateWorktree(hopspace, hubPath, branch)
if err != nil {
    output.Fatal("Failed to create worktree: %v", err)
}

// Run pre-worktree-add hook
if err := ExecuteHook("pre-worktree-add", worktreePath); err != nil {
    output.Fatal("Pre-worktree-add hook failed: %v", err)
}

// Register in Hopspace
if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
    output.Fatal("Failed to register branch: %v", err)
}

// Run post-worktree-add hook
if err := ExecuteHook("post-worktree-add", worktreePath); err != nil {
    output.Warn("Post-worktree-add hook failed (continuing): %v", err)
}
```

#### In `cmd/env.go` (before/after docker commands)
```go
case "start":
    // Run pre-env-start hook
    if err := ExecuteHook("pre-env-start", root); err != nil {
        output.Fatal("Pre-env-start hook failed: %v", err)
    }

    output.Info("Starting services...")
    if err := d.ComposeUp(root, true); err != nil {
        output.Fatal("Failed to start services: %v", err)
    }

    // Run post-env-start hook
    if err := ExecuteHook("post-env-start", root); err != nil {
        output.Warn("Post-env-start hook failed (continuing): %v", err)
    }
    output.Info("Services started.")

case "stop":
    // Run pre-env-stop hook
    if err := ExecuteHook("pre-env-stop", root); err != nil {
        output.Fatal("Pre-env-stop hook failed: %v", err)
    }

    output.Info("Stopping services...")
    if err := d.ComposeStop(root); err != nil {
        output.Fatal("Failed to stop services: %v", err)
    }

    // Run post-env-stop hook
    if err := ExecuteHook("post-env-stop", root); err != nil {
        output.Warn("Post-env-stop hook failed (continuing): %v", err)
    }
    output.Info("Services stopped.")
```

### Git Native Hooks

Git's own hooks (pre-commit, pre-push, etc.) are stored in bare repo's `.git/hooks/` directory:
```
/path/to/repo/
└── .git/hooks/          # Git native hooks (shared across all worktrees)
    ├── pre-commit
    ├── pre-push
    └── post-receive
```

These are separate from git-hop custom hooks and continue to work as normal.

### Hook Directory Summary

| Scope | Location | Classification | Example Use Case |
|--------|----------|----------------|------------------|
| Global | `$XDG_CONFIG_HOME/git-hop/hooks/` | CONFIG | User-wide notifications |
| Project | `$XDG_DATA_HOME/git-hop/{org}/{repo}/hooks/` | DATA | Project-specific docker wrapper |
| Git Native | `.git/hooks/` | REPO DATA | Git's own pre-commit, pre-push |


## Migration Path

### Phase 1: Create new structures
1. Add `internal/config/global.go` for `config.json` handling
2. Add `internal/state/state.go` for `state.json` handling
3. Add `internal/hooks/runner.go` for hook execution logic
4. Implement XDG directory resolution

### Hook System Architecture

#### Hook Runner (`internal/hooks/runner.go`)
```go
package hooks

import (
    "os"
    "os/exec"
    "path/filepath"
)

type Runner struct {
    config *GlobalConfig
    fs     afero.Fs
}

func NewRunner(config *GlobalConfig, fs afero.Fs) *Runner {
    return &Runner{
        config: config,
        fs:     fs,
    }
}

// ExecuteHook runs a hook script by priority
func (r *Runner) ExecuteHook(hookName string, worktreePath string, args ...string) error {
    // Find hook file by priority
    hookPath := r.findHookFile(hookName, worktreePath)

    if hookPath == "" {
        return nil // No hook found
    }

    // Prepare command with environment
    cmd := exec.Command(hookPath, args...)
    cmd.Dir = worktreePath
    cmd.Env = append(os.Environ(),
        "GIT_HOP_WORKTREE_PATH="+worktreePath,
        "GIT_HOP_HOOK_NAME="+hookName,
    )

    // Run and capture output
    output, err := cmd.CombinedOutput()
    if err != nil {
        output.Error("Hook %s failed: %s", hookName, string(output))
        return fmt.Errorf("hook failed: %w", err)
    }

    if len(output) > 0 {
        output.Info("Hook output: %s", string(output))
    }

    return nil
}

func (r *Runner) findHookFile(hookName string, worktreePath string) string {
    // Priority 1: Repo-level override (worktree-specific)
    repoHook := filepath.Join(worktreePath, ".git-hop", "hooks", hookName)
    if _, err := afero.Exists(r.fs, repoHook); err == nil {
        return repoHook
    }

    // Priority 2: Hopspace-level hook (project-specific)
    org, repo := getRepoFromPath(worktreePath)
    hopspaceHook := filepath.Join(GetDataHome(), "git-hop", org, repo, "hooks", hookName)
    if _, err := afero.Exists(r.fs, hopspaceHook); err == nil {
        return hopspaceHook
    }

    // Priority 3: Global hook (user preferences)
    globalHook := filepath.Join(GetConfigHome(), "git-hop", "hooks", hookName)
    if _, err := afero.Exists(r.fs, globalHook); err == nil {
        return globalHook
    }

    return ""
}

// InstallHooks installs hook wrapper scripts in .git/hooks/
func (r *Runner) InstallHooks(worktreePath string) error {
    gitHooksDir := filepath.Join(worktreePath, ".git", "hooks")
    gitHopHooksDir := filepath.Join(worktreePath, ".git-hop", "hooks")

    // Create .git-hop/hooks directory
    if err := r.fs.MkdirAll(gitHopHooksDir, 0755); err != nil {
        return fmt.Errorf("failed to create hooks directory: %w", err)
    }

    // Install wrapper scripts in .git/hooks/
    hooks := []string{
        "pre-worktree-add",
        "post-worktree-add",
    }

    for _, hook := range hooks {
        gitHookPath := filepath.Join(gitHooksDir, hook)
        wrapperScript := fmt.Sprintf(`#!/usr/bin/env bash
set -e

# Run git-hop hook if available
if command -v git-hop &> /dev/null; then
    git-hop hook run %s "$@"
fi

exit $?
`, hook)

        if err := afero.WriteFile(r.fs, gitHookPath, []byte(wrapperScript), 0755); err != nil {
            return fmt.Errorf("failed to write hook wrapper: %w", err)
        }
    }

    return nil
}
```


### Phase 2: Update existing code
1. Migrate `LoadGlobalConfig` to use `$XDG_CONFIG_HOME/git-hop/config.json`
2. Replace registry.json with `state.json`
3. Update `FindHub` with new algorithm
4. Update `CloneWorktree` to register in state

### Phase 3: Migrate existing data
1. Create migration tool: `git hop migrate`
2. Read old registry.json
3. Convert to new state.json format
4. **Scan for worktrees in each hub using `git worktree list`**
5. Populate worktrees map with discovered worktrees
6. **Migrate existing hooks** from worktree `.git/hooks/` to new structure
7. Move old configs to new locations
8. Backup old files (don't delete)

```go
func MigrateHooks(oldRegistry Registry, fs afero.Fs) error {
    for repoID, repo := range oldRegistry.Repositories {
        hubPath := repo.Path

        // Scan for worktrees
        worktrees, err := g.WorktreeList(hubPath)
        if err != nil {
            continue
        }

        for _, wt := range worktrees {
            // Create new .git-hop/hooks directory
            newHooksDir := filepath.Join(wt.Path, ".git-hop", "hooks")
            if err := fs.MkdirAll(newHooksDir, 0755); err != nil {
                output.Warn("Failed to create hooks dir for %s: %v", wt.Path, err)
                continue
            }

            // Move existing hooks if any
            oldHooksDir := filepath.Join(wt.Path, ".git", "hooks")
            if exists, _ := afero.Exists(fs, oldHooksDir); exists {
                // Move git-hop specific hooks to .git-hop/hooks/
                gitHopHooks := []string{
                    "pre-worktree-add",
                    "post-worktree-add",
                }

                for _, hook := range gitHopHooks {
                    oldHookPath := filepath.Join(oldHooksDir, hook)
                    newHookPath := filepath.Join(newHooksDir, hook)
                    if err := fs.Rename(oldHookPath, newHookPath); err != nil {
                        output.Warn("Failed to migrate hook %s: %v", hook, err)
                    }
                }

                // Install wrapper scripts for remaining hooks
                // (InstallHooks() handles this)
            }
        }
    }
    return nil
}
```

```go
func MigrateWorktrees(oldRegistry Registry, newState *State, g GitInterface) error {
    for repoID, repo := range oldRegistry.Repositories {
        hubPath := repo.Path

        // Scan for all worktrees using git
        worktrees, err := g.WorktreeList(hubPath)
        if err != nil {
            output.Warn("Failed to scan worktrees for %s: %v", repoID, err)
            continue
        }

        // Add to new state
        newState.Repositories[repoID].Worktrees = make(map[string]WorktreeState)
        for _, wt := range worktrees {
            newState.Repositories[repoID].Worktrees[wt.Branch] = WorktreeState{
                Path:        wt.Path,
                Type:       determineType(wt),
                HubPath:     hubPath,
                CreatedAt:   time.Now(),
                LastAccessed: time.Now(),
            }
        }
    }
    return nil
}

func determineType(wt Worktree) string {
    // Bare repos don't have a parent worktree
    if wt.Parent == "" {
        return "bare"
    }
    return "linked"
}
```

### Phase 4: Update commands
1. `git hop clone` → register in state.json, install hooks
2. `git hop add` → use FindHub, register worktree in state.json, install hooks
3. `git hop remove` → cleanup worktree and hub entries in state.json
4. `git hop list` → read from state.json
5. `git hop org/repo:branch` → verify worktree and change directory
6. `git hop prune` → clean up orphaned entries in state.json
7. `git hop doctor` → validate state.json consistency
8. `git hop install-hooks` → install hooks in existing repositories

#### `git hop add` - Update state.json and install hooks
```go
// Create worktree
worktreePath, err := wm.CreateWorktree(hopspace, hubPath, branch)
if err != nil {
    output.Fatal("Failed to create worktree: %v", err)
}

// Run pre-worktree-add hook
if err := ExecuteHook("pre-worktree-add", worktreePath); err != nil {
    output.Fatal("Pre-worktree-add hook failed: %v", err)
}

// Register in Hopspace
if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
    output.Fatal("Failed to register branch in hopspace: %v", err)
}

// Add to Hub
if err := hub.AddBranch(branch, branch, worktreePath); err != nil {
    output.Fatal("Failed to add branch to hub: %v", err)
}

// Update state.json
repoState := state.Repositories[repoID]
repoState.Worktrees[branch] = WorktreeState{
    Path:        worktreePath,
    Type:        "linked",
    HubPath:     hubPath,
    CreatedAt:   time.Now(),
    LastAccessed: time.Now(),
}
SaveState(state)

// Install hooks in worktree
if err := InstallHooks(worktreePath); err != nil {
    output.Warn("Failed to install hooks: %v", err)
}

// Run post-worktree-add hook
if err := ExecuteHook("post-worktree-add", worktreePath); err != nil {
    output.Warn("Post-worktree-add hook failed (continuing): %v", err)
}
```

#### Hook Installation
```go
func InstallHooks(worktreePath string) error {
    gitHooksDir := filepath.Join(worktreePath, ".git", "hooks")
    gitHopHooksDir := filepath.Join(worktreePath, ".git-hop", "hooks")

    // Create .git-hop/hooks directory
    if err := os.MkdirAll(gitHopHooksDir, 0755); err != nil {
        return fmt.Errorf("failed to create hooks directory: %w", err)
    }

    // Install wrapper scripts in .git/hooks/
    hooks := []string{
        "pre-worktree-add",
        "post-worktree-add",
    }

    for _, hook := range hooks {
        gitHookPath := filepath.Join(gitHooksDir, hook)
        wrapperScript := fmt.Sprintf(`#!/usr/bin/env bash
set -e

# Run git-hop hook if available
if command -v git-hop &> /dev/null; then
    git-hop hook run %s "$@"
fi

exit $?
`, hook)

        if err := os.WriteFile(gitHookPath, []byte(wrapperScript), 0755); err != nil {
            return fmt.Errorf("failed to write hook wrapper: %w", err)
        }
    }

    return nil
}
```

#### `git hop remove` - Cleanup state.json and run hooks
```go
// Run pre-worktree-remove hook
if err := ExecuteHook("pre-worktree-remove", worktreePath); err != nil {
    output.Fatal("Pre-worktree-remove hook failed: %v", err)
}

// Remove worktree
if err := wm.RemoveWorktree(hopspace, branch); err != nil {
    output.Fatal("Failed to remove worktree: %v", err)
}

// After worktree removal
delete(state.Repositories[repoID].Worktrees, branch)

// Also remove from hub if this was main branch (bare repo)
if worktreeType == "bare" {
    removeHubFromState(state, repoID, hubPath)
}
SaveState(state)

// Run post-worktree-remove hook
if err := ExecuteHook("post-worktree-remove", worktreePath); err != nil {
    output.Warn("Post-worktree-remove hook failed (continuing): %v", err)
}
```

#### `git hop list` - Read from state.json
```go
for repoID, repo := range state.Repositories {
    for branch, wt := range repo.Worktrees {
        fmt.Printf("%s:%s → %s\n", repoID, branch, wt.Path)
    }
}
```

#### `git hop org/repo:branch` - New command pattern
```go
func HopGlobal(org, repo, branch string) error {
    state := LoadState()
    repoID := fmt.Sprintf("github.com/%s/%s", org, repo)

    // Verify and get worktree path
    path, err := VerifyWorktree(state, repoID, branch, g)
    if err != nil {
        return err
    }

    // Change directory
    os.Chdir(path)
    return nil
}
```

#### `git hop env start/stop` - Run hooks around Docker operations
```go
case "start":
    // Run pre-env-start hook
    if err := ExecuteHook("pre-env-start", root); err != nil {
        output.Fatal("Pre-env-start hook failed: %v", err)
    }

    output.Info("Starting services...")
    if err := d.ComposeUp(root, true); err != nil {
        output.Fatal("Failed to start services: %v", err)
    }

    // Run post-env-start hook
    if err := ExecuteHook("post-env-start", root); err != nil {
        output.Warn("Post-env-start hook failed (continuing): %v", err)
    }
    output.Info("Services started.")

case "stop":
    // Run pre-env-stop hook
    if err := ExecuteHook("pre-env-stop", root); err != nil {
        output.Fatal("Pre-env-stop hook failed: %v", err)
    }

    output.Info("Stopping services...")
    if err := d.ComposeStop(root); err != nil {
        output.Fatal("Failed to stop services: %v", err)
    }

    // Run post-env-stop hook
    if err := ExecuteHook("post-env-stop", root); err != nil {
        output.Warn("Post-env-stop hook failed (continuing): %v", err)
    }
    output.Info("Services stopped.")
```

#### `git hop install-hooks` - Manual hook installation
```go
var installHooksCmd = &cobra.Command{
    Use:   "install-hooks",
    Short: "Install git-hop hooks in current repository",
    Run: func(cmd *cobra.Command, args []string) {
        cwd, err := os.Getwd()
        if err != nil {
            output.Fatal("Failed to get current directory: %v", err)
        }

        // Find worktree root
        root, err := g.GetRoot(cwd)
        if err != nil {
            output.Fatal("Not in a git worktree: %v", err)
        }

        // Install hooks
        if err := InstallHooks(root); err != nil {
            output.Fatal("Failed to install hooks: %v", err)
        }

        output.Info("Hooks installed in: %s", root)
    },
}
``` 

## Critical Files

1. **internal/hooks/runner.go** (new) - Hook execution logic
2. **cmd/install-hooks.go** (new) - Manual hook installation command
3. **cmd/add.go** (modify) - Add hook execution around worktree operations
4. **cmd/remove.go** (modify) - Add hook execution around worktree removal
5. **cmd/env.go** (modify) - Add hook execution around Docker operations
6. **internal/hop/paths.go** (modify) - Add GetConfigHome(), GetDataHome() helpers



## Benefits

1. **Clear separation**: Config is portable, state is machine-specific
2. **XDG compliant**: Follows Linux/Unix conventions
3. **Better discovery**: Can find hubs without .git directory walk
4. **Multi-hub support**: Same repo can have multiple hub locations
5. **Orphan detection**: Track and clean up stale entries
6. **Future-proof**: Versioned state.json for schema evolution
7. **Extensibility**: Hooks allow user customization without modifying core code
8. **Hook portability**: Global hooks sync across machines, project hooks stay local
9. **Priority system**: Fine-grained control over hook behavior at multiple levels


## Compatibility

- Existing hubs continue to work (local mode)
- Migration tool preserves all data
- Old registry.json backed up, not deleted
- Gradual migration: commands updated incrementally

## Performance Considerations

- **Lookup**: O(1) for known branches from state.json
- **Verification**: Runs `git worktree list` only when path invalid
- **Caching**: Optional - cache `git worktree list` results for 5 minutes
- **Write overhead**: Minimal - only on add/remove operations

## Open Questions

1. How to handle repos with no remote (local-only)?
2. Should we track additional worktree statistics (usage, size)?
3. How aggressive should orphan cleanup be?
4. Should we add optional caching for `git worktree list` results?
5. What should be the behavior when verification fails multiple times?
6. Should hooks be installed automatically or require explicit `git hop install-hooks`?
7. How to handle hook versioning and migration when hook interface changes?
8. Should we provide hook templates or require users to write them from scratch?
