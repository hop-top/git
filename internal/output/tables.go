package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// TableBuilder provides a fluent interface for building styled tables
type TableBuilder struct {
	headers []string
	rows    [][]string
	style   TableStyle
	colors  map[int]lipgloss.Color // column index -> color
}

// TableStyle defines the visual style of the table
type TableStyle int

const (
	TableStyleRounded TableStyle = iota
	TableStyleLight
	TableStyleBold
	TableStyleDouble
)

// NewTable creates a new table builder
func NewTable(headers ...string) *TableBuilder {
	return &TableBuilder{
		headers: headers,
		rows:    [][]string{},
		style:   TableStyleRounded,
		colors:  make(map[int]lipgloss.Color),
	}
}

// AddRow adds a row to the table
func (tb *TableBuilder) AddRow(cols ...string) *TableBuilder {
	tb.rows = append(tb.rows, cols)
	return tb
}

// SetStyle sets the table style
func (tb *TableBuilder) SetStyle(style TableStyle) *TableBuilder {
	tb.style = style
	return tb
}

// SetColumnColor sets color for a specific column
func (tb *TableBuilder) SetColumnColor(colIndex int, color lipgloss.Color) *TableBuilder {
	tb.colors[colIndex] = color
	return tb
}

// Render outputs the table
func (tb *TableBuilder) Render() string {
	if CurrentMode != ModeHuman {
		// For porcelain/JSON modes, return simple tab-separated output
		return tb.renderPlain()
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)

	// Set headers
	headerRow := make(table.Row, len(tb.headers))
	for i, h := range tb.headers {
		headerRow[i] = h
	}
	t.AppendHeader(headerRow)

	// Add rows
	for _, row := range tb.rows {
		tableRow := make(table.Row, len(row))
		for i, cell := range row {
			tableRow[i] = cell
		}
		t.AppendRow(tableRow)
	}

	// Apply style
	switch tb.style {
	case TableStyleRounded:
		t.SetStyle(table.StyleRounded)
	case TableStyleLight:
		t.SetStyle(table.StyleLight)
	case TableStyleBold:
		t.SetStyle(table.StyleBold)
	case TableStyleDouble:
		t.SetStyle(table.StyleDouble)
	}

	return t.Render()
}

// renderPlain returns plain text output for non-human modes
func (tb *TableBuilder) renderPlain() string {
	var lines []string

	// Headers
	lines = append(lines, strings.Join(tb.headers, "\t"))

	// Rows
	for _, row := range tb.rows {
		lines = append(lines, strings.Join(row, "\t"))
	}

	return strings.Join(lines, "\n")
}

// Print outputs the table to stdout
func (tb *TableBuilder) Print() {
	fmt.Println(tb.Render())
}

// StatusTable creates a table with status indicators
type StatusTable struct {
	headers []string
	rows    []StatusRow
}

// StatusRow represents a row with status information
type StatusRow struct {
	Cells  []string
	Status string // "success", "error", "warning", "info", "neutral"
}

// NewStatusTable creates a new status table
func NewStatusTable(headers ...string) *StatusTable {
	return &StatusTable{
		headers: headers,
		rows:    []StatusRow{},
	}
}

// AddRow adds a row with status
func (st *StatusTable) AddRow(status string, cells ...string) *StatusTable {
	st.rows = append(st.rows, StatusRow{
		Cells:  cells,
		Status: status,
	})
	return st
}

// Render outputs the status table
func (st *StatusTable) Render() string {
	if CurrentMode != ModeHuman {
		return st.renderPlain()
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)

	// Set headers
	headerRow := make(table.Row, len(st.headers))
	for i, h := range st.headers {
		headerRow[i] = h
	}
	t.AppendHeader(headerRow)

	// Add rows with status colors
	for _, row := range st.rows {
		tableRow := make(table.Row, len(row.Cells))
		for i, cell := range row.Cells {
			// First cell gets status icon
			if i == 0 {
				icon := getStatusIcon(row.Status)
				cell = icon + " " + cell
			}
			tableRow[i] = colorizeCell(cell, row.Status)
		}
		t.AppendRow(tableRow)
	}

	t.SetStyle(table.StyleRounded)
	return t.Render()
}

