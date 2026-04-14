package tui

import (
	"charm.land/lipgloss/v2"
	"hop.top/git/internal/output"
)

// Styles defines the TUI styles using semantic theme colors.
type Styles struct {
	Header lipgloss.Style
	Cell   lipgloss.Style
	Active lipgloss.Style
	Error  lipgloss.Style
}

// DefaultStyles returns styles derived from the kit/tui theme palette.
func DefaultStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(output.ColorAccent).
			Padding(0, 1),
		Cell: lipgloss.NewStyle().
			Padding(0, 1),
		Active: lipgloss.NewStyle().
			Foreground(output.ColorSuccess).
			Bold(true),
		Error: lipgloss.NewStyle().
			Foreground(output.ColorError).
			Bold(true),
	}
}
