package data

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// stq2barsDefaults contains the small set of reference listings Trader can
// resolve without asking an operator to repeat catalog metadata. Symbols
// outside this set require --exchange and --kind so the command never guesses
// an instrument identity from a Stooq directory name.
var stq2barsDefaults = map[string]struct {
	exchange string
	kind     string
}{
	"SPY":  {exchange: "ARCA", kind: "etf"},
	"QQQ":  {exchange: "NASDAQ", kind: "etf"},
	"AAPL": {exchange: "NASDAQ", kind: "equity"},
}

// newStq2BarsCmd implements the ergonomic Stooq D1 conversion path. It only
// resolves policy and defaults; extraction, raw import, canonicalization, and
// provenance remain owned by the existing data conversion service.
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
			if exchange == "" && kind == "" {
				defaults, known := stq2barsDefaults[symbol]
				if !known {
					return fmt.Errorf("unknown Stooq instrument %q: provide --exchange and --kind", symbol)
				}
				exchange, kind = defaults.exchange, defaults.kind
			} else if exchange == "" || kind == "" {
				return fmt.Errorf("--exchange and --kind must be provided together")
			}
			if from == "" {
				from = "1900-01-01"
			}
			if to == "" {
				to = "2100-01-01"
			}
			req, err := resolveDatasetRequest(cmd, []string{symbol, "D1"}, datasetArgFlags{
				from: from, to: to, exchange: exchange, kind: kind,
			})
			if err != nil {
				return err
			}
			if !rebuild {
				coverage, coverageErr := dc.Service.Coverage(cmd.Context(), svc.CoverageRequest{DatasetRequest: req})
				if coverageErr == nil && coverageIsComplete(coverage.Coverage) {
					_, err = fmt.Fprintf(cmd.OutOrStdout(), "skipped %s: canonical range is already current (use --rebuild to overwrite)\n", symbol)
					return err
				}
			}
			archivePath := archive
			if archivePath == "" {
				archivePath, err = findStooqArchive(dc.ArchiveRoot, symbol)
				if err != nil {
					return err
				}
			}
			action := "updated"
			if rebuild {
				action = "rebuilt"
			}
			tmp, err := os.MkdirTemp("", "trader-stooq-stq2bars-")
			if err != nil {
				return fmt.Errorf("create temporary extraction directory: %w", err)
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			extracted, err := extractStooqMember(cmd.Context(), archivePath, symbol, tmp)
			if err != nil {
				return err
			}
			resp, err := dc.Service.Convert(cmd.Context(), svc.ConvertRequest{
				DatasetRequest: req,
				ArchivePath:    extracted,
			})
			if err != nil {
				return err
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

func coverageIsComplete(coverage marketruntime.Coverage) bool {
	if len(coverage.Partitions) == 0 || len(coverage.Gaps) != 0 {
		return false
	}
	for _, partition := range coverage.Partitions {
		if partition.Status != marketruntime.PartitionCoverageCurrent {
			return false
		}
	}
	return true
}

func findStooqArchive(root, symbol string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("stooq archive root is not configured; provide --archive or --archive-root")
	}
	symbol = strings.ToLower(symbol)
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".zip") && strings.Contains(name, symbol) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search Stooq archive root %q: %w", root, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no Stooq archive for %s found under %q; provide --archive", strings.ToUpper(symbol), root)
	}
	sort.Strings(matches)
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple Stooq archives for %s found under %q; provide --archive", strings.ToUpper(symbol), root)
	}
	return matches[0], nil
}
