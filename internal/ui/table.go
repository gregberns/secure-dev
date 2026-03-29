package ui

import (
	"fmt"
	"io"
	"strings"
)

// Table renders tabular data for list/status/config commands.
type Table struct {
	headers []string
	rows    [][]string
	w       io.Writer
}

// NewTable creates a Table that writes to w with the given column headers.
func NewTable(w io.Writer, headers ...string) *Table {
	return &Table{
		headers: headers,
		w:       w,
	}
}

// AddRow appends a row to the table. The number of values should match the
// number of headers. Extra values are silently ignored; missing values are
// padded with empty strings.
func (t *Table) AddRow(values ...string) {
	row := make([]string, len(t.headers))
	for i := range row {
		if i < len(values) {
			row[i] = values[i]
		}
	}
	t.rows = append(t.rows, row)
}

// Render writes the formatted table. Columns are left-aligned and padded
// to the widest value in each column, separated by two spaces.
func (t *Table) Render() {
	if len(t.headers) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, val := range row {
			if len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}

	// Print header
	t.printRow(t.headers, widths)

	// Print separator
	sep := make([]string, len(t.headers))
	for i, w := range widths {
		sep[i] = strings.Repeat("-", w)
	}
	t.printRow(sep, widths)

	// Print rows
	for _, row := range t.rows {
		t.printRow(row, widths)
	}
}

func (t *Table) printRow(values []string, widths []int) {
	parts := make([]string, len(values))
	for i, val := range values {
		parts[i] = padRight(val, widths[i])
	}
	fmt.Fprintln(t.w, strings.Join(parts, "  "))
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
