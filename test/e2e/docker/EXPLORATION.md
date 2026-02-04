# Codebase Exploration Documentation

**Date:** 2026-02-04
**Purpose:** Document findings from comprehensive codebase exploration for implementing Docker integration tests

## Table of Contents

1. [Testing Infrastructure Overview](#testing-infrastructure-overview)
2. [Docker Implementation Details](#docker-implementation-details)
3. [Git-Hop Architecture](#git-hop-architecture)
4. [Key Files Reference](#key-files-reference)
5. [Implementation Patterns](#implementation-patterns)

---

## Testing Infrastructure Overview

### Test Organization

Git-hop uses a three-tier testing approach:

```
test/
├── e2e/                    # End-to-end tests (full system)
│   ├── utils.go           # Core test infrastructure
│   ├── commands_test.go   # Workflow tests
│   ├── e2e_test.go        # Docker isolation tests (smoke)
│   └── fixtures/          # Test fixtures
├── integration/            # Integration tests (in-memory)
└── mocks/                 # Mock implementations
```

### TestEnv Pattern

**Core Structure** (`test/e2e/utils.go:11-20`):
```go
type TestEnv struct {
    RootDir      string    // Temp directory root
    HubPath      string    // Path to hub
    DataHome     string    // GIT_HOP_DATA_HOME path
    BinPath      string    // Compiled binary path
    EnvVars      []string  // Environment variables
    BareRepoPath string    // Bare repo for testing
    SeedRepoPath string    // Seed repo for setup
}
```

**Setup Process** (`SetupTestEnv()` - utils.go:23-81):
1. Creates isolated temp directory
2. Compiles fresh git-hop binary for each test
3. Sets up isolated Git config (user, email, defaultBranch)
4. Configures Docker environment (DOCKER_CONFIG)
5. Isolates XDG directories (XDG_CONFIG_HOME, XDG_DATA_HOME)
6. Registers cleanup with `t.Cleanup()` for guaranteed teardown

**Key Characteristics:**
- **Full isolation**: Each test gets own filesystem, config, data directories
- **Automatic cleanup**: Uses `t.Cleanup()` for guaranteed teardown
- **Fresh binary**: Builds git-hop for each test (ensures no cross-contamination)
- **Real operations**: Tests actual git operations, not mocks

### Test Utilities

**Command Execution** (`utils.go:84-105`):
- `TestEnv.RunGitHop(t, dir, args...)` - Run git-hop with test environment
- `TestEnv.RunCommand(t, dir, name, args...)` - Run arbitrary commands
- Both capture stdout/stderr and auto-fail test on error
- Log commands for debugging

**File Operations** (`utils.go:138-154`):
- `CreateTempDir(t)` - Creates temp directories
- `WriteFile(t, path, content)` - Writes test files

### Existing Docker Tests (Current State)

**Location:** `test/e2e/e2e_test.go`

**Test:** `TestE2E_PortAndVolumeIsolation`

**Current Coverage:**
- ✅ Creates bare repo and hub structure
- ✅ Adds branches with docker-compose.yml
- ✅ Verifies .env file generation
- ✅ Verifies HOP_PORT_* and HOP_VOLUME_* variables exist
- ✅ Checks that different branches get different ports
- ✅ Runs `env start` and `env stop` commands

**Limitations:**
- ❌ No verification that containers actually start
- ❌ No health checks for services
- ❌ No HTTP endpoint accessibility testing
- ❌ No volume data persistence verification
- ❌ No network isolation testing
- ❌ No multi-service dependency testing

**Smoke Test Nature:**
These are "smoke tests" - they verify commands execute without error and files are created, but don't verify actual Docker behavior.

---

## Docker Implementation Details

### Architecture Overview

```
User Command
    ↓
cmd/add.go or cmd/env.go
    ↓
internal/docker/wrapper.go (Docker abstraction)
    ↓
internal/services/env.go (EnvManager)
    ↓
├─ internal/services/ports.go (PortAllocator)
├─ internal/services/volumes.go (VolumeManager)
└─ internal/docker/compose.go (Config parsing)
    ↓
.env file generation
    ↓
docker compose up/down/stop
```

### Docker Wrapper (`internal/docker/wrapper.go`)

**Purpose:** Abstracts `docker compose` CLI commands

**Key Components:**

1. **CommandRunner Interface** (lines 18-21):
```go
type CommandRunner interface {
    Run(cmd string, args ...string) (string, error)
    RunInDir(dir string, cmd string, args ...string) (string, error)
}
```
- Enables mocking for unit tests
- RealRunner implements using os/exec

2. **Docker Methods:**
- `ComposeUp(dir, detached)` - Start services (lines 63-78)
- `ComposeStop(dir)` - Stop services (lines 81-84)
- `ComposeDown(dir)` - Remove services (lines 87-90)
- `ComposePs(dir)` - List containers in JSON (lines 93-95)
- `IsAvailable()` - Check Docker is installed (lines 57-60)

**Important Details:**
- Explicitly loads `.env` file with `--env-file .env` (lines 67-70)
- All commands run in worktree directory context
- Returns stdout as string, stderr in error

### Docker Compose Parser (`internal/docker/compose.go`)

**Purpose:** Parse docker-compose.yml to extract services and volumes

**Methods:**

1. **GetConfig(dir)** (lines 18-30):
   - Runs `docker compose config` to get canonical configuration
   - Falls back to raw YAML parsing if command fails
   - Returns ServiceConfig struct with services and volumes

2. **GetServiceNames(dir)** (lines 58-69):
   - Extracts service names from config
   - Returns: `["web", "db"]`

3. **GetVolumeNames(dir)** (lines 72-83):
   - Extracts volume names from config
   - Returns: `["web_data", "db_data"]`

4. **HasDockerEnv(dir)** (lines 86-89):
   - Checks if directory has valid docker-compose config
   - Used to determine if Docker environment should be generated

**Fallback Strategy:**
- Primary: `docker compose config` (canonical, handles includes/extends)
- Fallback: Raw YAML parsing of docker-compose.yml/yaml files

### Environment Manager (`internal/services/env.go`)

**Purpose:** Orchestrates resource allocation and .env generation

**Generate() Method** (lines 32-69):
```go
func (m *EnvManager) Generate(branch, worktreePath string) (*config.BranchPorts, *config.BranchVolumes, error)
```

**Flow:**
1. Parse docker-compose.yml to get service names
2. Add new services to global config if not already tracked
3. Allocate ports for each service
4. Get volume names from compose file
5. Create volume directories
6. Write .env file with HOP_PORT_* and HOP_VOLUME_* variables

**writeEnvFile()** (lines 71-86):
- Generates .env format:
  ```bash
  # Generated by git-hop
  HOP_PORT_WEB=10000
  HOP_PORT_DB=10001
  HOP_VOLUME_WEB_DATA=/path/to/volumes/hop_feature-x_web_data
  HOP_VOLUME_DB_DATA=/path/to/volumes/hop_feature-x_db_data
  ```
- Variable naming: `HOP_PORT_{SERVICE}`, `HOP_VOLUME_{VOLUME}` (uppercase)

### Port Allocation (`internal/services/ports.go`)

**Two Allocation Modes:**

1. **Hash Mode (default)** (lines 51-81):
   - Uses CRC32 hash of branch name
   - Deterministic: same branch → same ports
   - Formula: `offset = hash % (rangeSize - blockSize + 1)`
   - Example: `feature-x` → hash → offset → port 11500

2. **Incremental Mode** (lines 28-49):
   - Finds highest used port
   - Allocates next sequential block
   - Ports change if branches are removed

**AllocatePorts(branch)** (lines 21-26):
- Returns: `map[string]int` (service → port)
- Example: `{"web": 10000, "db": 10001}`

**Configuration:**
Stored in `$GIT_HOP_DATA_HOME/<org>/<repo>/ports.json`:
```json
{
  "allocationMode": "hash",
  "baseRange": {"start": 10000, "end": 20000},
  "services": ["web", "db"],
  "branches": {
    "feature-x": {"ports": {"web": 10000, "db": 10001}}
  }
}
```

### Volume Management (`internal/services/volumes.go`)

**CreateVolumes(branch, volumeNames)** (lines 23-35):
- Creates dedicated directory for each volume
- Naming pattern: `hop_{branch}_{volumeName}`
- Example: `hop_feature-x_web_data`
- Physical location: `$GIT_HOP_DATA_HOME/<org>/<repo>/volumes/hop_feature-x_web_data/`

**Returns:** `map[string]string` (volume → path)

**Configuration:**
Stored in `$GIT_HOP_DATA_HOME/<org>/<repo>/volumes.json`:
```json
{
  "basePath": "/Users/user/.local/share/git-hop/org/repo/volumes",
  "branches": {
    "feature-x": {
      "volumes": {
        "web_data": "/path/to/hop_feature-x_web_data",
        "db_data": "/path/to/hop_feature-x_db_data"
      }
    }
  }
}
```

### Network Isolation

**Strategy:** Relies on Docker Compose's built-in project-based networking

- Docker Compose creates network named `{dirname}_default`
- Each worktree is in separate directory → separate network
- Example:
  - `/hub/hops/feature-x/` → Network: `feature-x_default`
  - `/hub/hops/feature-y/` → Network: `feature-y_default`

**No Explicit Management:**
- No network creation/cleanup code
- Docker wrapper runs in worktree directory context
- Docker Compose handles network isolation automatically

### Complete Lifecycle: Adding Branch with Docker

**Command:** `git hop add feature-x`

**Execution Flow:**
1. Create Git worktree
2. Register in hopspace and hub configs
3. Check for docker-compose.yml (`Docker.HasDockerEnv()`)
4. If exists:
   - Parse services: `GetServiceNames()` → `["web", "db"]`
   - Allocate ports: `PortAllocator.AllocatePorts()` → `{web: 10000, db: 10001}`
   - Get volumes: `GetVolumeNames()` → `["web_data", "db_data"]`
   - Create volumes: `VolumeManager.CreateVolumes()` → Create directories
   - Write .env: `writeEnvFile()` → Generate HOP_PORT_*, HOP_VOLUME_*
5. Save ports.json and volumes.json

**Starting Environment:** `git hop env start`

**Execution Flow:**
1. Find Git root and hub path
2. Get branch name from worktree path
3. Ensure dependencies (symlinks to shared node_modules, etc.)
4. Check docker-compose.yml exists
5. Execute: `docker compose --env-file .env up -d`
6. Docker Compose:
   - Reads .env file
   - Substitutes ${HOP_PORT_*} and ${HOP_VOLUME_*}
   - Creates network: `{branch}_default`
   - Starts containers with branch-specific ports/volumes

---

## Git-Hop Architecture

### Core Concepts

**Hub:**
- User's local working context
- Any directory with `hop.json`
- Contains `hops/` subdirectory with worktrees (if local storage)

**Hopspace:**
- Centralized storage: `$GIT_HOP_DATA_HOME/<org>/<repo>/`
- Tracks all worktrees for a repository
- Stores ports.json, volumes.json, deps/ directory
- Can contain worktrees if using centralized storage

**Worktree:**
- Git worktree linked to a branch
- Contains working files, .env, docker-compose.yml
- Can be local (hub/hops/) or centralized (hopspace/hops/)

### Data Model

**Hub Config** (`hub/hop.json`):
```json
{
  "repo": {"uri": "...", "org": "...", "repo": "..."},
  "branches": {
    "feature-x": {
      "path": "/path/to/worktree",
      "hopspaceBranch": "feature-x"
    }
  }
}
```

**Hopspace Config** (`$GIT_HOP_DATA_HOME/org/repo/hop.json`):
```json
{
  "repo": {"uri": "...", "org": "...", "repo": "..."},
  "branches": {
    "feature-x": {
      "exists": true,
      "path": "/path/to/worktree",
      "lastSync": "2024-01-01T00:00:00Z"
    }
  }
}
```

**Global State** (`$XDG_STATE_HOME/git-hop/state.json`):
- Tracks all repositories and worktrees system-wide
- Used by `git hop status --all`

### Worktree Location Patterns

**Default:** Centralized storage
- Pattern: `{dataHome}/{org}/{repo}/hops/{branch}`
- Example: `/Users/user/.local/share/git-hop/github.com/org/repo/hops/feature-x/`

**Local Storage:**
- Pattern: `hops/{branch}` (in global config)
- Example: `/path/to/hub/hops/feature-x/`

**Custom Patterns:**
- Variables: `{hubPath}`, `{branch}`, `{org}`, `{repo}`, `{dataHome}`
- Example: `../worktrees/{branch}`

### Filesystem Abstraction

**Uses:** `afero.Fs` throughout codebase
- Enables in-memory filesystem for unit tests
- Provides consistent interface for file operations
- Supports symlinks via `afero.Symlinker` interface

**Current Limitation:**
- MockGit is concrete struct, not interface
- Cannot directly test WorktreeManager with mocks
- Workaround: Integration tests with real Git operations

---

## Key Files Reference

### Testing Infrastructure

1. **`test/e2e/utils.go`** - Core E2E infrastructure
   - TestEnv struct and setup
   - Command execution utilities
   - File operations

2. **`test/e2e/commands_test.go`** - Full workflow test
   - Demonstrates complete git-hop usage
   - Tests add, list, status, env, remove commands
   - Shows subtest pattern

3. **`test/e2e/e2e_test.go`** - Docker isolation test (current)
   - Port and volume isolation verification
   - .env file validation
   - Smoke test example

### Docker Implementation

4. **`internal/docker/wrapper.go`** - Docker command abstraction
   - CommandRunner interface
   - ComposeUp, ComposeStop, ComposeDown, ComposePs
   - RealRunner implementation

5. **`internal/docker/compose.go`** - docker-compose.yml parsing
   - GetConfig, GetServiceNames, GetVolumeNames
   - Fallback to raw YAML parsing
   - HasDockerEnv check

6. **`internal/services/env.go`** - Environment orchestration
   - Generate() method - main entry point
   - writeEnvFile() - .env generation
   - Coordinates ports, volumes, Docker

7. **`internal/services/ports.go`** - Port allocation
   - Hash-based allocation (deterministic)
   - Incremental allocation
   - AllocatePorts() method

8. **`internal/services/volumes.go`** - Volume management
   - CreateVolumes() method
   - Volume directory creation
   - Naming convention: hop_{branch}_{volume}

### Configuration

9. **`internal/config/config.go`** - Data models
   - HubConfig, HopspaceConfig structures
   - PortsConfig, VolumesConfig
   - BranchPorts, BranchVolumes

### Commands

10. **`cmd/add.go`** - Branch addition workflow
    - Complete flow from worktree creation to env setup
    - Shows integration of all components

11. **`cmd/env.go`** - Environment commands
    - env start, env stop
    - Dependency management integration

---

## Implementation Patterns

### Test Pattern: Full Workflow

**Structure:**
```go
func TestFeature(t *testing.T) {
    env := SetupTestEnv(t)

    // Setup
    env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
    // ... seed repo, create branches

    // Initialize hub
    env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

    // Test with subtests
    t.Run("Subtest", func(t *testing.T) {
        out := env.RunGitHop(t, env.HubPath, "command", "args")
        // Assertions
    })
}
```

### Test Pattern: Docker Environment

**Current (Smoke Test):**
```go
func TestDockerIsolation(t *testing.T) {
    env := SetupTestEnv(t)

    // Setup repo and branches
    // ...

    // Add branches
    env.RunGitHop(t, env.HubPath, "add", "branch-a")

    // Verify .env generation
    content, _ := os.ReadFile(filepath.Join(branchPath, ".env"))
    if !strings.Contains(string(content), "HOP_PORT_WEB=") {
        t.Error("Missing HOP_PORT_WEB")
    }

    // Start environment
    env.RunGitHop(t, branchPath, "env", "start")

    // Currently: No container verification
}
```

**Needed (Integration Test):**
```go
func TestDockerIntegration_BasicStartup(t *testing.T) {
    SkipIfDockerNotAvailable(t)
    env := SetupTestEnv(t)

    // Setup...

    // Start services
    env.RunGitHop(t, branchPath, "env", "start")

    // Wait for health
    WaitForServiceHealthy(t, branchPath, "web", 30*time.Second)

    // Check HTTP endpoint
    CheckHTTPEndpoint(t, "http://localhost:10000", 200)

    // Stop services
    env.RunGitHop(t, branchPath, "env", "stop")

    // Verify stopped
    VerifyServiceStopped(t, branchPath, "web")
}
```

### Helper Pattern: Docker Verification

**Needed Helpers:**
```go
// Docker availability
func IsDockerAvailable(t *testing.T) bool
func SkipIfDockerNotAvailable(t *testing.T)

// Container management
func GetRunningContainers(t, projectName) []string
func WaitForServiceHealthy(t, dir, service, timeout)
func CleanupContainers(t, dir)

// Verification
func CheckHTTPEndpoint(t, url, expectedStatus)
func VerifyPortIsolation(t, port1, port2)
func VerifyVolumeIsolation(t, vol1, vol2)

// Data operations
func WriteDataToVolume(t, volumePath, filename, content)
func ReadDataFromVolume(t, volumePath, filename)
func ExecInContainer(t, containerName, command...)
```

### Cleanup Pattern

**Current:**
```go
env := SetupTestEnv(t)
// t.Cleanup() registered in SetupTestEnv
// Automatic cleanup of temp directory
```

**Needed for Docker:**
```go
t.Cleanup(func() {
    // Stop and remove containers
    CleanupContainers(t, branchPath)

    // Remove volumes
    CleanupVolumes(t, branchPath)

    // Prune networks
    CleanupNetworks(t, branchPath)
})
```

### Fixture Pattern

**Docker Compose Fixture:**
```yaml
# test/e2e/fixtures/docker-compose-simple.yml
version: '3'
services:
  web:
    image: nginx:alpine
    ports:
      - "${HOP_PORT_WEB}:80"
    volumes:
      - "${HOP_VOLUME_WEB_DATA}:/usr/share/nginx/html"
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost"]
      interval: 5s
      timeout: 3s
      retries: 3

  cache:
    image: redis:alpine
    ports:
      - "${HOP_PORT_CACHE}:6379"
    volumes:
      - "${HOP_VOLUME_CACHE_DATA}:/data"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 3
```

**Usage in Tests:**
```go
// Copy fixture to seed repo
dcContent, _ := os.ReadFile("fixtures/docker-compose-simple.yml")
WriteFile(t, filepath.Join(env.SeedRepoPath, "docker-compose.yml"), string(dcContent))
```

---

## Testing Conventions

### Best Practices Observed

1. **Isolation**: Each test gets fresh environment with `SetupTestEnv(t)`
2. **Cleanup**: Always use `t.Cleanup()` for guaranteed cleanup
3. **Helpers**: Mark helper functions with `t.Helper()` for better stack traces
4. **Logging**: Use `t.Logf()` for debugging information
5. **Subtests**: Group related assertions with `t.Run()`
6. **Real Operations**: E2E tests use actual Git/Docker, not mocks

### Test Organization

**Naming Convention:**
- E2E tests: `TestE2E_<Feature>`
- Integration tests: `Test<Package>_<Feature>`
- Unit tests: `Test<Function>_<Scenario>`

**File Organization:**
- One test file per major feature
- Related tests grouped with subtests
- Shared fixtures in `fixtures/` directory

### Assertion Style

**Using testify:**
```go
require.NoError(t, err)           // Fail immediately
assert.Equal(t, expected, actual) // Continue test
assert.Contains(t, haystack, needle)
```

**Manual assertions:**
```go
if condition {
    t.Errorf("Expected X, got Y")
}
```

---

## Summary

### Current State

**Testing:**
- ✅ Robust E2E test infrastructure
- ✅ TestEnv provides full isolation
- ✅ Real Git operations in tests
- ✅ Docker smoke tests exist
- ❌ No actual Docker container verification
- ❌ No service health checks
- ❌ No isolation verification

**Docker Integration:**
- ✅ Clean abstraction with CommandRunner
- ✅ Environment generation works
- ✅ Port allocation is deterministic
- ✅ Volume isolation via directories
- ✅ .env file generation
- ✅ Network isolation via Compose defaults

### Gaps to Fill

**Need to Implement:**
1. Docker availability detection
2. Container health waiting
3. HTTP endpoint verification
4. Port isolation testing
5. Volume data isolation testing
6. Multi-service dependency testing
7. Cleanup helpers
8. Docker fixtures with health checks

**Test Categories:**
1. Basic Startup - Containers actually start and respond
2. Port Isolation - Multiple branches use different ports
3. Volume Isolation - Data doesn't leak between branches

### Next Steps

1. Create `docker_helpers.go` with availability checks and verification functions
2. Create Docker fixtures with health checks
3. Implement `docker_integration_test.go` for basic startup
4. Implement `docker_isolation_test.go` for port and volume isolation
5. Add cleanup script for manual cleanup

---

**End of Exploration Documentation**
