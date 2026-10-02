package data

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

// newStq2BarsCmd implements the ergonomic Stooq D1 conversion path. It
// parses flags and formats output; listing defaults, archive discovery,
// extraction, raw import, and canonicalization belong to the
// service (svc.ConvertStooqArchive, issue #434).
func newStq2BarsCmd() *cobra.Command {
	var from, to, exchange, kind, archive string
	var rebuild bool

	cmd := &cobra.Command{
		Use:   "stq2bars SYMBOL",
		Short: "Convert a Stooq daily archive into canonical bars.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dc, ok := dataContextFrom(cmd.Context())
			if !ok {
				return fmt.Errorf("data service is not configured on this command's context")
			}
			if dc.Provider != "stooq" {
				return fmt.Errorf("stq2bars requires provider stooq, got %q", dc.Provider)
			}

			symbol := strings.ToUpper(strings.TrimSpace(args[0]))
			if symbol == "" {
				return fmt.Errorf("symbol must not be empty")
			}
			identity, err := svc.ResolveListingIdentity(symbol, exchange, kind)
			switch {
			case errors.Is(err, svc.ErrNoListingDefault):
				return fmt.Errorf("unknown Stooq instrument %q: provide --exchange and --kind", symbol)
			case errors.Is(err, svc.ErrInvalidRequest):
				return fmt.Errorf("--exchange and --kind must be provided together")
			case err != nil:
				return err
			}
			req, err := resolveStq2BarsRequest(dc, symbol, from, to, identity)
			if err != nil {
				return err
			}
			action := "updated"
			if rebuild {
				action = "rebuilt"
			}
			resp, err := dc.Service.ConvertStooqArchive(cmd.Context(), svc.ConvertStooqArchiveRequest{
				DatasetRequest: req,
				Symbol:         symbol,
				ArchivePath:    archive,
				ArchiveRoot:    dc.ArchiveRoot,
				Force:          rebuild,
			})
			if err != nil {
				return stooqArchiveError(err, symbol, dc.ArchiveRoot)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "converted %s (%s): imported %d rows across %d raw months; published %d canonical partitions\n",
				symbol, action, resp.Import.RowsImported, resp.Import.MonthsWritten, len(resp.Build.Result.Published))
			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "range start (YYYY-MM-DD or RFC3339; defaults to earliest source data)")
	cmd.Flags().StringVar(&to, "to", "", "range end (YYYY-MM-DD or RFC3339; defaults to latest source data)")
	cmd.Flags().StringVar(&archive, "archive", "", "native Stooq ZIP archive path (otherwise discover it under --archive-root)")
	cmd.Flags().StringVar(&exchange, "exchange", "", "listing exchange for an unregistered symbol (for example ARCA or NASDAQ)")
	cmd.Flags().StringVar(&kind, "kind", "", `instrument kind for an unregistered symbol: "equity" or "etf"`)
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "rebuild canonical partitions even when they already exist")
	cmd.Flags().BoolVar(&rebuild, "overwrite", false, "overwrite existing raw and canonical partitions (alias for --rebuild)")
	return cmd
}

func resolveStq2BarsRequest(dc dataContext, symbol, from, to string, identity svc.ListingDefault) (svc.DatasetRequest, error) {
	id, err := registerRequestedInstrument(dc, symbol, datasetArgFlags{exchange: identity.Exchange, kind: identity.Kind})
	if err != nil {
		return svc.DatasetRequest{}, err
	}
	req := svc.DatasetRequest{Instrument: id, Interval: marketdata.D1}
	if from == "" && to == "" {
		return req, nil
	}
	if from == "" || to == "" {
		return svc.DatasetRequest{}, fmt.Errorf("--from and --to must be provided together")
	}
	start, err := parseDate(from)
	if err != nil {
		return svc.DatasetRequest{}, err
	}
	end, err := parseDate(to)
	if err != nil {
		return svc.DatasetRequest{}, err
	}
	req.Range, err = marketdata.NewTimeRange(start, end)
	if err != nil {
		return svc.DatasetRequest{}, fmt.Errorf("invalid range: %w", err)
	}
	return req, nil
}

// stooqArchiveError adds the CLI's flag guidance to archive-discovery
// failures; other errors pass through unchanged.
func stooqArchiveError(err error, symbol, root string) error {
	switch {
	case errors.Is(err, svc.ErrArchiveRootNotConfigured):
		return fmt.Errorf("stooq archive root is not configured; provide --archive or --archive-root")
	case errors.Is(err, svc.ErrArchiveNotFound):
		return fmt.Errorf("no Stooq archive for %s found under %q; provide --archive", symbol, root)
	case errors.Is(err, svc.ErrAmbiguousArchive):
		return fmt.Errorf("multiple Stooq archives for %s found under %q; provide --archive", symbol, root)
	}
	return err
}
