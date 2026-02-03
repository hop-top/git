# Custom Environment Managers

## Problem

Currently hardcoded to Docker Compose. Users may want:
- Podman, Docker alternatives
- Custom service orchestration (systemd, supervisord, etc.)
- Scripts that run on lifecycle hooks
- Different tools per project (mix Docker + custom scripts)

## Solution

Configurable environment managers via user config. Support built-in (Docker Compose) + custom managers
with lifecycle hooks.

## Architecture

### Storage Structure

```
$XDG_CONFIG_HOME/git-hop/global.json
└── environmentManagers: [...]

$GIT_HOP_DATA_HOME/org/repo/
├── hop.json
│   └── environmentManager: "docker-compose" (or custom name)
└── scripts/
    ├── env-start.sh
    ├── env-stop.sh
    └── env-health.sh
```

### Global Config Format

```json
{
  "defaults": { ... },
  "environmentManagers": [
    {
      "name": "podman-compose",
      "detectFiles": ["compose.yaml", "compose.yml", "docker-compose.yml"],
      "commands": {
        "start": ["podman-compose", "up", "-d"],
        "stop": ["podman-compose", "stop"],
        "health": ["podman-compose", "ps", "--format", "json"]
      },
      "hooks": {
        "preStart": ["scripts/env-prestart.sh"],
        "postStart": ["scripts/env-poststart.sh"],
        "preStop": ["scripts/env-prestop.sh"],
        "postStop": ["scripts/env-poststop.sh"]
      }
    },
    {
      "name": "systemd-services",
      "detectFiles": ["services.conf"],
      "commands": {
        "start": ["systemctl", "--user", "start", "myapp.target"],
        "stop": ["systemctl", "--user", "stop", "myapp.target"],
        "health": ["systemctl", "--user", "is-active", "myapp.target"]
      }
    },
    {
      "name": "custom-script",
      "detectFiles": ["scripts/env-start.sh"],
      "commands": {
        "start": ["bash", "scripts/env-start.sh"],
        "stop": ["bash", "scripts/env-stop.sh"],
        "health": ["bash", "scripts/env-health.sh"]
      },
      "hooks": {
        "preStart": ["scripts/load-secrets.sh"]
      }
    }
  ]
}
```

### Per-Repo Override

```json
// $GIT_HOP_DATA_HOME/org/repo/hop.json
{
  "repo": { ... },
  "settings": {
    "environmentManager": "podman-compose",  // Override default
    "environmentConfig": {
      "hooks": {
        "postStart": ["scripts/seed-db.sh", "scripts/warm-cache.sh"]
      }
    }
  }
}
```

## Environment Manager Support

### Built-in Managers

| Manager | Detect File(s) | Start Command | Stop Command |
|---------|---------------|---------------|--------------|
| docker-compose | compose.yaml, docker-compose.yml | docker compose up -d | docker compose stop |
| podman-compose | compose.yaml | podman-compose up -d | podman-compose stop |
| systemd | *.service files in project | systemctl --user start <target> | systemctl --user stop <target> |

### Detection Strategy

1. Check per-repo `hop.json` for explicit `environmentManager`
2. If not set, auto-detect based on `detectFiles` in order of priority
3. First match wins
4. If no match, env manager is "none" (skip env lifecycle)

### Command Types

Required:
- **start**: Start services
- **stop**: Stop services

Optional:
- **health**: Check if services running (used by `git hop doctor`)
- **restart**: Restart services (defaults to stop + start)
- **logs**: Show service logs (used by `git hop env logs`)

## Workflow

### On Env Start (`git hop env start`)

1. Detect environment manager for this worktree
2. Run **preStart** hooks (if defined)
3. Run **start** command
4. Wait for services (health check if defined)
5. Run **postStart** hooks (if defined)
6. Update registry env state

Example output:
```
Environment Manager: docker-compose
  → Running preStart hook: scripts/load-secrets.sh
  ✓ Secrets loaded
  → Starting services: docker compose up -d
  ✓ Services started (postgres, redis, api)
  → Running postStart hook: scripts/seed-db.sh
  ✓ Database seeded
```

### On Env Stop (`git hop env stop`)

1. Detect environment manager
2. Run **preStop** hooks (if defined)
3. Run **stop** command
4. Run **postStop** hooks (if defined)
5. Update registry env state

