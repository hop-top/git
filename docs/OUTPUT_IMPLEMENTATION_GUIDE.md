# Output Enhancement Implementation Guide

Guide for implementing the enhanced output across all git-hop commands.

## ✅ Completed Foundation (Phase 1)

The following core infrastructure is now ready:

### Files Created

1. **`internal/output/icons.go`** - Icon and symbol constants
2. **`internal/output/styles.go`** - Color palette and lipgloss styles
3. **`internal/output/cards.go`** - Cards, sections, banners
4. **`internal/output/tables.go`** - Enhanced table builders
5. **`internal/output/prompts.go`** - Interactive confirmation prompts
6. **`internal/output/spinner.go`** - Spinner support (already existed)
7. **`internal/output/progress.go`** - Progress bars (already existed)

### Demonstration

Run the demo to see all available components:

```bash
go run examples/enhanced_output_demo.go
```

### Example: Enhanced List Command ✅

The `git hop list` command has been updated to demonstrate the new system:

- Status table with colored icons
- Summary counts
- Legend for symbols
- Graceful fallback for non-human modes

## 📋 Next Steps: Command Integration

### High Priority Commands

#### 1. `git hop <uri>` - Clone Flow

**Location**: `cmd/root.go` or similar

**Implementation**:
```go
import "github.com/jadb/git-hop/internal/output"

// Show header
fmt.Println(output.SimpleHeader("Cloning github.com/org/repo"))
fmt.Println()

// Multi-step progress
spinner := output.NewSpinner("Cloning repository...")
spinner.Start()
// ... do clone ...
spinner.Success("Cloned repository")

spinner = output.NewSpinner("Checking out main branch...")
spinner.Start()
// ... do checkout ...
spinner.Success("Checked out main")

// Success card
card := output.SuccessCard("Repository Ready", []output.CardField{
    {Key: "Hub Path", Value: hubPath},
    {Key: "Worktree", Value: worktreePath},
    {Key: "Branch", Value: branch},
    {Key: "Services", Value: "api, db (ports: 11500-11502)"},
})
fmt.Println(card)

// Next step hint
fmt.Println(output.NextStepHint("cd org/repo && git hop feature-branch"))
```

#### 2. `git hop <branch>` - Worktree Creation

**Location**: `cmd/root.go` or `cmd/add.go`

**Implementation**:
```go
fmt.Println(output.RenderHeader("Creating worktree: " + branch))
fmt.Println()

steps := []struct{ msg string; fn func() error }{
    {"Validating branch", validateBranch},
    {"Creating worktree", createWorktree},
    {"Allocating ports", allocatePorts},
    {"Generating environment", generateEnv},
}

for i, step := range steps {
    spinner := output.NewSpinner(fmt.Sprintf("[%d/%d] %s...", i+1, len(steps), step.msg))
    spinner.Start()

    if err := step.fn(); err != nil {
        spinner.Error(err.Error())
        return err
    }
    spinner.Success(step.msg)
}

card := output.SuccessCard("Worktree Ready: "+branch, []output.CardField{
    {Key: "Path", Value: worktreePath},
    {Key: "Services", Value: "api (11500), db (11501)"},
    {Key: "Volumes", Value: "2 persistent volumes created"},
    {Key: "Env File", Value: ".env generated"},
})
fmt.Println(card)
```

#### 3. `git hop status` - Detailed View

**Location**: `cmd/status.go`

**Implementation**:
```go
fmt.Println(output.RenderHeader("Worktree: " + branch))
fmt.Println()

// Repository section
repoInfo := output.Section(output.IconRepo, "Repository", []string{
    output.RenderKeyValue("Branch", branch),
    output.RenderKeyValue("Remote", remote),
    output.RenderKeyValue("Status", status),
    output.RenderKeyValue("Path", path),
})
fmt.Println(repoInfo)

// Environment section
envInfo := output.Section(output.IconDocker, "Environment", []string{
    output.RenderKeyValue("Status", statusIcon + " Running"),
    output.RenderKeyValue("Started", "2h ago"),
    "",
    "Services:",
    output.TreeItem(false, "api", statusText),
    output.TreeItem(false, "db", statusText),
    output.TreeItem(true, "cache", statusText),
})
fmt.Println(envInfo)

// Dependencies section
depsInfo := output.Section(output.IconPackage, "Dependencies", []string{
    output.RenderKeyValue("Status", "✓ Synced"),
    output.RenderKeyValue("Shared", "Yes (with main, feature-y)"),
})
fmt.Println(depsInfo)
```

