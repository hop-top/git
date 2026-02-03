# Dependency Sharing Across Worktrees

## Problem

Worktrees duplicate dependencies (node_modules, vendor, venv, etc.), wasting disk space and install
time. Each branch reinstalls identical deps even when lockfiles unchanged.

## Solution

Centralized dependency storage per repo, shared via symlinks. Install once per lockfile hash, reuse
across all branches with same lockfile.

## Architecture

### Storage Structure

```
$GIT_HOP_DATA_HOME/org/repo/
├── deps/
│   ├── node_modules.abc123/    # Hash of package-lock.json
│   ├── node_modules.def456/    # Different lockfile version
│   ├── vendor.789ghi/          # Hash of go.sum
│   ├── venv.jkl012/            # Hash of requirements.txt
│   └── .registry.json          # Tracks usage
└── hop.json
```

### Worktree Symlinks

```
.git/hop/worktrees/feature-xyz/
├── node_modules -> $GIT_HOP_DATA_HOME/org/repo/deps/node_modules.abc123
├── vendor -> $GIT_HOP_DATA_HOME/org/repo/deps/vendor.789ghi
└── venv -> $GIT_HOP_DATA_HOME/org/repo/deps/venv.jkl012
```

### Registry Format

```json
{
  "node_modules.abc123": {
    "lockfileHash": "abc123",
    "lockfilePath": "package-lock.json",
    "usedBy": ["main", "feature-x"],
    "lastUsed": "2026-02-02T10:30:00Z",
    "installedAt": "2026-01-15T09:00:00Z"
  },
  "vendor.789ghi": {
    "lockfileHash": "789ghi",
    "lockfilePath": "go.sum",
    "usedBy": ["main", "feature-y"],
    "lastUsed": "2026-02-02T10:30:00Z",
    "installedAt": "2026-01-20T14:00:00Z"
  }
}
```

## Package Manager Support

### Configuration

Package managers can be defined in code (built-in) or user config (custom/overrides).

**Custom PM config in `$XDG_CONFIG_HOME/git-hop/global.json`:**

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

**Loading strategy:**
1. Load built-in PMs from code
2. Load custom PMs from config
3. Merge: config overrides built-in if same `name`

**Validation:**
- Required fields: name, detectFiles, lockFiles, depsDir, installCmd
- Warn if detectFiles/lockFiles referenced but not found
- Test install command availability before first use

### Detection Strategy

Detect all package managers present in repo (supports multiple PMs per repo):

| Package Manager | Detect File(s) | Lockfile | Deps Dir |
|----------------|----------------|----------|----------|
| npm | package.json | package-lock.json, npm-shrinkwrap.json | node_modules |
| pnpm | pnpm-lock.yaml | pnpm-lock.yaml | node_modules |
| yarn classic | yarn.lock (v1) | yarn.lock | node_modules |
| yarn berry | yarn.lock (v2+) | yarn.lock | .yarn/cache |
| Go | go.mod | go.sum | vendor |
| pip | requirements.txt, setup.py | requirements.txt | venv |
| cargo | Cargo.toml | Cargo.lock | target |
| composer | composer.json | composer.lock | vendor |
| bundler | Gemfile | Gemfile.lock | vendor/bundle |

### Install Commands

| PM | Install Command |
|----|----------------|
| npm | `npm ci` (if lockfile exists), else `npm install` |
| pnpm | `pnpm install --frozen-lockfile` |
| yarn | `yarn install --frozen-lockfile` |
| Go | `go mod download && go mod vendor` |
| pip | `python -m venv venv && source venv/bin/activate && pip install -r requirements.txt` |
| cargo | `cargo fetch` |
| composer | `composer install --no-dev` |
| bundler | `bundle install --deployment` |

## Workflow

### On Branch Create/Checkout (`git hop env start`)

1. Detect all package managers in worktree
2. For each PM:
   - Read lockfile, compute hash (SHA256 first 6 chars)
   - Check if `deps/{depsdir}.{hash}/` exists
     - **Exists:** Create symlink, add branch to `usedBy[]`, update `lastUsed`
     - **Not exists:** Install to `deps/{depsdir}.{hash}/`, create registry entry
3. Write symlinks to worktree

### On Doctor (`git hop doctor`)

