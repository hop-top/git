package output

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"charm.land/lipgloss/v2"
	kitout "hop.top/kit/go/console/output"
)

// TableBuilder provides a fluent interface for building aligned tables.
// Internally delegates to kit/output.Render for JSON/YAML and
// text/tabwriter for human-readable table output.
type TableBuilder struct {
	headers []string
	rows    [][]string
	style   TableStyle
}

// TableStyle defines the visual style of the table (retained for API
// compat; the concrete style differences are dropped — kit/output
// always uses tabwriter alignment).
type TableStyle int

const (
	TableStyleRounded TableStyle = iota
	TableStyleLight
	TableStyleBold
	TableStyleDouble
)

// NewTable creates a new table builder.
func NewTable(headers ...string) *TableBuilder {
	return &TableBuilder{
		headers: headers,
		rows:    [][]string{},
		style:   TableStyleRounded,
	}
}

// AddRow adds a row to the table.
func (tb *TableBuilder) AddRow(cols ...string) *TableBuilder {
	tb.rows = append(tb.rows, cols)
	return tb
}

// SetStyle sets the table style (kept for API compat; no-op under
// kit/output).
func (tb *TableBuilder) SetStyle(style TableStyle) *TableBuilder {
	tb.style = style
	return tb
}

// Render returns the table as a string. In human mode it uses
// tabwriter-aligned columns; in non-human modes it returns plain
// tab-separated output (no ANSI).
func (tb *TableBuilder) Render() string {
	if CurrentMode != ModeHuman {
		return tb.renderPlain()
	}
	return tb.renderTabwriter()
}

// renderTabwriter produces aligned plain-text output via tabwriter.
func (tb *TableBuilder) renderTabwriter() string {
	return strings.Join(alignRows(append([][]string{tb.headers}, tb.rows...)), "\n")
}

