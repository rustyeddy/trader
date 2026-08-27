package data

import (
	"fmt"
	"io"

	svc "github.com/rustyeddy/trader/service/marketdata"
)

// Supported --format values (issue #111). "table" is every command's
// default -- human-readable, matching what #109/#110 already printed
// before this issue existed.
const (
	formatTable = "table"
	formatJSON  = "json"
)

// Formatter renders one CLI response to w in some presentation
// format. It is the one seam issue #111 introduces: every data
// command calls a Formatter instead of formatting a response itself,
// so adding a future format (Org, say) never touches a command
// handler or, more importantly, never touches service/marketdata --
// the service layer returns the same structured response regardless
// of which Formatter a command ends up using.
//
// Each method's own contract matches the corresponding service
// response's own partial-progress contract (SyncResponse,
// BuildResponse, UpdateResponse's own doc comments): a Formatter must
// render whatever the response actually contains, including a
// zero/partial value, without assuming the caller only ever formats a
// successful result -- update.go's own error path is exactly this
// case (see FormatUpdateProgress).
type Formatter interface {
	FormatBars(w io.Writer, resp svc.BarsResponse) error
	FormatCoverage(w io.Writer, resp svc.CoverageResponse) error
	FormatPlan(w io.Writer, resp svc.PlanResponse) error
	FormatSync(w io.Writer, resp svc.SyncResponse) error
	FormatBuild(w io.Writer, resp svc.BuildResponse) error
	// FormatUpdate renders resp as a completed, successful Update.
	FormatUpdate(w io.Writer, resp svc.UpdateResponse) error
	// FormatUpdateProgress renders only resp's partial Sync/Build
	// progress, never claiming the dataset is now current -- the
	// shape update.go's own error branch needs (see its doc
	// comment for why FormatUpdate itself must not be reused there).
	FormatUpdateProgress(w io.Writer, resp svc.UpdateResponse) error
}

// formatters is the format-name -> Formatter registry every data
// command resolves against. Adding a future format is exactly one
// entry here plus one new Formatter implementation -- nothing else in
// this package, and nothing in service/marketdata, changes.
var formatters = map[string]Formatter{
	formatTable: tableFormatter{},
	formatJSON:  jsonFormatter{},
}

// resolveFormatter looks up the Formatter named by format, returning a
// clear error for an unsupported name -- the same "formatting errors
// are handled consistently" contract issue #111 asks for, applied
// before any service call rather than after, matching every other
// dataset command's own "validate first" convention
// (resolveDatasetRequest).
func resolveFormatter(format string) (Formatter, error) {
	f, ok := formatters[format]
	if !ok {
		return nil, fmt.Errorf("invalid --format %q: expected %s or %s", format, formatTable, formatJSON)
	}
	return f, nil
}
