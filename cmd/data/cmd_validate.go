package data

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rustyeddy/trader/service"
	"github.com/spf13/cobra"
)

func newValidateCandlesCmd() *cobra.Command {
	var (
		instrumentsCSV string
		fromStr        string
		toStr          string
		timeframe      string
		source         string
		includeRaw     bool
		rawDir         string
		reportPath     string
	)

	cmd := &cobra.Command{
		Use:   "validate-candles",
		Short: "Scan stored candle months for missing expected slots and raw-source mismatches",
		RunE: func(cmd *cobra.Command, args []string) error {
			instruments := splitCSV(instrumentsCSV)
			if len(instruments) == 0 {
				return fmt.Errorf("missing --instruments (example: EURUSD,USDJPY,GBPUSD)")
			}

			start, err := parseMonthStart(fromStr)
			if err != nil {
				return fmt.Errorf("bad --from: %w", err)
			}
			end, err := parseMonthEndExclusive(toStr)
			if err != nil {
				return fmt.Errorf("bad --to: %w", err)
			}

			report, err := (&service.Service{}).ValidateCandleData(cmd.Context(), service.ValidateCandleDataRequest{
				Instruments: instruments,
				Source:      strings.TrimSpace(source),
				Timeframe:   timeframe,
				From:        start,
				To:          end,
				IncludeRaw:  includeRaw,
				RawDir:      rawDir,
			})
			if err != nil {
				return err
			}

			if reportPath != "" {
				if err := writeValidationReport(reportPath, report); err != nil {
					return err
				}
			}

			cmd.Printf("scanned %d month(s), found %d issue(s)\n", report.MonthsScanned, report.IssueCount())
			for _, issue := range report.Issues {
				cmd.Printf("[%s] %s %s %s %04d-%02d: %s\n",
					issue.Severity, issue.Source, issue.Instrument, issue.Timeframe, issue.Year, issue.Month, issue.Message)
			}
			if reportPath != "" {
				cmd.Printf("report written to %s\n", reportPath)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&instrumentsCSV, "instruments", "", "Comma-separated instruments (e.g. EURUSD,USDJPY)")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start month inclusive, format YYYY-MM")
	cmd.Flags().StringVar(&toStr, "to", "", "End month inclusive, format YYYY-MM")
	cmd.Flags().StringVar(&timeframe, "timeframe", "H1", "Candle timeframe to validate: M1, H1, or D1")
	cmd.Flags().StringVar(&source, "source", "oanda", "Stored candle source to validate")
	cmd.Flags().BoolVar(&includeRaw, "check-raw", true, "Also compare canonical candles with raw source data when supported")
	cmd.Flags().StringVar(&rawDir, "raw-dir", "", "Optional root dir for raw source validation (defaults to the store sibling raw dir)")
	cmd.Flags().StringVar(&reportPath, "report", "", "Optional JSON report output path")

	_ = cmd.MarkFlagRequired("instruments")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func writeValidationReport(path string, report any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
