package data

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/instrument"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

// parseInterval and parseDate read the CLI's interval names and dates
// through the service layer's shared parsers (ADR-069), so the CLI and
// MCP accept exactly the same values.
func parseInterval(s string) (marketdata.Interval, error) { return svc.ParseInterval(s) }

func parseDate(s string) (time.Time, error) { return svc.ParseDate(s) }

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
	if err != nil {
		return instrument.ID{}, instrumentFlagError(err, dc.Provider, flags)
	}
	return listing.InstrumentID(), nil
}

// instrumentFlagError translates the service's instrument-resolution
// errors into --exchange/--kind guidance; any other error passes through.
func instrumentFlagError(err error, provider string, flags datasetArgFlags) error {
	switch {
	case errors.Is(err, svc.ErrNoListingDefault),
		errors.Is(err, svc.ErrIncompleteListingIdentity) && flags.exchange == "":
		return fmt.Errorf(
			"--exchange is required for provider %q (for example --exchange ARCA or --exchange NASDAQ)", provider)
	case errors.Is(err, svc.ErrIncompleteListingIdentity):
		return fmt.Errorf(`--kind is required for provider %q: expected "equity" or "etf"`, provider)
	case errors.Is(err, svc.ErrInvalidListingKind):
		return fmt.Errorf(`invalid --kind %q: expected "equity" or "etf"`, flags.kind)
	default:
		return err
	}
}

// parseDatasetsRequest parses args (exactly [INSTRUMENT, INTERVAL]) and
// flags into a one-instrument svc.DatasetsRequest for the multi-symbol
// operations (issue #439), without resolving the instrument: the service
// operation does that. With rangeOptional, omitting both --from and --to
// leaves the range for the service to default.
func parseDatasetsRequest(args []string, flags datasetArgFlags, rangeOptional bool) (svc.DatasetsRequest, error) {
	if len(args) != 2 {
		return svc.DatasetsRequest{}, fmt.Errorf("expected exactly two arguments: INSTRUMENT INTERVAL")
	}
	interval, err := parseInterval(args[1])
	if err != nil {
		return svc.DatasetsRequest{}, err
	}
	req := svc.DatasetsRequest{
		Instruments: []svc.InstrumentRequest{{Symbol: args[0], Exchange: flags.exchange, Kind: flags.kind}},
		Interval:    interval,
	}
	if rangeOptional && flags.from == "" && flags.to == "" {
		return req, nil
	}
	if flags.from == "" || flags.to == "" {
		if rangeOptional {
			return svc.DatasetsRequest{}, fmt.Errorf("--from and --to must be provided together")
		}
		return svc.DatasetsRequest{}, fmt.Errorf("--from and --to are both required")
	}
	from, err := parseDate(flags.from)
	if err != nil {
		return svc.DatasetsRequest{}, err
	}
	to, err := parseDate(flags.to)
	if err != nil {
		return svc.DatasetsRequest{}, err
	}
	if req.Range, err = marketdata.NewTimeRange(from, to); err != nil {
		return svc.DatasetsRequest{}, fmt.Errorf("invalid range: %w", err)
	}
	return req, nil
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
