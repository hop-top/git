package output

import (
	"github.com/charmbracelet/lipgloss"
)

// Color definitions
var (
	ColorSuccess      = lipgloss.Color("2")   // Green
	ColorSuccessBold  = lipgloss.Color("10")  // Bright Green
	ColorError        = lipgloss.Color("1")   // Red
	ColorErrorBold    = lipgloss.Color("9")   // Bright Red
	ColorWarning      = lipgloss.Color("3")   // Yellow
	ColorWarningBold  = lipgloss.Color("11")  // Bright Yellow
	ColorInfo         = lipgloss.Color("4")   // Blue
	ColorInfoBold     = lipgloss.Color("12")  // Bright Blue
	ColorMuted        = lipgloss.Color("240") // Dark Gray
	ColorNeutral      = lipgloss.Color("8")   // Gray
	ColorAccent       = lipgloss.Color("205") // Pink
	ColorAccentBold   = lipgloss.Color("212") // Bright Pink
	ColorCyan         = lipgloss.Color("14")  // Cyan for paths
	ColorMagenta      = lipgloss.Color("5")   // Magenta for config
)

// Text styles
var (
	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	StyleInfo = lipgloss.NewStyle().
			Foreground(ColorInfo)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleAccent = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	StyleHeader = lipgloss.NewStyle().
			Foreground(ColorAccentBold).
			Bold(true)

	StylePath = lipgloss.NewStyle().
			Foreground(ColorCyan)

	StyleKey = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Bold(false)

	StyleValue = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")) // Bright white
)

// Border styles
var (
	StyleBorderSuccess = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorSuccess).
				Padding(0, 1)

	StyleBorderWarning = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorWarning).
				Padding(0, 1)

	StyleBorderError = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorError).
				Padding(0, 1)

	StyleBorderInfo = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorInfo).
			Padding(0, 1)

	StyleBorderNeutral = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorNeutral).
				Padding(0, 1)

	// Heavy border for emphasis (success cards, etc.)
	StyleBorderHeavy = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(ColorSuccess).
				Padding(0, 1)
)

// Box styles (simpler, no rounded borders)
var (
	StyleBoxHeader = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, true, true, true).
			BorderForeground(ColorAccent).
			Padding(0, 1).
			Bold(true)

	StyleBoxSection = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(ColorMuted).
			PaddingLeft(1)
)

// Utility functions

// Colorize applies the appropriate color style based on status
func Colorize(text string, status string) string {
	switch status {
	case "success", "running", "active", "pass", "up":
		return StyleSuccess.Render(text)
	case "error", "failed", "fail", "down", "broken":
		return StyleError.Render(text)
	case "warning", "warn", "attention":
		return StyleWarning.Render(text)
	case "info", "neutral", "stopped", "clean":
		return StyleInfo.Render(text)
	case "muted", "inactive":
		return StyleMuted.Render(text)
	default:
		return text
	}
}

// ColorizeIcon returns the icon with appropriate color
func ColorizeIcon(icon string, status string) string {
	return Colorize(icon, status)
}

// RenderKeyValue renders a key-value pair with styling
func RenderKeyValue(key, value string) string {
	return StyleKey.Render(key) + " " + StyleValue.Render(value)
}

// RenderHeader renders a header with accent color
func RenderHeader(text string) string {
	return StyleHeader.Render(text)
}

// RenderPath renders a file path with cyan color
func RenderPath(path string) string {
	return StylePath.Render(path)
}