// alignRows lays rows out as lines of columns two spaces apart. Cells
// must be plain text: text/tabwriter counts every byte, so a colour code
// in a cell would pad its column by the code's length. Style the lines
// it returns instead.
func alignRows(rows [][]string) []string {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

// renderPlain returns plain text output for non-human modes.
func (tb *TableBuilder) renderPlain() string {
	var lines []string
	lines = append(lines, strings.Join(tb.headers, "\t"))
	for _, row := range tb.rows {
		lines = append(lines, strings.Join(row, "\t"))
	}
	return strings.Join(lines, "\n")
}

// Print outputs the table to stdout.
func (tb *TableBuilder) Print() {
	fmt.Println(tb.Render())
}

// RenderTo writes the table to w using kit/output.Render with the
// specified format. This is the preferred path for commands that
// support --format (table, json, yaml).
func (tb *TableBuilder) RenderTo(w *os.File, format kitout.Format) error {
	if format == kitout.JSON || format == kitout.YAML {
		return kitout.Render(w, format, tb.toMaps())
	}
	_, err := fmt.Fprintln(w, tb.Render())
	return err
}

// toMaps converts rows to []map[string]string for JSON/YAML output.
func (tb *TableBuilder) toMaps() []map[string]string {
	out := make([]map[string]string, 0, len(tb.rows))
	for _, row := range tb.rows {
		m := make(map[string]string, len(tb.headers))
		for i, h := range tb.headers {
			if i < len(row) {
				m[h] = row[i]
			}
		}
		out = append(out, m)
	}
	return out
}

// StatusTable creates a table with status indicators.
type StatusTable struct {
	headers []string
	rows    []StatusRow
}

// StatusRow represents a row with status information.
type StatusRow struct {
	Cells  []string
	Status string // "success", "error", "warning", "info", "neutral"
}

// NewStatusTable creates a new status table.
func NewStatusTable(headers ...string) *StatusTable {
	return &StatusTable{
		headers: headers,
		rows:    []StatusRow{},
	}
}

// AddRow adds a row with status.
func (st *StatusTable) AddRow(status string, cells ...string) *StatusTable {
	st.rows = append(st.rows, StatusRow{
		Cells:  cells,
		Status: status,
	})
	return st
}

// Render outputs the status table.
func (st *StatusTable) Render() string {
	if CurrentMode != ModeHuman {
		return st.renderPlain()
	}

	rows := [][]string{st.headers}
	for _, row := range st.rows {
		rows = append(rows, row.Cells)
	}
	lines := alignRows(rows)

	// The row status only colours the row; callers spell the state out
	// in a column of words, as git does, so it survives without colour.
	for i, row := range st.rows {
		lines[i+1] = colorizeCell(lines[i+1], row.Status)
	}
	return strings.Join(lines, "\n")
}

// renderPlain returns plain text output.
func (st *StatusTable) renderPlain() string {
	var lines []string
	lines = append(lines, strings.Join(st.headers, "\t"))
	for _, row := range st.rows {
		lines = append(lines, strings.Join(row.Cells, "\t"))
	}
	return strings.Join(lines, "\n")
}

// Print outputs the status table.
func (st *StatusTable) Print() {
	fmt.Println(st.Render())
}

// RenderTo writes the status table to w using the specified format.
func (st *StatusTable) RenderTo(w *os.File, format kitout.Format) error {
	if format == kitout.JSON || format == kitout.YAML {
		return kitout.Render(w, format, st.toMaps())
	}
	_, err := fmt.Fprintln(w, st.Render())
	return err
}

// toMaps converts rows to []map[string]string for JSON/YAML output.
func (st *StatusTable) toMaps() []map[string]string {
	out := make([]map[string]string, 0, len(st.rows))
	for _, row := range st.rows {
		m := make(map[string]string, len(st.headers))
		for i, h := range st.headers {
			if i < len(row.Cells) {
				m[h] = row.Cells[i]
			}
		}
		m["Status"] = row.Status
		out = append(out, m)
	}
	return out
}

// Helper functions

func colorizeCell(cell string, status string) string {
	if CurrentMode != ModeHuman {
		return cell
	}
	return Colorize(cell, status)
}

// SummaryTable creates a simple two-column key-value table.
func SummaryTable(items map[string]string) string {
	if CurrentMode != ModeHuman {
		var lines []string
		for k, v := range items {
			lines = append(lines, fmt.Sprintf("%s\t%s", k, v))
		}
		return strings.Join(lines, "\n")
	}

	keyWidth := 0
	for k := range items {
		keyWidth = max(keyWidth, lipgloss.Width(k))
	}
	var lines []string
	for k, v := range items {
		pad := strings.Repeat(" ", keyWidth-lipgloss.Width(k)+2)
		lines = append(lines, Paint(StyleKey, k)+pad+Paint(StyleValue, v))
	}
	return strings.Join(lines, "\n")
}

// CompactList creates a compact, indented list coloured by status.
func CompactList(items []string, status string) string {
	if CurrentMode != ModeHuman {
		return strings.Join(items, "\n")
	}

	var lines []string
	for _, item := range items {
		lines = append(lines, "  "+colorizeCell(item, status))
	}
	return strings.Join(lines, "\n")
}

// AlignedList creates an aligned list with labels and values.
func AlignedList(items []struct{ Label, Value string }) string {
	if CurrentMode != ModeHuman {
		var lines []string
		for _, item := range items {
			lines = append(lines, fmt.Sprintf(
				"%s\t%s", item.Label, item.Value,
			))
		}
		return strings.Join(lines, "\n")
	}

	maxWidth := 0
	for _, item := range items {
		if lipgloss.Width(item.Label) > maxWidth {
			maxWidth = lipgloss.Width(item.Label)
		}
	}

	var lines []string
	for _, item := range items {
		padding := strings.Repeat(" ", maxWidth-lipgloss.Width(item.Label))
		line := fmt.Sprintf("  %s%s  %s",
			Paint(StyleKey, item.Label),
			padding,
			Paint(StyleValue, item.Value),
		)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// Legend explains the words a table uses, as "word = meaning" pairs in
// sorted order so the line is stable from run to run.
func Legend(items map[string]string) string {
	if CurrentMode != ModeHuman {
		return ""
	}

	words := make([]string, 0, len(items))
	for word := range items {
		words = append(words, word)
	}
	sort.Strings(words)

	parts := make([]string, 0, len(words))
	for _, word := range words {
		parts = append(parts, word+" = "+items[word])
	}

	return Paint(StyleMuted, "Legend: "+strings.Join(parts, ", "))
}

// RenderStructTable renders a slice of structs using kit/output.Render.
// Structs must have `table:""` tags for table format. This is the
// recommended path for new code — use instead of TableBuilder when
// the data model is known at compile time.
func RenderStructTable(
	w *os.File, format kitout.Format, v any,
) error {
	return kitout.Render(w, format, v)
}
