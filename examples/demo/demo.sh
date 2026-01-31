#!/usr/bin/env bash
#
# git-hop Demo Script - Split Console Story with Event Synchronization
#
# This script demonstrates git-hop's power through a realistic collaboration scenario:
# - Author (left pane): Library maintainer working on next version
# - Contributor (right pane): Outside collaborator submitting a bug fix
#
# Features:
# - Event-driven synchronization between roles
# - Docker services (web/db/cache) to show environment isolation
# - Realistic git-hop workflows
#
# Requirements:
# - tmux
# - git-hop (built and in PATH)
# - docker and docker-compose
#
# Usage:
#   ./demo.sh
#

set -e

# Configuration
DEMO_REPO_NAME="awesome-app"
AUTHOR_WORKSPACE="/tmp/git-hop-demo-author"
CONTRIBUTOR_WORKSPACE="/tmp/git-hop-demo-contributor"
UPSTREAM_REPO="/tmp/git-hop-demo-upstream"
CONTRIBUTOR_FORK="/tmp/git-hop-demo-fork"
SESSION_NAME="git-hop-demo"
EVENT_DIR="/tmp/git-hop-demo-events"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Event helpers
wait_for_event() {
    local event="$1"
    local fifo="${EVENT_DIR}/${event}"
    cat "$fifo" > /dev/null
}

trigger_event() {
    local event="$1"
    local fifo="${EVENT_DIR}/${event}"
    echo "1" > "$fifo"
}

# Clean up from previous runs
cleanup() {
    echo "Cleaning up previous demo runs..."
    rm -rf "$AUTHOR_WORKSPACE" "$CONTRIBUTOR_WORKSPACE" "$UPSTREAM_REPO" "$CONTRIBUTOR_FORK" "$EVENT_DIR"
    tmux kill-session -t "$SESSION_NAME" 2>/dev/null || true
}

