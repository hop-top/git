#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

create_skeleton() {
    local target="$1"
    local repo="$target/git-sample"
    
    echo "Creating directory structure..."
    
    mkdir -p "$repo/socket-server/cmd"
    mkdir -p "$repo/services/worker"
    mkdir -p "$repo/services/scheduler"
    mkdir -p "$repo/api/app"
    mkdir -p "$repo/frontend/src"
    mkdir -p "$repo/database/sqlite"
    mkdir -p "$repo/scripts"
    
    cat > "$repo/socket-server/main.go" <<'EOF'
package main

import "fmt"

func main() {
    fmt.Println("Socket server v1.0.0")
}
EOF

    cat > "$repo/socket-server/go.mod" <<'EOF'
module github.com/hop-top/git-sample/socket-server

go 1.21
EOF

    cat > "$repo/socket-server/go.sum" <<'EOF'
github.com/gorilla/websocket v1.5.0 h1:0RrjyDigidhe9qPsP/t7X2CK/03uHrHDGJiQbFAp9g8M3x6tKd0Qw3U=
EOF

    cat > "$repo/services/worker/main.py" <<'EOF'
print("Worker v1.0.0")
EOF

    cat > "$repo/services/scheduler/main.py" <<'EOF'
print("Scheduler v1.0.0")
EOF

    cat > "$repo/services/requirements.txt" <<'EOF'
redis==4.5.1
requests==2.28.1
EOF

    cat > "$repo/api/composer.json" <<'EOF'
{
  "name": "hop-top/git-sample-api",
  "require": {
    "php": "^8.1",
    "laravel/framework": "^10.0"
  }
}
EOF

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

    cat > "$repo/.gitignore" <<'EOF'
.env.secrets
venv/
node_modules/
vendor/
*.db
EOF

    echo "✓ Created directory structure"
}

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

main() {
    local target=""
    local dry_run=false
    
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)
                usage
                exit 0
                ;;
            --dry-run)
                dry_run=true
                shift
                ;;
            *)
                if [ -z "$target" ]; then
                    target="$1"
                    shift
                fi
                ;;
        esac
    done
    
    if [ -z "$target" ]; then
        usage
        exit 1
    fi
    
    check_dependencies
    
    if [ "$dry_run" = true ]; then
        echo "DRY RUN - Would create repo at: $target"
        echo "  → Directory structure"
        echo "  → Git repository"
        echo "  → GitHub repo: hop-top/git-sample"
        exit 0
    fi
    
    create_skeleton "$target"
    create_git_repo "$target"
    
    echo ""
    echo "Demo repository ready at: $target/git-sample"
    echo "GitHub: https://github.com/hop-top/git-sample"
    echo ""
    echo "Next: Run setup-demo-worktrees.sh"
}

main "$@"
