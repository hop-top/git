# Demo Repo Generator Scripts

## Problem

Need reproducible demo repo showcasing git-hop features:
- Multi-language project (Go, Python, PHP, React)
- Docker services (postgres, redis)
- Multiple worktrees with different lockfile versions
- Demonstrates dependency sharing across worktrees

## Solution

Two shell scripts:
1. `create-demo-repo.sh` - Creates and pushes skeleton repo
2. `setup-demo-worktrees.sh` - Clones with git-hop and creates worktrees

## Demo Repo Structure

```
git-sample/
├── .git/
├── .gitignore
├── docker-compose.yml
├── README.md
├── socket-server/          # Go service
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   └── cmd/
├── services/               # Python microservices
│   ├── requirements.txt
│   ├── venv/
│   ├── worker/
│   └── scheduler/
├── api/                    # PHP Laravel
│   ├── composer.json
│   ├── composer.lock
│   ├── artisan
│   └── app/
├── frontend/               # React
│   ├── package.json
│   ├── package-lock.json
│   ├── src/
│   └── public/
├── database/
│   ├── sqlite/
│   │   └── app.db
│   └── migrations/
└── scripts/
    ├── env-prestart.sh
    └── env-poststart.sh
```

### Docker Compose Services

```yaml
version: '3.8'
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
```

## Script 1: Create Demo Repo

### Location
`scripts/create-demo-repo.sh`

### Requirements
- git
- gh CLI (authenticated)
- Write permissions to target directory

### Usage
```bash
./scripts/create-demo-repo.sh /path/to/parent/dir
```

### Flow

1. **Create directory structure**
   - `mkdir -p /path/to/parent/dir/git-sample`
   - `cd /path/to/parent/dir/git-sample`

2. **Initialize git**
   - `git init -b main`
   - `git config user.name "Git Hop Demo"`
   - `git config user.email "demo@git-hop.dev"`

3. **Create skeleton files**

   **Go socket server:**
   ```go
   // socket-server/main.go
   package main

   import "fmt"

   func main() {
       fmt.Println("Socket server v1.0.0")
   }
   ```

   ```go.mod
   // socket-server/go.mod
   module github.com/hop-top/git-sample/socket-server

   go 1.21

   require (
       github.com/gorilla/websocket v1.5.0
   )
   ```

   **Python services:**
   ```python
   # services/worker/main.py
   print("Worker v1.0.0")
   ```

   ```
   # services/requirements.txt
   redis==4.5.1
   requests==2.28.1
   ```

   **PHP Laravel API:**
   ```json
   {
     "name": "hop-top/git-sample-api",
     "require": {
       "php": "^8.1",
       "laravel/framework": "^10.0"
     }
   }
   ```

   **React frontend:**
   ```json
   {
     "name": "git-sample-frontend",
     "version": "1.0.0",
     "dependencies": {
       "react": "^18.2.0",
       "react-dom": "^18.2.0"
     }
   }
   ```

   **Docker Compose:**
   - Create `docker-compose.yml` as shown above

   **Env hooks:**
   ```bash
   #!/bin/bash
   # scripts/env-prestart.sh
   echo "Loading demo secrets..."
   echo "DB_PASSWORD=demo123" > .env.secrets
   ```

   ```bash
   #!/bin/bash
   # scripts/env-poststart.sh
   echo "Running database migrations..."
   sleep 2
   echo "Migrations complete"
   ```

   **Gitignore:**
   ```
   .env.secrets
   venv/
   node_modules/
   vendor/
   *.db
   ```

   **README:**
   ```markdown
   # Git Hop Demo Repository

   Multi-service application demonstrating git-hop features.

   ## Services
   - Socket Server (Go)
   - Worker & Scheduler (Python)
   - API (PHP Laravel)
   - Frontend (React)
   - Database (PostgreSQL + Redis + SQLite)

   ## Usage
   See setup-demo-worktrees.sh
   ```

4. **Install initial dependencies**
   ```bash
   cd socket-server && go mod download && cd ..
   cd services && pip install -r requirements.txt && cd ..
   cd api && composer install && cd ..
   cd frontend && npm install && cd ..
   ```

5. **Git commit**
   ```bash
   git add .
   git commit -m "feat: initial demo repo structure

   - Go socket server
   - Python services (worker, scheduler)
   - PHP Laravel API
   - React frontend
   - Docker compose (postgres, redis)
   - Environment hooks"
   ```

6. **Create GitHub repo and push**
   ```bash
   gh repo create hop-top/git-sample \
     --private \
     --remote origin \
     --push \
     --source . \
     --description "Git Hop demo repository"
   ```

### Output