### On Doctor (`git hop doctor`)

1. For each worktree with env manager:
   - Run **health** command (if defined)
   - Report service status
   - Detect mismatches (registry says "up" but health check fails)

Example output:
```
Environment Status:
  ✓ main (docker-compose): all services running
  ✗ feature-x (docker-compose): postgres down, redis running
  ⚠ feature-y (custom-script): no health check available
```

## Hook System

### Hook Types

- **preStart**: Before starting services (load secrets, check prerequisites)
- **postStart**: After starting services (seed DB, warm cache, run migrations)
- **preStop**: Before stopping services (backup data, notify monitoring)
- **postStop**: After stopping services (cleanup temp files)

### Hook Execution

- Hooks are shell commands executed in worktree directory
- Exit code 0 = success, non-zero = fail and abort operation
- STDOUT/STDERR captured and shown to user
- Hooks run in order defined in config
- Environment variables available to hooks:
  - `HOP_WORKTREE_PATH`: Absolute path to worktree
  - `HOP_BRANCH`: Current branch name
  - `HOP_REPO_PATH`: Absolute path to repo data dir
  - `HOP_COMMAND`: The command being run (start, stop)

### Hook Sources

Hooks can be defined in:
1. **Global config** - applies to all repos using that manager
2. **Per-repo config** - overrides/extends global hooks
3. Execution order: global hooks first, then per-repo hooks

### Example Hook Script

```bash
#!/bin/bash
# scripts/env-prestart.sh

set -e

echo "Loading secrets from vault..."
vault kv get -field=db_password secret/myapp > .env.secrets

echo "Checking prerequisites..."
if ! command -v docker &> /dev/null; then
    echo "Error: Docker not installed"
    exit 1
fi

echo "Prerequisites OK"
```

## Implementation

### Task List

- [ ] Update `internal/config/config.go` - add EnvironmentManager config
- [ ] Create `internal/services/env_managers.go` - manager detection/definitions
- [ ] Create `internal/services/env_hooks.go` - hook execution
- [ ] Refactor `internal/docker/docker.go` - make it one of many managers
- [ ] Update `cmd/env.go` - use manager abstraction instead of docker directly
- [ ] Update `cmd/doctor.go` - add env health checks
- [ ] Add tests for built-in manager detection
- [ ] Add tests for custom manager config loading
- [ ] Add tests for hook execution (success, failure, timeout)
- [ ] Add tests for multi-manager repos
- [ ] Update docs

### Files to Create/Modify

#### Modify: internal/config/config.go

Add to `GlobalConfig`:

```go
type GlobalConfig struct {
    Defaults            DefaultSettings          `json:"defaults"`
    PackageManagers     []PackageManagerConfig   `json:"packageManagers,omitempty"`
    EnvironmentManagers []EnvManagerConfig       `json:"environmentManagers,omitempty"`
    Backup              BackupSettings           `json:"backup,omitempty"`
    Conversion          ConversionSettings       `json:"conversion,omitempty"`
}

type EnvManagerConfig struct {
    Name        string              `json:"name"`
    DetectFiles []string            `json:"detectFiles"`
    Commands    EnvCommands         `json:"commands"`
    Hooks       EnvHooks            `json:"hooks,omitempty"`
}

type EnvCommands struct {
    Start   []string `json:"start"`
    Stop    []string `json:"stop"`
    Health  []string `json:"health,omitempty"`
    Restart []string `json:"restart,omitempty"`
    Logs    []string `json:"logs,omitempty"`
}

type EnvHooks struct {
    PreStart  []string `json:"preStart,omitempty"`
    PostStart []string `json:"postStart,omitempty"`
    PreStop   []string `json:"preStop,omitempty"`
    PostStop  []string `json:"postStop,omitempty"`
}
```

Add to `HubSettings`:

```go
type HubSettings struct {
    CompareBranch      *string     `json:"compareBranch,omitempty"`
    EnvPatterns        []string    `json:"envPatterns"`
    EnvironmentManager *string     `json:"environmentManager,omitempty"`  // Override manager
    EnvironmentConfig  *EnvConfig  `json:"environmentConfig,omitempty"`   // Per-repo hooks
}

type EnvConfig struct {
    Hooks EnvHooks `json:"hooks,omitempty"`
}
```

#### Create: internal/services/env_managers.go

