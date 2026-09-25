# Dependency Sharing Across Worktrees

## Overview

git-hop's dependency sharing feature eliminates the need to install dependencies separately in each worktree. Instead, dependencies are installed once per lockfile version and shared across all branches with identical lockfiles using symlinks.

This provides:
- **Space savings** - One installation per lockfile instead of per branch
- **Time savings** - No reinstall when switching to a branch with the same lockfile
- **Multi-PM support** - Handles repos with multiple package managers (npm + Go + pip, etc.)
- **Atomic updates** - Different lockfile versions coexist safely

## How It Works

### Storage Structure

Dependencies are stored once per hopspace, in `<hopspace>/deps/`. A default
clone keeps its hopspace in the hub, so that is `<hub>/deps/`; a `--global`
clone keeps it in the data home, so that is `$GIT_HOP_DATA_HOME/<org>/<repo>/deps/`:

```
<hopspace>/
├── deps/
│   ├── abc123/node_modules/    # Hash of package-lock.json
│   ├── abc123/node_modules.git-hop.json  # Entries the install was made with
│   ├── def456/node_modules/    # Different lockfile version
│   ├── 789ghi/vendor/          # Hash of composer.lock
│   └── .registry.json          # Tracks which branches use which deps
└── hop.json
```

Each install sits in a directory named like the one it stands in for
(`node_modules`, `vendor`, `vendor/bundle`), under a directory named by the
lockfile hash. Node needs this: it resolves a package's imports from the
package's real path, symlinks followed, by looking in each directory named
`node_modules` above it. An install in a directory named anything else could
not import its own packages from one another (`ERR_MODULE_NOT_FOUND` from
inside the store, e.g. under vitest).

