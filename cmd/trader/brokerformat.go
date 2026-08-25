package main

import (
	"fmt"
	"io"

	svcbroker "github.com/rustyeddy/trader/service/broker"
)

// BrokerFormatter renders one broker CLI response to w in some
// presentation format — the broker command group's own analog of
// Formatter (format.go), kept as a separate interface rather than
// added to Formatter itself: the two cover unrelated response shapes
// (market-data datasets versus broker accounts/orders), and Trader's
// established convention throughout this codebase is small,
// domain-scoped interfaces over one broad one.
type BrokerFormatter interface {
	FormatAccounts(w io.Writer, resp svcbroker.AccountsResponse) error
	FormatSnapshot(w io.Writer, resp svcbroker.SnapshotResponse) error
	FormatSubmit(w io.Writer, resp svcbroker.SubmitResponse) error
}

// brokerFormatters is the format-name -> BrokerFormatter registry
// every broker command resolves against, reusing the same --format
// flag values (formatTable/formatJSON, format.go) the data command
// group already established.
var brokerFormatters = map[string]BrokerFormatter{
	formatTable: brokerTableFormatter{},
	formatJSON:  brokerJSONFormatter{},
}

// resolveBrokerFormatter looks up the BrokerFormatter named by format,
// mirroring resolveFormatter's own contract (format.go): a clear error
// for an unsupported name, checked before any service call.
func resolveBrokerFormatter(format string) (BrokerFormatter, error) {
	f, ok := brokerFormatters[format]
	if !ok {
		return nil, fmt.Errorf("invalid --format %q: expected %s or %s", format, formatTable, formatJSON)
	}
	return f, nil
}
