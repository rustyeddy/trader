// Package view renders arbitrary display data through text/template. The
// package itself has no notion of output format (ASCII table, org-mode,
// or anything else) — that decision lives entirely in the template text a
// caller supplies. Table and PropertyList are convenience helpers layered
// on top for the common cases of tabular and key/value data: they do the
// Go-side width computation and padding (text/template has no good way to
// do either) and hand the result to Render as pre-formatted lines.
package view

import (
	"io"
	"text/template"
)

// Render parses tmplText and executes it against data, writing the result
// to w. Callers that render the same template repeatedly should parse it
// once via template.Must(template.New(...).Parse(...)) at package scope
// instead of calling Render, which reparses tmplText on every call.
func Render(w io.Writer, tmplText string, data any) error {
	tmpl, err := template.New("view").Parse(tmplText)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, data)
}
