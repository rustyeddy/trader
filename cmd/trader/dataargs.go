package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// intervalsByName is the CLI's own string vocabulary for
// marketdata.Interval, deliberately separate from Interval.String()
// (documented one-directional, never parsed in core code — ADR-012).
// Parsing a fixed set of predefined values at the CLI boundary is
// exactly where that parsing is supposed to happen.
var intervalsByName = map[string]marketdata.Interval{
	"M1": marketdata.M1,
	"H1": marketdata.H1,
	"H4": marketdata.H4,
	"D1": marketdata.D1,
	"W1": marketdata.W1,
}

func parseInterval(s string) (marketdata.Interval, error) {
	iv, ok := intervalsByName[strings.ToUpper(strings.TrimSpace(s))]
	if !ok {
		return marketdata.Interval{}, fmt.Errorf(
			"invalid interval %q: expected one of M1, H1, H4, D1, W1", s)
	}
	return iv, nil
}

// parseDate accepts a bare date (assumed UTC midnight) or a full
// RFC3339 timestamp, covering both the "--from 2026-01-01" style
// ADR-022's own CLI sketch uses and a caller that needs sub-day
// precision.
func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD or RFC3339", s)
}

// datasetArgFlags holds the --from/--to/--format flag values every
// dataset command (bars, coverage, plan, sync, build, update) shares.
type datasetArgFlags struct {
	from   string
	to     string
	format string
}

// addDatasetArgFlags registers --from, --to (both required), and
// --format (issue #111; defaults to "table") on cmd.
func addDatasetArgFlags(cmd *cobra.Command, flags *datasetArgFlags) {
	cmd.Flags().StringVar(&flags.from, "from", "", "range start (YYYY-MM-DD or RFC3339), required")
	cmd.Flags().StringVar(&flags.to, "to", "", "range end (YYYY-MM-DD or RFC3339), required")
	cmd.Flags().StringVar(&flags.format, "format", formatTable,
		"output format: "+formatTable+" or "+formatJSON)
}

// resolveDatasetRequest parses args (exactly [INSTRUMENT, INTERVAL])
// and flags into a svc.DatasetRequest. It is the one place every
// dataset command (#109-#110) builds its request, so instrument/
// interval/range parsing behaves identically across all of them
// (issue #109's own "common ... arguments are consistent" acceptance
// criterion).
//
// Instrument resolution — turning the bare INSTRUMENT string into a
// registered instrument.ID the service's Manager can resolve — is
// deliberately not done here: svc.RegisterFXInstrument owns that,
// living in the service layer rather than this transport, since it
// has to invent domain/execution metadata (tick size and friends) that
// a CLI adapter has no business fabricating itself. See its own doc
// comment for the full reasoning.
func resolveDatasetRequest(cmd *cobra.Command, args []string, flags datasetArgFlags) (svc.DatasetRequest, error) {
	dc, ok := dataContextFrom(cmd.Context())
	if !ok {
		return svc.DatasetRequest{}, fmt.Errorf("data service is not configured on this command's context")
	}

	if len(args) != 2 {
		return svc.DatasetRequest{}, fmt.Errorf("expected exactly two arguments: INSTRUMENT INTERVAL")
	}

	instrumentID, err := svc.RegisterFXInstrument(dc.Resolver, dc.Provider, args[0])
	if err != nil {
		return svc.DatasetRequest{}, err
	}
	interval, err := parseInterval(args[1])
	if err != nil {
		return svc.DatasetRequest{}, err
	}

	if flags.from == "" || flags.to == "" {
		return svc.DatasetRequest{}, fmt.Errorf("--from and --to are both required")
	}
	from, err := parseDate(flags.from)
	if err != nil {
		return svc.DatasetRequest{}, err
	}
	to, err := parseDate(flags.to)
	if err != nil {
		return svc.DatasetRequest{}, err
	}
	timeRange, err := marketdata.NewTimeRange(from, to)
	if err != nil {
		return svc.DatasetRequest{}, fmt.Errorf("invalid range: %w", err)
	}

	return svc.DatasetRequest{
		Instrument: instrumentID,
		Interval:   interval,
		Range:      timeRange,
	}, nil
}
