package output

// Status indicators
const (
	IconSuccess = "✓"
	IconError   = "✗"
	IconWarning = "⚠"
	IconRunning = "●"
	IconStopped = "○"
	IconClean   = "○"
	IconDirty   = "✗"
	IconActive  = "✓"
)

// Category icons (emoji)
const (
	IconRepo    = "📁"
	IconDocker  = "🐳"
	IconPackage = "📦"
	IconVolume  = "💾"
	IconConfig  = "🔧"
	IconNetwork = "🌐"
	IconSpeed   = "⚡"
	IconHealth  = "❤️"
)

// Tree structure elements
const (
	IconTreeBranch = "├─"
	IconTreeLast   = "└─"
	IconTreeLine   = "│"
	IconTreeSpace  = "  "
)

// Navigation and action hints
const (
	IconArrow       = "▶"
	IconArrowRight  = "→"
	IconArrowLeft   = "←"
	IconArrowUp     = "↑"
	IconArrowDown   = "↓"
	IconBulletPoint = "•"
)

// Spinner frames (for manual spinner if needed)
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
