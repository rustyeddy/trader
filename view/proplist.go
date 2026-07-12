package view

import (
	"fmt"
	"io"
)

type property struct{ Key, Value string }

// PropertyList is a data-prep helper for single-record key/value output
// (e.g. "trader bot detail"'s field dump): a key-width-aligned block with
// no header row, unlike Table. There's no org-mode variant today — nothing
// in the repo emits property lists as org — but Lines stays available for
// callers composing a larger document template later.
type PropertyList struct {
	items []property
}

// NewPropertyList returns an empty PropertyList.
func NewPropertyList() *PropertyList {
	return &PropertyList{}
}

// Add appends a key/value line.
func (p *PropertyList) Add(key, value string) {
	p.items = append(p.items, property{Key: key, Value: value})
}

// AddIf appends a key/value line only when cond is true, for optional
// fields that shouldn't print at all rather than print empty.
func (p *PropertyList) AddIf(cond bool, key, value string) {
	if cond {
		p.Add(key, value)
	}
}

// Lines returns the key-width-padded "key   value" lines, ready to embed
// in a larger template.
func (p *PropertyList) Lines() []string {
	width := 0
	for _, it := range p.items {
		if len(it.Key) > width {
			width = len(it.Key)
		}
	}
	lines := make([]string, len(p.items))
	for i, it := range p.items {
		lines[i] = fmt.Sprintf("%-*s  %s", width, it.Key, it.Value)
	}
	return lines
}

// Render writes Lines to w, one per line.
func (p *PropertyList) Render(w io.Writer) {
	for _, line := range p.Lines() {
		fmt.Fprintln(w, line)
	}
}
