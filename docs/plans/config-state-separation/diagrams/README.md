# Diagrams

Mermaid diagrams for config/state separation plan.

## Diagrams

- [Directory Structure](directory-structure.mmd)
  - 3-tier XDG layout
  - Config, state, data separation
  - Hook locations

- [Hook Priority System](hook-priority-system.mmd)
  - 6-tier priority flow
  - Git native to built-in defaults
  - Hook wrapper dispatch

- [Worktree Verification Flow](worktree-verification-flow.mmd)
  - State lookup
  - Path verification
  - Rescan on failure
  - Git validation

- [State Update Flow](state-update-flow.mmd)
  - Operations that write state
  - Add, remove, clone, verify
  - Atomic write pattern

- [Migration Process](migration-process.mmd)
  - 6-phase migration
  - Old to new data
  - Hook migration
  - Backup and rollback

## Viewing Diagrams

Most Mermaid viewers support these diagrams directly.

### GitHub
- Push to repository
- View on GitHub (auto-renders Mermaid)
- Or use [GitHub Mermaid Preview](https://mermaid.live/)

### Local
- [Mermaid Live Editor](https://mermaid.live/)
- [Mermaid CLI](https://github.com/mermaid-js/mermaid-cli)
- [VS Code Mermaid Preview](https://marketplace.visualstudio.com/items?itemName=bierner.markdown-mermaid)

## Diagram Naming Convention

Format: `{topic}-{type}.mmd`

Examples:
- directory-structure.mmd
- hook-priority-system.mmd
- worktree-verification-flow.mmd
- state-update-flow.mmd
- migration-process.mmd

## Related

- [Main Plan](../README.md)
- [Schema Definitions](../schemas/README.md)
- [Implementation Guides](../implementation/README.md)
