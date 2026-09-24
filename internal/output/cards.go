package output

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Card represents a styled information card: a title line followed by
// aligned key/value fields. There is no border; git never boxes its
// output.
type Card struct {
	Title  string
	Fields []CardField
	Style  lipgloss.Style
}

// CardField represents a key-value field in a card
type CardField struct {
	Key   string
	Value string
}

// SuccessCard creates a success-styled card
func SuccessCard(title string, fields []CardField) string {
	return renderCard(title, fields, StyleSuccess)
}

// WarningCard creates a warning-styled card
func WarningCard(title string, fields []CardField) string {
	return renderCard(title, fields, StyleWarning)
}

// InfoCard creates an info-styled card
func InfoCard(title string, fields []CardField) string {
	return renderCard(title, fields, StyleHeader)
}

// ErrorCard creates an error-styled card
func ErrorCard(title string, fields []CardField) string {
	return renderCard(title, fields, StyleError)
}

func renderCard(title string, fields []CardField, style lipgloss.Style) string {
	if CurrentMode != ModeHuman {
		return ""
	}
	card := Card{Title: title, Fields: fields, Style: style}
	return card.Render()
}

// Render outputs the card as a formatted string
func (c *Card) Render() string {
	lines := []string{Paint(c.Style, c.Title)}

	maxKeyWidth := 0
	for _, field := range c.Fields {
		if lipgloss.Width(field.Key) > maxKeyWidth {
			maxKeyWidth = lipgloss.Width(field.Key)
		}
	}

	for _, field := range c.Fields {
		keyPadded := field.Key + strings.Repeat(
			" ", maxKeyWidth-lipgloss.Width(field.Key),
		)
		lines = append(lines, "  "+Paint(StyleKey, keyPadded)+
			"  "+Paint(StyleValue, field.Value))
	}

	return strings.Join(lines, "\n")
}

// SimpleHeader creates a plain, styled header line
func SimpleHeader(text string) string {
	if CurrentMode != ModeHuman {
		return ""
	}
	return Paint(StyleHeader, text)
}

// Section creates a titled section with indented content lines
func Section(title string, content []string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	lines := []string{"", Paint(StyleHeader, title)}
	for _, line := range content {
		if line == "" {
			lines = append(lines, "")
			continue
		}
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

// StatusLine creates a status-coloured message line. Errors and
// warnings carry git's lowercase "error:" / "warning:" prefix so the
// line still reads correctly without colour.
func StatusLine(status, message string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	switch status {
	case "success":
		return Paint(StyleSuccess, message)
	case "error":
		return Paint(StyleError, IconError+": "+message)
	case "warning":
		return Paint(StyleWarning, IconWarning+": "+message)
	case "info":
		return Paint(StyleInfo, message)
	case "stopped":
		return Paint(StyleMuted, message)
	default:
		return message
	}
}

// NextStepHint creates a styled next action hint
func NextStepHint(command string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	arrow := Paint(StyleAccent, IconArrow)
	cmd := Paint(StylePath, command)
	return fmt.Sprintf("\n%s %s\n", arrow, cmd)
}

// Banner creates a simple text banner
func Banner(text string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	style := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	return Paint(style, text)
}
