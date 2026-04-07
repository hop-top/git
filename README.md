# git-hop

> [!WARNING]
> **🚧 Do Not Use — History Will Be Rewritten 🚧**
>
> This repo is undergoing major restructuring as we selectively
> open-source internal tools built at
> [Idea Crafters LLC](https://ideacrafters.com). Git history **will be
> force-pushed and rewritten** multiple times. Do not fork, clone, or
> depend on this repo in any capacity until we tag a stable release.

A Git subcommand that wraps `git worktree` with deterministic, isolated, and reproducible multi-branch development environments.

## Overview

git-hop eliminates the friction of managing multiple Git worktrees by automatically organizing worktrees, allocating resources (ports, volumes, networks), and providing lifecycle management for Docker environments. Work on multiple branches in parallel without port conflicts, manual configuration, or lost context.

**Perfect for:**
- Multi-branch development workflows
- Testing multiple PRs locally
- Feature branch isolation
- Parallel development with Docker services

**Not for:**
- Distributed team orchestration
- Production deployment management
- Multi-tenant setups
- Monorepo optimization (see Git's native monorepo support)

## Features

- 🚀 **Automatic Navigation** - Optional shell integration for seamless worktree switching
- 🔗 **Smart Symlinks** - `current` symlink always points to your last worktree
- 🐳 **Docker Integration** - Isolated environments with deterministic port allocation
- 📦 **Dependency Management** - Automatic npm/yarn/pnpm installation per worktree
- 🔄 **State Tracking** - Track all worktrees across your system
- 🛠️ **Zero Config** - Sensible defaults, works out of the box

## Quick Start

### Install

**From source** (requires Go 1.21+)

```bash
git clone https://github.com/jadb/git-hop.git
cd git-hop
make build
sudo mv git-hop /usr/local/bin/
```

Verify installation:

```bash
git hop --version
```

**From go install**

```bash
go install github.com/jadb/git-hop@latest
```

### First Run (60 seconds)

Initialize git-hop in an existing repository:

```bash
cd /path/to/my/repo
git hop init
```

The interactive setup will guide you through:
1. Converting to bare repo + worktrees (recommended)
2. Setting up initial branch worktree
3. Creating hop.json configuration

Then create a worktree for a feature branch:

```bash
git hop add feature-x
cd feature-x
```

**Optional:** Install shell integration for automatic directory switching:

```bash
git hop install-shell-integration
# Now use: git-hop feature-x (automatically cd to worktree)
```

You now have:
- A clean worktree for `feature-x`
- Deterministic ports allocated (no conflicts)
- Docker environment configured (if docker-compose.yml exists)
- Full isolation from other branches
- (Optional) Automatic navigation with `git-hop` command

List all worktrees:

```bash
git hop list
```

Stop the environment:

```bash
git hop env stop
```

## Commands

### Core Commands

| Command | Description |
|---------|-------------|
| `git hop init` | Initialize git-hop in a repository (interactive setup) |
| `git hop <branch>` | Navigate to an existing worktree (updates `current` symlink) |
| `git hop add <branch>` | Create a new worktree and environment for a branch |
| `git hop list` | List all managed worktrees and their status |
| `git hop status` | Show status of current worktree or hub |
| `git hop remove <target>` | Remove a worktree, hopspace, or hub |
| `git hop prune` | Clean up orphaned worktrees and hubs |
| `git hop doctor` | Check and repair environment issues |

### Shell Integration (Optional)

Enable automatic directory switching when hopping between worktrees:

| Command | Description |
|---------|-------------|
| `git hop install-shell-integration` | Install shell wrapper for auto-cd (bash/zsh/fish) |
| `git hop uninstall-shell-integration` | Remove shell wrapper function |
| `git-hop <branch>` | Navigate and auto-cd (requires shell integration) |

**[→ Shell Integration Guide](docs/shell-integration.md)**

### Environment Commands

Manage Docker services and resources:

```bash
git hop env start    # Start Docker services for current worktree
git hop env stop     # Stop Docker services
```

### Global Flags

Available on all commands:

```bash
--config <path>           # Path to config file
--json                    # Output in JSON format
--porcelain               # Machine-readable output
--quiet, -q               # Suppress output
--verbose, -v             # Verbose output
--force                   # Bypass safety checks
--dry-run                 # Preview changes without applying
--global, -g              # Use global hopspace instead of local
--git-domain <domain>     # Git domain for shorthand notation (default: github.com)
--help, -h                # Show command help
--version                 # Show version information
```

## Architecture

### Hubs

A **hub** is a directory that serves as your local working context. It contains:
- A `hop.json` configuration file tracking all worktrees
- A `.git` reference to the bare repository
- Direct access to worktrees via paths stored in config

```
my-repo/                    # Hub directory (local context)
  .git                      # Bare repository reference
  hop.json                  # Hub configuration (tracks worktree paths)
```

The hub's `hop.json` maintains references to all worktrees with their full paths, allowing you to quickly switch between branches without manual path management.

### Hopspaces

A **hopspace** is the canonical storage location for all worktrees of a repository. Located at:

```
$GIT_HOP_DATA_HOME/<domain>/<org>/<repo>/
  hop.json                  # Hopspace configuration
  ports.json                # Port allocations
  volumes.json              # Volume allocations
  feature-x/                # Actual worktree directory
  feature-y/
  ...
```

All worktrees for a repository reference the same hopspace, ensuring consistency.

### Deterministic Resource Allocation

Ports, volumes, and networks are derived from stable hashing:

- **Same branch = same ports** across worktrees (reproducible)
- **Different branches = different ports** (no conflicts)
- **Predictable allocation** (no manual configuration)

Example: branch `feature-x` always gets ports 11500-11505 if not already assigned.

## Configuration

Configuration follows a hierarchy (higher priority overrides lower):

1. Environment variables
2. Command-line flags
3. `$XDG_CONFIG_HOME/git-hop/config.json` (global)
4. Hub-level `hop.json`
5. Hopspace-level `hop.json`

### Configuration File

Create `$XDG_CONFIG_HOME/git-hop/config.json`:

```json
{
  "auto_env_start": "detect",
  "port_base": 10000,
  "port_limit": 5000,
  "defaults": {
    "worktree_location": "hops"
  }
}
```

Configuration options:

- `auto_env_start` - Auto-start Docker services (`true`, `false`, or `"detect"` to start only if services exist)
- `port_base` - Starting port for allocation (default: 10000)
- `port_limit` - Maximum ports available (default: 5000)
- `defaults.worktree_location` - Directory for storing worktrees (default: `hops`)

### Environment Variables

```bash
GIT_HOP_DATA_HOME      # Hopspace storage location (OS-specific default)
GIT_HOP_CONFIG_HOME    # Config directory (default: $XDG_CONFIG_HOME/git-hop)
GIT_HOP_CACHE_DIR      # Cache directory (default: $XDG_CACHE_HOME/git-hop)
GIT_HOP_LOG_LEVEL      # Log level: debug, info, warn, error (default: info)
```

### Data Directory Defaults

- **Linux/Unix**: `~/.local/share/git-hop`
- **macOS**: `~/Library/Application Support/git-hop`
- **Windows**: `%LOCALAPPDATA%\git-hop`

## Common Workflows

### Add a New Feature Branch

```bash
# From hub directory
git hop add feature-new-ui

# Hop to the new worktree
cd feature-new-ui
```

### Switch Between Branches

```bash
# List all worktrees
git hop list

# Switch to existing worktree (via filesystem)
cd ../feature-existing

# Or use your shell navigation (e.g., cd /path/to/hop/feature-existing)
```

### Start/Stop Services

```bash
# Start Docker services for current worktree
git hop env start

# Stop services
git hop env stop

# Auto-start on worktree creation
git hop config auto_env_start detect
```

### Inspect Environment State

```bash
# Show current worktree status
git hop status

# Show all repositories and worktrees
git hop status --all

# Output as JSON
git hop status --json
```

### Clean Up

```bash
# Remove a single worktree
git hop remove feature-old

# Clean up orphaned worktrees (deleted on filesystem)
git hop prune

# Check and fix issues
git hop doctor --fix
```

## Troubleshooting

### Port Conflicts

**Symptom:** Docker containers fail to start due to port conflicts.

**Solution:**

```bash
git hop doctor
```

The doctor command detects conflicting port allocations and helps resolve them.

### Orphaned Worktrees

**Symptom:** `git hop list` shows worktrees that no longer exist.

**Solution:**

```bash
git hop prune
```

This removes worktrees from state if their directories don't exist on the filesystem.

### Services Won't Start

**Symptom:** `git hop env start` fails or services don't start.

**Steps:**
1. Verify Docker is running: `docker ps`
2. Check environment status: `git hop status`
3. Review docker-compose.yml in worktree: `cat docker-compose.yml`
4. Check logs: `git hop status --verbose`

### Can't Find Hopspace

**Symptom:** "Failed to load hopspace" error.

**Solution:**

```bash
git hop doctor --fix
```

This initializes missing hopspace directories and configurations.

### Repository Not Initialized

**Symptom:** "Not in a git-hop hub" error.

**Solution:**

Run `git hop init` to convert your repository:

```bash
cd /path/to/repo
git hop init
```

## Integration with Git Hooks

git-hop installs lightweight Git hook wrappers that integrate with your workflow:

### Available Hooks

- `pre-worktree-add` - Before creating a worktree
- `post-worktree-add` - After creating a worktree
- `pre-env-start` - Before starting environment
- `post-env-start` - After starting environment
- `pre-env-stop` - Before stopping environment
- `post-env-stop` - After stopping environment

### Hook Resolution Order

1. Repo-level hop hook override (if exists)
2. Hopspace-level hook (if exists)
3. Global hook (if exists)
4. Built-in default behavior

Place hooks in `$GIT_HOP_CONFIG_HOME/hooks/` or hopspace-specific directories to customize behavior.

See [Hooks System](docs/hooks.md) for detailed examples.

## Output Formats

Control output with flags:

```bash
# Human-readable (default)
git hop list

# JSON format
git hop list --json

# Machine-readable (porcelain)
git hop list --porcelain

# Suppress all output
git hop status --quiet
```

## Configuration Examples

### Multi-Repository Setup

Manage multiple repositories with different configurations:

```bash
# Clone repo 1
git hop https://github.com/org/repo1.git
cd org/repo1
git hop init

# Clone repo 2 with custom settings
git hop https://github.com/org/repo2.git
cd org/repo2
git hop init
git hop config port_base 20000
```

Each repository has its own hopspace with independent resource allocation.

### Docker with Custom Services

Repositories with docker-compose.yml automatically detect and allocate resources:

```yaml
# docker-compose.yml in worktree
version: '3'
services:
  api:
    ports:
      - "${API_PORT}:3000"
  db:
    ports:
      - "${DB_PORT}:5432"
```

git-hop automatically injects `${API_PORT}` and `${DB_PORT}` based on branch allocations.

## Development

### Building from Source

```bash
make build          # Build the binary
make install        # Install to /usr/local/bin
make test           # Run tests
make lint           # Run linter
make fmt            # Format code
make clean          # Clean build artifacts
```

### Project Structure

```
git-hop/
  cmd/                  # Command implementations
  internal/
    cli/                # CLI framework and root command
    hop/                # Core worktree and hopspace logic
    config/             # Configuration management
    docker/             # Docker integration
    state/              # State file management
    services/           # Environment and dependency services
    output/             # Output formatting and styling
  docs/                 # Documentation and guides
  main.go               # Entry point
  Makefile              # Build and test targets
```

### Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature-name`
3. Make changes and add tests
4. Run tests: `make test`
5. Commit with clear messages
6. Push and open a pull request

### Running Tests

```bash
make test              # Run all tests
go test ./cmd -v      # Test specific package
go test -run TestName  # Run specific test
```

The default `go test ./...` covers tier-1 (unit) and tier-2 (local-git
e2e) tests. Tier-3 docker e2e tests are gated behind the `dockere2e`
build tag and run nightly via `.github/workflows/dockere2e.yml`. To run
them locally:

```bash
go test -tags dockere2e ./test/e2e/docker/...
```

See [docs/testing.md](docs/testing.md) for the full testing
architecture, CI workflow split, the xrr-aware binary contract, and
common gotchas.

## Documentation

For detailed information, see:

- **[Configuration](docs/configuration.md)** - Directory structure, config files, environment variables, XDG compliance
- **[Dependency Sharing](docs/dependency-sharing.md)** - How worktrees share dependencies to save disk space
- **[Hooks System](docs/hooks.md)** - Lifecycle hooks for customizing worktree and environment behavior
- **[Error Recovery](docs/error-recovery.md)** - Understanding and fixing state issues with the doctor command
- **[Package Manager Overrides](docs/package-manager-overrides.md)** - Custom dependency installation per branch
- **[Testing](docs/testing.md)** - Test tiers, CI workflow split, dockere2e gate, xrr-aware test runtime

## FAQ

**Q: Can I use git-hop with a monorepo?**

A: Yes, but standard Git tools may be more suitable. git-hop shines with multi-branch development in single repositories.

**Q: Do I need Docker?**

A: No. Docker is optional. git-hop works perfectly for non-containerized projects.

**Q: How much disk space do worktrees use?**

A: Each worktree is a separate directory with checked-out files. git-hop optimizes with dependency sharing and caching. See [Dependency Sharing](docs/dependency-sharing.md).

**Q: Can I move a hub or hopspace?**

A: Yes. Hub paths are flexible. Hopspace paths should be updated in configuration. Use `git hop doctor` after moving.

**Q: What Git versions are supported?**

A: Git 2.7+ (worktree support was added in Git 2.5, git-hop requires 2.7+ for full compatibility).

**Q: How do I uninstall git-hop?**

A: Remove the binary and clean up data directories:

```bash
sudo rm /usr/local/bin/git-hop
rm -rf ~/.local/share/git-hop      # Linux
rm -rf ~/Library/Application\ Support/git-hop  # macOS
```

State and config files remain for recovery if needed.

## Support

- **Issues**: Report bugs at [GitHub Issues](https://github.com/jadb/git-hop/issues)
- **Discussions**: Ask questions at [GitHub Discussions](https://github.com/jadb/git-hop/discussions)
- **Documentation**: See [docs/](docs/) directory for guides and troubleshooting

## License

MIT © 2025 Jad Bitar
