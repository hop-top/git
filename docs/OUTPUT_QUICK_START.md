# Quick Start: Using Enhanced Output

Get started with git-hop's new enhanced output system in 5 minutes.

## Try It Now

### 1. See the Demo
```bash
go run examples/enhanced_output_demo.go
```

This shows all available components in action.

### 2. Try Enhanced Commands

```bash
# See all worktrees with rich formatting
./git-hop list

# NEW: See system-wide git-hop status
./git-hop status --all
```

## Basic Usage

### Success Cards

```go
import "github.com/jadb/git-hop/internal/output"

card := output.SuccessCard("Operation Complete", []output.CardField{
    {Key: "Path", Value: "~/code/repo"},
    {Key: "Status", Value: "Ready"},
})
fmt.Println(card)
```

Output:
```
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ✓ Operation Complete                           ┃
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃  Path   │ ~/code/repo                          ┃
┃  Status │ Ready                                ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

### Status Tables

```go
table := output.NewStatusTable("Item", "Status", "Details")
table.AddRow("success", "Worktree", "Active", "Running")
table.AddRow("error", "Service", "Down", "Error")
table.Print()
```

Output:
```
┏━━━━━━━━━━━┯━━━━━━━━┯━━━━━━━━━┓
┃ Item      │ Status │ Details ┃
┣━━━━━━━━━━━┿━━━━━━━━┿━━━━━━━━━┫
┃ ✓ Worktree│ Active │ Running ┃
┃ ✗ Service │ Down   │ Error   ┃
┗━━━━━━━━━━━┷━━━━━━━━┷━━━━━━━━━┛
```

### Sections with Trees

```go
section := output.Section(output.IconDocker, "Services", []string{
    "Status: Running",
    "",
    "Services:",
    output.TreeItem(false, "api", "● Running (11500)"),
    output.TreeItem(true, "db", "● Running (11501)"),
})
fmt.Println(section)
```

Output:
```
🐳 Services
  Status: Running

  Services:
    ├─ api        ● Running (11500)
    └─ db         ● Running (11501)
```

### Spinners

```go
spinner := output.NewSpinner("Processing...")
spinner.Start()

// ... do work ...

spinner.Success("Done")
```

### Confirmations

```go
if output.Confirm("Delete this worktree?") {
    // Proceed with deletion
}

// Or with warning card
if output.ConfirmDeletion("feature-x", []output.CardField{
    {Key: "Path", Value: path},
    {Key: "Changes", Value: "3 uncommitted"},
}) {
    // Proceed
}
```

## Mode-Aware Code

Always respect output modes:

```go
if output.CurrentMode != output.ModeHuman {
    // Simple fallback for JSON/porcelain/quiet
    output.Info("Simple message")
    return
}

// Enhanced output for humans
fmt.Println(output.SuccessCard(...))
```

## Common Patterns

### Multi-Step Operation

```go
steps := []struct{ name string; fn func() error }{
    {"Validate", validate},
    {"Create", create},
    {"Setup", setup},
}

for i, step := range steps {
    spinner := output.NewSpinner(
        fmt.Sprintf("[%d/%d] %s...", i+1, len(steps), step.name))
    spinner.Start()

    if err := step.fn(); err != nil {
        spinner.Error(err.Error())
        return err
    }
    spinner.Success(step.name)
}
```

### Result Summary

```go
card := output.SuccessCard("Worktree Ready", []output.CardField{
    {Key: "Branch", Value: branch},
    {Key: "Path", Value: path},
    {Key: "Services", Value: "2 running"},
})
fmt.Println(card)

// Add next step hint
fmt.Println(output.NextStepHint("cd " + branch))
```

### Status Display

```go
info := output.Section(output.IconRepo, "Repository", []string{
    output.RenderKeyValue("Branch", "main"),
    output.RenderKeyValue("Status", status),
    output.RenderKeyValue("Path", path),
})
fmt.Println(info)
```

## Available Icons

```go
// Status
output.IconSuccess   // ✓
output.IconError     // ✗
output.IconWarning   // ⚠
output.IconRunning   // ●
output.IconStopped   // ○

// Category
output.IconRepo      // 📁
output.IconDocker    // 🐳
output.IconPackage   // 📦
output.IconVolume    // 💾
output.IconConfig    // 🔧

// Tree
output.IconTreeBranch // ├─
output.IconTreeLast   // └─
```

## Available Colors

```go
output.Colorize("text", "success")  // Green
output.Colorize("text", "error")    // Red
output.Colorize("text", "warning")  // Yellow
output.Colorize("text", "info")     // Blue
output.Colorize("text", "muted")    // Gray
```

## Next Steps

1. **Read the full guide**: `docs/OUTPUT_IMPLEMENTATION_GUIDE.md`
2. **See all components**: `docs/OUTPUT_README.md`
3. **Review examples**: Look at `cmd/list.go` and `cmd/status.go`
4. **Check the spec**: `docs/OUTPUT_ENHANCEMENT_PROPOSAL.md`

## Tips

1. ✅ Always provide mode-aware fallbacks
2. ✅ Use icons for status + color for accessibility
3. ✅ Add legends when using symbols
4. ✅ Include summary lines with counts
5. ✅ Use emoji headers for sections
6. ✅ Add next-step hints where helpful
7. ✅ Keep it simple - clarity over decoration

## Common Mistakes to Avoid

❌ **Don't**: Only enhance for human mode
```go
fmt.Println(output.SuccessCard(...))  // Fails in other modes
```

✅ **Do**: Check mode first
```go
if output.CurrentMode != output.ModeHuman {
    output.Info("Simple")
    return
}
fmt.Println(output.SuccessCard(...))
```

❌ **Don't**: Use color without icons
```go
fmt.Println(output.Colorize("Failed", "error"))  // Hard for color-blind
```

✅ **Do**: Combine icon + color
```go
fmt.Println(output.StatusLine("error", "Failed"))  // ✗ Failed
```

❌ **Don't**: Forget summaries
```go
table.Print()  // Just the table
```

✅ **Do**: Add context
```go
table.Print()
fmt.Printf("\nTotal: %d items\n", count)
```

## Questions?

- Check `docs/OUTPUT_README.md` for component reference
- See `examples/enhanced_output_demo.go` for live examples
- Review `cmd/list.go` for a complete implementation example
