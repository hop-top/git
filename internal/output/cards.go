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
	lines := []string{c.Style.Render(c.Title)}

	maxKeyWidth := 0
	for _, field := range c.Fields {
		if len(field.Key) > maxKeyWidth {
			maxKeyWidth = len(field.Key)
		}
	}

	for _, field := range c.Fields {
		keyPadded := field.Key + strings.Repeat(
			" ", maxKeyWidth-len(field.Key),
		)
		lines = append(lines, "  "+StyleKey.Render(keyPadded)+
			"  "+StyleValue.Render(field.Value))
	}

	return strings.Join(lines, "\n")
}

// SimpleHeader creates a plain, styled header line
func SimpleHeader(text string) string {
	if CurrentMode != ModeHuman {
		return ""
	}
	return StyleHeader.Render(text)
}

// Section creates a titled section with indented content lines
func Section(title string, content []string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	lines := []string{"", StyleHeader.Render(title)}
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
		return StyleSuccess.Render(message)
	case "error":
		return StyleError.Render(IconError + ": " + message)
	case "warning":
		return StyleWarning.Render(IconWarning + ": " + message)
	case "info":
		return StyleInfo.Render(message)
	case "stopped":
		return StyleMuted.Render(message)
	default:
		return message
	}
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

	style := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	return style.Render(text)
}
