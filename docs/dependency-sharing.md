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

Dependencies are stored centrally per repository:

```
$GIT_HOP_DATA_HOME/<org>/<repo>/
├── deps/
│   ├── node_modules.abc123/    # Hash of package-lock.json
│   ├── node_modules.def456/    # Different lockfile version
│   ├── vendor.789ghi/          # Hash of go.sum
│   └── .registry.json          # Tracks which branches use which deps
└── hop.json
```

### Worktree Symlinks

Each worktree gets a symlink to the shared storage:

```
.git/hop/hops/feature-xyz/
├── node_modules -> $GIT_HOP_DATA_HOME/org/repo/deps/node_modules.abc123
└── vendor -> $GIT_HOP_DATA_HOME/org/repo/deps/vendor.789ghi
```

### Lockfile Hashing

Dependencies are identified by the SHA256 hash (first 6 characters) of the lockfile:

```json
{
  "node_modules.abc123": {
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

Save this to `$XDG_CONFIG_HOME/git-hop/global.json` (usually `~/.config/git-hop/global.json` on Linux or `~/Library/Preferences/git-hop/global.json` on macOS).

Custom package managers with the same `name` as built-in ones will override the built-in configuration.

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
  node_modules.def456  (last used: 7 days ago)  ~120MB
  venv.jkl012         (last used: 2 days ago)   ~45MB

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

Use `--force` to skip the confirmation prompt:

```bash
git hop env gc --force
```

## Troubleshooting

### Doctor Command

The `git hop doctor` command checks for dependency issues:

```bash
git hop doctor
```

It detects:
- **Local folders** instead of symlinks (user ran `rm -rf node_modules && npm install`)
- **Broken symlinks** pointing to missing dependencies
- **Stale symlinks** pointing to old lockfile versions
- **Missing dependencies** that should exist

Example output:
```
Dependencies Status:
  ✓ node_modules.abc123 used by: main, feature-x
  ✓ vendor.789ghi used by: main
  ⚠ venv.jkl012 orphaned (no branches use it) - run 'git hop env gc' to clean
  ✗ feature-y: broken symlink node_modules -> (missing abc999)
  ⚠ feature-z: has local node_modules (720MB) instead of symlink
  ⚠ main: stale symlink vendor -> vendor.old123 (lockfile now abc456)

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

Example output:
```
Dependency Issues:
  ⚠ feature-x: local node_modules (720MB) instead of symlink
  ⚠ feature-y: broken symlink → deps/node_modules.xyz999 (missing)
  ⚠ main: stale symlink → deps/vendor.old123 (lockfile changed to abc456)

Fix these issues? [y/N]: y
  ✓ feature-x: trashed local folder, created symlink → deps/node_modules.abc123
  ✓ feature-y: removed broken symlink, installed deps, created symlink → deps/node_modules.abc123
  ✓ main: removed stale symlink, created symlink → deps/vendor.abc456

Reclaimed: 720MB
Orphaned: vendor.old123 (45MB) - run 'git hop env gc' to clean
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

### Concurrent Access

Multiple branches can safely share the same dependency installation:

- Dependencies are read-only
- Each branch gets its own symlink to the same shared storage
- No locking needed for reads

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
- **Dependency management**: `internal/services/deps_manager.go`
- **Registry tracking**: `internal/services/deps_registry.go`
- **Trash utility**: `internal/services/trash.go`
- **Command integration**: `cmd/env.go`, `cmd/env_gc.go`, `cmd/doctor.go`

The system uses:
- SHA256 hashing for lockfile fingerprints
- Symlinks for zero-copy sharing
- JSON registry for usage tracking
- System trash for safe deletion (recoverable)