Beside each install, `<DepsDir>.git-hop.json` records the entries the
install had when it was made. An install missing any of them is damaged
(see [Emptying a shared install through its link](#emptying-a-shared-install-through-its-link)).
Entries added later, such as a tool's cache, do not count.

### Worktree Symlinks

Each worktree gets a symlink to the shared storage:

```
<hub>/hops/feature-xyz/
├── node_modules -> <hopspace>/deps/abc123/node_modules
└── vendor -> <hopspace>/deps/789ghi/vendor
```

### Installs from earlier releases

Earlier releases named each install after the directory and the hash,
`deps/node_modules.abc123/`, holding the packages directly. Node cannot
resolve from that layout, so those installs are no longer reused:

- `git hop add` installs into the current layout once per lockfile, even
  when an old install for the same lockfile exists. Links other worktrees
  hold into the old install are left alone.
- A worktree still linked to an old install is relinked by its next install
  (`git hop env start`) or by `git hop doctor --fix`, and only once the
  install in the current layout is in place: if the install fails, the
  worktree keeps its old link. The old install itself is never written.
- `git hop doctor` reports such links as warnings.
- `git hop env gc` removes an old install once no worktree of any hub
  git-hop records links into it.

### Stores from earlier releases

Earlier releases put the store of a hub whose path was longer than the data
home at `$GIT_HOP_DATA_HOME/<end of the hub path>/deps/`: the hub path with
as many leading characters dropped as the data home path has. Worktrees that
link there keep working until relinked, as above; their installs are not
reused, and nothing there is written. New installs go to `<hopspace>/deps/`.

Hubs whose paths end the same way could share one old store, so an old
store is only unused once no worktree of any hub git-hop records links
into it. `git hop doctor` lists those stores with their size, and
`git hop env gc` removes them (`--dry-run` lists them without removing
anything). A store any worktree still links into is kept.

### Lockfile Hashing

Dependencies are identified by the SHA256 hash (first 6 characters) of the
lockfile. The registry keys each install by its path under the store:

```json
{
  "abc123/node_modules": {
    "lockfileHash": "abc123",
    "lockfilePath": "package-lock.json",
    "usedBy": ["main", "feature-x"],
    "lastUsed": "2026-02-02T10:30:00Z",
    "installedAt": "2026-01-15T09:00:00Z"
  }
}
```

## Supported Package Managers

git-hop includes built-in support for common package managers:

| Package Manager | Detect File | Lockfile | Deps Dir |
|----------------|-------------|----------|----------|
| npm | package.json | package-lock.json, npm-shrinkwrap.json | node_modules |
| pnpm | pnpm-lock.yaml | pnpm-lock.yaml | node_modules |
| yarn | yarn.lock | yarn.lock | node_modules |
| Go | go.mod | go.sum | vendor |
| pip | requirements.txt, setup.py | requirements.txt | venv |
| cargo | Cargo.toml | Cargo.lock | target |
| composer | composer.json | composer.lock | vendor |
| bundler | Gemfile | Gemfile.lock | vendor/bundle |

Multiple package managers are supported in a single repository (e.g., Go backend + React frontend).

## Automatic Dependency Setup

When you create or switch to a branch, git-hop automatically:

1. Detects package managers in the worktree
2. Computes the hash of each lockfile
3. Checks if dependencies are already installed for that hash
4. If installed: creates a symlink to the shared storage
5. If not installed: installs to shared storage, then creates symlink

No manual intervention required!

## Custom Package Managers

You can add custom package managers or override built-in ones in your global config:

```json
{
  "defaults": { ... },
  "packageManagers": [
    {
      "name": "bun",
      "detectFiles": ["bun.lockb"],
      "lockFiles": ["bun.lockb"],
      "depsDir": "node_modules",
      "installCmd": ["bun", "install", "--frozen-lockfile"]
    },
    {
      "name": "poetry",
      "detectFiles": ["poetry.lock"],
      "lockFiles": ["poetry.lock"],
      "depsDir": ".venv",
      "installCmd": ["poetry", "install", "--no-root"]
    }
  ]
}
```

Save this to `$XDG_CONFIG_HOME/git-hop/managers.json` (usually `~/.config/git-hop/managers.json` on Linux or `~/Library/Preferences/git-hop/managers.json` on macOS).

Custom package managers with the same `name` as built-in ones will override the built-in configuration.

## Install Command Overrides

You can customize the install command for package managers at three levels: **global** (default), **repository** (all branches), or **branch** (specific worktree). The lockfile detection and hashing remain unchanged - only the install command is overridden.

### Why Override Install Commands?

Different projects or branches may need different installation flags:
- Legacy projects requiring `--legacy-peer-deps`
- Experimental branches testing new package manager behavior
- Production builds using `--production` flag
- Projects with peer dependency conflicts requiring `--force`

### Resolution Hierarchy

Git Hop resolves install commands in this order (highest priority first):

1. **Branch-level** override in `hop.json`
2. **Repository-level** override in `hop.json`
3. **Global** package manager config (fallback)

### Repository-Level Overrides

Override install commands for **all branches** in a repository by editing the hopspace `hop.json` (the hub's own `hop.json`; `$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json` for a hub cloned with `--global`):

```json
{
  "repo": {
    "uri": "github.com/myorg/myrepo",
    "org": "myorg",
    "repo": "myrepo",
    "defaultBranch": "main"
  },
  "packageManagers": {
    "npm": {
      "installCmd": ["npm", "install", "--legacy-peer-deps"]
    },
    "pnpm": {
      "installCmd": ["pnpm", "install", "--no-frozen-lockfile"]
    }
  },
  "branches": {
    "main": {
      "exists": true,
      "path": "/path/to/main"
    }
  }
}
```

Now all branches in this repository will use `npm install --legacy-peer-deps` instead of the default `npm ci`.

**Important**: The lockfile (`package-lock.json`) is still used for hashing and cache keys. Only the install command changes.

### Branch-Level Overrides

Override install commands for **specific branches/worktrees** in the same `hop.json`:

```json
{
  "repo": {
    "uri": "github.com/myorg/myrepo",
    "org": "myorg",
    "repo": "myrepo",
    "defaultBranch": "main"
  },
  "packageManagers": {
    "npm": {
      "installCmd": ["npm", "install", "--legacy-peer-deps"]
    }
  },
  "branches": {
    "main": {
      "exists": true,
      "path": "/path/to/main"
    },
    "feature-experimental": {
      "exists": true,
      "path": "/path/to/feature-experimental",
      "packageManagers": {
        "npm": {
          "installCmd": ["npm", "install", "--force"]
        }
      }
    }
  }
}
```

In this example:
- `main` uses: `npm install --legacy-peer-deps` (repo-level)
- `feature-experimental` uses: `npm install --force` (branch-level)
- Other branches use: `npm install --legacy-peer-deps` (repo-level)

### Lockfile Hashing Stays the Same

The lockfile is **always** used for dependency cache keys, regardless of install command overrides:

```
# Both use the same cached dependencies if package-lock.json is identical
main:        npm install --legacy-peer-deps  → abc123/node_modules/
feature-x:   npm install --force             → abc123/node_modules/  (same!)
```

The install command only affects **how** dependencies are installed when the cache key doesn't exist yet.

### Common Override Patterns

**Legacy peer dependencies** (entire repo):
```json
{
  "packageManagers": {
    "npm": {
      "installCmd": ["npm", "install", "--legacy-peer-deps"]
    }
  }
}
```

**Production builds** (specific branch):
```json
{
  "branches": {
    "production": {
      "packageManagers": {
        "npm": {
          "installCmd": ["npm", "ci", "--production"]
        }
      }
    }
  }
}
```

**Python with extra requirements** (specific branch):
```json
{
  "branches": {
    "ml-experiment": {
      "packageManagers": {
        "pip": {
          "installCmd": ["sh", "-c", "pip install -r requirements.txt -r requirements-ml.txt"]
        }
      }
    }
  }
}
```

**Complex shell commands** (using sh wrapper):
```json
{
  "packageManagers": {
    "npm": {
      "installCmd": ["sh", "-c", "npm ci && npm run postinstall"]
    }
  }
}
```

### Finding Your hop.json

The hopspace configuration is the hub's own `hop.json`. For a hub cloned
with `--global` (its `hop.json` has `"repo": {"mode": "global"}`) it is:
```
$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json
```

Default `$GIT_HOP_DATA_HOME` locations:
- Linux: `~/.local/share/git-hop/`
- macOS: `~/Library/Application Support/git-hop/`
- Windows: `%LOCALAPPDATA%\git-hop\`

Example:
```
~/.local/share/git-hop/facebook/react/hop.json
```

## Garbage Collection

Over time, you may accumulate dependencies that are no longer used by any branch.

### Check for Orphaned Dependencies

```bash
git hop env gc --dry-run
```

Example output:
```
Running dependency audit...
  ✓ Scanned 3 worktrees
  ✓ Updated dependency registry

Orphaned dependencies:
  def456/node_modules  (last used: 7 days ago)  ~120MB
  jkl012/venv         (last used: 2 days ago)   ~45MB

Total reclaimable: 165MB

(Dry run - no changes made)
```

### Clean Up Orphaned Dependencies

```bash
git hop env gc
```

This will:
1. Scan all worktrees to identify which dependencies are in use
2. Find dependencies not referenced by any branch
3. Calculate total space that can be reclaimed
4. Prompt for confirmation
5. Delete orphaned dependencies and update the registry

Use `--no-prompt` (or its equivalent `--force`) to skip the confirmation
prompt:

```bash
git hop env gc --no-prompt
```

Piping an answer also works (`echo y | git hop env gc`). If the prompt is
reached with nothing readable on stdin, the command exits `129` with a
`fatal:` message on stderr rather than quietly cancelling with exit `0`.

## Troubleshooting

### Doctor Command

The `git hop doctor` command checks for dependency issues:

```bash
git hop doctor
```

It detects:
- **Local folders** instead of symlinks (user ran `rm -rf node_modules && npm install`) — error
- **Broken symlinks** pointing to missing dependencies — error
- **Missing dependencies** that should exist — error
- **Damaged shared installs**: a link to an install missing entries it was
  made with, e.g. emptied by `npm ci` in another worktree — error
- **Stale symlinks** pointing to old lockfile versions — warning

Stale symlinks are reported as warnings, not errors: the dependencies are
present and usable, they just predate the current lockfile, and the next
install refreshes them. A worktree that has only stale symlinks does not
make `doctor` report the installation as unhealthy.

### Go `vendor/`

Go's `vendor/` is not a regenerable cache — when a project uses it, it is
committed to git and materialised by `git checkout`. `doctor` therefore
inspects `vendor/` only when *vendor mode is active*: `vendor/` already
exists in the worktree **and** is not listed in `.gitignore`.

A repository that gitignores `vendor/`, or simply has no `vendor/`, has
opted out of vendoring. `doctor` reports nothing for it, and `doctor --fix`
never creates the directory. This is the same rule the worktree-create path
applies, so the two cannot disagree.

Note this applies to the Go package manager only. Other managers that also
install into `vendor/` (composer, bundler) treat it as a regenerable cache
and are audited normally.

Example output:
```
Dependencies Status:
  ✓ abc123/node_modules used by: main, feature-x
  ✓ 789ghi/vendor used by: main
  ⚠ jkl012/venv orphaned (no branches use it) - run 'git hop env gc' to clean
  ✗ feature-y: broken symlink node_modules -> (missing abc999)
  ⚠ feature-z: has local node_modules (720MB) instead of symlink
  warn: main: stale symlink node_modules -> old123/node_modules (lockfile changed to abc456); refreshed by the next install

Recommendations:
  - Run 'git hop doctor --fix' to restore shared deps
  - Run 'git hop env gc' to reclaim 45MB from orphaned deps
```

### Auto-Fix Issues

```bash
git hop doctor --fix
```

This automatically repairs:

1. **Local folder instead of symlink:**
   - Moves the local folder to system trash (safe, recoverable)
   - Installs to shared storage if the hash doesn't exist
   - Creates symlink to shared storage

2. **Broken symlink:**
   - Removes the broken symlink
   - Installs dependencies to shared storage
   - Creates new symlink

3. **Stale symlink:**
   - Removes the old symlink
   - Installs new version to shared storage (if needed)
   - Creates symlink to new hash
   - Old version becomes orphaned (cleaned by GC later)

4. **Damaged shared install:**
   - Reinstalls it in place, which repairs every worktree linked to it

`--fix` never touches Go `vendor/` unless vendor mode is active (see above),
so it cannot create the directory in a repository that gitignores it.

Example output:
```
Dependency Issues:
  ⚠ feature-x: local node_modules (720MB) instead of symlink
  ✗ feature-y: broken symlink → deps/xyz999/node_modules (missing)
  warn: main: stale symlink node_modules → old123/node_modules (lockfile changed to abc456); refreshed by the next install

Fix these issues? [y/N]: y
  ✓ feature-x: trashed local folder, created symlink → deps/abc123/node_modules
  ✓ feature-y: removed broken symlink, installed deps, created symlink → deps/abc123/node_modules
  ✓ main: removed stale symlink, created symlink → deps/abc456/vendor

Reclaimed: 720MB
Orphaned: old123/vendor (45MB) - run 'git hop env gc' to clean
```

## Common Scenarios

### Missing Lockfile

If a worktree has no lockfile (e.g., no `package-lock.json`), dependencies cannot be shared:

- git-hop will skip dependency sharing for that package manager
- Dependencies will be installed directly in the worktree (if you run the install command manually)
- A warning will be shown suggesting you commit a lockfile

### Hash Collisions

SHA256 first 6 characters provides ~16M combinations. Collisions are extremely unlikely but handled:

- If a collision is detected (same hash, different lockfile content)
- git-hop automatically uses the first 12 characters instead
- This provides ~68 billion combinations

### Manual Installation

If you manually delete a symlink and install locally:

```bash
rm -rf node_modules
npm install
```

This creates a real folder in the worktree, disconnecting it from shared storage:

- Other branches remain unaffected (still use shared version)
- `git hop doctor` detects this: "has local folder instead of symlink"
- `git hop doctor --fix` restores the symlink to shared storage
- The local folder is moved to trash (recoverable if needed)

### Emptying a shared install through its link

A worktree's `node_modules` is a link to the shared install, so a command
that empties it empties the install for every worktree linked to it:

- `npm ci` removes every entry of `node_modules` before it installs, then
  installs a real `node_modules` in the worktree it ran in
- `rm -rf node_modules/*` removes the visible entries

git-hop cannot prevent this without giving up the link, but it notices:
the install is missing entries it was made with. `git hop doctor`
reports each worktree linked to it
(`shared install abc123/node_modules is missing entries`), and the next
install git-hop runs for that lockfile reinstalls it: `git hop doctor --fix`,
`git hop add`, or `git hop env start` in a worktree with an environment.

To give a worktree its own install, remove the link itself first:
`rm node_modules` (no trailing slash), then install.

### Concurrent Access

Multiple branches can safely share the same dependency installation:

- Each branch gets its own symlink to the same shared storage
- No locking needed for reads
- Anything that writes into `node_modules` of one worktree writes into the
  install every linked worktree uses

However, avoid running installs for the **same lockfile hash** simultaneously in different terminals, as this could corrupt the shared installation.

## Best Practices

### 1. Commit Lockfiles

Always commit lockfiles to your repository:

```bash
git add package-lock.json go.sum requirements.txt
git commit -m "Add lockfiles for dependency sharing"
```

### 2. Run Doctor After Issues

If you experience dependency problems:

```bash
git hop doctor
git hop doctor --fix
```

### 3. Regular Garbage Collection

Clean up orphaned dependencies periodically:

```bash
# Check what can be cleaned
git hop env gc --dry-run

# Clean up when ready
git hop env gc
```

### 4. Understand Symlinks

Some tools may not work correctly with symlinked dependencies. If you encounter issues:

- Check if the tool supports symlinks
- If not, you may need to use local installations for that specific branch
- Report the issue to the tool maintainer

## Implementation Details

For developers interested in the implementation:

- **Package manager detection**: `internal/services/package_managers.go`
- **Dependency management**: `internal/services/deps_manager.go`, `internal/services/deps_link.go`
- **Registry tracking**: `internal/services/deps_registry.go`
- **Trash utility**: `internal/services/trash.go`
- **Command integration**: `cmd/env.go`, `cmd/env_gc.go`, `cmd/doctor.go`

The system uses:
- SHA256 hashing for lockfile fingerprints
- Symlinks for zero-copy sharing
- JSON registry for usage tracking
- System trash for safe deletion (recoverable)
