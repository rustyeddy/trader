package broker

import (
	"fmt"
	"io"

	svcbroker "github.com/rustyeddy/trader/service/broker"
)

// Supported --format values, duplicated from cmd/trader/data's own
// format.go rather than shared: each CLI command family package is
// deliberately independent (issue #201).
const (
	formatTable = "table"
	formatJSON  = "json"
)

// Formatter renders one broker CLI response to w in some
// presentation format — the broker command group's own analog of
// Formatter (format.go), kept as a separate interface rather than
// added to Formatter itself: the two cover unrelated response shapes
// (market-data datasets versus broker accounts/orders), and Trader's
// established convention throughout this codebase is small,
// domain-scoped interfaces over one broad one.
type Formatter interface {
	FormatAccounts(w io.Writer, resp svcbroker.AccountsResponse) error
	FormatSnapshot(w io.Writer, resp svcbroker.SnapshotResponse) error
	FormatSubmit(w io.Writer, resp svcbroker.SubmitResponse) error
}

// formatters is the format-name -> Formatter registry
// every broker command resolves against, reusing the same --format
// flag values (formatTable/formatJSON, format.go) the data command
// group already established.
var formatters = map[string]Formatter{
	formatTable: tableFormatter{},
	formatJSON:  jsonFormatter{},
}

// resolveFormatter looks up the Formatter named by format,
// mirroring resolveFormatter's own contract (format.go): a clear error
// for an unsupported name, checked before any service call.
func resolveFormatter(format string) (Formatter, error) {
	f, ok := formatters[format]
	if !ok {
		return nil, fmt.Errorf("invalid --format %q: expected %s or %s", format, formatTable, formatJSON)
	}
	return f, nil
}
