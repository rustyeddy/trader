package account

import (
	"fmt"
	"io"

	rpt "github.com/rustyeddy/trader/internal/service/accountreport"
)

// Supported --format values.
const (
	formatTable = "table"
	formatJSON  = "json"
	formatOrg   = "org"
)

// Formatter renders one response to w. The handlers call it and never format
// a response themselves, so a new format touches neither a handler nor a
// service.
type Formatter interface {
	FormatSync(w io.Writer, out SyncOutput) error
	FormatList(w io.Writer, out ListOutput) error
	FormatTrades(w io.Writer, out TradesOutput) error
	FormatTrade(w io.Writer, d rpt.Detail) error
}

func formatterFor(name string) (Formatter, error) {
	switch name {
	case formatTable, "":
		return tableFormatter{}, nil
	case formatJSON:
		return jsonFormatter{}, nil
	case formatOrg:
		return orgFormatter{}, nil
	default:
		return nil, fmt.Errorf("--format %q: want %s, %s or %s", name, formatTable, formatJSON, formatOrg)
	}
}

// requireActivityFormat rejects, before any work is done, a format that
// cannot render activity. The org journal renders completed trades only, so
// `sync` must not run and then fail to print its result.
func requireActivityFormat(f Formatter) error {
	if _, ok := f.(orgFormatter); ok {
		return errOrgTradesOnly
	}
	return nil
}

// errOrgTradesOnly is the one message for every org request that is not a
// completed trade.
var errOrgTradesOnly = fmt.Errorf("--format %s renders completed trades only (trades, trades show <id>)", formatOrg)

// errWriter remembers the first write error so a broken pipe is reported
// instead of looking like success.
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

// pf and pl write to a sticky-error writer or a tabwriter, whose first failure
// is reported by the Formatter at the end (errWriter.err or Flush), so
// individual write results are deliberately not checked.
func pf(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }

func pl(w io.Writer, args ...any) { _, _ = fmt.Fprintln(w, args...) }
