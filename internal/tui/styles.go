package tui

import "github.com/charmbracelet/lipgloss"

// Styles defines the TUI styles
type Styles struct {
	Header lipgloss.Style
	Cell   lipgloss.Style
	Active lipgloss.Style
	Error  lipgloss.Style
}

// DefaultStyles returns the default styles
func DefaultStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			Padding(0, 1),
		Cell: lipgloss.NewStyle().
			Padding(0, 1),
		Active: lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")).
			Bold(true),
		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true),
	}
}
