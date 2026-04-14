package tui

import (
	"hop.top/git/internal/output"
)

// Table wraps output.TableBuilder, replacing the former go-pretty
// wrapper. The interface is intentionally kept identical to avoid
// breaking callers in cmd/*.go.
type Table struct {
	tb *output.TableBuilder
}

// NewTable creates a new table. headers are converted from
// []interface{} to []string for the underlying TableBuilder.
func NewTable(headers []interface{}) *Table {
	h := make([]string, len(headers))
	for i, v := range headers {
		h[i], _ = v.(string)
	}
	return &Table{tb: output.NewTable(h...)}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(row ...interface{}) {
	cells := make([]string, len(row))
	for i, v := range row {
		cells[i], _ = v.(string)
	}
	t.tb.AddRow(cells...)
}

// Render prints the table to stdout.
func (t *Table) Render() {
	t.tb.Print()
}
