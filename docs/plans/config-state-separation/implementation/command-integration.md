# Command Integration

## Overview

Update git-hop commands to use new config/state structure. Integrate hooks at lifecycle points.

## Commands

### git hop clone

**Function:** Clone repository, register in state.

**Updates:**
1. Read default config from config.json
2. Register repository in state.json
3. Register default worktree (bare repo)
4. Register hub in state.json
5. Install hooks

**Code:**
```go
func CloneWorktree(
    fs afero.Fs,
    g GitInterface,
    uri string,
    projectPath string,
    useBare bool,
    globalConfig bool,
) error
```

**Location:** [CloneWorktree](../../internal/hop/clone.go) (updated)

**State Updates:**
- Add to Repositories map
- Initialize Worktrees with bare entry
- Add to Hubs array

**Hooks:**
- Call InstallHooks() on new worktree

### git hop add

**Function:** Add worktree to existing hub.

**Updates:**
1. Create worktree via WorktreeManager
2. Execute pre-worktree-add hook
3. Register in hopspace
4. Add to hub branches
5. Add to state.json Worktrees map
6. Execute post-worktree-add hook
7. Install hooks

**Code:**
```go
func (cmd *cobra.Command) RunAdd(args []string) {
    // 1. Create worktree
    wm := hop.NewWorktreeManager(fs, g)
    worktreePath, err := wm.CreateWorktree(hopspace, hubPath, branch)
    
    // 2. Run pre-worktree-add hook
    hookRunner := hooks.NewRunner(config, fs)
    if err := hookRunner.ExecuteHook("pre-worktree-add", worktreePath); err != nil {
        output.Fatal("Pre-worktree-add hook failed: %v", err)
    }
    
    // 3. Register in hopspace
    if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
        output.Fatal("Failed to register branch: %v", err)
    }
    
    // 4. Update hub
    if err := hub.AddBranch(branch, branch, worktreePath); err != nil {
        output.Fatal("Failed to add branch to hub: %v", err)
    }
    
    // 5. Update state.json
    repoID := getRepoID(hopspace.Config.Repo.Org, hopspace.Config.Repo.Repo)
    repoState := state.Repositories[repoID]
    repoState.Worktrees[branch] = hop.WorktreeState{
        Path:        worktreePath,
        Type:        "linked",
        HubPath:     hubPath,
        CreatedAt:   time.Now(),
        LastAccessed: time.Now(),
    }
    SaveState(state)
    
    // 6. Install hooks
    if err := hookRunner.InstallHooks(worktreePath); err != nil {
        output.Warn("Failed to install hooks: %v", err)
    }
    
    // 7. Run post-worktree-add hook
    if err := hookRunner.ExecuteHook("post-worktree-add", worktreePath); err != nil {
        output.Warn("Post-worktree-add hook failed: %v", err)
    }
}
```

**Location:** [add.go](../../cmd/add.go) (updated)

### git hop remove

**Function:** Remove worktree from hub.

**Updates:**
1. Execute pre-worktree-remove hook
2. Remove worktree via WorktreeManager
3. Unregister from hopspace
4. Remove from hub branches
5. Delete from state.json Worktrees map
6. Execute post-worktree-remove hook
7. Check if bare repo, remove hub if needed

**Code:**
```go
func (cmd *cobra.Command) RunRemove(args []string) {
    // 1. Run pre-worktree-remove hook
    hookRunner := hooks.NewRunner(config, fs)
    if err := hookRunner.ExecuteHook("pre-worktree-remove", worktreePath); err != nil {
        output.Fatal("Pre-worktree-remove hook failed: %v", err)
    }
    
    // 2. Remove worktree
    if err := wm.RemoveWorktree(hopspace, branch); err != nil {
        output.Fatal("Failed to remove worktree: %v", err)
    }
    
    // 3. Unregister from hopspace
    if err := hopspace.UnregisterBranch(branch); err != nil {
        output.Warn("Failed to unregister branch: %v", err)
    }
    
    // 4. Update hub
    hub.RemoveBranch(branch)
    
    // 5. Update state.json
    repoID := getRepoID(hopspace.Config.Repo.Org, hopspace.Config.Repo.Repo)
    delete(state.Repositories[repoID].Worktrees, branch)
    
    // 6. Check if bare repo
    if worktreeType == "bare" {
        removeHubFromState(state, repoID, hubPath)
    }
    SaveState(state)
    
    // 7. Run post-worktree-remove hook
    if err := hookRunner.ExecuteHook("post-worktree-remove", worktreePath); err != nil {
        output.Warn("Post-worktree-remove hook failed: %v", err)
    }
}
```

**Location:** [remove.go](../../cmd/remove.go) (updated)

### git hop env start

**Function:** Start Docker environment.

**Updates:**
1. Execute pre-env-start hook
2. Start Docker compose
3. Execute post-env-start hook

**Code:**
```go
func (cmd *cobra.Command) RunEnvStart(args []string) {
    hookRunner := hooks.NewRunner(config, fs)
    
    // 1. Run pre-env-start hook
    if err := hookRunner.ExecuteHook("pre-env-start", root); err != nil {
        output.Fatal("Pre-env-start hook failed: %v", err)
    }
    
    // 2. Start services
    output.Info("Starting services...")
    if err := d.ComposeUp(root, true); err != nil {
        output.Fatal("Failed to start services: %v", err)
    }
    
    // 3. Run post-env-start hook
    if err := hookRunner.ExecuteHook("post-env-start", root); err != nil {
        output.Warn("Post-env-start hook failed: %v", err)
    }
    
    output.Info("Services started.")
}
```

**Location:** [env.go](../../cmd/env.go) (updated)

