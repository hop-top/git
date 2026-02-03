#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

check_git_hop() {
    if ! command -v git-hop &> /dev/null; then
        echo "Error: git-hop not found in PATH"
        echo "Install: go install github.com/jadb/git-hop@latest"
        exit 1
    fi
}

clone_repo() {
    local workspace="$1"
    local target="$workspace/git-hop-sample"
    
    echo "Cloning demo repository..."
    
    cd "$workspace"
    
    git-hop clone hop-top/git-sample git-hop-sample
    cd git-hop-sample
}

create_worktree() {
    local worktree_name="$1"
    local repo=".git/hop/git-hop-sample"
    
    cd "$repo"
    
    echo "Creating worktree: $worktree_name"
    git-hop add "$worktree_name"
    cd ".git/hop/worktrees/$worktree_name"
    
    local commit_msg=""
    
    case "$worktree_name" in
        bug/same-lockfile|fix/same-lockfile)
            commit_msg="fix: socket connection timeout"
            ;;
        feat/diff-lockfile)
            commit_msg="feat: add axios and celery dependencies"
            ;;
        ci/another-lockfile)
            commit_msg="ci: update websocket and sanctum versions"
            ;;
    esac
    
    if [ -n "$commit_msg" ]; then
        git commit --allow-empty -m "$commit_msg"
        git push -u origin "$worktree_name"
    fi
}

show_dependency_sharing() {
    echo ""
    echo "=== Expected Dependency Sharing ==="
    echo ""
    echo "After setup, deps structure should be:"
    echo ""
    echo "\$GIT_HOP_DATA_HOME/hop-top/git-sample/deps/"
    echo "├── node_modules.abc123/     # Used by: main, bug, fix"
    echo "├── node_modules.def456/     # Used by: feat"
    echo "├── vendor.789abc/          # Used by: main, bug, fix, feat"
    echo "├── vendor.xyz999/           # Used by: ci"
    echo "└── .registry.json"
    echo ""
    echo "=== Sharing Summary ==="
    echo "node_modules.abc123 (main, bug, fix): 234MB"
    echo "node_modules.def456 (feat): 234MB"
    echo "Total disk usage: 468MB"
}

main() {
    local workspace=""
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
                if [ -z "$workspace" ]; then
                    workspace="$1"
                    shift
                fi
                ;;
        esac
    done
    
    if [ -z "$workspace" ]; then
        usage
        exit 1
    fi
    
    check_git_hop
    
    if [ "$dry_run" = true ]; then
        echo "DRY RUN - Would set up worktrees at: $workspace"
        echo "  → Clone repo to: $workspace/git-hop-sample"
        echo "  → Create worktree: bug/same-lockfile"
        echo "  → Create worktree: fix/same-lockfile"
        echo "  → Create worktree: feat/diff-lockfile"
        echo "  → Create worktree: ci/another-lockfile"
        exit 0
    fi
    
    clone_repo "$workspace"
    
    for worktree in bug/same-lockfile fix/same-lockfile feat/diff-lockfile ci/another-lockfile; do
        create_worktree "$worktree"
    done
    
    cd "$workspace/git-hop-sample"
    
    for worktree in bug/same-lockfile fix/same-lockfile; do
        cd "../frontend"
        npm install axios@1.6.0
        cd ..
    done
    
    cd "$workspace/git-hop-sample"
    
    for worktree in bug/same-lockfile fix/same-lockfile; do
        cd "../services"
        echo "celery==5.3.4" >> requirements.txt
        pip install celery==5.3.4
        cd ..
    done
    
    cd "$workspace/git-hop-sample"
    
    cd "../socket-server"
    go get github.com/gorilla/websocket@v1.5.1
    go mod tidy
    cd ..
    
    cd "../api"
    composer require laravel/sanctum:^3.4
    cd ..
    
    git add socket-server/go.mod socket-server/go.sum api/composer.lock
    git commit -m "ci: update websocket and sanctum versions"
    git push -u origin ci/another-lockfile
    
    show_dependency_sharing "$workspace"
}

main "$@"
