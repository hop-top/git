package tui

import (
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
)

// Table wraps go-pretty table
type Table struct {
	t table.Writer
}

// NewTable creates a new table
func NewTable(headers []interface{}) *Table {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)

	row := make(table.Row, len(headers))
	copy(row, headers)
	t.AppendHeader(row)

	t.SetStyle(table.StyleRounded)
	return &Table{t: t}
}

// AddRow adds a row
func (t *Table) AddRow(row ...interface{}) {
	r := make(table.Row, len(row))
	copy(r, row)
	t.t.AppendRow(r)
}

// Render prints the table
func (t *Table) Render() {
	t.t.Render()
}
