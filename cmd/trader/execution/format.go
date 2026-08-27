package execution

import (
	"errors"
	"fmt"
	"io"

	"github.com/rustyeddy/trader/pipeline"
	svcexecution "github.com/rustyeddy/trader/service/execution"
)

// Supported --format values, duplicated from cmd/trader/data/broker's
// own format.go rather than shared: each CLI command family package
// is deliberately independent (issue #201).
const (
	formatTable = "table"
	formatJSON  = "json"
)

// isRejected reports whether err is a risk rejection
// (errors.Is(err, pipeline.ErrRejected)) rather than an operational
// failure. evaluate/submit both still want to render the structured
// SubmitResponse a rejection carries (Proposal/Decision) instead of
// treating it as a command failure — a rejection is a normal,
// expected admission outcome (service/execution's own doc comment),
// not something Cobra should report as an error exit.
func isRejected(err error) bool {
	return errors.Is(err, pipeline.ErrRejected)
}

// Formatter renders one execution CLI response to w in some
// presentation format — this command group's own analog of
// cmd/trader/data's Formatter and cmd/trader/broker's Formatter, kept
// as a separate interface rather than shared across command-family
// packages (issue #201).
type Formatter interface {
	FormatEvaluate(w io.Writer, resp svcexecution.SubmitResponse) error
	FormatSubmit(w io.Writer, resp svcexecution.SubmitResponse) error
}

// formatters is the format-name -> Formatter registry every execution
// command resolves against, reusing the same --format flag values
// (formatTable/formatJSON) every other command group already
// establishes.
var formatters = map[string]Formatter{
	formatTable: tableFormatter{},
	formatJSON:  jsonFormatter{},
}

// resolveFormatter looks up the Formatter named by format: a clear
// error for an unsupported name, checked before any service call.
func resolveFormatter(format string) (Formatter, error) {
	f, ok := formatters[format]
	if !ok {
		return nil, fmt.Errorf("invalid --format %q: expected %s or %s", format, formatTable, formatJSON)
	}
	return f, nil
}