### git hop env stop

**Function:** Stop Docker environment.

**Updates:**
1. Execute pre-env-stop hook
2. Stop Docker compose
3. Execute post-env-stop hook

**Code:**
```go
func (cmd *cobra.Command) RunEnvStop(args []string) {
    hookRunner := hooks.NewRunner(config, fs)
    
    // 1. Run pre-env-stop hook
    if err := hookRunner.ExecuteHook("pre-env-stop", root); err != nil {
        output.Fatal("Pre-env-stop hook failed: %v", err)
    }
    
    // 2. Stop services
    output.Info("Stopping services...")
    if err := d.ComposeStop(root); err != nil {
        output.Fatal("Failed to stop services: %v", err)
    }
    
    // 3. Run post-env-stop hook
    if err := hookRunner.ExecuteHook("post-env-stop", root); err != nil {
        output.Warn("Post-env-stop hook failed: %v", err)
    }
    
    output.Info("Services stopped.")
}
```

**Location:** [env.go](../../cmd/env.go) (updated)

### git hop list

**Function:** List all worktrees from state.

**Updates:**
1. Read state.json
2. Iterate Repositories map
3. Display worktrees

**Code:**
```go
func (cmd *cobra.Command) RunList(args []string) {
    state := LoadState()
    
    for repoID, repo := range state.Repositories {
        for branch, wt := range repo.Worktrees {
            output.Info("%s:%s → %s", repoID, branch, wt.Path)
        }
    }
}
```

**Location:** [list.go](../../cmd/list.go) (updated)

### git hop org/repo:branch

**Function:** Jump to worktree from anywhere.

**Updates:**
1. Parse org/repo:branch format
2. Verify worktree via VerifyWorktree
3. Update lastAccessed timestamp
4. Change directory

**Code:**
```go
func HopGlobal(org, repo, branch string) error {
    state := LoadState()
    repoID := fmt.Sprintf("%s/%s/%s", getDefaultDomain(), org, repo)
    
    // 1. Verify and get worktree path
    path, err := VerifyWorktree(state, repoID, branch, g)
    if err != nil {
        return err
    }
    
    // 2. Update lastAccessed
    repoState := state.Repositories[repoID]
    repoState.Worktrees[branch].LastAccessed = time.Now()
    SaveState(state)
    
    // 3. Change directory
    os.Chdir(path)
    return nil
}
```

**Location:** New in [root.go](../../internal/cli/root.go) or separate file

### git hop install-hooks

**Function:** Manual hook installation.

**Updates:**
1. Find worktree root
2. Install hooks via hook runner

**Code:**
```go
var installHooksCmd = &cobra.Command{
    Use:   "install-hooks",
    Short: "Install git-hop hooks in current repository",
    Run: func(cmd *cobra.Command, args []string) {
        cwd, err := os.Getwd()
        if err != nil {
            output.Fatal("Failed to get current directory: %v", err)
        }
        
        root, err := g.GetRoot(cwd)
        if err != nil {
            output.Fatal("Not in a git worktree: %v", err)
        }
        
        hookRunner := hooks.NewRunner(config, fs)
        if err := hookRunner.InstallHooks(root); err != nil {
            output.Fatal("Failed to install hooks: %v", err)
        }
        
        output.Info("Hooks installed in: %s", root)
    },
}
```

**Location:** New [install-hooks.go](../../cmd/install-hooks.go)

### git hop prune

**Function:** Clean orphaned entries.

**Updates:**
1. Verify orphaned worktrees
2. Remove from state.json
3. Remove orphaned git metadata

**Code:**
```go
func (cmd *cobra.Command) RunPrune(args []string) {
    state := LoadState()
    
    for repoID, repo := range state.Repositories {
        for branch, wt := range repo.Worktrees {
            // Verify worktree still exists
            if _, err := os.Stat(wt.Path); err != nil {
                // Remove from state
                delete(repo.Worktrees, branch)
                output.Info("Pruned orphaned worktree: %s", branch)
            }
        }
        
        // Check hubs still exist
        for i, hub := range repo.Hubs {
            if _, err := os.Stat(hub.Path); err != nil {
                repo.Hubs = append(repo.Hubs[:i], repo.Hubs[i+1:]...)
                output.Info("Pruned orphaned hub: %s", hub.Path)
            }
        }
    }
    
    SaveState(state)
}
```

**Location:** [prune.go](../../cmd/prune.go) (updated)

### git hop doctor

**Function:** Validate state consistency.

**Updates:**
1. Validate worktree state
2. Verify hub paths exist
3. Check config consistency
4. Detect orphaned artifacts

**Code:**
```go
func (cmd *cobra.Command) RunDoctor(args []string) {
    state := LoadState()
    issuesFound := false
    
    // Validate worktree state
    for repoID, repo := range state.Repositories {
        for branch, wt := range repo.Worktrees {
            if _, err := os.Stat(wt.Path); err != nil {
                output.Error("Worktree not found: %s:%s", repoID, branch)
                issuesFound = true
            }
        }
    }
    
    // Fix mode
    if doctorFix {
        output.Info("Auto-fixing issues...")
        // Re-scan worktrees, update state
        // Prune orphaned entries
        SaveState(state)
    }
}
```

**Location:** [doctor.go](../../cmd/doctor.go) (updated)

## Diagrams

- [State Update Flow](../diagrams/state-update-flow.mmd)
- [Worktree Verification Flow](../diagrams/worktree-verification-flow.mmd)

## Related

- [Hooks System](hooks-system.md)
- [Worktree Verification](worktree-verification.md)
- [Migration Guide](migration-guide.md)
- [State Schema](../schemas/state-json.md)
