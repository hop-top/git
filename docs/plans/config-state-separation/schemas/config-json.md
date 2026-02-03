# Config Schema

## Location
`$XDG_CONFIG_HOME/git-hop/config.json`

## Purpose
User preferences, tool settings, global configuration.

## Full Schema

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "git-hop global configuration - user preferences and tool settings",
  
  "defaults": {
    "gitDomain": "github.com",
    "bareRepo": true,
    "autoEnvStart": false,
    "editor": "${EDITOR}",
    "shell": "${SHELL}"
  },
  
  "output": {
    "format": "human",
    "colorScheme": "auto",
    "verbose": false,
    "quiet": false
  },
  
  "ports": {
    "allocationMode": "hash",
    "baseRange": {
      "start": 10000,
      "end": 15000
    }
  },
  
  "volumes": {
    "basePath": "${XDG_DATA_HOME}/git-hop/volumes",
    "cleanup": {
      "onRemove": true,
      "orphanedAfterDays": 30
    }
  },
  
  "hooks": {
    "preWorktreeAdd": null,
    "postWorktreeAdd": null,
    "preEnvStart": null,
    "postEnvStart": null,
    "preEnvStop": null,
    "postEnvStop": null
  },
  
  "doctor": {
    "autoFix": false,
    "checksEnabled": [
      "worktreeState",
      "configConsistency",
      "orphanedDirectories",
      "gitMetadata"
    ]
  }
}
```

## Field Descriptions

### defaults

gitDomain: Default Git hosting domain.
- Used for shorthand: org/repo → git@github.com:org/repo.git
- Default: github.com

bareRepo: Use bare repo structure.
- Recommended: true
- Creates bare .git + worktree directories

autoEnvStart: Auto-start Docker env on worktree entry.
- Default: false

editor: Preferred editor.
- Variable expansion: ${EDITOR}

shell: Preferred shell.
- Variable expansion: ${SHELL}

### output

format: Output format.
- Options: human, json, porcelain
- Default: human

colorScheme: Color mode.
- Options: auto, always, never
- Default: auto

verbose: Enable verbose logging.
- Default: false

quiet: Suppress non-essential output.
- Default: false

### ports

allocationMode: Port allocation strategy.
- Options: hash, sequential, random
- Default: hash

baseRange: Port range.
- start: Lower bound
- end: Upper bound

### volumes

basePath: Root for Docker volumes.
- Variable expansion: ${XDG_DATA_HOME}

cleanup: Volume cleanup settings.
- onRemove: Remove volumes on worktree removal
- orphanedAfterDays: Cleanup threshold

### hooks

preWorktreeAdd: Hook path.
- Executed before worktree creation.
- null: no hook

postWorktreeAdd: Hook path.
- Executed after worktree creation.
- null: no hook

preEnvStart: Hook path.
- Executed before Docker start.
- null: no hook

postEnvStart: Hook path.
- Executed after Docker start.
- null: no hook

preEnvStop: Hook path.
- Executed before Docker stop.
- null: no hook

postEnvStop: Hook path.
- Executed after Docker stop.
- null: no hook

### doctor

autoFix: Auto-fix issues.
- Default: false

checksEnabled: Health checks to run.
- worktreeState: Validate worktree state
- configConsistency: Check config alignment
- orphanedDirectories: Find orphaned artifacts
- gitMetadata: Validate git worktree metadata
