package report

import (
	"io"
	"time"

	"github.com/rustyeddy/trader/num"
)

// undefinedValue is the human-readable rendering of a nil optional
// metric (issue #220 review, point 6: "explicit representation for
// undefined optional metrics ... n/a for human formats"). JSON's own
// representation is the language's native null, produced automatically
// by encoding/json for a nil pointer field — no special-casing needed
// there.
const undefinedValue = "n/a"

// timeLayout is the canonical human-readable timestamp format Org and
// text rendering use — RFC3339 seconds precision, always UTC since
// NewBacktestReport already normalized every timestamp.
const timeLayout = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return undefinedValue
	}
	return t.UTC().Format(timeLayout)
}

func formatMoney(m num.Money) string {
	return m.String()
}

func formatOptionalMoney(m *num.Money) string {
	if m == nil {
		return undefinedValue
	}
	return m.String()
}

func formatRate(r num.Rate) string {
	return r.String()
}

func formatOptionalRate(r *num.Rate) string {
	if r == nil {
		return undefinedValue
	}
	return r.String()
}

// errWriter wraps an io.Writer and remembers the first error any Write
// call returns, turning every write after that into a no-op — the same
// sticky-error idiom cmd/trader/execution's own errWriter establishes
// (duplicated rather than shared, matching that package's own note
// that each formatter is deliberately independent).
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}