#### 4. `git hop add` - Add Worktree

**Location**: `cmd/add.go`

Similar to branch creation flow - use multi-step spinner + success card.

#### 5. `git hop init` - Initialize

**Location**: `cmd/init.go`

Use multi-step spinner for setup wizard + success card for completion.

### Medium Priority Commands

#### 6. `git hop env start` - Service Startup

**Location**: `cmd/env.go`

**Implementation**:
```go
fmt.Println(output.RenderHeader("Starting environment for " + branch))
fmt.Println()

// Service tree with spinners
services := []string{"api", "db", "cache"}
for _, svc := range services {
    spinner := output.NewSpinner(fmt.Sprintf("Starting %s...", svc))
    spinner.Start()

    if err := startService(svc); err != nil {
        spinner.Error(err.Error())
        continue
    }
    spinner.Success(fmt.Sprintf("%s started (%s)", svc, port))
}

card := output.SuccessCard("Environment Running", []output.CardField{
    {Key: "Services", Value: "3 running"},
    {Key: "Ports", Value: "11500-11502"},
    {Key: "Logs", Value: "docker compose logs -f"},
})
fmt.Println(card)
```

#### 7. `git hop remove` - Remove Worktree

**Location**: `cmd/remove.go`

**Implementation**:
```go
// Show impact
if !output.ConfirmDeletion("feature-x worktree", []output.CardField{
    {Key: "Path", Value: path},
    {Key: "Changes", Value: "3 uncommitted files"},
    {Key: "Services", Value: "2 running (will stop)"},
}) {
    return
}

// Progress
spinner := output.NewSpinner("Removing worktree...")
spinner.Start()
// ... do removal ...
spinner.Success("Worktree removed")
```

#### 8. `git hop doctor` - Health Check

**Location**: `cmd/doctor.go`

**Implementation**:
```go
fmt.Println(output.RenderHeader("Running diagnostics..."))
fmt.Println()

table := output.NewStatusTable("Check", "Status", "Details")

checks := []struct{ name, status, details string }{
    {"Git Installation", "success", "v2.45.0"},
    {"Docker Available", "success", "24.0.7"},
    {"Config Files", "warning", "hop.json outdated"},
    {"Orphaned Symlinks", "error", "2 broken links found"},
}

for _, check := range checks {
    table.AddRow(check.status, check.name, check.status, check.details)
}
table.Print()

// Issues summary
if errorCount > 0 || warnCount > 0 {
    fmt.Println()
    fmt.Printf("Issues found: %d error, %d warning\n", errorCount, warnCount)
    fmt.Println()
    fmt.Println(output.NextStepHint("git hop doctor --fix"))
}
```

### Low Priority Commands

#### 9. `git hop prune`

Show progress bar for scanning, table for review, summary for cleanup.

#### 10. `git hop migrate`

Multi-step spinner for migration phases + backup info card.

#### 11. `git hop install-hooks`

Simple status lines with checkmarks for each hook.

## 🎨 Style Guidelines

### When to Use Each Component

| Component | Use Case | Example |
|-----------|----------|---------|
| `SuccessCard` | Operation completion | Worktree created, environment ready |
| `WarningCard` | Confirmations, risks | Removal confirmation, uncommitted changes |
| `InfoCard` | General information | Configuration display |
| `StatusTable` | List views | Worktrees, services, checks |
| `Section` | Grouped information | Repository info, environment details |
| `TreeItem` | Hierarchical data | Service lists, volume lists |
| `StatusLine` | Simple feedback | Single operation result |
| `Spinner` | Active operations | Cloning, starting services |
| `Progress` | Long operations | File downloads, migrations |
| `Confirm` | User decisions | Destructive actions |

### Color Usage

- **Green** (success): Active, running, healthy, completed
- **Red** (error): Failed, missing, broken, stopped
- **Yellow** (warning): Attention needed, dirty state, outdated
- **Blue** (info): Neutral information, stopped services
- **Gray** (muted): Secondary information, legend