```go
type EnvManager struct {
    Name        string
    DetectFiles []string
    Commands    EnvCommands
    Hooks       EnvHooks
}

type EnvCommands struct {
    Start   []string
    Stop    []string
    Health  []string
    Restart []string
    Logs    []string
}

type EnvHooks struct {
    PreStart  []string
    PostStart []string
    PreStop   []string
    PostStop  []string
}

// Load built-in + custom managers from config
func LoadEnvManagers(globalConfig *config.GlobalConfig) ([]EnvManager, error)

// Detect which manager to use for worktree
func DetectEnvManager(worktreePath string, repoConfig *config.HubConfig,
                      availableManagers []EnvManager) (*EnvManager, error)

// Execute command with hooks
func (m *EnvManager) Start(worktreePath, branch string, repoConfig *config.HubConfig) error
func (m *EnvManager) Stop(worktreePath, branch string, repoConfig *config.HubConfig) error
func (m *EnvManager) Health(worktreePath string) (bool, error)

func (m *EnvManager) Validate() error  // Check required fields, cmd availability
```

#### Create: internal/services/env_hooks.go

```go
type HookContext struct {
    WorktreePath string
    Branch       string
    RepoPath     string
    Command      string  // "start", "stop"
}

func ExecuteHooks(hooks []string, ctx HookContext) error
func ExecuteHook(hook string, ctx HookContext) error
func buildHookEnv(ctx HookContext) []string  // Build env vars for hook
```

#### Refactor: internal/docker/docker.go

Keep existing implementation but make it conform to EnvManager interface.
The docker package becomes an implementation detail, not the only option.

#### Modify: cmd/env.go

```go
func runEnvCommand(action string) {
    // Load managers from config
    globalCfg := config.LoadGlobalConfig()
    managers := services.LoadEnvManagers(globalCfg)

    // Detect which manager to use
    hubCfg := config.LoadHubConfig(repoPath)
    manager, err := services.DetectEnvManager(worktreePath, hubCfg, managers)
    if err != nil {
        output.Fatal("Failed to detect environment manager: %v", err)
    }

    if manager == nil {
        output.Info("No environment manager detected, skipping")
        return
    }

    switch action {
    case "start":
        if err := manager.Start(worktreePath, branch, hubCfg); err != nil {
            output.Fatal("Failed to start: %v", err)
        }
    case "stop":
        if err := manager.Stop(worktreePath, branch, hubCfg); err != nil {
            output.Fatal("Failed to stop: %v", err)
        }
    }
}
```

## Edge Cases

### Missing Hook Script

Hook defined but script doesn't exist.
- Detect before execution
- Error: "Hook script not found: scripts/missing.sh"
- Suggest: "Check environmentConfig.hooks in hop.json"

### Hook Timeout

Hook runs too long (stuck, infinite loop).
- Default timeout: 5 minutes per hook
- Configurable: `hookTimeout: 300` (seconds)
- Kill process if timeout exceeded
- Show STDOUT/STDERR captured so far

### Hook Failure

Hook exits non-zero.
- Stop execution immediately
- Show hook output (STDOUT/STDERR)
- Don't run remaining hooks
- Don't run main command (start/stop)
- Exit with error

### Multiple Managers Detected

Multiple detect files match (e.g., both docker-compose.yml and systemd services).
- Use first match in priority order
- Priority = order in config (global first, then per-repo)
- User can override via `environmentManager` in hop.json

### No Manager Detected

No detect files found.
- `git hop env start` is no-op (skip silently)
- `git hop doctor` reports: "No environment manager detected"
- User can explicitly set `environmentManager: "none"` to suppress warnings

### Command Not Found

Manager configured but command not installed (e.g., podman not in PATH).
- Detect on first use
- Error: "podman-compose not found in PATH"
- Suggest: "Install podman-compose or configure different environmentManager"

## Benefits

- **Tool flexibility:** Use Docker, Podman, systemd, custom scripts
- **Hook system:** Run setup/teardown scripts automatically
- **Per-repo customization:** Override manager and hooks per project
- **Industry standards:** Support common orchestration tools out of box
- **Extensibility:** Add new managers without code changes

## Future Enhancements

- `git hop env logs` - show service logs
- `git hop env restart` - restart services
- Health check retry/backoff
- Parallel hook execution
- Hook dependencies (run X before Y)
- Template hooks (substitute variables)