```
Creating demo repository structure...
  ✓ Created directory: /path/to/parent/dir/git-sample
  ✓ Initialized git repository
  ✓ Created Go socket server skeleton
  ✓ Created Python services skeleton
  ✓ Created PHP Laravel API skeleton
  ✓ Created React frontend skeleton
  ✓ Created Docker Compose config
  ✓ Created environment hooks
  ✓ Installing dependencies...
    - Go modules: 5 packages
    - Python packages: 12 packages
    - Composer packages: 47 packages
    - NPM packages: 1,234 packages
  ✓ Committed initial structure
  ✓ Created GitHub repository: hop-top/git-sample
  ✓ Pushed to origin/main

Demo repository ready at: /path/to/parent/dir/git-sample
GitHub: https://github.com/hop-top/git-sample
```

## Script 2: Setup Demo Worktrees

### Location
`scripts/setup-demo-worktrees.sh`

### Requirements
- git-hop installed and in PATH
- GitHub authentication (for clone)
- Write permissions to target directory

### Usage
```bash
./scripts/setup-demo-worktrees.sh /path/to/workspace
```

### Flow

1. **Clone with git-hop**
   ```bash
   cd /path/to/workspace
   git hop clone hop-top/git-sample git-hop-sample
   cd git-hop-sample
   ```

2. **Create worktree: bug/same-lockfile**
   ```bash
   git hop add bug/same-lockfile
   cd .git/hop/hops/bug-same-lockfile

   # Lockfiles identical to main (no changes)
   git commit --allow-empty -m "fix: socket connection timeout"
   git push -u origin bug/same-lockfile

   cd ../../../../
   ```

3. **Create worktree: fix/same-lockfile**
   ```bash
   git hop add fix/same-lockfile
   cd .git/hop/hops/fix-same-lockfile

   # Lockfiles identical to main (no changes)
   git commit --allow-empty -m "fix: redis connection pool leak"
   git push -u origin fix/same-lockfile

   cd ../../../../
   ```

4. **Create worktree: feat/diff-lockfile**
   ```bash
   git hop add feat/diff-lockfile
   cd .git/hop/hops/feat-diff-lockfile

   # Bump frontend dependency
   cd frontend
   npm install axios@1.6.0
   cd ..

   # Bump Python dependency
   cd services
   echo "celery==5.3.4" >> requirements.txt
   pip install celery==5.3.4
   cd ..

   git add frontend/package-lock.json services/requirements.txt
   git commit -m "feat: add axios and celery dependencies"
   git push -u origin feat/diff-lockfile

   cd ../../../../
   ```

5. **Create worktree: ci/another-lockfile**
   ```bash
   git hop add ci/another-lockfile
   cd .git/hop/hops/ci-another-lockfile

   # Bump Go dependency
   cd socket-server
   go get github.com/gorilla/websocket@v1.5.1
   go mod tidy
   cd ..

   # Bump PHP dependency
   cd api
   composer require laravel/sanctum:^3.3
   cd ..

   git add socket-server/go.mod socket-server/go.sum api/composer.lock
   git commit -m "ci: update websocket and sanctum versions"
   git push -u origin ci/another-lockfile

   cd ../../../../
   ```

6. **Show dependency sharing status**
   ```bash
   git hop doctor
   ```

### Expected Dependency Sharing

After setup, deps structure should be:

```
$GIT_HOP_DATA_HOME/hop-top/git-sample/deps/
├── node_modules.abc123/     # Used by: main, bug/same-lockfile, fix/same-lockfile
├── node_modules.def456/     # Used by: feat/diff-lockfile
├── vendor.789abc/           # Used by: main, bug/same-lockfile, fix/same-lockfile, feat/diff-lockfile
├── vendor.xyz999/           # Used by: ci/another-lockfile
├── venv.ghi789/             # Used by: main, bug/same-lockfile, fix/same-lockfile
├── venv.jkl012/             # Used by: feat/diff-lockfile
└── .registry.json
```

**Sharing summary:**
- 3 worktrees share same `node_modules` (main, bug, fix)
- 1 worktree has different `node_modules` (feat)
- 1 worktree has unique `node_modules` + `vendor` (ci)

### Output

```
Setting up git-hop demo worktrees...
  ✓ Cloned hop-top/git-sample → git-hop-sample
  ✓ Created worktree: bug/same-lockfile
    → Sharing deps with main (lockfiles identical)
  ✓ Created worktree: fix/same-lockfile
    → Sharing deps with main (lockfiles identical)
  ✓ Created worktree: feat/diff-lockfile
    → Installing new deps (lockfile changed)
    → node_modules: 234MB (new version)
    → venv: 12MB (new version)
  ✓ Created worktree: ci/another-lockfile
    → Installing new deps (lockfile changed)
    → vendor: 45MB (new version)

Dependency Sharing Summary:
  node_modules.abc123 (main, bug, fix): 234MB
  node_modules.def456 (feat): 234MB
  vendor.789abc (main, bug, fix, feat): 45MB
  vendor.xyz999 (ci): 45MB
  venv.ghi789 (main, bug, fix): 12MB
  venv.jkl012 (feat): 12MB

Total disk usage: 582MB
Without sharing: 1,746MB (3x savings)

All worktrees ready at: /path/to/workspace/git-hop-sample
```

## Implementation

### Task List