1. Scan all worktrees for this repo
2. For each worktree:
   - Detect package managers
   - Hash lockfiles
   - Record expected deps hash
3. Rebuild `usedBy[]` arrays from scratch (trust filesystem state, not registry)
4. Report issues:
   - **Local folder instead of symlink** (user ran `rm -rf node_modules && npm install`)
   - **Broken symlinks** (pointing to missing deps)
   - **Stale symlinks** (lockfile changed, symlink points to old hash)
   - **Missing deps** (expected hash not installed)
   - **Orphaned deps** (empty `usedBy[]`)

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

### On Doctor Fix (`git hop doctor --fix`)

Automatically repairs all symlink issues with user confirmation (skip with `--force`).

**Issues fixed:**

1. **Local folder instead of symlink:**
   - Trash local folder (using system trash, not `rm`)
   - Install to shared storage if hash doesn't exist
   - Create symlink to shared storage
   - Update registry

2. **Broken symlink:**
   - Remove broken symlink
   - Install deps to shared storage
   - Create new symlink
   - Update registry

3. **Stale symlink:**
   - Remove old symlink
   - Install new version to shared storage (if not exists)
   - Create symlink to new hash
   - Old version becomes orphaned (cleaned by GC later)
   - Update registry

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

### On GC (`git hop env gc`)

1. Run doctor logic first (rebuild `usedBy[]`)
2. Identify orphaned deps (empty `usedBy[]`)
3. Calculate total reclaimable space
4. Show preview, prompt for confirmation (unless `--force`)
5. Delete orphaned deps directories
6. Delete orphaned entries from registry

Example output:
```
Running dependency audit...
  ✓ Scanned 3 worktrees
  ✓ Updated dependency registry

Orphaned dependencies:
  node_modules.def456  (last used: 7 days ago)  ~120MB
  venv.jkl012         (last used: 2 days ago)   ~45MB

Total reclaimable: 165MB

Delete these dependencies? [y/N]:
```

### On Branch Delete

Remove branch from `usedBy[]` arrays. Do NOT auto-delete deps (wait for manual GC).

## Implementation

### Task List

- [ ] Update `internal/config/config.go` - add PackageManager config to GlobalConfig
- [ ] Create `internal/services/package_managers.go` - PM detection/definitions
- [ ] Create `internal/services/deps_registry.go` - registry CRUD operations
- [ ] Create `internal/services/deps_manager.go` - install/symlink logic
- [ ] Update `cmd/env.go` - add deps logic before docker
- [ ] Update `cmd/doctor.go` - add deps audit and `--fix` flag
- [ ] Add `git hop env gc` command
- [ ] Add trash utility for safe deletion (not `rm -rf`)
- [ ] Add tests for built-in PM detection
- [ ] Add tests for custom PM config loading
- [ ] Add tests for config override behavior
- [ ] Add tests for hash collision handling
- [ ] Add tests for multi-PM repos
- [ ] Update docs

### Files to Create/Modify

#### Modify: internal/config/config.go

Add to `GlobalConfig`:

```go
type GlobalConfig struct {
    Defaults         DefaultSettings       `json:"defaults"`
    PackageManagers  []PackageManagerConfig `json:"packageManagers,omitempty"`
    Backup           BackupSettings        `json:"backup,omitempty"`
    Conversion       ConversionSettings    `json:"conversion,omitempty"`
}

type PackageManagerConfig struct {
    Name        string   `json:"name"`
    DetectFiles []string `json:"detectFiles"`
    LockFiles   []string `json:"lockFiles"`
    DepsDir     string   `json:"depsDir"`
    InstallCmd  []string `json:"installCmd"`
}
```

#### Create: internal/services/package_managers.go

```go
type PackageManager struct {
    Name        string
    DetectFiles []string  // Files indicating PM presence
    LockFiles   []string  // Lockfile paths (in priority order)
    DepsDir     string    // Where deps get installed
    InstallCmd  []string  // Install command
    HashLockfile(path string) (string, error)
}

// Load built-in + custom PMs from config
func LoadPackageManagers(globalConfig *config.GlobalConfig) ([]PackageManager, error)
func DetectPackageManagers(worktreePath string, availablePMs []PackageManager) ([]PackageManager, error)
func (pm *PackageManager) Install(targetDir string, worktreePath string) error
func (pm *PackageManager) Validate() error  // Check required fields, cmd availability
```