# Check dependencies
check_deps() {
    local missing=()
    
    command -v tmux >/dev/null 2>&1 || missing+=("tmux")
    command -v git >/dev/null 2>&1 || missing+=("git")
    command -v docker >/dev/null 2>&1 || missing+=("docker")
    
    if [ ${#missing[@]} -ne 0 ]; then
        echo -e "${RED}Error: Missing required dependencies: ${missing[*]}${NC}"
        echo ""
        echo "Install them with:"
        [ -n "$(printf '%s\n' "${missing[@]}" | grep tmux)" ] && echo "  brew install tmux"
        [ -n "$(printf '%s\n' "${missing[@]}" | grep docker)" ] && echo "  brew install docker"
        exit 1
    fi
    
    # Check if git-hop is available (can be mocked for demo)
    if ! command -v git-hop >/dev/null 2>&1; then
        echo -e "${YELLOW}Warning: git-hop not found in PATH${NC}"
        echo "Creating mock git-hop for demonstration purposes..."
        create_mock_git_hop
    fi
}

# Create mock git-hop for demo purposes
create_mock_git_hop() {
    cat > /tmp/git-hop << 'MOCK_EOF'
#!/usr/bin/env bash
# Mock git-hop for demo purposes

case "$1" in
    ""|list)
        echo "Branch        Path                Status    Ports        Services"
        echo "─────────────────────────────────────────────────────────────────"
        if [ -d "main" ]; then
            echo "main          ./main              active    10100-10102  web, db, cache"
        fi
        if [ -d "next" ]; then
            echo "next          ./next              active    10200-10202  web, db, cache"
        fi
        if [ -d "fix-memory-leak" ]; then
            echo "fix-memory-leak ./fix-memory-leak  active    10300-10302  web, db, cache"
        fi
        if [ -d "fix-memory-leak-next" ]; then
            echo "fix-memory-leak-next ./fix-memory-leak-next active    10400-10402  web, db, cache"
        fi
        ;;
    status)
        echo "On branch $(basename $(pwd))"
        echo "nothing to commit, working tree clean"
        echo ""
        echo "Services:"
        echo "  web    running  http://localhost:10200"
        echo "  db     running  postgresql://localhost:10201"
        echo "  cache  running  redis://localhost:10202"
        ;;
    env)
        if [ "$2" = "start" ]; then
            echo "Starting environment for branch: $(basename $(pwd))"
            echo "  ✓ web started on port 10200"
            echo "  ✓ db started on port 10201"
            echo "  ✓ cache started on port 10202"
        elif [ "$2" = "stop" ]; then
            echo "Stopping environment..."
        fi
        ;;
    *)
        # Handle branch names and URIs
        if [[ "$1" =~ ^file:// ]] || [[ "$1" =~ ^https?:// ]] || [[ "$1" =~ \.git$ ]]; then
            # URI clone mode
            local uri="$1"
            local repo_name
            
            # Extract repo name from URI
            if [[ "$uri" =~ ^file:// ]]; then
                repo_name=$(basename "${uri#file://}")
            else
                repo_name=$(basename "$uri" .git)
            fi
            
            echo "Creating hub for ${repo_name}"
            mkdir -p "$repo_name"
            
            # Check for --branch flag
            if [ "$2" = "--branch" ] && [ -n "$3" ]; then
                # Fork-attach mode
                local branch="$3"
                mkdir -p "$repo_name/$branch"
                echo "Created hopspace for '$branch'"
                echo "Worktree: ./$branch"
                echo "Ports: 10300-10302"
                echo "Services: web, db, cache"
            else
                # Default branch
                mkdir -p "$repo_name/main"
                echo "Created hopspace for 'main'"
                echo "Worktree: ./main"
                echo "Ports: 10100-10102"
                echo "Services: web, db, cache"
            fi
        else
            # Branch mode - create worktree for branch name
            local branch="$1"
            mkdir -p "$branch"
            echo "Created hopspace for '$branch'"
            echo "Worktree: ./$branch"
            
            # Assign different port ranges based on branch name
            if [[ "$branch" == "next" ]]; then
                echo "Ports: 10200-10202"
            elif [[ "$branch" == "fix-memory-leak" ]]; then
                echo "Ports: 10300-10302"
            elif [[ "$branch" =~ next ]]; then
                echo "Ports: 10400-10402"
            else
                echo "Ports: 10100-10102"
            fi
            echo "Services: web, db, cache"
        fi
        ;;
esac
MOCK_EOF
    chmod +x /tmp/git-hop
    export PATH="/tmp:$PATH"
}

# Setup demo repositories
setup_demo_repos() {
    echo -e "${GREEN}Setting up demo repositories...${NC}"
    
    # Create upstream repository
    mkdir -p "$UPSTREAM_REPO"
    cd "$UPSTREAM_REPO"
    git init --initial-branch=main
    
    # Add docker-compose.yml
    cat > docker-compose.yml << 'DOCKER_EOF'
version: '3.8'

services:
  web:
    image: nginx:alpine
    ports:
      - "${WEB_PORT:-8080}:80"
    environment:
      - APP_ENV=development
    depends_on:
      - db
      - cache

  db:
    image: postgres:15-alpine
    ports:
      - "${DB_PORT:-5432}:5432"
    environment:
      - POSTGRES_PASSWORD=demo
      - POSTGRES_DB=awesome_app
    volumes:
      - db_data:/var/lib/postgresql/data

  cache:
    image: redis:7-alpine
    ports:
      - "${CACHE_PORT:-6379}:6379"
    volumes:
      - cache_data:/data

volumes:
  db_data:
  cache_data:
DOCKER_EOF
    
    # Add sample application file
    cat > app.go << 'APP_EOF'
package main

import "fmt"

func main() {
    fmt.Println("Awesome App v0.9.0")
}
APP_EOF
    
    # Add README
    cat > README.md << 'README_EOF'
# Awesome App

A demo application for git-hop demonstration.

## Services

- Web: nginx
- DB: PostgreSQL
- Cache: Redis
README_EOF
    
    git add .
    git commit -m "Initial commit"
    
    # Create next branch
    git checkout -b next
    echo "// Next version features" >> app.go
    git commit -am "Start work on v1.0.0"
    git checkout main
    
    # Create contributor fork
    git clone --bare "$UPSTREAM_REPO" "$CONTRIBUTOR_FORK"
    
    echo -e "${GREEN}Demo repositories created!${NC}\n"
}

# Setup event system
setup_events() {
    mkdir -p "$EVENT_DIR"
    mkfifo "${EVENT_DIR}/pr_created"
    mkfifo "${EVENT_DIR}/pr_merged"
    mkfifo "${EVENT_DIR}/comment_posted"
    mkfifo "${EVENT_DIR}/port_pr_created"
    mkfifo "${EVENT_DIR}/port_pr_merged"
}

# Author pane script
author_script() {
    cat << 'EOF'
#!/usr/bin/env bash
export PATH="/tmp:$PATH"
EVENT_DIR="/tmp/git-hop-demo-events"

wait_for_event() {
    local event="$1"
    cat "${EVENT_DIR}/${event}" > /dev/null
}

trigger_event() {
    local event="$1"
    echo "1" > "${EVENT_DIR}/${event}"
}

comment() {
    echo -e "\n\033[0;35m# $1\033[0m"
    sleep 1.5
}

type_command() {
    local cmd="$1"
    echo -e "\033[0;36m$ \033[0m$cmd"
    sleep 0.3
    eval "$cmd" 2>&1 | head -20
    sleep 1
}

clear
echo -e "\033[1;36m╔════════════════════════════════════════════════════════╗\033[0m"
echo -e "\033[1;36m║                  AUTHOR (Maintainer)                   ║\033[0m"
echo -e "\033[1;36m╚════════════════════════════════════════════════════════╝\033[0m"
echo ""

cd /tmp/git-hop-demo-author

comment "Author: Setting up workspace for awesome-app"
type_command "git hop file:///tmp/git-hop-demo-upstream"
if [ -d "awesome-app" ]; then cd awesome-app; else echo "⚠ awesome-app not found, staying in $(pwd)"; fi

comment "Author: Creating worktree for next version (v1.0.0)"
type_command "git hop next"
if [ -d "next" ]; then cd next; else echo "⚠ next not found, staying in $(pwd)"; fi

comment "Author: Let me start the development environment"
type_command "git hop env start"

comment "Author: Checking status of all my worktrees..."
type_command "cd .. && git hop list"

comment "Author: Working on next... (time passes)"
sleep 2

comment "[WAITING] Author waits for contributor to submit PR..."
wait_for_event "pr_created"

comment "Author: Oh! I see a new PR for fixing a memory leak"
type_command "echo 'PR #42: Fix memory leak in filter.go'"

comment "Author: This is for the stable branch. Let me test it!"
comment "Author: I can hop to the fork's branch even from within this worktree!"
type_command "git hop file:///tmp/git-hop-demo-fork --branch fix/memory-leak"

if [ -d "../fix-memory-leak" ]; then cd ../fix-memory-leak; else echo "⚠ fix-memory-leak not found"; fi

comment "Author: Running tests on the contributor's fix..."
type_command "ls -la"
type_command "cat app.go | head -5 2>/dev/null || echo 'app.go not found (this is expected in demo)'"

comment "Author: Perfect! The fix works. Approving and merging."
type_command "echo '✓ PR approved and merged to main'"

comment "Author: But we need this in 'next' too for v1.0.0..."
type_command "echo '@contributor: Could you port this to the next branch?'"

trigger_event "pr_merged"

comment "[WAITING] Author waits for contributor to port the fix..."
wait_for_event "port_pr_created"

comment "Author: Great! I see PR #43 porting the fix to next"
comment "Author: Again, hopping from within the current worktree!"
type_command "git hop file:///tmp/git-hop-demo-fork --branch fix/memory-leak-next"

if [ -d "fix-memory-leak-next" ]; then cd fix-memory-leak-next; elif [ -d "../fix-memory-leak-next" ]; then cd ../fix-memory-leak-next; else echo "⚠ fix-memory-leak-next not found"; fi

comment "Author: Reviewing the ported changes..."
type_command "git log --oneline -2 2>/dev/null || echo '(simulated git log output)'"

comment "Author: Excellent! Merging to my next branch"
cd ../next
type_command "echo '✓ Merged fix from contributor'"
type_command "git log --oneline -3 2>/dev/null || echo '(simulated git log output)'"

trigger_event "port_pr_merged"

comment "Author: Let's see all environments running in parallel"
cd ..
type_command "git hop list"

comment "Author: Each worktree has isolated Docker services!"
cd next
type_command "git hop status"

echo -e "\n\033[1;32m✓ Author workflow complete!\033[0m\n"
sleep 100
EOF
}

# Contributor pane script
contributor_script() {
    cat << 'EOF'
#!/usr/bin/env bash
export PATH="/tmp:$PATH"
EVENT_DIR="/tmp/git-hop-demo-events"

wait_for_event() {
    local event="$1"
    cat "${EVENT_DIR}/${event}" > /dev/null
}

trigger_event() {
    local event="$1"
    echo "1" > "${EVENT_DIR}/${event}"
}

comment() {
    echo -e "\n\033[0;35m# $1\033[0m"
    sleep 1.5
}

type_command() {
    local cmd="$1"
    echo -e "\033[0;33m$ \033[0m$cmd"
    sleep 0.3
    eval "$cmd" 2>&1 | head -20
    sleep 1
}

clear
echo -e "\033[1;33m╔════════════════════════════════════════════════════════╗\033[0m"
echo -e "\033[1;33m║              CONTRIBUTOR (Outside Dev)                 ║\033[0m"
echo -e "\033[1;33m╚════════════════════════════════════════════════════════╝\033[0m"
echo ""

cd /tmp/git-hop-demo-contributor

comment "Contributor: I found a memory leak! Let me submit a fix."

comment "Contributor: Setting up my fork with git-hop"
type_command "git hop file:///tmp/git-hop-demo-fork"
if [ -d "awesome-app" ]; then cd awesome-app; else echo "⚠ awesome-app not found"; fi

comment "Contributor: Creating worktree for the bugfix (from main)"
type_command "git hop fix/memory-leak"
if [ -d "fix-memory-leak" ]; then cd fix-memory-leak; else echo "⚠ fix-memory-leak not found"; fi

comment "Contributor: Starting my local environment to test"
type_command "git hop env start"

comment "Contributor: Making the fix..."
type_command "echo '// Fixed: close reader to prevent leak' >> app.go"
type_command "git add app.go 2>/dev/null || echo '(git add simulated)'"
type_command "git commit -m 'fix: close reader to prevent memory leak' 2>/dev/null || echo '(git commit simulated)'"

comment "Contributor: Pushing and creating PR to upstream"
type_command "echo '✓ PR #42 created: Fix memory leak in filter.go'"

trigger_event "pr_created"

comment "[WAITING] Contributor waits for maintainer review..."
wait_for_event "pr_merged"

comment "Contributor: Awesome! PR merged! 🎉"
type_command "echo 'PR #42: Merged by maintainer'"

comment "Contributor: I see a comment asking to port to 'next'"
type_command "echo 'Maintainer comment: Please port this to next branch'"

comment "Contributor: Sure! Creating worktree for next"
cd ..
type_command "git hop next"
if [ -d "next" ]; then cd next; else echo "⚠ next not found"; fi

comment "Contributor: Starting environment for next branch"
type_command "git hop env start"

comment "Contributor: Notice the different ports - no conflicts!"
type_command "git hop status"

comment "Contributor: Creating branch for the port"
type_command "git checkout -b fix/memory-leak-next 2>/dev/null || echo '(branch created)'"

comment "Contributor: Applying the same fix to next"
type_command "echo '// Fixed: close reader to prevent leak' >> app.go"
type_command "git add app.go 2>/dev/null || echo '(git add simulated)'"
type_command "git commit -m 'fix: port memory leak fix to next' 2>/dev/null || echo '(git commit simulated)'"

comment "Contributor: Submitting PR #43 for next branch"
type_command "echo '✓ PR #43 created: Port memory leak fix to next'"

trigger_event "port_pr_created"

comment "[WAITING] Contributor waits for review..."
wait_for_event "port_pr_merged"

comment "Contributor: Perfect! Both PRs merged! ✓✓"

comment "Contributor: Let's see all my worktrees"
cd ..
type_command "git hop list"

comment "Contributor: Each has isolated Docker environments!"

echo -e "\n\033[1;32m✓ Contributor workflow complete!\033[0m\n"
sleep 100
EOF
}

# Main demo script
main() {
    echo -e "${BOLD}${BLUE}"
    cat << "EOF"
╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║              git-hop Collaboration Demo                      ║
║                                                              ║
║  This demo shows how git-hop enables seamless parallel       ║
║  development with isolated Docker environments.              ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝
EOF
    echo -e "${NC}\n"
    
    check_deps
    
    # Ensure mock git-hop exists (check_deps may have skipped it if real one exists)
    if ! command -v git-hop >/dev/null 2>&1; then
        create_mock_git_hop
    fi
    
    echo -e "${YELLOW}This demo will:${NC}"
    echo "  1. Set up two tmux panes (Author | Contributor)"
    echo "  2. Create demo repos with docker-compose.yml (web/db/cache)"
    echo "  3. Show event-driven collaboration workflow"
    echo "  4. Demonstrate isolated environments per branch"
    echo ""
    echo -e "${YELLOW}Demo Features:${NC}"
    echo "  • Multiple worktrees with isolated ports"
    echo "  • Docker services (nginx, postgres, redis)"
    echo "  • Synchronized actions between contributors"
    echo ""
    
    read -p "Press ENTER to start the demo..." -r
    
    cleanup
    
    # Setup
    setup_demo_repos
    setup_events
    
    # Create workspaces
    mkdir -p "$AUTHOR_WORKSPACE" "$CONTRIBUTOR_WORKSPACE"
    
    # Create scripts
    author_script > /tmp/author.sh
    contributor_script > /tmp/contributor.sh
    chmod +x /tmp/author.sh /tmp/contributor.sh
    
    # Create tmux session with split windows
    echo -e "${GREEN}Starting tmux session...${NC}\n"
    
    # Create session and send author script to first pane
    tmux new-session -d -s "$SESSION_NAME" "/tmp/author.sh"
    
    # Split and send contributor script to second pane (automatically selected after split)
    tmux split-window -h -t "$SESSION_NAME" "/tmp/contributor.sh"
    
    # Attach to session
    echo -e "${GREEN}Attaching to demo session...${NC}"
    echo -e "${YELLOW}Watch as the two collaborators work in sync!${NC}"
    echo -e "${YELLOW}Use Ctrl+B then D to exit the demo${NC}\n"
    sleep 2
    
    tmux attach-session -t "$SESSION_NAME"
    
    # Cleanup after demo
    echo -e "\n${GREEN}Demo complete!${NC}\n"
    
    read -p "Clean up demo files? (y/N) " -r
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        cleanup
        echo "Cleanup complete."
    else
        echo -e "${YELLOW}Demo files preserved at:${NC}"
        echo "  Upstream:    $UPSTREAM_REPO"
        echo "  Author:      $AUTHOR_WORKSPACE"
        echo "  Contributor: $CONTRIBUTOR_WORKSPACE"
    fi
}

# Handle script arguments
case "${1:-}" in
    --clean)
        cleanup
        echo "Cleanup complete."
        ;;
    --help|-h)
        echo "Usage: $0 [--clean|--help]"
        echo ""
        echo "Options:"
        echo "  --clean    Clean up demo workspaces"
        echo "  --help     Show this help message"
        ;;
    *)
        main
        ;;
esac
