package data

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/instrument"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
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

// datasetArgFlags holds the --from/--to/--format/--exchange/--kind
// flag values every dataset command (bars, coverage, plan, sync,
// build, update) shares. exchange/kind are used only when the
// configured provider needs equity/ETF instrument registration rather
// than FX (issue #331), and may be omitted for a symbol with a listing
// default — see registerRequestedInstrument.
type datasetArgFlags struct {
	from     string
	to       string
	format   string
	exchange string
	kind     string
}

// addDatasetArgFlags registers --from, --to (both required), --format
// (issue #111; defaults to "table"), and --exchange/--kind (issue
// #331; required only for a non-FX provider) on cmd.
func addDatasetConversionFlags(cmd *cobra.Command, flags *datasetArgFlags) {
	cmd.Flags().StringVar(&flags.from, "from", "", "range start (YYYY-MM-DD or RFC3339), required")
	cmd.Flags().StringVar(&flags.to, "to", "", "range end (YYYY-MM-DD or RFC3339), required")
	cmd.Flags().StringVar(&flags.exchange, "exchange", "",
		"listing exchange (for example ARCA or NASDAQ); required for a non-FX provider such as alpaca")
	cmd.Flags().StringVar(&flags.kind, "kind", "",
		`instrument kind, "equity" or "etf"; required for a non-FX provider such as alpaca`)
}

func addDatasetArgFlags(cmd *cobra.Command, flags *datasetArgFlags) {
	addDatasetConversionFlags(cmd, flags)
	cmd.Flags().StringVar(&flags.format, "format", formatTable,
		"output format: "+formatTable+" or "+formatJSON)
}

// registerRequestedInstrument resolves symbol into a registered
// instrument.ID through the service layer's one resolution path
// (svc.RegisterInstrument, issue #448): FX for an FX provider, otherwise
// an equity/ETF from --exchange/--kind or the symbol's listing default.
// It only translates the service's errors into flag guidance.
func registerRequestedInstrument(dc dataContext, symbol string, flags datasetArgFlags) (instrument.ID, error) {
	listing, err := svc.RegisterInstrument(dc.Resolver, dc.Provider, svc.InstrumentRequest{
		Symbol: symbol, Exchange: flags.exchange, Kind: flags.kind,
	})
	switch {
	case err == nil:
		return listing.InstrumentID(), nil
	case errors.Is(err, svc.ErrNoListingDefault),
		errors.Is(err, svc.ErrIncompleteListingIdentity) && flags.exchange == "":
		return instrument.ID{}, fmt.Errorf(
			"--exchange is required for provider %q (for example --exchange ARCA or --exchange NASDAQ)", dc.Provider)
	case errors.Is(err, svc.ErrIncompleteListingIdentity):
		return instrument.ID{}, fmt.Errorf(
			`--kind is required for provider %q: expected "equity" or "etf"`, dc.Provider)
	case errors.Is(err, svc.ErrInvalidListingKind):
		return instrument.ID{}, fmt.Errorf(`invalid --kind %q: expected "equity" or "etf"`, flags.kind)
	default:
		return instrument.ID{}, err
	}
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
// deliberately not done here beyond calling registerRequestedInstrument:
// svc.RegisterInstrument owns it, in the service layer rather than this
// transport, since it has to invent domain/execution metadata (tick
// size and friends) that a CLI adapter has no business fabricating
// itself, and the MCP transport resolves through the same code.
func resolveDatasetRequest(cmd *cobra.Command, args []string, flags datasetArgFlags) (svc.DatasetRequest, error) {
	dc, ok := dataContextFrom(cmd.Context())
	if !ok {
		return svc.DatasetRequest{}, fmt.Errorf("data service is not configured on this command's context")
	}

	if len(args) != 2 {
		return svc.DatasetRequest{}, fmt.Errorf("expected exactly two arguments: INSTRUMENT INTERVAL")
	}

	instrumentID, err := registerRequestedInstrument(dc, args[0], flags)
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
