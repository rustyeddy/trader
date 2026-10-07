package data

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// newStq2BarsCmd implements the ergonomic Stooq D1 conversion path. It
// parses flags and formats output; resolution, listing defaults, archive
// discovery, extraction, raw import, and canonicalization belong to the
// service's CanonicalizeDatasets (issues #434, #439).
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
			if dc.Service.ProviderInfo().Archive != marketruntime.ArchiveStooqZIP {
				return fmt.Errorf("stq2bars requires provider stooq, got %q", dc.Provider)
			}

			symbol := strings.ToUpper(strings.TrimSpace(args[0]))
			if symbol == "" {
				return fmt.Errorf("symbol must not be empty")
			}
			req, err := parseDatasetsRequest([]string{symbol, "D1"},
				datasetArgFlags{from: from, to: to, exchange: exchange, kind: kind}, true)
			if err != nil {
				return err
			}
			action := "updated"
			if rebuild {
				action = "rebuilt"
			}
			// The same canonicalize operation MCP runs (issue #439), for
			// one symbol.
			res, err := singleResult(dc.Service.CanonicalizeDatasets(cmd.Context(), req,
				svc.CanonicalizeOptions{Force: rebuild, ArchivePath: archive}))
			if err != nil {
				return err
			}
			if res.Err != nil {
				return stq2barsError(res.Err, symbol, kind, dc.ArchiveRoot)
			}
			if res.Convert == nil {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "converted %s (%s): no archive found; built from existing raw data; published %d canonical partitions\n",
					symbol, action, res.PublishedPartitions)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "converted %s (%s): imported %d rows across %d raw months; published %d canonical partitions\n",
				symbol, action, res.Convert.Import.RowsImported, res.Convert.Import.MonthsWritten, res.PublishedPartitions)
			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "range start (YYYY-MM-DD or RFC3339; defaults to earliest source data)")
	cmd.Flags().StringVar(&to, "to", "", "range end (YYYY-MM-DD or RFC3339; defaults to latest source data)")
	cmd.Flags().StringVar(&archive, "archive", "", "native Stooq ZIP archive path (otherwise found under --archive-root: a ZIP named for the symbol, else a bundle such as d_us_txt.zip holding it)")
	cmd.Flags().StringVar(&exchange, "exchange", "", "listing exchange for an unregistered symbol (for example ARCA or NASDAQ)")
	cmd.Flags().StringVar(&kind, "kind", "", `instrument kind for an unregistered symbol: "equity" or "etf"`)
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "rebuild canonical partitions even when they already exist")
	cmd.Flags().BoolVar(&rebuild, "overwrite", false, "overwrite existing raw and canonical partitions (alias for --rebuild)")
	return cmd
}

// stq2barsError adds stq2bars's flag guidance to identity errors, and
// stooqArchiveError's to archive errors.
func stq2barsError(err error, symbol, kind, root string) error {
	switch {
	case errors.Is(err, svc.ErrNoListingDefault):
		return fmt.Errorf("unknown Stooq instrument %q: provide --exchange and --kind", symbol)
	case errors.Is(err, svc.ErrIncompleteListingIdentity):
		return fmt.Errorf("--exchange and --kind must be provided together")
	case errors.Is(err, svc.ErrInvalidListingKind):
		return fmt.Errorf(`invalid --kind %q: expected "equity" or "etf"`, kind)
	}
	return stooqArchiveError(err, symbol, root)
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
