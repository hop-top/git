# Implementation Guide

## Overview

Two shell scripts create demo repo:
- `create-demo-repo.sh` - Initial setup
- `setup-demo-worktrees.sh` - Worktree creation

## Script 1: create-demo-repo.sh

### Location

`[create-demo-repo.sh](scripts/create-demo-repo.sh)`

### Requirements

- git (for git init, commit)
- gh CLI (authenticated)
- Write permissions to target directory

### Functions

#### usage()

Display help message with arguments.

```bash
usage() {
    cat <<EOF
Usage: $0 <target-directory>

Creates git-hop demo repository.

Arguments:
  target-directory  Parent directory where git-sample will be created

Options:
  -h, --help     Show this help message
  --dry-run        Show what would be created without creating

Example:
  $0 ~/workspace
  # Creates ~/workspace/git-sample
EOF
}
```

#### check_dependencies()

Verify required tools in PATH.

```bash
check_dependencies() {
    local missing=()
    
    for cmd in git gh; do
        if ! command -v $cmd &> /dev/null; then
            missing+=($cmd)
        fi
    done
    
    if [ ${#missing[@]} -gt 0 ]; then
        echo "Error: Required tools not found: ${missing[*]}"
        exit 1
    fi
}
```

#### create_skeleton()

Create directory structure for each service.

```bash
create_skeleton() {
    local target="$1"
    local repo="$target/git-sample"
    
    # Create directories
    mkdir -p "$repo/socket-server/cmd"
    mkdir -p "$repo/services/worker"
    mkdir -p "$repo/services/scheduler"
    mkdir -p "$repo/api/app"
    mkdir -p "$repo/frontend/src"
    mkdir -p "$repo/database/sqlite"
    mkdir -p "$repo/scripts"
    
    # Go socket server
    cat > "$repo/socket-server/main.go" <<'EOF'
package main

import "fmt"

func main() {
    fmt.Println("Socket server v1.0.0")
}
EOF

    # Python services
    cat > "$repo/services/worker/main.py" <<'EOF'
print("Worker v1.0.0")
EOF

    cat > "$repo/services/scheduler/main.py" <<'EOF'
print("Scheduler v1.0.0")
EOF

    # PHP Laravel API
    cat > "$repo/api/composer.json" <<'EOF'
{
  "name": "hop-top/git-sample-api",
  "require": {
    "php": "^8.1",
    "laravel/framework": "^10.0"
  }
}
EOF

    # React frontend
    cat > "$repo/frontend/package.json" <<'EOF'
{
  "name": "git-sample-frontend",
  "version": "1.0.0",
  "dependencies": {
    "react": "^18.2.0",
    "react-dom": "^18.2.0"
  }
}
EOF

    # Docker Compose
    cat > "$repo/docker-compose.yml" <<'EOF'
version: "3.8"

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: githopdemo
      POSTGRES_USER: demo
      POSTGRES_PASSWORD: demo
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "${HOP_PORT_POSTGRES:-5432}:5432"

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data
    ports:
      - "${HOP_PORT_REDIS:-6379}:6379"

volumes:
  postgres_data:
    name: ${HOP_VOLUME_POSTGRES_DATA:-postgres_data}
  redis_data:
    name: ${HOP_VOLUME_REDIS_DATA:-redis_data}
EOF

    # Environment hooks
    cat > "$repo/scripts/env-prestart.sh" <<'EOF'
#!/bin/bash
echo "Loading demo secrets..."
echo "DB_PASSWORD=demo123" > .env.secrets
EOF

    cat > "$repo/scripts/env-poststart.sh" <<'EOF'
#!/bin/bash
echo "Running database migrations..."
sleep 2
echo "Migrations complete"
EOF

    chmod +x "$repo/scripts/env-prestart.sh"
    chmod +x "$repo/scripts/env-poststart.sh"

    # Gitignore
    cat > "$repo/.gitignore" <<'EOF'
.env.secrets
venv/
node_modules/
vendor/
*.db
EOF

    echo "✓ Created directory structure"
}
```

#### create_git_repo()

Initialize git repository with remote.

```bash
create_git_repo() {
    local repo="$1"
    
    cd "$repo"
    
    echo "Initializing git repository..."
    
    git init -b main
    git config user.name "Git Hop Demo"
    git config user.email "demo@git-hop.dev"
    
    echo "Committing initial structure..."
    git add .
    git commit -m "feat: initial demo repo structure"
    
    echo "Creating GitHub repository..."
    gh repo create hop-top/git-sample \
        --private \
        --remote origin \
        --push \
        --source . \
        --description "Git Hop demo repository"
    
    echo "✓ Repository created: https://github.com/hop-top/git-sample"
}
```

## Script 2: setup-demo-worktrees.sh

### Location