// renderPlain returns plain text output
func (st *StatusTable) renderPlain() string {
	var lines []string

	// Headers
	lines = append(lines, strings.Join(st.headers, "\t"))

	// Rows
	for _, row := range st.rows {
		lines = append(lines, strings.Join(row.Cells, "\t"))
	}

	return strings.Join(lines, "\n")
}

// Print outputs the status table
func (st *StatusTable) Print() {
	fmt.Println(st.Render())
}

// Helper functions

func getStatusIcon(status string) string {
	switch status {
	case "success", "pass", "running", "up", "active":
		return ColorizeIcon(IconSuccess, "success")
	case "error", "fail", "down", "broken":
		return ColorizeIcon(IconError, "error")
	case "warning", "warn":
		return ColorizeIcon(IconWarning, "warning")
	case "stopped", "clean", "neutral":
		return ColorizeIcon(IconStopped, "info")
	default:
		return ColorizeIcon(IconStopped, "muted")
	}
}

func colorizeCell(cell string, status string) string {
	// Only colorize in human mode
	if CurrentMode != ModeHuman {
		return cell
	}

	return Colorize(cell, status)
}

// SummaryTable creates a simple two-column key-value table
func SummaryTable(items map[string]string) string {
	if CurrentMode != ModeHuman {
		var lines []string
		for k, v := range items {
			lines = append(lines, fmt.Sprintf("%s\t%s", k, v))
		}
		return strings.Join(lines, "\n")
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleLight)

	// Disable header
	t.Style().Options.DrawBorder = false
	t.Style().Options.SeparateHeader = false

	for k, v := range items {
		t.AppendRow(table.Row{
			StyleKey.Render(k),
			StyleValue.Render(v),
		})
	}

	return t.Render()
}

// CompactList creates a compact list with bullets
func CompactList(items []string, status string) string {
	if CurrentMode != ModeHuman {
		return strings.Join(items, "\n")
	}

	icon := getStatusIcon(status)
	var lines []string
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("  %s %s", icon, item))
	}
	return strings.Join(lines, "\n")
}

// AlignedList creates an aligned list with labels and values
func AlignedList(items []struct{ Label, Value string }) string {
	if CurrentMode != ModeHuman {
		var lines []string
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("%s\t%s", item.Label, item.Value))
		}
		return strings.Join(lines, "\n")
	}

	// Calculate max label width
	maxWidth := 0
	for _, item := range items {
		if len(item.Label) > maxWidth {
			maxWidth = len(item.Label)
		}
	}

	var lines []string
	for _, item := range items {
		padding := strings.Repeat(" ", maxWidth-len(item.Label))
		line := fmt.Sprintf("  %s%s  %s",
			StyleKey.Render(item.Label),
			padding,
			StyleValue.Render(item.Value),
		)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// Legend creates a legend for table symbols
func Legend(items map[string]string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	var parts []string
	for icon, desc := range items {
		parts = append(parts, fmt.Sprintf("%s %s", icon, StyleMuted.Render(desc)))
	}

	return StyleMuted.Render("Legend: ") + strings.Join(parts, "  ")
}

// ConfigureTableWriter configures a go-pretty table writer with common settings
func ConfigureTableWriter(t table.Writer) {
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.Style().Options.DrawBorder = true
	t.Style().Options.SeparateColumns = true
	t.Style().Options.SeparateHeader = true
	t.Style().Options.SeparateRows = false

	// Color configuration
	t.Style().Color.Header = text.Colors{text.Bold}
	t.Style().Color.Border = text.Colors{text.FgHiBlack}
}
