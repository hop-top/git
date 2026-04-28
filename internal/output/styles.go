package output

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
	"hop.top/kit/cli"
)

// defaultTheme returns a cli.Theme built from the Neon palette.
// This is a local helper until the root command migrates to cli.New()
// (task #4), at which point styles should accept Root.Theme instead.
func defaultTheme() cli.Theme {
	p := cli.Neon
	muted := color.Color(charmtone.Squid)
	white := color.Color(lipgloss.Color("#FFFFFF"))

	return cli.Theme{
		Palette:   p,
		Accent:    p.Command,
		Secondary: p.Flag,
		Muted:     muted,
		Error:     color.Color(charmtone.Cherry),
		Success:   color.Color(charmtone.Guac),

		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(white),
		Subtle: lipgloss.NewStyle().
			Foreground(muted),
		Bold: lipgloss.NewStyle().
			Bold(true),
	}
}

// theme is the package-level default; all styles derive from it.
var theme = defaultTheme()

// Semantic color aliases derived from the theme.
var (
	ColorSuccess = theme.Success
	ColorError   = theme.Error
	ColorWarning = theme.Secondary // warm secondary
	ColorInfo    = theme.Accent
	ColorMuted   = theme.Muted
	ColorAccent  = theme.Accent
	ColorPath    = theme.Accent
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
			Foreground(ColorAccent).
			Bold(true)

	StylePath = lipgloss.NewStyle().
			Foreground(ColorPath)

	StyleKey = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Bold(false)

	StyleValue = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))
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
				BorderForeground(ColorMuted).
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

// RenderPath renders a file path with accent color
func RenderPath(path string) string {
	return StylePath.Render(path)
}
