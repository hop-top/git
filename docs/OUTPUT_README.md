# Git-Hop Output Enhancement System

Modern, polished CLI output for git-hop using Charm's Bubbles and Lipgloss.

## 🎨 Quick Start

```go
import "github.com/jadb/git-hop/internal/output"

// Success card
card := output.SuccessCard("Operation Complete", []output.CardField{
    {Key: "Path", Value: "/path/to/worktree"},
    {Key: "Status", Value: "Ready"},
})
fmt.Println(card)

// Status table
table := output.NewStatusTable("Item", "Status", "Details")
table.AddRow("success", "Worktree", "Active", "~/code/repo/main")
table.AddRow("warning", "Config", "Outdated", "Need update")
table.Print()

// Spinner
spinner := output.NewSpinner("Processing...")
spinner.Start()
// ... do work ...
spinner.Success("Done")
```

## 📦 Available Components

### Cards & Banners
- `SuccessCard()` - Green card for completed operations
- `WarningCard()` - Yellow card for confirmations
- `ErrorCard()` - Red card for errors
- `InfoCard()` - Blue card for information
- `SimpleHeader()` - Bordered header text
- `Banner()` - Text banner

### Tables
- `NewTable()` - Basic table builder
- `NewStatusTable()` - Table with status icons
- `SummaryTable()` - Key-value table
- `CompactList()` - Bulleted list
- `AlignedList()` - Aligned label-value list
- `Legend()` - Symbol legend

### Sections & Trees
- `Section()` - Emoji-headed section
- `TreeItem()` - Tree structure item
- `StatusLine()` - Single status line

### Interactive
- `Confirm()` - Yes/no prompt
- `ConfirmWithWarning()` - Styled warning prompt
- `ConfirmDeletion()` - Deletion confirmation with preview
- `Select()` - Option selection
- `Input()` - Text input
- `InputWithDefault()` - Input with default value

### Styling
- `Colorize()` - Apply color based on status
- `RenderKeyValue()` - Styled key-value pair
- `RenderHeader()` - Styled header
- `RenderPath()` - Styled file path
- `NextStepHint()` - Action hint with arrow

### Dynamic
- `NewSpinner()` - Operation spinner (existing)
- `NewProgress()` - Progress bar (existing)

## 🎯 Icons Reference

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
output.IconArrowRight // →
```

## 🎨 Color Scheme

```go
// Status colors
ColorSuccess      // Green
ColorError        // Red
ColorWarning      // Yellow
ColorInfo         // Blue
ColorMuted        // Gray
ColorAccent       // Pink

// Usage
Colorize(text, "success")  // Green text
Colorize(text, "error")    // Red text
Colorize(text, "warning")  // Yellow text
```

## 📋 Output Modes

All components respect the output mode:

- **`ModeHuman`** - Rich, styled output (default)
- **`ModeJSON`** - JSON output
- **`ModePorcelain`** - Plain text for scripts
- **`ModeQuiet`** - Minimal output

```go
if output.CurrentMode != output.ModeHuman {
    // Fallback to simple output
    output.Info("Simple message")
    return
}

// Enhanced output for humans
fmt.Println(output.SuccessCard(...))
```

## 🧪 Demo

Run the comprehensive demo:

```bash
go run examples/enhanced_output_demo.go
```

## 📚 Documentation

- **Implementation Guide**: `docs/OUTPUT_IMPLEMENTATION_GUIDE.md`
- **Full Proposal**: `docs/OUTPUT_ENHANCEMENT_PROPOSAL.md`
- **Quick Reference**: `docs/OUTPUT_QUICK_REF.md`

## ✅ Status

**Completed:**
- ✅ Core infrastructure (icons, styles, cards, tables, prompts)
- ✅ `git hop list` (demonstration)
- ✅ Comprehensive demo
- ✅ Documentation

**Next Steps:**
- [ ] `git hop <uri>` (clone flow)
- [ ] `git hop <branch>` (worktree creation)
- [ ] `git hop status` (detailed view)
- [ ] Additional commands (see implementation guide)

## 🎯 Example: Before & After

**Before:**
```
Creating worktree for 'feature-x'...
Environment generated
Worktree ready
```

**After:**
```
Creating worktree: feature-x

  [1/4] ✓ Validating branch (0.1s)
  [2/4] ✓ Creating worktree (0.8s)
  [3/4] ✓ Allocating ports (0.2s)
  [4/4] ✓ Generating environment (0.3s)

┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ✓ Worktree Ready: feature-x                     ┃
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃  Path        │ ~/code/org/repo/feature-x        ┃
┃  Services    │ api (11500), db (11501)          ┃
┃  Volumes     │ 2 persistent volumes created     ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

▶ cd feature-x && git hop env start
```
