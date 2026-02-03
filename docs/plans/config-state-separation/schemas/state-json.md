# State Schema

## Location
`$XDG_STATE_HOME/git-hop/state.json`

## Purpose
Repository tracking, hub locations, worktree paths.

## Full Schema

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "git-hop state - tracks repositories and their locations",
  
  "version": "1.0.0",
  "lastUpdated": "2026-02-02T19:45:00Z",
  
  "repositories": {
    "github.com/jadb/git-hop": {
      "uri": "git@github.com:jadb/git-hop.git",
      "org": "jadb",
      "repo": "git-hop",
      "defaultBranch": "main",
      
      "worktrees": {
        "main": {
          "path": "/Users/jadb/code/git-hop",
          "type": "bare",
          "hubPath": "/Users/jadb/code/git-hop",
          "createdAt": "2026-01-15T10:30:00Z",
          "lastAccessed": "2026-02-02T19:45:00Z"
        },
        "feature-x": {
          "path": "/Users/jadb/code/git-hop/hops/feature-x",
          "type": "linked",
          "hubPath": "/Users/jadb/code/git-hop",
          "createdAt": "2026-01-20T14:15:00Z",
          "lastAccessed": "2026-02-02T18:20:00Z"
        }
      },
      
      "hubs": [
        {
          "path": "/Users/jadb/code/git-hop",
          "mode": "local",
          "createdAt": "2026-01-15T10:30:00Z",
          "lastAccessed": "2026-02-02T19:45:00Z"
        },
        {
          "path": "/Users/jadb/work/git-hop-fork",
          "mode": "local",
          "createdAt": "2026-02-01T14:20:00Z",
          "lastAccessed": "2026-02-02T15:30:00Z"
        }
      ],
      
      "globalHopspace": {
        "enabled": false,
        "path": null
      }
    },
    
    "github.com/user/project": {
      "uri": "git@github.com:user/project.git",
      "org": "user",
      "repo": "project",
      "defaultBranch": "main",
      
      "worktrees": {},
      
      "hubs": [],
      
      "globalHopspace": {
        "enabled": true,
        "path": "${XDG_DATA_HOME}/git-hop/user/project"
      }
    }
  },
  
  "orphaned": [
    {
      "path": "/tmp/old-project",
      "detectedAt": "2026-01-20T08:00:00Z",
      "reason": "hub directory no longer exists"
    }
  ]
}
```

## Field Descriptions

### Root

version: Schema version.
- Used for migration compatibility
- Format: MAJOR.MINOR.PATCH

lastUpdated: Last modification timestamp.
- ISO 8601 format
- Updated on every write

### repositories

Map of repository ID to repository state.

**Repository ID format:** `{domain}/{org}/{repo}`

Example: github.com/jadb/git-hop

#### uri

Full Git URI.
- Format: git@domain:org/repo.git
- Parse for org/repo extraction

#### org

Organization or user name.
- Extracted from URI
- Used for directory structure

#### repo

Repository name.
- Extracted from URI
- Used for directory structure

#### defaultBranch

Default branch name.
- Usually main or master
- Determined from git remote

#### worktrees

Map of branch to worktree state.

**Key:** Branch name

**Fields:**

path: Absolute path to worktree directory.
- Example: /Users/jadb/code/git-hop/worktrees/feature-x
- Verified on access
- Updated if moved

type: Worktree classification.
- bare: Main repository (bare repo)
- linked: Git worktree

hubPath: Path to hub containing worktree.
- Used for git commands
- Parent directory

createdAt: Worktree creation timestamp.
- ISO 8601 format
- Set on first registration

lastAccessed: Last access timestamp.
- ISO 8601 format
- Updated on every access

#### hubs

Array of hub locations for repository.

**Fields:**

path: Absolute path to hub directory.
- Used for git worktree list
- Bare repo location

mode: Hub configuration mode.
- local: Hopspace in hub directory
- global: Hopspace in $XDG_DATA_HOME

createdAt: Hub creation timestamp.
- ISO 8601 format

lastAccessed: Last hub access timestamp.
- ISO 8601 format
- Updated on every hub access

#### globalHopspace

Global hopspace configuration.

**Fields:**

enabled: Global hopspace active.
- true: Using $XDG_DATA_HOME hopspace
- false: Using local hopspace

path: Global hopspace path.
- Variable expansion: ${XDG_DATA_HOME}
- Absolute path when enabled

### orphaned

Array of detected orphaned entries.

**Fields:**

path: Path to orphaned artifact.
- Directory or file
- No longer referenced

detectedAt: Detection timestamp.
- ISO 8601 format
- When orphan was detected

reason: Orphan detection reason.
- Example: hub directory no longer exists
- User-friendly message
