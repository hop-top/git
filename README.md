# git-hop

Work on multiple branches in parallel without manual port setup, directory management, or lost context. Each branch gets its own isolated environment with deterministic ports and volumes.

> **Status:** Active development. Usable today, with some rough edges as features evolve.

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

## Quick Start

### Install

**From source** (requires Go 1.26+)

```bash
git clone https://github.com/hop-top/git.git git-hop
cd git-hop
make build
sudo mv git-hop /usr/local/bin/
```

The binary must be named `git-hop` and sit on `PATH` for git to run it as
`git hop`. Don't use `go install hop.top/git@latest`: it names the binary
`git`, which shadows git itself.

Verify installation:

```bash
git hop --version
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

**Verify:** Confirm setup succeeded:
```bash
git hop list  # See all worktrees
pwd           # Should show .../feature-x
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
| `git hop repair` | Repair a corrupted or stale worktree |
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

## Common Workflows

### Add a New Feature Branch

```bash
# From hub directory
git hop add feature-new-ui

# Hop to the new worktree
cd feature-new-ui
```

**Verify:** Confirm the worktree was created:
```bash
git hop list  # See feature-new-ui in the list
pwd           # Should show .../feature-new-ui
```

The new worktree also receives the git-ignored local files (`.env`, tool
config) from the worktree it forked from. Pass `--no-copy-ignored` to skip
that, or put `#-hop-#` on a comment line above a `.gitignore` pattern to
keep that path out for good. See [Configuration](docs/configuration.md#hopaddcopyignored).

### Switch Between Branches

```bash
# List all worktrees
git hop list

# Switch to existing worktree (via filesystem)
cd ../feature-existing

# Or use your shell navigation (e.g., cd /path/to/hop/feature-existing)
```

**Verify:** Confirm you're in the right worktree:
```bash
git branch  # Should show the feature branch checked out
```

### Start/Stop Services

```bash
# Start Docker services for current worktree
git hop env start

# Stop services
git hop env stop
```

**Verify:** Check service status:
```bash
git hop status  # Shows running services
docker ps       # Verify containers are running
```

Services are not started on worktree creation unless you ask. To have
`git hop add` and clone start them once the worktree exists:
```bash
git config --global hop.env.autoStart true   # standing choice
git hop add feat/x --env-start              # this run only (--no-env-start to skip)
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
# Remove a single worktree (clean + merged: silent; otherwise the gate fires)
git hop remove feature-old

# Force flags for risky removals — see `git hop remove --help`:
#   --force       allow removal of an unmerged branch
#                 (never discards a dirty worktree; that needs --no-verify)
#   --no-verify   allow removal with dirty worktree or unpushed commits
#                 (never unlocks an unmerged branch; that needs --force)
git hop remove feature-old --force --no-verify

# Clean up orphaned worktrees (deleted on filesystem)
git hop prune

# Check and fix issues
git hop doctor --fix
```

## Configuration

For detailed configuration, see [Configuration Guide](docs/configuration.md).

Preferences are `hop.*` keys in git config:

```bash
git config --get-regexp '^hop\.'                    # View your settings
git config --global hop.add.fetch true               # Change one for every repo
git config --global --unset hop.add.fetch            # Back to the default
```

Custom package and environment managers live in
`~/.config/git-hop/managers.json`.
git-hop reads no other settings file: an old `~/.config/git-hop/config.json`
is ignored (`git hop doctor` points it out), and so is git-hop's own
`-c`/`--config`. For a one-off setting, use `git -c hop.<key>=<value> hop ...`.

Configuration hierarchy (first found wins):
1. Environment variables
2. Hub-level `hop.json`
3. Hopspace-level `hop.json`
4. git config `hop.*` keys (repo-local, then `--global`)
5. Built-in defaults

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

## Lifecycle Hooks

git-hop runs your own executable scripts at points in the worktree lifecycle. These are git-hop's hooks, not git's — they never live in `.git/hooks/`.

### Available Hooks

- `pre-worktree-add` / `post-worktree-add` - Around `git hop add`
- `pre-worktree-remove` / `post-worktree-remove` - Around `git hop remove`
- `pre-worktree-move` / `post-worktree-move` - Around `git hop move`
- `pre-worktree-switch` / `post-worktree-switch` - Around `git hop <branch>`; the `post-` hook also fires on a plain `cd` into a registered worktree
- `pre-clone` / `post-clone` - Around `git hop clone`
- `pre-repair` / `post-repair` - Around `git hop repair` (global-level only; resolved by a separate code path)

`pre-env-start`, `post-env-start`, `pre-env-stop`, and `post-env-stop` are accepted as names but are **never dispatched**. Environment services use a separate, config-declared hook mechanism instead.

### Hook Resolution Order

1. Repo-level hook — `<worktree>/.git-hop/hooks/<name>` (the runner also walks parent directories)
2. Hopspace-level hook — `$GIT_HOP_DATA_HOME/<org>/<repo>/hooks/<name>` (per `hop.dataLayout`; hooks earlier releases left under `github.com/<org>/<repo>/hooks/` are still read)
3. Global hook — `$GIT_HOP_CONFIG_HOME/hooks/<name>`

First found wins. A missing hook is a silent no-op.

`git hop clone` and `git hop init` mirror committed `.git-hop/hooks/` into your hopspace (`--hooks=symlink|copy|prompt|none`) so team-shared hooks fire from the very first worktree.

See [Hooks System](docs/hooks.md) for the exhaustive table, environment variables, the navigation-handled directive, and [`examples/tmux/`](examples/tmux/) for a complete worked integration.

## Advanced: How It Works

### How git-hop Organizes Your Work

Git-hop keeps your branches isolated across two layers:

**Hub** — the directory where you run `git hop` commands. Usually `~/my-repo/`. Tells git-hop which branches exist locally. Contains `hop.json` and a `.git` reference.

**Hopspace** — where a repository's branch metadata, port allocations and volume mappings live. By default the hub is its own hopspace; a hub cloned with `--global` keeps it at `$GIT_HOP_DATA_HOME/<org>/<repo>/`, where other hubs of the same repository can share it.

Why two? Hubs are your local workspace; hopspace is shared storage. This lets you have multiple hubs (checkouts) of the same repository, all using the same branches without duplication.

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

A **hopspace** holds a repository's branch metadata and resource
allocations. It is the hub directory itself, or, for a hub cloned with
`--global`, `$GIT_HOP_DATA_HOME/<org>/<repo>/` (the path under the data home
follows `hop.dataLayout`, default `{org}/{repo}`):

```
<hopspace>/
  hop.json                  # Hopspace configuration
  ports.json                # Port range and allocations
  volumes.json              # Volume allocations
  deps/                     # Shared dependency installs
```

All worktrees for a repository reference the same hopspace, ensuring consistency.

### Deterministic Resource Allocation

Ports, volumes, and networks are derived from stable hashing:

- **Same branch = same ports** across worktrees (reproducible)
- **Different branches = different ports** (no conflicts)
- **Predictable allocation** (no manual configuration)

Example: branch `feature-x` always gets ports 11500-11505 if not already assigned.

### Output Formats

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

### Configuration Examples

#### Multi-Repository Setup

Manage multiple repositories with different configurations:

```bash
# Clone repo 1
git hop https://github.com/org/repo1.git
cd org/repo1
git hop init

# Clone repo 2 and move its port range
git hop https://github.com/org/repo2.git
cd org/repo2
git hop init
# edit baseRange in ports.json (created on the first Docker worktree)
```

Each repository has its own hopspace with independent resource allocation.

#### Docker with Custom Services

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
make install        # Install to $GOBIN (default: $(go env GOPATH)/bin)
make test           # Run internal/ package tests
make lint           # Run go vet + staticcheck
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

See [CONTRIBUTING.md](CONTRIBUTING.md) for toolchain, the CI steps to run locally,
commit conventions, and the release flow.

### Running Tests

```bash
go test ./...          # Full suite (what CI runs)
make test              # internal/ packages only
go test ./cmd -v       # Test specific package
go test -run TestName ./...  # Run specific test
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

- **Issues**: Report bugs at [GitHub Issues](https://github.com/hop-top/git/issues)
- **Security**: Report privately per the [security policy](https://github.com/hop-top/.github/blob/main/SECURITY.md)
- **Documentation**: See [docs/](docs/) directory for guides and troubleshooting

## License

MIT © 2025 Jad Bitar
