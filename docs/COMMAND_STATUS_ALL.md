# `git hop status --all` - System-Wide Status

Display comprehensive git-hop system information including all repositories, configurations, and resource usage.

## Usage

```bash
# Show current worktree/hub status (default)
git hop status

# Show system-wide git-hop meta information
git hop status --all
```

## What It Shows

### 🔧 Configuration
- **Data Home**: Location where worktrees and volumes are stored
- **Config**: Path to git-hop configuration file
- **Version**: git-hop version information

### 📦 Resources
- **Repositories**: Total number of tracked repositories
- **Total Worktrees**: Sum of all worktrees across all repositories
- **Active**: Number of worktrees that exist on disk
- **Missing**: Number of tracked but missing worktrees
- **Disk Usage**: Total disk space used by all worktrees

### 🐳 Environment
- **Running Services**: Number of Docker services currently running
- **Port Range**: Port range allocated for services
- **Active Volumes**: Number of Docker volumes in use

### 📁 Repositories
Tree view of all tracked repositories showing:
- Repository identifier (shortened for display)
- Number of worktrees per repository
- Running status indicator (● running / ○ stopped)
- Count of running services

## Example Output

```
┌──────────────────────────────────────────────────┐
│ Git-Hop System Status                            │
└──────────────────────────────────────────────────┘


🔧 Configuration
  Data Home /Users/user/.local/share/git-hop
  Config /Users/user/.config/git-hop/config.json
  Version git-hop

📦 Resources
  Repositories 3
  Total Worktrees 12
  Active 10
  Missing 2
  Disk Usage 2.3 GB

🐳 Environment
  Running Services 4
  Port Range 11500-11520
  Active Volumes 8

📁 Repositories

    ├─ ...thub.com/org/repo1    5 worktrees  ● 2 running
    ├─ ...thub.com/org/repo2    4 worktrees  ○ 0 stopped
    └─ ...tlab.com/org/repo3    3 worktrees  ● 2 running

Tracking 12 worktrees across 3 repositories · 4 services running
```

## Use Cases

### System Health Check
Quickly see overall system state and resource usage:
```bash
git hop status --all
```

### Before Cleanup
Check what's using space before running prune:
```bash
git hop status --all
# Review disk usage and missing worktrees
git hop prune
```

### Multi-Repo Overview
See all repositories and their status at a glance:
```bash
git hop status --all
# See which repos have running services
```

### Troubleshooting
Identify configuration issues or missing worktrees:
```bash
git hop status --all
# Check for "Missing" count
# Verify data home and config paths
```

## Output Modes

The command respects git-hop's output modes:

### Human Mode (Default)
Rich, colorful output with emojis and styled sections.

### JSON Mode
```bash
git hop status --all --json
```
Machine-readable JSON output for scripting.

### Porcelain Mode
```bash
git hop status --all --porcelain
```
Simple, parseable text output.

### Quiet Mode
```bash
git hop status --all --quiet
```
Minimal output (errors only).

## Comparison with Related Commands

| Command | Purpose | Scope |
|---------|---------|-------|
| `git hop status` | Current worktree/hub details | Single context |
| `git hop status --all` | System-wide overview | All repositories |
| `git hop list` | List all worktrees | All worktrees (tabular) |
| `git hop doctor` | Health checks and diagnostics | System health |

## When to Use Which

- **`git hop status`**: "What's going on in this worktree?"
- **`git hop status --all`**: "What's the state of my entire git-hop system?"
- **`git hop list`**: "Show me all worktrees in a table"
- **`git hop doctor`**: "Is everything working correctly?"

## Tips

1. **Regular checks**: Run `git hop status --all` periodically to monitor system state
2. **Before migrations**: Check status before running `git hop migrate`
3. **Disk space**: Monitor disk usage to identify cleanup opportunities
4. **Missing worktrees**: Use the "Missing" count to find orphaned entries

## Integration with Other Commands

```bash
# Check system state
git hop status --all

# Clean up missing worktrees
git hop prune

# Verify system health
git hop doctor

# See detailed worktree list
git hop list
```

## Technical Details

### Disk Usage Calculation
Walks the entire worktree directory tree and sums file sizes. This can take a moment for large repositories but provides accurate usage information.

### Service Detection
Checks for `docker-compose.yml` files and queries Docker for running containers. Services not managed by git-hop are not counted.

### Repository Identification
Uses the repository state tracking system. Only repositories managed by git-hop are included.

## Future Enhancements

Potential improvements for future versions:

- [ ] Show per-repository disk usage breakdown
- [ ] Display last access time for each worktree
- [ ] Include shared dependency cache information
- [ ] Show port allocation details per repository
- [ ] Add `--verbose` for more detailed metrics
- [ ] Export capability (CSV, JSON)
- [ ] Historical usage trends

## See Also

- [Enhanced Output System](OUTPUT_README.md)
- [Implementation Guide](OUTPUT_IMPLEMENTATION_GUIDE.md)
- [git hop list](../README.md#commands)
- [git hop doctor](../README.md#commands)
