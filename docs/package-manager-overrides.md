# Package Manager Install Command Overrides

Git Hop allows you to customize package manager install commands at multiple levels with a clear hierarchy:

1. **Branch-level** (highest priority) - per worktree customization
2. **Repository-level** - shared across all branches in a repo
3. **Global-level** (fallback) - default behavior for all repos

## Configuration Hierarchy

### 1. Global Configuration

Define default package managers in `~/.config/git-hop/global.json`:

```json
{
  "packageManagers": [
    {
      "name": "npm",
      "detectFiles": ["package.json"],
      "lockFiles": ["package-lock.json"],
      "depsDir": "node_modules",
      "installCmd": ["npm", "ci"]
    },
    {
      "name": "pnpm",
      "detectFiles": ["pnpm-lock.yaml"],
      "lockFiles": ["pnpm-lock.yaml"],
      "depsDir": "node_modules",
      "installCmd": ["pnpm", "install", "--frozen-lockfile"]
    }
  ]
}
```

### 2. Repository-Level Overrides

Override install commands for all branches in a repository in `$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json`:

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
    }
  }
}
```

This overrides the global npm config for **all branches** in this repository.

### 3. Branch-Level Overrides

Override install commands for specific branches/worktrees in `$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json`:

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
- `main` branch uses: `npm install --legacy-peer-deps` (repo-level override)
- `feature-experimental` branch uses: `npm install --force` (branch-level override)
- Any other branch would use: `npm install --legacy-peer-deps` (repo-level)
- If repo-level wasn't set, would use: `npm ci` (global default)

## Use Cases

### Use Case 1: Legacy Peer Dependencies

Some projects need `--legacy-peer-deps` for all branches:

```json
{
  "packageManagers": {
    "npm": {
      "installCmd": ["npm", "install", "--legacy-peer-deps"]
    }
  }
}
```

### Use Case 2: Experimental Branch with Different Flags

Testing a migration to a newer package manager behavior:

```json
{
  "branches": {
    "migration-npm-9": {
      "exists": true,
      "path": "/path/to/migration-npm-9",
      "packageManagers": {
        "npm": {
          "installCmd": ["npm", "install", "--strict-peer-deps"]
        }
      }
    }
  }
}
```

### Use Case 3: Development vs Production Builds

Development branches with dev dependencies:

```json
{
  "packageManagers": {
    "npm": {
      "installCmd": ["npm", "ci"]
    }
  },
  "branches": {
    "production": {
      "exists": true,
      "path": "/path/to/production",
      "packageManagers": {
        "npm": {
          "installCmd": ["npm", "ci", "--production"]
        }
      }
    }
  }
}
```

### Use Case 4: Custom Package Manager (Bun)

Add a custom package manager globally and override per repo:

**Global** (`~/.config/git-hop/global.json`):
```json
{
  "packageManagers": [
    {
      "name": "bun",
      "detectFiles": ["bun.lockb"],
      "lockFiles": ["bun.lockb"],
      "depsDir": "node_modules",
      "installCmd": ["bun", "install", "--frozen-lockfile"]
    }
  ]
}
```

**Repository** (`hop.json`):
```json
{
  "packageManagers": {
    "bun": {
      "installCmd": ["bun", "install", "--no-cache"]
    }
  }
}
```

### Use Case 5: Python Virtual Environments

Different Python projects might need different flags:

**Global**:
```json
{
  "packageManagers": [
    {
      "name": "pip",
      "detectFiles": ["requirements.txt"],
      "lockFiles": ["requirements.txt"],
      "depsDir": "venv",
      "installCmd": ["pip", "install", "-r", "requirements.txt"]
    }
  ]
}
```

**Branch with extras**:
```json
{
  "branches": {
    "ml-experiment": {
      "exists": true,
      "path": "/path/to/ml-experiment",
      "packageManagers": {
        "pip": {
          "installCmd": ["sh", "-c", "pip install -r requirements.txt -r requirements-ml.txt"]
        }
      }
    }
  }
}
```

## How It Works

1. When you run `git hop add <branch>`, Git Hop:
   - Detects package managers in the worktree
   - Resolves the install command using the hierarchy
   - Runs the resolved command to install dependencies
   - Creates symlinks to shared dependency storage

2. The resolution happens in this order:
   ```
   Branch Override → Repo Override → Global Config
   ```

3. Only the `installCmd` can be overridden at repo/branch level. Other properties (detectFiles, lockFiles, depsDir) are inherited from the global package manager definition.

## Built-in Package Managers

Git Hop includes these built-in package managers:

| Name | Detect Files | Lock Files | Deps Dir | Install Command |
|------|--------------|------------|----------|-----------------|
| npm | package.json | package-lock.json | node_modules | `npm ci` |
| pnpm | pnpm-lock.yaml | pnpm-lock.yaml | node_modules | `pnpm install --frozen-lockfile` |
| yarn | yarn.lock | yarn.lock | node_modules | `yarn install --frozen-lockfile` |
| go | go.mod | go.sum | vendor | `go mod download && go mod vendor` |
| pip | requirements.txt | requirements.txt | venv | `pip install -r requirements.txt` |
| cargo | Cargo.toml | Cargo.lock | target | `cargo fetch` |
| composer | composer.json | composer.lock | vendor | `composer install --no-dev` |
| bundler | Gemfile | Gemfile.lock | vendor/bundle | `bundle install --deployment` |

## Commands Affected

Package manager overrides affect these commands:

- `git hop add <branch>` - Uses resolved install command during worktree creation
- `git hop env start` - Uses resolved install command when ensuring dependencies
- `git hop deps fix` - Uses resolved install command when fixing dependency issues
- `git hop deps audit` - Reports using resolved configuration

## Tips

1. **Start with global config** - Define common package managers globally
2. **Override at repo level** - When all branches need the same special handling
3. **Override at branch level** - Only when specific branches need different behavior
4. **Use shell commands** - For complex installations, wrap in shell:
   ```json
   {
     "installCmd": ["sh", "-c", "npm ci && npm run postinstall"]
   }
   ```
5. **Validate commands exist** - Git Hop validates that the first command in `installCmd` exists in PATH

## Troubleshooting

### Override not working?

1. Check the hop.json syntax is valid JSON
2. Ensure package manager name matches exactly (case-sensitive)
3. Verify the command exists in PATH
4. Check precedence - branch overrides repo, repo overrides global

### Where is hop.json located?

The hopspace configuration is at:
```
$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json
```

Default `$GIT_HOP_DATA_HOME` is `~/.local/share/git-hop/`

### How to see what command will be used?

Currently, Git Hop silently resolves the command. A future version may add:
```bash
git hop deps show-config <branch>
```

## API Reference

### PackageManagerOverride Structure

```go
type PackageManagerOverride struct {
    InstallCmd []string `json:"installCmd,omitempty"`
}
```

### Configuration Locations

```
~/.config/git-hop/global.json          # Global config
$GIT_HOP_DATA_HOME/<org>/<repo>/hop.json   # Hopspace config
```

### Example Full Configuration

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
      "path": "/Users/user/.local/share/git-hop/myorg/myrepo/main",
      "lastSync": "2024-01-15T10:30:00Z"
    },
    "feature-x": {
      "exists": true,
      "path": "/Users/user/.local/share/git-hop/myorg/myrepo/feature-x",
      "lastSync": "2024-01-15T10:35:00Z",
      "packageManagers": {
        "npm": {
          "installCmd": ["npm", "install", "--force"]
        }
      }
    }
  }
}
```
