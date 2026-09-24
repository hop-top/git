package output

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
	"hop.top/kit/go/console/cli"
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

// Text styles. Render them with Paint (or a helper built on it), never
// with Style.Render: Paint drops the colour stdout cannot show.
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

// Utility functions

// Colorize applies the appropriate color style based on status
func Colorize(text string, status string) string {
	switch status {
	case "success", "running", "active", "pass", "up":
		return Paint(StyleSuccess, text)
	case "error", "failed", "fail", "down", "broken":
		return Paint(StyleError, text)
	case "warning", "warn", "attention":
		return Paint(StyleWarning, text)
	case "info", "neutral", "stopped", "clean":
		return Paint(StyleInfo, text)
	case "muted", "inactive":
		return Paint(StyleMuted, text)
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
	return Paint(StyleKey, key) + " " + Paint(StyleValue, value)
}

// RenderHeader renders a header with accent color
func RenderHeader(text string) string {
	return Paint(StyleHeader, text)
}

// RenderPath renders a file path with accent color
func RenderPath(path string) string {
	return Paint(StylePath, path)
}
