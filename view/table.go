package view

import (
	"fmt"
	"io"
	"strings"
	"text/template"
)

// Table is a data-prep helper for row/column content: it computes per-column
// widths and padding in Go (text/template has no good way to do either) and
// exposes the result both as ready-to-embed formatted lines (Lines/OrgLines,
// for callers composing a larger document template) and via convenience
// standalone renderers (RenderASCII/RenderOrg) for the common case of a
// table being the entire output.
type Table struct {
	Headers []string
	Rows    [][]string
	Right   map[int]bool // per-column right-justify; default left
	Groups  []int        // row-count boundaries where a new group starts; nil/empty = one group
}

// NewTable returns a Table with the given column headers.
func NewTable(headers ...string) *Table {
	return &Table{Headers: append([]string{}, headers...), Right: map[int]bool{}}
}

// SetRight marks the given zero-based column indexes as right-justified.
func (t *Table) SetRight(cols ...int) {
	if t.Right == nil {
		t.Right = map[int]bool{}
	}
	for _, c := range cols {
		t.Right[c] = true
	}
}

// AddRow appends one row of cell values.
func (t *Table) AddRow(cells ...string) {
	t.Rows = append(t.Rows, cells)
}

// AddGroup marks a group boundary at the current row count, so a blank
// line (ASCII) or hline (org) separates the rows added before this call
// from the rows added after it.
func (t *Table) AddGroup() {
	t.Groups = append(t.Groups, len(t.Rows))
}

// widths returns the max cell width per column, scanning headers and every
// row so alignment holds across the whole table, not just one group.
func (t *Table) widths() []int {
	n := len(t.Headers)
	widths := make([]int, n)
	for i, h := range t.Headers {
		widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i := 0; i < n && i < len(row); i++ {
			if l := len(row[i]); l > widths[i] {
				widths[i] = l
			}
		}
	}
	return widths
}

func padCell(width int, s string, right bool) string {
	if right {
		return fmt.Sprintf("%*s", width, s)
	}
	return fmt.Sprintf("%-*s", width, s)
}

func (t *Table) formatASCIIRow(cells []string, widths []int) string {
	parts := make([]string, len(widths))
	for i := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		parts[i] = padCell(widths[i], cell, t.Right[i])
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

func (t *Table) formatOrgRow(cells []string, widths []int) string {
	var b strings.Builder
	b.WriteString("|")
	for i := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		b.WriteString(" ")
		b.WriteString(padCell(widths[i], cell, t.Right[i]))
		b.WriteString(" |")
	}
	return b.String()
}

func orgSeparator(widths []int) string {
	var b strings.Builder
	b.WriteString("|")
	for i, w := range widths {
		b.WriteString(strings.Repeat("-", w+2))
		if i < len(widths)-1 {
			b.WriteString("+")
		}
	}
	b.WriteString("|")
	return b.String()
}

// groupedRows splits Rows into segments at the boundaries recorded by
// AddGroup, defensively ignoring out-of-order or out-of-range boundaries.
func (t *Table) groupedRows() [][][]string {
	if len(t.Rows) == 0 {
		return nil
	}
	var groups [][][]string
	prev := 0
	for _, b := range t.Groups {
		if b <= prev || b > len(t.Rows) {
			continue
		}
		groups = append(groups, t.Rows[prev:b])
		prev = b
	}
	groups = append(groups, t.Rows[prev:])
	return groups
}

// Header returns the ASCII-formatted, padded header line.
func (t *Table) Header() string {
	return t.formatASCIIRow(t.Headers, t.widths())
}

// Rule returns an ASCII underline line (dashes sized to each header's own
// text, then padded to column width like any other row).
func (t *Table) Rule() string {
	widths := t.widths()
	underline := make([]string, len(t.Headers))
	for i, h := range t.Headers {
		underline[i] = strings.Repeat("-", len(h))
	}
	return t.formatASCIIRow(underline, widths)
}

// Lines returns the ASCII-formatted, padded rows, grouped by AddGroup
// boundaries — one inner slice per group, ready to embed in a larger
// template or feed to RenderASCII's built-in one.
func (t *Table) Lines() [][]string {
	widths := t.widths()
	groups := t.groupedRows()
	out := make([][]string, len(groups))
	for gi, g := range groups {
		lines := make([]string, len(g))
		for i, row := range g {
			lines[i] = t.formatASCIIRow(row, widths)
		}
		out[gi] = lines
	}
	return out
}

// OrgHeader returns the org-mode pipe-delimited, padded header line.
func (t *Table) OrgHeader() string {
	return t.formatOrgRow(t.Headers, t.widths())
}

// OrgRule returns an org-mode hline ("|---+---|") sized to each column's
// width, valid org-table syntax for separating a header from its body or
// one group of rows from the next.
func (t *Table) OrgRule() string {
	return orgSeparator(t.widths())
}

// OrgLines returns the org-mode pipe-delimited, padded rows, grouped by
// AddGroup boundaries — one inner slice per group.
func (t *Table) OrgLines() [][]string {
	widths := t.widths()
	groups := t.groupedRows()
	out := make([][]string, len(groups))
	for gi, g := range groups {
		lines := make([]string, len(g))
		for i, row := range g {
			lines[i] = t.formatOrgRow(row, widths)
		}
		out[gi] = lines
	}
	return out
}

type tableView struct {
	Header string
	Rule   string
	Groups [][]string
}

var asciiTableTmpl = template.Must(template.New("view.table.ascii").Parse(
	`{{.Header}}
{{.Rule}}
{{range $i, $group := .Groups}}{{if $i}}
{{end}}{{range $group}}{{.}}
{{end}}{{end}}`))

var orgTableTmpl = template.Must(template.New("view.table.org").Parse(
	`{{.Header}}
{{.Rule}}
{{range $i, $group := .Groups}}{{if $i}}{{$.Rule}}
{{end}}{{range $group}}{{.}}
{{end}}{{end}}`))

// RenderASCII writes the table as a human-readable, column-aligned block:
// header, dash underline, rows, with a blank line between AddGroup groups.
func (t *Table) RenderASCII(w io.Writer) error {
	return asciiTableTmpl.Execute(w, tableView{Header: t.Header(), Rule: t.Rule(), Groups: t.Lines()})
}

// RenderOrg writes the table as a valid org-mode table: header, hline,
// rows, with an hline between AddGroup groups (org tables don't tolerate
// blank lines mid-table).
func (t *Table) RenderOrg(w io.Writer) error {
	return orgTableTmpl.Execute(w, tableView{Header: t.OrgHeader(), Rule: t.OrgRule(), Groups: t.OrgLines()})
}