`[setup-demo-worktrees.sh](scripts/setup-demo-worktrees.sh)`

### Requirements

- git-hop installed
- GitHub authentication (for clone)

### Functions

#### usage()

Display help with workspace argument.

```bash
usage() {
    cat <<EOF
Usage: $0 <workspace-directory>

Sets up git-hop demo worktrees with different lockfile versions.

Arguments:
  workspace-directory  Directory where to clone demo repo

Options:
  -h, --help     Show this help message
  --dry-run        Show what would be created without creating

Example:
  $0 ~/workspace
  # Clones to ~/workspace/git-hop-sample
  # Creates worktrees in .git-hops/
EOF
}
```

#### check_git_hop()

Verify git-hop installation.

```bash
check_git_hop() {
    if ! command -v git-hop &> /dev/null; then
        echo "Error: git-hop not found in PATH"
        echo "Install: go install github.com/jadb/git-hop@latest"
        exit 1
    fi
}
```

#### clone_repo()

Clone demo repository using git-hop.

```bash
clone_repo() {
    local workspace="$1"
    local target="$workspace/git-hop-sample"
    
    echo "Cloning demo repository..."
    
    cd "$workspace"
    
    git-hop clone hop-top/git-sample git-hop-sample
    cd git-hop-sample
}
```

#### create_worktrees()

Create worktrees with different lockfile states.

```bash
create_worktrees() {
    local worktree_name="$1"
    local repo=".git/hop/git-hop-sample"
    
    cd "$repo"
    
    # Worktree 1: bug/same-lockfile
    echo "  Creating worktree: bug/same-lockfile"
    git-hop add "$worktree_name"
    cd ".git-hop/hops/$worktree_name"
    
    # Lockfiles identical to main (no changes)
    git commit --allow-empty -m "fix: socket connection timeout"
    git push -u origin "$worktree_name"
    
    cd "../.."
    
    # Worktree 2: fix/same-lockfile
    echo "  Creating worktree: fix/same-lockfile"
    git-hop add "$worktree_name"
    cd ".git-hop/hops/$worktree_name"
    
    # Lockfiles identical to main (no changes)
    git commit --allow-empty -m "fix: redis connection pool leak"
    git push -u origin "$worktree_name"
    
    cd "../.."
    
    # Worktree 3: feat/diff-lockfile
    echo "  Creating worktree: feat/diff-lockfile"
    git-hop add "$worktree_name"
    cd ".git-hop/hops/$worktree_name"
    
    # Bump frontend dependency
    cd "../frontend"
    npm install axios@1.6.0
    cd ".."
    
    git add frontend/package-lock.json services/requirements.txt
    git commit -m "feat: add axios and celery dependencies"
    git push -u origin "$worktree_name"
    
    cd "$repo"
    
    # Worktree 4: ci/another-lockfile
    echo "  Creating worktree: ci/another-lockfile"
    git-hop add "$worktree_name"
    cd ".git-hop/hops/$worktree_name"
    
    # Bump Go dependency
    cd "../socket-server"
    go get github.com/gorilla/websocket@v1.5.1
    go mod tidy
    cd ".."
    
    # Bump PHP dependency
    cd "../api"
    composer require laravel/sanctum:^3.4
    cd ".."
    
    git add socket-server/go.mod socket-server/go.sum api/composer.lock
    git commit -m "ci: update websocket and sanctum versions"
    git push -u origin "$worktree_name"
    
    cd "../.."
}
```

#### show_dependency_sharing()

Display expected dependency sharing structure.

```bash
show_dependency_sharing() {
    echo ""
    echo "=== Expected Dependency Sharing ==="
    echo ""
    echo "After setup, deps structure should be:"
    echo ""
    echo "\$GIT_HOP_DATA_HOME/hop-top/git-sample/deps/"
    echo "├── node_modules.abc123/     # Used by: main, bug, fix"
    echo "├── node_modules.def456/     # Used by: feat"
    echo "├── vendor.789abc/          # Used by: main, bug, fix"
    echo "├── vendor.xyz999/           # Used by: ci"
    echo "├── venv.ghi789/            # Used by: main, bug, fix"
    echo "├── venv.jkl012/             # Used by: feat"
    echo "└── .registry.json"
    echo ""
    echo "=== Sharing Summary ==="
    echo "node_modules.abc123 (main, bug, fix): 234MB"
    echo "node_modules.def456 (feat): 234MB"
    echo "Total disk usage: 582MB"
    echo "Without sharing: 1746MB (3.3x savings)"
    echo ""
    echo "All worktrees ready at: $workspace/git-hop-sample"
}
```

## Related

- [Problem & Solution](../problem-solution.md)
- [Demo Structure](../demo-structure.md)
- [Testing Plan](../testing-plan.md)
- [Diagrams](../diagrams/README.md)
