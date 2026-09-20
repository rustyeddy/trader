package report

import "io"

// Renderer renders a report value of type T to w (architecture doc's
// own `** =report=` sketch). A Renderer performs no computation: every
// value it writes must already exist on report. File naming, output
// selection (stdout vs. a path), and opening/closing w are a caller's
// concern, not a Renderer's — this keeps Render(io.Writer, T) trivial
// to golden-test (issue #220 review).
type Renderer[T any] interface {
	Render(w io.Writer, report T) error
}