### Icon Usage

- **✓** Success, active, pass
- **✗** Error, fail, dirty, missing
- **⚠** Warning, attention needed
- **●** Running, active service
- **○** Stopped, clean, neutral
- **📁** Repository, files
- **🐳** Docker, services
- **📦** Dependencies, packages
- **💾** Volumes, storage
- **🔧** Configuration

## 🧪 Testing Checklist

For each command you enhance:

- [ ] Test in `ModeHuman` (default)
- [ ] Test in `ModeJSON` (should output JSON)
- [ ] Test in `ModePorcelain` (simple text)
- [ ] Test in `ModeQuiet` (minimal output)
- [ ] Test with `--verbose` flag
- [ ] Test without TTY (redirect to file)
- [ ] Test on narrow terminal (80 chars)
- [ ] Test on wide terminal (120+ chars)

## 📝 Code Patterns

### Basic Pattern

```go
// Check mode first
if output.CurrentMode != output.ModeHuman {
    // Fallback to simple output
    output.Info("Simple message")
    return
}

// Enhanced output for human mode
fmt.Println(output.RenderHeader("Title"))
// ... styled content ...
```

### Multi-Step Operation Pattern

```go
steps := []struct {
    name string
    fn   func() error
}{
    {"Step 1", step1Func},
    {"Step 2", step2Func},
}

for i, step := range steps {
    spinner := output.NewSpinner(fmt.Sprintf("[%d/%d] %s...", i+1, len(steps), step.name))
    spinner.Start()

    if err := step.fn(); err != nil {
        spinner.Error(err.Error())
        return err
    }
    spinner.Success(step.name)
}
```

### Confirmation Pattern

```go
if !output.ConfirmDeletion(target, []output.CardField{
    {Key: "Path", Value: path},
    {Key: "Impact", Value: impact},
}) {
    output.Info("Cancelled")
    return nil
}

// Proceed with deletion
```

## 🚀 Quick Migration Guide

### Before (old pattern):

```go
output.Info("Creating worktree for '%s'...", branch)
// ... work ...
output.Success("Worktree ready")
```

### After (enhanced pattern):

```go
if output.CurrentMode != output.ModeHuman {
    output.Info("Creating worktree for '%s'...", branch)
    // ... work ...
    output.Success("Worktree ready")
    return
}

spinner := output.NewSpinner("Creating worktree...")
spinner.Start()
// ... work ...
spinner.Success("Worktree created")

card := output.SuccessCard("Worktree Ready", []output.CardField{
    {Key: "Branch", Value: branch},
    {Key: "Path", Value: path},
})
fmt.Println(card)
```

## 💡 Tips

1. **Always check mode**: Use `output.CurrentMode != output.ModeHuman` to provide fallbacks
2. **Use spinners for active ops**: Any operation that takes >0.5s
3. **Use cards for summaries**: Show key info after operations complete
4. **Use tables for lists**: Multiple items with structured data
5. **Use sections for grouping**: Related information under emoji headers
6. **Add legends**: When using symbols, add a legend at the bottom
7. **Provide next steps**: Use `NextStepHint` to guide users
8. **Keep it simple**: Don't over-design; clarity > decoration

## 🔗 Resources

- **Demo**: `examples/enhanced_output_demo.go`
- **Proposal**: `docs/OUTPUT_ENHANCEMENT_PROPOSAL.md`
- **Quick Ref**: `docs/OUTPUT_QUICK_REF.md`
- **Example**: `cmd/list.go` (enhanced list command)

## 📊 Progress Tracking

Track your progress by checking off commands as you enhance them:

**High Priority:**
- [x] `git hop list` (✅ completed as demo)
- [ ] `git hop <uri>` (clone)
- [ ] `git hop <branch>` (worktree)
- [ ] `git hop status`
- [ ] `git hop add`
- [ ] `git hop init`

**Medium Priority:**
- [ ] `git hop env start`
- [ ] `git hop env stop`
- [ ] `git hop remove`
- [ ] `git hop doctor`

**Low Priority:**
- [ ] `git hop prune`
- [ ] `git hop migrate`
- [ ] `git hop install-hooks`
