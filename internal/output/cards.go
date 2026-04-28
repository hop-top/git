package output

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Card represents a styled information card
type Card struct {
	Title  string
	Fields []CardField
	Style  lipgloss.Style
	Width  int
}

// CardField represents a key-value field in a card
type CardField struct {
	Key   string
	Value string
}

// SuccessCard creates a success-styled card
func SuccessCard(title string, fields []CardField) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	card := Card{
		Title:  IconSuccess + " " + title,
		Fields: fields,
		Style:  StyleBorderHeavy,
		Width:  50,
	}
	return card.Render()
}

// WarningCard creates a warning-styled card
func WarningCard(title string, fields []CardField) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	card := Card{
		Title:  IconWarning + " " + title,
		Fields: fields,
		Style:  StyleBorderWarning,
		Width:  50,
	}
	return card.Render()
}

// InfoCard creates an info-styled card
func InfoCard(title string, fields []CardField) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	card := Card{
		Title:  title,
		Fields: fields,
		Style:  StyleBorderInfo,
		Width:  50,
	}
	return card.Render()
}

// ErrorCard creates an error-styled card
func ErrorCard(title string, fields []CardField) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	card := Card{
		Title:  IconError + " " + title,
		Fields: fields,
		Style:  StyleBorderError,
		Width:  50,
	}
	return card.Render()
}

// Render outputs the card as a formatted string
func (c *Card) Render() string {
	var lines []string

	// Title line
	titleStyle := lipgloss.NewStyle().Bold(true)
	lines = append(lines, titleStyle.Render(c.Title))

	// Separator
	if len(c.Fields) > 0 {
		lines = append(lines, strings.Repeat("─", c.Width-4))
	}

	// Calculate max key width for alignment
	maxKeyWidth := 0
	for _, field := range c.Fields {
		if len(field.Key) > maxKeyWidth {
			maxKeyWidth = len(field.Key)
		}
	}

	// Field lines
	for _, field := range c.Fields {
		keyPadded := field.Key + strings.Repeat(
			" ", maxKeyWidth-len(field.Key),
		)
		line := StyleKey.Render(" "+keyPadded) +
			" │ " + StyleValue.Render(field.Value)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return c.Style.Width(c.Width).Render(content)
}

// SimpleHeader creates a simple bordered header
func SimpleHeader(text string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorAccent).
		Padding(0, 1).
		Width(50)

	return style.Render(text)
}

// Section creates a section with an emoji header
func Section(emoji, title string, content []string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	var lines []string

	// Section header
	header := emoji + " " + StyleHeader.Render(title)
	lines = append(lines, "", header)

	// Content lines with indentation
	for _, line := range content {
		lines = append(lines, "  "+line)
	}

	return strings.Join(lines, "\n")
}

// TreeItem creates a tree structure item
func TreeItem(isLast bool, label, value string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	prefix := IconTreeBranch
	if isLast {
		prefix = IconTreeLast
	}

	if value == "" {
		return fmt.Sprintf("  %s %s", prefix, label)
	}

	return fmt.Sprintf("  %s %-15s %s", prefix, label, value)
}

// StatusLine creates a status line with icon and message
func StatusLine(status, message string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	var icon string
	var style lipgloss.Style

	switch status {
	case "success":
		icon = IconSuccess
		style = StyleSuccess
	case "error":
		icon = IconError
		style = StyleError
	case "warning":
		icon = IconWarning
		style = StyleWarning
	case "info":
		icon = IconRunning
		style = StyleInfo
	case "stopped":
		icon = IconStopped
		style = StyleMuted
	default:
		return message
	}

	return style.Render(icon + " " + message)
}

// NextStepHint creates a styled next action hint
func NextStepHint(command string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	arrow := StyleAccent.Render(IconArrow)
	cmd := StylePath.Render(command)
	return fmt.Sprintf("\n%s %s\n", arrow, cmd)
}

// Banner creates a simple text banner
func Banner(text string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	width := len(text) + 4
	border := strings.Repeat("─", width)

	style := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	lines := []string{
		"┌" + border + "┐",
		"│ " + text + " │",
		"└" + border + "┘",
	}

	return style.Render(strings.Join(lines, "\n"))
}