#### Create: internal/services/deps_registry.go

```go
type DepsRegistry struct {
    Entries map[string]DepsEntry
}

type DepsEntry struct {
    LockfileHash string
    LockfilePath string
    UsedBy       []string
    LastUsed     time.Time
    InstalledAt  time.Time
}

func LoadRegistry(repoPath string) (*DepsRegistry, error)
func (r *DepsRegistry) Save(repoPath string) error
func (r *DepsRegistry) AddUsage(depsKey, branch string)
func (r *DepsRegistry) RemoveUsage(depsKey, branch string)
func (r *DepsRegistry) RebuildFromWorktrees(worktrees []string, pms []PackageManager) error
func (r *DepsRegistry) GetOrphaned() []string
```

#### Create: internal/services/deps_manager.go

```go
type DepsManager struct {
    Registry *DepsRegistry
    RepoPath string
    fs       afero.Fs
}

func NewDepsManager(fs afero.Fs, repoPath string) (*DepsManager, error)
func (m *DepsManager) EnsureDeps(worktreePath, branch string) error
func (m *DepsManager) Audit(worktrees []string) ([]Issue, error)
func (m *DepsManager) Fix(issues []Issue, force bool) error
func (m *DepsManager) GarbageCollect(dryRun bool) ([]string, int64, error)
```

#### Modify: cmd/env.go#runEnvCommand

```go
// Before docker.ComposeUp, call:
depsManager := services.NewDepsManager(fs, repoPath)
if err := depsManager.EnsureDeps(worktreePath, branch); err != nil {
    output.Fatal("Failed to ensure dependencies: %v", err)
}
```

#### Modify: cmd/doctor.go

Add deps audit section after existing checks. Add `--fix` flag:

```go
var fixFlag bool

func init() {
    doctorCmd.Flags().BoolVar(&fixFlag, "fix", false, "Auto-fix symlink issues")
}

// In Run function:
issues, err := depsManager.Audit(worktrees)
if fixFlag {
    err := depsManager.Fix(issues, forceFlag)
}
```

#### Create: cmd/env_gc.go

New subcommand under `env gc` that calls `DepsManager.GarbageCollect()`.

## Edge Cases

### Hash Collisions

SHA256 first 6 chars = ~16M combinations. Collision unlikely but possible.
- Detection: Check if `deps/{dir}.{hash}/` exists with different lockfile content
- Resolution: Use first 12 chars if collision detected

### Concurrent Installs

Two branches install same deps simultaneously.
- Use atomic rename: install to temp dir, rename when complete
- Lock file: `deps/.lock.{hash}` during install

### Symlink vs Copy

Some tools break with symlinked deps (e.g., certain webpack configs).
- Config option: `symlinkDeps: true` (default) vs `false` (copy)
- Per-PM override if needed

### Missing Lockfile

No lockfile = can't hash = can't share.
- Fallback: install directly in worktree (no sharing)
- Warn user to commit lockfile

### User Deletes Symlink (`rm -rf node_modules && npm install`)

User removes symlink and installs locally.
- Creates real folder in worktree (disconnects from sharing)
- Other branches unaffected (still use shared version)
- `git hop doctor` detects: "has local folder instead of symlink"
- `git hop doctor --fix` restores symlink to shared storage
- Old shared version may become orphaned if this was last user

### Main Branch Detection

Use `RepoConfig.DefaultBranch` from hop.json (set during clone from remote HEAD).
Fallback: check for "main", then "master".
If neither exists: prompt user.

### Removing Main Branch

Deps stored centrally, not tied to any specific branch.
Last branch using deps keeps them alive via registry.

## Benefits

- **Space savings:** 1 install per lockfile instead of per branch
- **Time savings:** No reinstall if lockfile unchanged
- **Multi-PM support:** Handle repos with npm + Go + pip simultaneously
- **Safe cleanup:** Manual GC prevents accidental deletion
- **Atomic updates:** Different lockfile versions coexist

## Future Enhancements

- Auto-GC on threshold (e.g., > 1GB orphaned)
- Per-PM config overrides
- Compression for old lockfile versions
- Integration with pnpm/yarn berry native sharing
