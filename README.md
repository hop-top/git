# git-hop

> A Git subcommand that wraps `git worktree` with deterministic, isolated, and reproducible multi-branch development environments.

<p align="center">
  <a href="#commands">Commands</a> •
  <a href="#quick-start">Quick Start</a> •
  <a href="#documentation">Documentation</a>
</p>

---

## TL;DR

- **This is:** a stand-alone CLI for parallel Git development using worktrees
- **Best for:** multi-branch development, feature switching, testing PRs locally
- **Not for:** distributed teams, production orchestration, multi-tenant setups
- **Installation time:** seconds
- **Works with:** any OS + Git 2.7+ + Docker (optional)

---

## Quick Start

### Install

```bash
# From source (requires Go 1.21+)
go build -o git-hop ./cmd/git-hop
sudo mv git-hop /usr/local/bin/

# Verify installation
git hop --version
```

### First Run

```bash
# Create a hub for a repository
git hop https://github.com/org/repo.git

# Switch to a feature branch
cd org/repo
git hop feature-x
```

Output:

```txt
Created hopspace for 'feature-x'
Worktree: ./feature-x
Ports: 11500-11505
Services: api, db
```

---

## Commands

- `git hop <uri>` — Create a hub and hopspace for a remote repository
- `git hop <branch>` — Create/sync worktree + environment for a branch
- `git hop` — List all hopspaces in the current hub
- `git hop status` — Show detailed environment state for current worktree
- `git hop env start|stop` — Start or stop Docker services
- `git hop remove <target>` — Remove a hub, hopspace, or branch
- `git hop prune` — Remove orphaned or broken hopspaces
- `git hop doctor [--fix]` — Check for inconsistencies and optionally fix them

---

## Architecture

### Hubs

A *hub* is a directory created when you clone a repository. It contains symlinks to all worktrees:

```
hub-repo/
  main -> $GIT_HOP_DATA_HOME/org/repo/main
  feature-x -> $GIT_HOP_DATA_HOME/org/repo/feature-x
  hop.json
```

### Hopspaces

A *hopspace* is the canonical storage location for all worktrees of a single repository:

```
$GIT_HOP_DATA_HOME/<server>/<org>/<repo>/
  hop.json
  ports/
  volumes/
  services/
  <worktrees>/
```

### Deterministic Allocation

Ports, volumes, and networks are derived from stable hashing:
- Same branch = same ports (reproducible)
- Different branches = different ports (no conflicts)
- Predictable allocation = no manual config

---

## Configuration

Config hierarchy (higher priority overrides lower):

1. Environment variables
2. Global git config
3. `$GIT_HOP_CONFIG_HOME/global.json`
4. Hub-level `hop.json`
5. Hopspace-level `hop.json`

Example config:

```json
{
  "auto_env_start": true,
  "port_base": 10000,
  "port_limit": 5000
}
```

Environment variables:

```bash
GIT_HOP_DATA_HOME      # defaults to OS-specific location (see below)
GIT_HOP_CONFIG_HOME    # defaults to $XDG_CONFIG_HOME/git-hop
GIT_HOP_CACHE_DIR      # defaults to $XDG_CACHE_HOME/git-hop
GIT_HOP_LOG_LEVEL      # debug, info, warn, error
```

Data directory defaults by OS:
- **Linux/Unix**: `~/.local/share/git-hop`
- **macOS**: `~/Library/Application Support/git-hop`
- **Windows**: `%LOCALAPPDATA%\git-hop`

---

## Environment Lifecycle

Start services manually:

```bash
git hop env start
```

Or enable auto-start in config:

```json
{
  "auto_env_start": "detect"  // start only if services are defined
}
```

Stop services:

```bash
git hop env stop
```

---

## Hooks

git-hop installs lightweight Git hook wrappers. Hook order:

1. Git → hop wrapper
2. hop wrapper → repo-level hop hook override
3. If none → hopspace-level hook
4. If none → global hook
5. If none → built-in default

Available hooks:

```
pre-worktree-add      post-worktree-add
pre-env-start         post-env-start
pre-env-stop          post-env-stop
```

---

## Global Flags

Available to all commands:

```
--json                    # shorthand for --format=json
--format <table|json|porcelain|raw>
--quiet, -q
--verbose, -v
--force
--hub <name|path>
--hopspace <name|path>
--dry-run
--help, -h
--version
```

---

## Documentation

Comprehensive guides for git-hop features:

- **[Configuration](docs/configuration.md)** - Directory structure, config files, environment variables, and XDG compliance
- **[Dependency Sharing](docs/dependency-sharing.md)** - How git-hop shares dependencies across worktrees to save disk space
- **[Hooks System](docs/hooks.md)** - Lifecycle hooks for customizing worktree and environment behavior
- **[Error Recovery](docs/error-recovery.md)** - Understanding and fixing state issues with the doctor command

---

## Troubleshooting

- **Port conflicts** → Run `git hop doctor` to detect and clean up
- **Orphaned worktrees** → Run `git hop prune` to remove dead hop data
- **Services won't start** → Check Docker is running and `git hop status`
- **Can't find hopspace** → Run `git hop doctor --fix` to initialize missing hopspace
- **Missing directories** → Run `git hop doctor --fix` to create required paths

---

## Development

Build:

```bash
make build
```

Test:

```bash
make test
```

Install:

```bash
make install
```

---

## License

MIT © jadb
