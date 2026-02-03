# Output Package

Modern, polished CLI output for git-hop with rich visual components using Charm's Bubbles and Lipgloss.

## 🎨 Features

- **Rich Visual Components**: Cards, tables, sections, trees, banners
- **Styled Output**: Colors, icons, borders using Lipgloss
- **Dynamic Feedback**: Spinners, progress bars, multi-step progress
- **Interactive Prompts**: Confirmations, selections, text input
- **Mode-Aware**: Automatically adapts to human, JSON, porcelain, and quiet modes
- **Git-Style Messages**: Familiar error/warning/info patterns

## 📦 Components

### Visual Components (NEW)
- **Cards**: `SuccessCard`, `WarningCard`, `ErrorCard`, `InfoCard`
- **Tables**: `NewTable`, `NewStatusTable`, `SummaryTable`
- **Sections**: `Section` (with emoji headers)
- **Trees**: `TreeItem` (hierarchical data)
- **Banners**: `SimpleHeader`, `Banner`
- **Status Lines**: `StatusLine` (colored status)
- **Legends**: `Legend` (symbol explanations)

### Dynamic Components
- **Spinners**: Visual feedback for long-running operations
- **Progress Bars**: Track progress of operations with known duration
- **Multi-Step Progress**: Show progress across sequential steps

### Interactive Components (NEW)
- **Confirm**: Yes/no prompts
- **Select**: Option selection from list
- **Input**: Text input with optional defaults
- **ConfirmDeletion**: Deletion preview and confirmation

### Styling (NEW)
- **Colors**: Success (green), error (red), warning (yellow), info (blue), muted (gray)
- **Icons**: ✓✗⚠●○ for status, 📁🐳📦💾🔧 for categories
- **Tree Elements**: ├─└─│ for hierarchical display

## 🚀 Quick Start

### Success Card
```go
card := output.SuccessCard("Operation Complete", []output.CardField{
    {Key: "Path", Value: "~/code/repo"},
    {Key: "Status", Value: "Ready"},
})
fmt.Println(card)
```

### Status Table
```go
table := output.NewStatusTable("Item", "Status", "Details")
table.AddRow("success", "Worktree", "Active", "Ready")
table.AddRow("error", "Service", "Down", "Error")
table.Print()
```

### Section with Tree
```go
section := output.Section(output.IconDocker, "Services", []string{
    "Status: Running",
    "",
    output.TreeItem(false, "api", "● Running (11500)"),
    output.TreeItem(true, "db", "● Running (11501)"),
})
fmt.Println(section)
```

### Spinner
```go
spinner := output.NewSpinner("Processing...")
spinner.Start()
// ... do work ...
spinner.Success("Done")
```

### Confirmation
```go
if output.Confirm("Continue?") {
    // Proceed
}

// Or with preview
if output.ConfirmDeletion("worktree", []output.CardField{
    {Key: "Path", Value: path},
    {Key: "Changes", Value: "3 uncommitted"},
}) {
    // Delete
}
```

## 📋 Classic Usage (Still Supported)

### Spinners
```go
// Simple spinner
spinner := output.NewSpinner("Cloning repository...")
spinner.Start()
// ... do work ...
spinner.Stop()

// With error handling
if err != nil {
    spinner.StopWithError(err)
}

// Convenience wrapper
err := output.WithSpinner("Processing files...", func() error {
    return doWork()
})
```

### Progress Bars
```go
pb := output.NewProgressBar("Downloading dependencies")
pb.Start()

for i := 0; i <= 100; i++ {
    pb.Update(float64(i) / 100.0)
    time.Sleep(30 * time.Millisecond)
}

pb.Finish()
```

### Multi-Step Progress
```go
steps := []string{
    "Initializing repository",
    "Creating worktree",
    "Setting up environment",
}

msp := output.NewMultiStepProgress(steps)
msp.Start()

for i := 0; i < len(steps); i++ {
    // ... do work for step ...
    msp.Next()
}

msp.Finish()
```

### Simple Messages
```go
output.Info("Repository cloned successfully")
output.Success("Operation completed!")
output.Warn("Branch already exists")
output.Error("Failed to connect: %v", err)
output.Fatal("Configuration file not found")
output.Debug("Processing file: %s", filename)
```

## 🎯 Mode-Aware Pattern

Always check mode before using rich components:

```go
if output.CurrentMode != output.ModeHuman {
    // Simple fallback for JSON/porcelain/quiet
    output.Info("Operation complete")
    return
}

// Rich output for humans
fmt.Println(output.SuccessCard(...))
```

## 📋 Output Modes

- **ModeHuman**: Rich visual feedback with colors, icons, spinners
- **ModeJSON**: Structured JSON output, no visual elements
- **ModePorcelain**: Minimal machine-parseable output
- **ModeQuiet**: Only errors and critical messages

