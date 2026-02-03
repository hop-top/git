# Testing Plan

## Overview

Validate demo repo scripts and git-hop integration.

## Tasks

- [ ] Test create-demo-repo.sh
  - [Demo Structure](demo-structure.md)
  - [ ] Implement create-demo-repo.sh
  - [ ] Test on macOS
  - [ ] Test on Linux
  - [ ] Document usage

## Structure

- [Demo Structure](demo-structure.md)
- [Problem & Solution](../problem-solution.md)
- [Implementation Guide](../implementation-guide.md)
- [Testing Plan](../testing-plan.md)
- [Diagrams](../diagrams/README.md)

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

## Related

- [Problem & Solution](../problem-solution.md)
- [Demo Structure](../demo-structure.md)
- [Implementation Guide](../implementation-guide.md)
- [Diagrams](../diagrams/README.md)
