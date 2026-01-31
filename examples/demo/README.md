# git-hop Demo

This demo showcases git-hop's event-driven collaboration workflow with isolated Docker environments.

## What It Shows

When running correctly, you'll see two tmux panes:
- **Left (Author)**: Library maintainer working on next version
- **Right (Contributor)**: Outside collaborator submitting a bug fix

They synchronize through named pipes:
1. Contributor creates PR → triggers event
2. Author reviews and merges → triggers event
3. Author requests port to `next` → triggers event
4. Contributor ports fix → triggers event
5. Author merges port → completion

Each role demonstrates `git-hop` commands creating isolated environments with different port ranges.

## How to Run

```bash
# Navigate to the demo directory
cd examples/demo

# Make the script executable
chmod +x demo.sh

# Run the demo
./demo.sh
```

## Controls

- **Enter**: Start the demo
- **Ctrl+B then D**: Exit the demo
- The demo will prompt to clean up files after completion

## Manual Cleanup

To manually clean up demo artifacts:
```bash
./demo.sh --clean
```

## Demo Architecture

The demo creates:
- **Upstream repo**: `/tmp/git-hop-demo-upstream`
- **Author workspace**: `/tmp/git-hop-demo-author`
- **Contributor workspace**: `/tmp/git-hop-demo-contributor`
- **Fork**: `/tmp/git-hop-demo-fork`
- **Event pipes**: `/tmp/git-hop-demo-events/`

Each workspace has isolated Docker services:
- Web service on different ports
- PostgreSQL database
- Redis cache