Set the mode using:
```go
output.SetupLogger(output.ModeHuman, verbose)
```

## 🎨 Icons Reference

```go
// Status
output.IconSuccess   // ✓
output.IconError     // ✗
output.IconWarning   // ⚠
output.IconRunning   // ●
output.IconStopped   // ○

// Category (emoji)
output.IconRepo      // 📁
output.IconDocker    // 🐳
output.IconPackage   // 📦
output.IconVolume    // 💾
output.IconConfig    // 🔧

// Tree
output.IconTreeBranch // ├─
output.IconTreeLast   // └─
output.IconTreeLine   // │

// Action
output.IconArrow      // ▶
```

## 🎨 Color Functions

```go
// Colorize text based on status
output.Colorize("text", "success")  // Green
output.Colorize("text", "error")    // Red
output.Colorize("text", "warning")  // Yellow
output.Colorize("text", "info")     // Blue

// Colorize icons
output.ColorizeIcon(output.IconSuccess, "success")

// Styled rendering
output.RenderHeader("Title")
output.RenderKeyValue("Key", "Value")
output.RenderPath("/path/to/file")
```

## 📝 Files

- `logger.go` - Core logging and mode handling
- `spinner.go` - Spinner progress indicators
- `progress.go` - Progress bars and multi-step
- **`icons.go`** - Icon and symbol constants (NEW)
- **`styles.go`** - Color palette and lipgloss styles (NEW)
- **`cards.go`** - Cards, sections, banners (NEW)
- **`tables.go`** - Table builders and status tables (NEW)
- **`prompts.go`** - Interactive prompts (NEW)

## 🎯 Examples

### Complete Demo
```bash
go run examples/enhanced_output_demo.go
```

### Real Implementations
- `cmd/list.go` - Enhanced list command with status table
- `cmd/status.go` - Enhanced status with system-wide view

## 📚 Documentation

- **Quick Start**: `../../docs/OUTPUT_QUICK_START.md`
- **Complete Guide**: `../../docs/OUTPUT_README.md`
- **Implementation**: `../../docs/OUTPUT_IMPLEMENTATION_GUIDE.md`
- **Full Proposal**: `../../docs/OUTPUT_ENHANCEMENT_PROPOSAL.md`
- **Quick Reference**: `../../docs/OUTPUT_QUICK_REF.md`

## 💡 Best Practices

1. ✅ **Check mode first**: Use `output.CurrentMode != output.ModeHuman`
2. ✅ **Provide fallbacks**: Simple text for non-human modes
3. ✅ **Use icons + color**: Accessibility for color-blind users
4. ✅ **Add legends**: When using symbols in tables
5. ✅ **Include summaries**: Show counts and totals
6. ✅ **Stop indicators**: Always finish spinners/progress bars
7. ✅ **Emoji headers**: Use for visual section breaks

## 🎨 Component Examples

### Card Types
```go
output.SuccessCard(title, fields)  // ✓ Green border
output.WarningCard(title, fields)  // ⚠ Yellow border
output.ErrorCard(title, fields)    // ✗ Red border
output.InfoCard(title, fields)     // Blue border
```

### Table Types
```go
output.NewTable(headers...)           // Basic table
output.NewStatusTable(headers...)     // With status icons
output.SummaryTable(keyValueMap)      // Key-value pairs
output.CompactList(items, status)     // Bulleted list
output.AlignedList(labelValuePairs)   // Aligned pairs
```

### Status Lines
```go
output.StatusLine("success", "Done")   // ✓ Done (green)
output.StatusLine("error", "Failed")   // ✗ Failed (red)
output.StatusLine("warning", "Alert")  // ⚠ Alert (yellow)
```

### Interactive
```go
output.Confirm("Continue?")
output.ConfirmWithWarning("Title", "Message")
output.Select("Choose:", options)
output.Input("Name:")
output.InputWithDefault("Port:", "8080")
```

## 🚀 Migration from Old API

Old code continues to work. To use new features:

```go
// Old: Plain text
output.Info("Repository: %s", repo)
output.Info("Worktrees: %d", count)

// New: Rich card
card := output.SuccessCard("Repository Ready", []output.CardField{
    {Key: "Repository", Value: repo},
    {Key: "Worktrees", Value: fmt.Sprintf("%d", count)},
})
fmt.Println(card)
```

## 📊 What's New

- ✅ Rich visual components (cards, tables, sections)
- ✅ Status tables with colored icons
- ✅ Interactive prompts
- ✅ Comprehensive icon set
- ✅ Color styling system
- ✅ Tree structure display
- ✅ Mode-aware by design
- ✅ Full documentation suite
