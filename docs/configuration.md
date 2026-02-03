# Configuration

## Overview

git-hop uses a layered configuration system that separates user preferences (configuration) from repository tracking (state). This follows the XDG Base Directory specification for portability and organization.

## Directory Structure

git-hop uses different directories for different types of data:

### Configuration (User Preferences)

**Linux/Unix:**
```
~/.config/git-hop/
├── global.json        # Global settings
└── hooks/             # Global hooks
```

**macOS:**
```
~/Library/Preferences/git-hop/
├── global.json
└── hooks/
```

**Environment variable override:** `$XDG_CONFIG_HOME/git-hop/`

### Data (Repository Storage)

**Linux/Unix:**
```
~/.local/share/git-hop/
└── <org>/<repo>/
    ├── hop.json              # Hopspace config
    ├── deps/                 # Shared dependencies
    │   └── .registry.json    # Dependency tracking
    ├── hops/                 # Worktrees
    │   ├── main/
    │   └── feature-x/
    └── hooks/                # Hopspace-level hooks
```

**macOS:**
```
~/Library/Application Support/git-hop/
└── <org>/<repo>/
    ├── hop.json
    ├── deps/
    ├── hops/
    └── hooks/
```

**Environment variable override:** `$GIT_HOP_DATA_HOME`

### State (Tracking)

**Linux/Unix:**
```
~/.local/state/git-hop/
└── state.json         # Repository tracking
```

**macOS:**
```
~/Library/Application Support/git-hop/state/
└── state.json
```

**Environment variable override:** `$XDG_STATE_HOME/git-hop/`

### Cache (Temporary Data)

**Linux/Unix:**
```
~/.cache/git-hop/
```

**macOS:**
```
~/Library/Caches/git-hop/
```

**Environment variable override:** `$XDG_CACHE_HOME/git-hop/`

## Environment Variables

Override default directory locations:

| Variable | Description | Default (Linux/Unix) | Default (macOS) |
|----------|-------------|---------------------|-----------------|
| `GIT_HOP_CONFIG_HOME` | Configuration directory | `~/.config/git-hop` | `~/Library/Preferences/git-hop` |
| `GIT_HOP_DATA_HOME` | Data/repository storage | `~/.local/share/git-hop` | `~/Library/Application Support/git-hop` |
| `XDG_STATE_HOME` | State tracking | `~/.local/state` | `~/Library/Application Support` |
| `XDG_CACHE_HOME` | Cache directory | `~/.cache` | `~/Library/Caches` |
| `GIT_HOP_LOG_LEVEL` | Logging verbosity | `info` | `info` |

Example usage:

```bash
export GIT_HOP_DATA_HOME=/mnt/storage/git-hop
export GIT_HOP_LOG_LEVEL=debug
git hop clone https://github.com/org/repo.git
```

## Global Configuration

The global configuration file (`global.json`) stores user preferences that apply across all repositories.

### Location

- **Linux/Unix:** `~/.config/git-hop/global.json`
- **macOS:** `~/Library/Preferences/git-hop/global.json`
- **Custom:** `$XDG_CONFIG_HOME/git-hop/global.json`

### Schema

```json
{
  "defaults": {
    "autoEnvStart": false,
    "showAllManagedRepos": false,
    "unusedThresholdDays": 30,
    "bareRepo": true,
    "enforceCleanForConversion": true,
    "conventionWarning": true,
    "gitDomain": "github.com",
    "worktreeLocation": "hops"
  },
  "packageManagers": [
    {
      "name": "bun",
      "detectFiles": ["bun.lockb"],
      "lockFiles": ["bun.lockb"],
      "depsDir": "node_modules",
      "installCmd": ["bun", "install", "--frozen-lockfile"]
    }
  ],
  "backup": {
    "enabled": true,
    "keepBackup": true,
    "maxBackups": 5,
    "cleanupAgeDays": 90,
    "preserveStashes": true
  },
  "conversion": {
    "enforceClean": true,
    "allowDirtyForce": false,
    "autoRollback": true
  }
}
```

### Default Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `autoEnvStart` | boolean | `false` | Automatically start environment services when switching to a branch |
| `showAllManagedRepos` | boolean | `false` | Show all managed repositories in list command |
| `unusedThresholdDays` | number | `30` | Days before a worktree is considered unused |
| `bareRepo` | boolean | `true` | Use bare repository structure for new clones |
| `enforceCleanForConversion` | boolean | `true` | Require clean working directory for repo conversion |
| `conventionWarning` | boolean | `true` | Warn when worktree doesn't follow naming conventions |
| `gitDomain` | string | `"github.com"` | Default Git hosting domain |
| `worktreeLocation` | string | `"hops"` | Directory name for worktrees |

### Package Managers

Custom package managers can be defined to extend or override built-in support. See [Dependency Sharing](dependency-sharing.md) for details.