- [ ] Create `scripts/create-demo-repo.sh`
- [ ] Create `scripts/setup-demo-worktrees.sh`
- [ ] Create skeleton templates for each language
- [ ] Add error handling (check command availability)
- [ ] Add cleanup on failure
- [ ] Make paths configurable via arguments
- [ ] Add `--help` flag to scripts
- [ ] Add dry-run mode (`--dry-run`)
- [ ] Test on macOS
- [ ] Test on Linux
- [ ] Update main README with demo instructions

### Script Structure

#### create-demo-repo.sh

```bash
#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
    cat <<EOF
Usage: $0 <target-directory>

Creates a demo multi-service repository for git-hop.

Arguments:
  target-directory  Parent directory where git-sample will be created

Options:
  -h, --help       Show this help message
  --dry-run        Show what would be created without creating it

Example:
  $0 ~/workspace
  # Creates ~/workspace/git-sample
EOF
}

check_dependencies() {
    for cmd in git gh go python3 pip composer npm; do
        if ! command -v $cmd &> /dev/null; then
            echo "Error: $cmd not found in PATH"
            exit 1
        fi
    done
}

create_skeleton() {
    local target="$1"
    # ... implementation
}

install_dependencies() {
    # ... implementation
}

main() {
    # ... implementation
}

main "$@"
```

#### setup-demo-worktrees.sh

```bash
#!/bin/bash
set -e

usage() {
    cat <<EOF
Usage: $0 <workspace-directory>

Sets up git-hop demo worktrees with different lockfile versions.

Arguments:
  workspace-directory  Directory where to clone the demo repo

Options:
  -h, --help          Show this help message
  --dry-run           Show what would be created without creating it

Example:
  $0 ~/workspace
  # Clones to ~/workspace/git-hop-sample
EOF
}

check_dependencies() {
    if ! command -v git-hop &> /dev/null; then
        echo "Error: git-hop not found in PATH"
        echo "Install from: https://github.com/jadb/git-hop"
        exit 1
    fi
}

create_worktrees() {
    # ... implementation
}

show_summary() {
    # ... implementation
}

main() {
    # ... implementation
}

main "$@"
```

## Testing Plan

### Manual Testing

1. **Clean environment test:**
   ```bash
   # Start fresh
   rm -rf /tmp/demo-test

   # Run scripts
   ./scripts/create-demo-repo.sh /tmp/demo-test
   ./scripts/setup-demo-worktrees.sh /tmp/demo-test

   # Verify
   cd /tmp/demo-test/git-hop-sample
   git hop doctor
   git hop env start
   ```

2. **Dependency sharing verification:**
   ```bash
   # Check symlinks
   ls -la .git/hop/hops/*/node_modules

   # Check registry
   cat $GIT_HOP_DATA_HOME/hop-top/git-sample/deps/.registry.json

   # Check disk usage
   du -sh $GIT_HOP_DATA_HOME/hop-top/git-sample/deps/*
   ```

3. **Environment manager test:**
   ```bash
   # Start in each worktree
   cd .git/hop/hops/main
   git hop env start
   docker compose ps

   cd ../bug-same-lockfile
   git hop env start
   docker compose ps  # Should use different ports
   ```

### Automated Testing

Add to `test/e2e/demo_test.go`:

```go
func TestDemoRepoCreation(t *testing.T) {
    // Run create-demo-repo.sh
    // Verify directory structure
    // Verify git commits
    // Verify GitHub repo created
}

func TestDemoWorktreeSetup(t *testing.T) {
    // Run setup-demo-worktrees.sh
    // Verify worktrees created
    // Verify dependency sharing
    // Verify lockfile versions
}
```

## Edge Cases

### GitHub Auth Not Configured

```bash
if ! gh auth status &> /dev/null; then
    echo "Error: GitHub CLI not authenticated"
    echo "Run: gh auth login"
    exit 1
fi
```

### Target Directory Exists

```bash
if [ -d "$TARGET/git-sample" ]; then
    read -p "Directory exists. Overwrite? [y/N] " -n 1 -r
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
    rm -rf "$TARGET/git-sample"
fi
```

### Dependency Install Fails

```bash
install_npm_deps() {
    if ! npm install; then
        echo "Warning: npm install failed"
        echo "Continuing anyway (lockfile still valid)"
    fi
}
```

### Git-hop Not Installed

```bash
if ! command -v git-hop &> /dev/null; then
    echo "Error: git-hop not found"
    echo "Install: go install github.com/jadb/git-hop@latest"
    exit 1
fi
```

## Benefits

- **Reproducible demos:** Same setup every time
- **Feature showcase:** Demonstrates all git-hop capabilities
- **Testing fixture:** Use for integration tests
- **Documentation:** Living example in docs
- **Onboarding:** New users can try git-hop immediately

## Future Enhancements

- Interactive mode (choose languages/services)
- Parameterized lockfile versions
- Add GitHub Actions workflow to repo
- Create issues/PRs in demo repo automatically
- Video walkthrough using these scripts
