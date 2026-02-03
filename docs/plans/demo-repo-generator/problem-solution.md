# Problem & Solution

## Overview

Reproducible demo repository showcasing git-hop features.

## Tasks

- [ ] Create demo-repo structure
  - [Demo Structure](demo-structure.md)
  - [ ] Implement create-demo-repo.sh
  - [ ] Implement setup-demo-worktrees.sh
  - [ ] Test on macOS
  - [ ] Test on Linux
  - [ ] Document usage

## Structure

- [Demo Structure](demo-structure.md)
- [Implementation Guide](implementation-guide.md)
- [Testing Plan](testing-plan.md)

## Demo Features

Multi-language project:
- Socket server (Go)
- Worker & scheduler (Python)
- API (PHP Laravel)
- Frontend (React)
- Database (PostgreSQL, Redis, SQLite)
- Docker services
- Environment hooks

## Dependency Sharing

Different lockfile versions per worktree:
- bug/same-lockfile (same as main)
- fix/same-lockfile (same as main)
- feat/diff-lockfile (diff deps)
- ci/another-lockfile (diff deps)

Expected sharing structure:
- Shared deps: $GIT_HOP_DATA_HOME/hop-top/git-sample/deps/
- Symlinks to .git-hop/deps/

## Worktree Scenarios

Three worktrees demonstrating git-hop capabilities:

1. main (bare repo)
   - Base lockfile version
   - Primary development state

2. bug/same-lockfile
   - Same lockfile as main
   - No dependency changes needed
   - Demonstrates shared deps

3. fix/same-lockfile
   - Same lockfile as main
   - Bug fix in services
   - Demonstrates shared deps

4. feat/diff-lockfile
   - Different lockfile
   - New dependency added
   - Demonstrates isolated deps

5. ci/another-lockfile
   - Different lockfile
   - CI-specific deps
   - Demonstrates isolated deps

## Related

- [Demo Structure](demo-structure.md)
- [Implementation Guide](implementation-guide.md)
- [Testing Plan](testing-plan.md)
- [Diagrams](diagrams/README.md)