```json
{
  "packageManagers": [
    {
      "name": "custom-pm",
      "detectFiles": ["custom.lock"],
      "lockFiles": ["custom.lock"],
      "depsDir": "dependencies",
      "installCmd": ["custom-pm", "install"]
    }
  ]
}
```

### Backup Settings

Control automatic backup behavior during repository conversions:

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable automatic backups |
| `keepBackup` | boolean | `true` | Keep backup after successful conversion |
| `maxBackups` | number | `5` | Maximum number of backups to retain |
| `cleanupAgeDays` | number | `90` | Delete backups older than this many days |
| `preserveStashes` | boolean | `true` | Include stashes in backups |

### Conversion Settings

Control repository conversion behavior:

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `enforceClean` | boolean | `true` | Require clean working directory |
| `allowDirtyForce` | boolean | `false` | Allow `--force` to bypass clean check |
| `autoRollback` | boolean | `true` | Automatically rollback on conversion failure |

## Hopspace Configuration

Each repository has its own configuration stored in the hopspace directory.

### Location

`$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json`

Example: `~/.local/share/git-hop/github.com/myorg/myrepo/hop.json`

### Schema

```json
{
  "repo": {
    "uri": "https://github.com/org/repo.git",
    "org": "org",
    "repo": "repo",
    "defaultBranch": "main"
  },
  "branches": {
    "main": {
      "exists": true,
      "path": "/home/user/.local/share/git-hop/github.com/org/repo/hops/main",
      "lastSync": "2026-02-01T10:00:00Z"
    },
    "feature-x": {
      "exists": true,
      "path": "/home/user/.local/share/git-hop/github.com/org/repo/hops/feature-x",
      "lastSync": "2026-02-02T14:30:00Z"
    }
  },
  "forks": {}
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `repo.uri` | string | Remote repository URL |
| `repo.org` | string | Organization or user name |
| `repo.repo` | string | Repository name |
| `repo.defaultBranch` | string | Default branch (usually `main` or `master`) |
| `branches` | object | Map of branch names to branch metadata |
| `branches[].exists` | boolean | Whether the worktree exists on disk |
| `branches[].path` | string | Absolute path to the worktree |
| `branches[].lastSync` | string | ISO 8601 timestamp of last sync |
| `forks` | object | Fork repositories (for PR testing) |

## Hub Configuration

Each hub (workspace) has its own configuration.

### Location

`<hub-path>/hop.json`

Example: `~/projects/myrepo/hop.json`

### Schema

```json
{
  "repo": {
    "uri": "https://github.com/org/repo.git",
    "org": "org",
    "repo": "repo",
    "defaultBranch": "main"
  },
  "branches": {
    "main": {
      "path": "main",
      "hopspaceBranch": "main"
    },
    "feature-x": {
      "path": "feature-x",
      "hopspaceBranch": "feature-x"
    }
  },
  "settings": {
    "compareBranch": "main",
    "envPatterns": ["*.env", ".env.*"]
  },
  "migrated": true
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `branches` | object | Map of branch names to hub branch info |
| `branches[].path` | string | Full path to the worktree directory |
| `branches[].hopspaceBranch` | string | Corresponding branch name in hopspace |
| `branches[].fork` | string | Fork URI if this is a fork branch |
| `settings.compareBranch` | string | Default branch for comparisons |
| `settings.envPatterns` | array | Glob patterns for environment files |
| `migrated` | boolean | Whether this hub has been migrated to the registry system |

## State Tracking

The state file tracks all repositories and their locations across the system.

### Location

**Linux/Unix:** `~/.local/state/git-hop/state.json`
**macOS:** `~/Library/Application Support/git-hop/state/state.json`

### Schema

```json
{
  "version": "1.0.0",
  "lastUpdated": "2026-02-03T12:00:00Z",
  "repositories": {
    "github.com/org/repo": {
      "uri": "https://github.com/org/repo.git",
      "org": "org",
      "repo": "repo",
      "defaultBranch": "main",
      "worktrees": {
        "main": {
          "path": "/home/user/.local/share/git-hop/github.com/org/repo/hops/main",
          "type": "bare",
          "hubPath": "/home/user/projects/repo",
          "createdAt": "2026-01-15T09:00:00Z",
          "lastAccessed": "2026-02-03T11:30:00Z"
        }
      },
      "hubs": [
        {
          "path": "/home/user/projects/repo",
          "mode": "local",
          "createdAt": "2026-01-15T09:00:00Z",
          "lastAccessed": "2026-02-03T11:30:00Z"
        }
      ],
      "globalHopspace": {
        "enabled": true,
        "path": "/home/user/.local/share/git-hop/github.com/org/repo"
      }
    }
  },
  "orphaned": []
}
```

### Purpose

The state file enables:
- Fast hub discovery without scanning the filesystem
- Tracking of repository locations across the system
- Detection of orphaned worktrees and hubs
- Multi-hub support for the same repository

**Note:** This file is managed automatically by git-hop. Manual editing is not recommended.

## Dependency Registry

Tracks shared dependencies across worktrees. See [Dependency Sharing](dependency-sharing.md) for details.

### Location

`$GIT_HOP_DATA_HOME/<org>/<repo>/deps/.registry.json`

### Schema

```json
{
  "entries": {
    "node_modules.abc123": {
      "lockfileHash": "abc123",
      "lockfilePath": "package-lock.json",
      "usedBy": ["main", "feature-x"],
      "lastUsed": "2026-02-02T10:30:00Z",
      "installedAt": "2026-01-15T09:00:00Z"
    }
  }
}
```

## Ports and Volumes

Port and volume configurations are stored per repository for deterministic allocation.

### Ports Configuration

`$GIT_HOP_DATA_HOME/<org>/<repo>/ports.json`

```json
{
  "allocationMode": "hash-based",
  "baseRange": {
    "start": 10000,
    "end": 15000
  },
  "branches": {
    "main": {
      "ports": {
        "api": 10234,
        "db": 10235,
        "redis": 10236
      }
    }
  },
  "services": ["api", "db", "redis"]
}
```

### Volumes Configuration

`$GIT_HOP_DATA_HOME/<org>/<repo>/volumes.json`

```json
{
  "basePath": "/home/user/.local/share/git-hop/github.com/org/repo/volumes",
  "branches": {
    "main": {
      "volumes": {
        "postgres_data": "main_postgres_data",
        "redis_data": "main_redis_data"
      }
    }
  },
  "cleanup": {
    "orphaned": "manual",
    "unusedThresholdDays": 30
  }
}
```

## Configuration Hierarchy

When git-hop needs a setting, it searches in this order (first found wins):

1. Environment variables
2. Hub-level config (`<hub>/hop.json`)
3. Hopspace-level config (`$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json`)
4. Global config (`$XDG_CONFIG_HOME/git-hop/global.json`)
5. Built-in defaults

This allows you to:
- Set global defaults for all repositories
- Override for specific repositories (hopspace)
- Override for specific hubs (workspace-specific)
- Override for individual commands (environment variables)

## Best Practices

### 1. Version Control Separation

**DO commit:**
- Repo-level hooks (`.git-hop/hooks/`)
- Environment file patterns in hub settings
- Documentation about repository-specific configuration

**DO NOT commit:**
- Global config (`global.json`)
- State tracking (`state.json`)
- Dependency registry (`.registry.json`)
- Personal overrides

### 2. Portable Configuration

Global config can be synced across machines:

```bash
# Backup your config
cp ~/.config/git-hop/global.json ~/Dropbox/git-hop-config.json

# Restore on another machine
mkdir -p ~/.config/git-hop
cp ~/Dropbox/git-hop-config.json ~/.config/git-hop/global.json
```

### 3. Team Sharing

For team-wide conventions:

1. Document recommended global settings in your repository README
2. Use repo-level hooks (`.git-hop/hooks/`) for team-wide automation
3. Share Docker Compose files for consistent environments

### 4. Debugging Configuration

Check effective configuration:

```bash
# Show current settings
git hop config

# Show where settings come from
git hop config --verbose

# Show only specific setting
git hop config defaults.autoEnvStart
```

## Migration from Legacy Config

If you have an old `~/.config/git-hop/config.json`, migrate to the new structure:

```bash
git hop migrate
```

This will:
1. Read the legacy `config.json`
2. Create the new `global.json` with equivalent settings
3. Migrate repository tracking to `state.json`
4. Create the `.registry.json` for dependency tracking
5. Back up the old config files

## Troubleshooting

### Finding Configuration Files

```bash
# Show all config file locations
git hop config --paths

# Verify XDG directories
echo $XDG_CONFIG_HOME
echo $XDG_DATA_HOME
echo $XDG_STATE_HOME
echo $XDG_CACHE_HOME
```

### Resetting Configuration

To start fresh:

```bash
# Backup current config
cp ~/.config/git-hop/global.json ~/git-hop-config-backup.json

# Remove config (will use defaults)
rm ~/.config/git-hop/global.json

# Run git-hop - it will create new default config
git hop --version
```

### Invalid JSON

If you get JSON parsing errors:

```bash
# Validate your config file
jq . ~/.config/git-hop/global.json
```

Fix any syntax errors, or restore from backup.

## Implementation Details

For developers interested in the implementation:

- **Global config loader**: `internal/config/global.go`
- **State management**: `internal/state/state.go`
- **Hub config**: `internal/config/config.go` (`HubConfig`)
- **Hopspace config**: `internal/config/config.go` (`HopspaceConfig`)
- **XDG directory resolution**: `internal/state/state.go` (`GetStateHome()`)

The configuration system:
- Follows XDG Base Directory specification
- Uses JSON for human-readable config files
- Provides atomic writes for config updates
- Supports environment variable overrides
- Maintains backward compatibility with legacy config
