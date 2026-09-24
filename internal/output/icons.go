package output

import "charm.land/bubbles/v2/spinner"

// Status words. Output is plain ASCII like git's, and git names states
// in words ("modified", "deleted", "ahead") rather than symbols, so the
// status indicators are words too.
const (
	IconSuccess = "ok"
	IconError   = "error"
	IconWarning = "warning"
	IconRunning = "running"
	IconStopped = "stopped"
	IconClean   = "clean"
	IconDirty   = "dirty"
	IconActive  = "active"
)

// Tree structure elements, as drawn by tree(1) with --charset=ascii.
const (
	IconTreeBranch = "|-"
	IconTreeLast   = "`-"
	IconTreeLine   = "|"
	IconTreeSpace  = "  "
)

// Navigation and action hints
const (
	IconArrow       = ">"
	IconArrowRight  = "->"
	IconArrowLeft   = "<-"
	IconArrowUp     = "^"
	IconArrowDown   = "v"
	IconBulletPoint = "-"
)

// SpinnerFrames is the classic |/-\ spinner.
var SpinnerFrames = spinner.Line.Frames
