# Error Recovery in git-hop

## Overview

git-hop includes a robust 3-layer error handling and recovery system to gracefully handle failures and inconsistent states. This system ensures that partial failures don't leave your repository in a broken state, and provides clear paths to recovery when issues occur.

### The Three Layers

1. **Detection Layer** - Validates state before operations and detects partial artifacts
2. **Transaction Layer** - Multi-step operations with automatic rollback on failure
3. **Recovery Layer** - Cleanup handlers and enhanced error messages with fix instructions

## Common Scenarios

### Orphaned Directories

**What happened:** A directory exists in `hops/` but is not registered in the hopspace configuration.

**Symptoms:**
- Directory exists but `git hop list` doesn't show it
- Attempting to create a worktree with the same name fails

**Example:**
```bash
$ git hop add feature-x
Error: Cannot create worktree due to state issues:
  OrphanedDirectory at /path/to/hops/feature-x: Orphaned directory exists but is not registered in hopspace config

Run 'git hop doctor --fix' to resolve these issues
```

**Resolution:**
```bash
# Let doctor diagnose and fix the issue
git hop doctor --fix

# Or manually remove the directory
rm -rf hops/feature-x
```

### Stale Git Metadata

**What happened:** Git's worktree metadata references a directory that no longer exists.

**Symptoms:**
- `git worktree list` shows a path that doesn't exist
- Cannot create a new worktree at that location

**Example:**
```bash
$ git hop doctor
=== Checking Worktree State ===
Found 1 orphaned git worktree references
  - /path/to/hops/old-feature
```

**Resolution:**
```bash
# Prune stale references
git hop doctor --fix

# Or use git directly
git worktree prune
```

### Retry After Failure

**What happened:** A `git hop add` command failed partway through, leaving partial state.

**Symptoms:**
- Command failed with an error
- Subsequent retry attempts also fail

**Example:**
```bash
# First attempt fails
$ git hop add feature-y
Error: Failed to create worktree: permission denied

# Retry succeeds automatically
$ git hop add feature-y
Adding branch feature-y...
Successfully added branch feature-y
```

**How it works:** The transactional system automatically rolls back partial changes, allowing clean retries. If rollback isn't possible, `git hop doctor --fix` can clean up the state.

### Config Mismatches

**What happened:** The hub and hopspace configurations have diverged.

**Symptoms:**
- Branch exists in hub but not in hopspace (or vice versa)
- Path mismatches between hub and hopspace

**Example:**
```bash
$ git hop doctor
=== Checking Worktree State ===
ConfigMismatch: Branch 'feature-z' exists in hub but not in hopspace
```

**Resolution:**
```bash
# Review and fix manually
# The doctor command will identify which branches are out of sync
# You can either add the missing branch or remove it from the hub
```

## Using Doctor Command

The `git hop doctor` command is your primary tool for diagnosing and fixing state issues.

### Check for Issues

```bash
git hop doctor
```

This will:
- Check if you're in a hub
- Validate worktree state
- Detect orphaned directories
- Detect stale git metadata
- Check for config mismatches
- Verify dependencies (if configured)

### Auto-Fix Issues

```bash
git hop doctor --fix
```

This will automatically fix issues that can be safely resolved:
- Remove orphaned directories (if they have no uncommitted changes)
- Prune stale git worktree metadata
- Clean up broken symlinks

**Safety:** The doctor command will never remove directories with uncommitted changes. You'll need to manually resolve those cases.

## Error Types Reference

### OrphanedDirectory
A directory exists in `hops/` but is not registered in the hopspace configuration.

**Auto-fixable:** Yes (if no uncommitted changes)

**Manual fix:**
```bash
rm -rf hops/<directory-name>
```

### PartialWorktree
Git metadata for a worktree exists, but the directory is missing or incomplete.

**Auto-fixable:** Yes

**Manual fix:**
```bash
git worktree prune
```

### OrphanedGitMetadata
Git knows about a worktree, but the directory doesn't exist.

**Auto-fixable:** Yes

**Manual fix:**
```bash
git worktree prune
```

### ConfigMismatch
The hub and hopspace configurations have diverged.

**Auto-fixable:** No

**Manual fix:** Review the output and manually add or remove branches as needed.

## Transaction Behavior

When you run `git hop add`, the system uses transactions to ensure atomicity:

1. **Pre-flight validation** - Check for existing state issues
2. **Create worktree** - Git operation with rollback support
3. **Register in hopspace** - Update config
4. **Register in hub** - Update hub config

If any step fails:
- All previous steps are automatically rolled back
- Clear error message explains what happened
- You can retry the operation immediately

### Example Transaction Flow

```bash
$ git hop add feature-a
# Step 1: Validate state... ✓
# Step 2: Create worktree... ✓
# Step 3: Register in hopspace... ✗ (disk full)
# Rollback: Remove worktree... ✓
Error: Failed to register branch: disk full

# Fix the issue (free up disk space)
$ git hop add feature-a
# Retry succeeds because rollback was clean
```

## Best Practices

### 1. Run Doctor Regularly

Add `git hop doctor` to your workflow, especially after:
- Failed commands
- Manual file operations
- System crashes

### 2. Let Transactions Work

If a command fails, try running it again. The transaction system will clean up and retry.

### 3. Use --fix Wisely

The `--fix` flag is safe, but review the output first:
```bash
# Check what needs fixing
git hop doctor

# Review the issues, then fix
git hop doctor --fix
```

### 4. Keep Configs in Sync

If you manually edit worktrees or configs:
```bash
# Verify everything is consistent
git hop doctor

# Fix any mismatches
git hop doctor --fix
```

## Implementation Details

For developers interested in the implementation, see the [error recovery implementation plan](plans/error-recovery.md).

The system consists of:
- `/internal/hop/errors.go` - Error types and parsing
- `/internal/hop/validator.go` - State validation logic
- `/internal/hop/cleanup.go` - Cleanup handlers
- `/internal/hop/transaction.go` - Transaction framework
- `/cmd/add.go`, `/cmd/remove.go`, `/cmd/doctor.go` - Command integration

## Limitations

### What Can't Be Auto-Fixed

1. **Uncommitted changes** - Directories with uncommitted work are never removed automatically
2. **Config mismatches** - Requires manual review to determine correct state
3. **Permission errors** - System-level issues need manual intervention

### Edge Cases

1. **Concurrent operations** - Running multiple `git hop` commands simultaneously is not supported
2. **External modifications** - Changes made outside git-hop (direct git commands) may require `doctor --fix`
3. **Network failures** - Operations requiring network access (clone, fetch) don't have automatic retry

## Getting Help

If you encounter an issue that `git hop doctor --fix` can't resolve:

1. Run `git hop doctor` and review the output
2. Check this documentation for your specific scenario
3. File an issue at [github.com/jadb/git-hop/issues](https://github.com/jadb/git-hop/issues) with:
   - The command you ran
   - The error message
   - Output of `git hop doctor`
   - Steps to reproduce
