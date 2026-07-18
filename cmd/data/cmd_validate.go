package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/types"
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
		quiet          bool
		hideExpected   bool
		repair         bool
		token          string
		env            string
	)

	cmd := &cobra.Command{
		Use:   "validate-candles",
		Short: "Scan stored candle months for missing expected slots and raw-source mismatches",
		Long: `Validate derived candle data in the local store.

By default all instruments, the full date range, and all timeframes (M1, H1,
H4, D1) are validated. Supply --timeframe to check a single one.
Supply --instruments, --from, or --to to narrow the scope.

Output shows one row per instrument per year:

  EURUSD 2024  .  .  !  .  .  .  .  .  .  .  .  .

  .  month in range, no issues
  !  month has one or more issues
  -  month outside the requested range

Use --quiet to suppress the grid and show only the summary line and issues.

Use --hide-expected-gaps to drop "expected candle slots are missing" issues
from the grid, printed issue list, and summary counts. This only affects
display: --repair and --report still see the full, unfiltered issue set.

Use --repair to re-download every month that has missing expected slots from
OANDA. All validated timeframes are repaired.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			normSource := strings.TrimSpace(strings.ToLower(source))
			if normSource == "" {
				normSource = market.SourceOanda
			}

			timeframes := splitCSV(timeframe)
			if len(timeframes) == 0 {
				timeframes = []string{"M1", "H1", "H4", "D1"}
			}

			baseInstruments := splitCSV(instrumentsCSV)

			var allReports []*datamanager.CandleValidationReport
			totalMonths, totalIssues, totalRepairable := 0, 0, 0

			for _, tf := range timeframes {
				tfInstruments := baseInstruments
				tfFrom, tfTo := fromStr, toStr

				if len(tfInstruments) == 0 || tfFrom == "" || tfTo == "" {
					parsedTF, err := types.ParseTimeframe(tf)
					if err != nil {
						return fmt.Errorf("bad timeframe %q: %w", tf, err)
					}
					var resErr error
					tfInstruments, tfFrom, tfTo, resErr = resolveValidateDefaults(
						tfInstruments, tfFrom, tfTo, normSource, parsedTF,
					)
					if resErr != nil {
						cmd.Printf("── %s: %v (skipping)\n", tf, resErr)
						continue
					}
				}

				start, err := parseMonthStart(tfFrom)
				if err != nil {
					return fmt.Errorf("bad --from for %s: %w", tf, err)
				}
				end, err := parseMonthEndExclusive(tfTo)
				if err != nil {
					return fmt.Errorf("bad --to for %s: %w", tf, err)
				}

				report, err := (&service.Service{}).ValidateCandleData(cmd.Context(), service.ValidateCandleDataRequest{
					Instruments: tfInstruments,
					Source:      normSource,
					Timeframe:   tf,
					From:        start,
					To:          end,
					IncludeRaw:  includeRaw,
					RawDir:      rawDir,
				})
				if err != nil {
					return err
				}

				displayReport := report
				if hideExpected {
					displayReport = filterReportIssues(report, "missing_expected_candles")
				}

				if !quiet {
					if len(timeframes) > 1 {
						cmd.Printf("── %s ──\n", tf)
					}
					printValidationGrid(cmd, tfInstruments, start.Year(), end.Year(), start.Month(), end.Month(), displayReport)
				}

				cmd.Printf("%s: scanned %d month(s), found %d issue(s)\n", tf, displayReport.MonthsScanned, displayReport.IssueCount())
				for _, issue := range displayReport.Issues {
					cmd.Printf("  [%s] %s %s %04d-%02d: %s\n",
						issue.Severity, issue.Instrument, issue.Timeframe, issue.Year, issue.Month, issue.Message)
				}

				totalMonths += displayReport.MonthsScanned
				totalIssues += displayReport.IssueCount()
				totalRepairable += report.IssueCount()
				allReports = append(allReports, report)
			}

			if len(timeframes) > 1 {
				cmd.Printf("\ntotal: scanned %d month(s), found %d issue(s)\n", totalMonths, totalIssues)
			}

			if reportPath != "" && len(allReports) == 1 {
				if err := writeValidationReport(reportPath, allReports[0]); err != nil {
					return err
				}
				cmd.Printf("report written to %s\n", reportPath)
			}

			if repair && totalRepairable > 0 {
				return repairMissingCandles(cmd, allReports, rawDir, token, env)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&instrumentsCSV, "instruments", "", "Comma-separated instruments; default: all in store")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start month inclusive YYYY-MM; default: earliest in store")
	cmd.Flags().StringVar(&toStr, "to", "", "End month inclusive YYYY-MM; default: latest in store")
	cmd.Flags().StringVar(&timeframe, "timeframe", "", "Candle timeframe(s) to validate: M1, H1, H4, D1 (default: all)")
	cmd.Flags().StringVar(&source, "source", "oanda", "Stored candle source to validate")
	cmd.Flags().BoolVar(&includeRaw, "check-raw", true, "Also compare canonical candles with raw source data when supported")
	cmd.Flags().StringVar(&rawDir, "raw-dir", "", "Optional root dir for raw source validation (defaults to the store sibling raw dir)")
	cmd.Flags().StringVar(&reportPath, "report", "", "Optional JSON report output path (single-timeframe only)")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress the per-instrument month grid; show only summary and issues")
	cmd.Flags().BoolVar(&hideExpected, "hide-expected-gaps", false, "Hide 'expected candle slots are missing' issues from the grid, issue list, and summary counts (does not affect --repair or --report)")
	cmd.Flags().BoolVar(&repair, "repair", false, "Re-download from OANDA every month that has missing expected candle slots")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (used with --repair)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live (used with --repair)")

	return cmd
}

// repairMissingCandles implements the repair pipeline:
//  1. Collect all months with missing_expected_candles or missing_candle_month issues.
//  2. For each month with an existing raw file: derive canonical from it. If
//     that fills every expected slot, the month is repaired without touching
//     the network.
//  3. Otherwise — raw missing entirely, or present but unable to fill every
//     expected slot (a file on disk is not proof the month is complete; it
//     may be a partial fetch) — download the full month from OANDA fresh,
//     overwriting the raw file, and derive again.
func repairMissingCandles(
	cmd *cobra.Command,
	reports []*datamanager.CandleValidationReport,
	rawDir, token, env string,
) error {
	// Default rawDir to the store's sibling raw tree.
	if rawDir == "" {
		rawDir = datamanager.RawRoot()
	}

	type monthKey struct {
		instrument string
		timeframe  string
		year       int
		month      int
	}
	seen := make(map[monthKey]bool)
	var toRepair []monthKey

	for _, report := range reports {
		for _, iss := range report.Issues {
			if iss.Kind != "missing_expected_candles" && iss.Kind != "missing_candle_month" {
				continue
			}
			k := monthKey{iss.Instrument, iss.Timeframe, iss.Year, iss.Month}
			if !seen[k] {
				seen[k] = true
				toRepair = append(toRepair, k)
			}
		}
	}

	if len(toRepair) == 0 {
		cmd.Println("repair: no missing_expected_candles issues to fix")
		return nil
	}

	svc, err := service.New(service.Config{Env: env, Token: token})
	if err != nil {
		return fmt.Errorf("repair: connect to OANDA: %w", err)
	}

	cmd.Printf("\nrepairing %d month(s) (raw dir: %s)...\n", len(toRepair), rawDir)

	type logEntry struct {
		Instrument     string   `json:"instrument"`
		Timeframe      string   `json:"timeframe"`
		Year           int      `json:"year"`
		Month          int      `json:"month"`
		Status         string   `json:"status"`
		CandlesWritten int      `json:"candles_written,omitempty"`
		MissingSlots   int      `json:"missing_slots,omitempty"`
		SampleMissing  []string `json:"sample_missing,omitempty"`
		Error          string   `json:"error,omitempty"`
	}
	type repairLog struct {
		RunAt       string     `json:"run_at"`
		RawDir      string     `json:"raw_dir"`
		Entries     []logEntry `json:"entries"`
		TotalErrors int        `json:"total_errors"`
	}
	log := repairLog{
		RunAt:  time.Now().UTC().Format(time.RFC3339),
		RawDir: rawDir,
	}

	var repairErrors int

	for _, k := range toRepair {
		entry := logEntry{Instrument: k.instrument, Timeframe: k.timeframe, Year: k.year, Month: k.month}

		tf, err := types.ParseTimeframe(k.timeframe)
		if err != nil {
			entry.Status = "error"
			entry.Error = err.Error()
			log.Entries = append(log.Entries, entry)
			cmd.Printf("  [error] %s %s %04d-%02d: %v\n", k.instrument, k.timeframe, k.year, k.month, err)
			repairErrors++
			continue
		}

		rawKey := datamanager.Key{
			Kind:       datamanager.KindCandle,
			Source:     market.SourceOanda,
			Instrument: k.instrument,
			TF:         tf,
			Year:       k.year,
			Month:      k.month,
		}
		rawPath := datamanager.RawCandlePathAt(rawDir, rawKey)

		// Step 2: if raw exists, try deriving from it first — repaired
		// without touching the network iff it fills every expected slot.
		rawExists := true
		if _, statErr := os.Stat(rawPath); os.IsNotExist(statErr) {
			rawExists = false
		}

		var preResult *service.DeriveResult
		if rawExists {
			r, deriveErr := svc.DeriveCanonicalFromRaw(cmd.Context(), rawPath, rawKey)
			if deriveErr != nil {
				entry.Status = "error"
				entry.Error = "derive: " + deriveErr.Error()
				log.Entries = append(log.Entries, entry)
				cmd.Printf("  [error] %s %s %04d-%02d: derive: %v\n", k.instrument, k.timeframe, k.year, k.month, deriveErr)
				repairErrors++
				continue
			}
			preResult = r
		}

		preMissing := 0
		if preResult != nil {
			preMissing = preResult.MissingSlots
		}
		if !repairNeedsDownload(rawExists, preMissing) {
			entry.Status = "derived"
			entry.CandlesWritten = preResult.CandlesWritten
			log.Entries = append(log.Entries, entry)
			cmd.Printf("  [ok]    %s %s %04d-%02d: %d candle(s) derived from existing raw\n",
				k.instrument, k.timeframe, k.year, k.month, preResult.CandlesWritten)
			continue
		}

		// Step 3: raw missing, or present but incomplete — download the
		// full month fresh (overwriting raw) and derive again.
		if rawExists {
			cmd.Printf("  [raw partial] %s %s %04d-%02d: %d expected slot(s) unfilled by existing raw — re-downloading\n",
				k.instrument, k.timeframe, k.year, k.month, preMissing)
		}
		oandaInst := k.instrument
		if len(k.instrument) == 6 {
			oandaInst = k.instrument[:3] + "_" + k.instrument[3:]
		}
		monthStart := time.Date(k.year, time.Month(k.month), 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, 0).Add(-24 * time.Hour)

		_, dlErr := svc.DownloadOandaCandles(cmd.Context(), service.DownloadOandaCandlesRequest{
			Instrument: oandaInst,
			Timeframe:  k.timeframe,
			From:       monthStart,
			To:         monthEnd,
			RawDir:     rawDir,
			OnProgress: func(line string) { cmd.Printf("    %s\n", line) },
		})
		if dlErr != nil {
			entry.Status = "error"
			entry.Error = "download: " + dlErr.Error()
			log.Entries = append(log.Entries, entry)
			cmd.Printf("  [error] %s %s %04d-%02d: download: %v\n", k.instrument, k.timeframe, k.year, k.month, dlErr)
			repairErrors++
			continue
		}
		cmd.Printf("  [downloaded] %s %s %04d-%02d\n", k.instrument, k.timeframe, k.year, k.month)
		entry.Status = "downloaded"

		result, deriveErr := svc.DeriveCanonicalFromRaw(cmd.Context(), rawPath, rawKey)
		if deriveErr != nil {
			entry.Status = "error"
			entry.Error = "derive: " + deriveErr.Error()
			log.Entries = append(log.Entries, entry)
			cmd.Printf("  [error] %s %s %04d-%02d: derive: %v\n", k.instrument, k.timeframe, k.year, k.month, deriveErr)
			repairErrors++
			continue
		}
		entry.CandlesWritten = result.CandlesWritten
		entry.MissingSlots = result.MissingSlots
		entry.SampleMissing = result.SampleMissing
		log.Entries = append(log.Entries, entry)

		if preResult != nil && result.CandlesWritten < preResult.CandlesWritten {
			// The fresh fetch has already overwritten the raw file, so
			// surface loudly that OANDA returned less than the archive had.
			cmd.Printf("  [warn]  %s %s %04d-%02d: re-download produced FEWER candles than existing raw (%d -> %d)\n",
				k.instrument, k.timeframe, k.year, k.month, preResult.CandlesWritten, result.CandlesWritten)
		}
		if result.MissingSlots > 0 {
			cmd.Printf("  [warn]  %s %s %04d-%02d: %d candle(s) derived, %d market-hours slot(s) missing from raw (sample: %v)\n",
				k.instrument, k.timeframe, k.year, k.month,
				result.CandlesWritten, result.MissingSlots, result.SampleMissing)
		} else {
			cmd.Printf("  [ok]    %s %s %04d-%02d: %d candle(s) derived\n",
				k.instrument, k.timeframe, k.year, k.month, result.CandlesWritten)
		}
	}

	log.TotalErrors = repairErrors

	// Write validation log to the data root.
	dataRoot := filepath.Dir(datamanager.RawRoot())
	logPath := filepath.Join(dataRoot, "validation-"+time.Now().UTC().Format("2006-01-02")+".json")
	if err := writeValidationReport(logPath, log); err != nil {
		cmd.Printf("\n[warn] could not write validation log %s: %v\n", logPath, err)
	} else {
		cmd.Printf("\nvalidation log: %s\n", logPath)
	}

	if repairErrors > 0 {
		return fmt.Errorf("repair: %d month(s) failed", repairErrors)
	}
	return nil
}

// repairNeedsDownload reports whether a repair month must be re-downloaded
// from OANDA. A raw file's presence alone is not proof the month is complete
// or correct — it may be a partial fetch covering only part of the month —
// so only a derive that fills every expected market-hours slot counts as
// evidence the existing raw suffices.
func repairNeedsDownload(rawExists bool, missingAfterDerive int) bool {
	return !rawExists || missingAfterDerive > 0
}

// printValidationGrid prints a compact year-per-row grid for each instrument.
// Each month cell shows '.' (ok), '!' (has issues), or '-' (out of range).
func printValidationGrid(
	cmd *cobra.Command,
	instruments []string,
	startYear, endYear int,
	startMonth, endMonth time.Month,
	report *datamanager.CandleValidationReport,
) {
	// Build an issue index: instrument → year → month → true.
	hasIssue := make(map[string]map[int]map[int]bool)
	for _, iss := range report.Issues {
		inst := iss.Instrument
		if hasIssue[inst] == nil {
			hasIssue[inst] = make(map[int]map[int]bool)
		}
		if hasIssue[inst][iss.Year] == nil {
			hasIssue[inst][iss.Year] = make(map[int]bool)
		}
		hasIssue[inst][iss.Year][iss.Month] = true
	}

	// Header: pad to match label width then month numbers.
	labelW := maxInstrumentLen(instruments) + 5 // "EURUSD 2024" = name + space + 4-digit year
	cmd.Printf("%*s  01 02 03 04 05 06 07 08 09 10 11 12\n", labelW, "")

	for _, inst := range instruments {
		for year := startYear; year <= endYear; year++ {
			label := fmt.Sprintf("%-*s %4d", labelW-5, inst, year)
			var row strings.Builder
			row.WriteString(label)
			for m := 1; m <= 12; m++ {
				inRange := monthInRange(year, m, startYear, endYear, startMonth, endMonth)
				switch {
				case !inRange:
					row.WriteString("  -")
				case hasIssue[inst][year][m]:
					row.WriteString("  !")
				default:
					row.WriteString("  .")
				}
			}
			cmd.Println(row.String())
		}
	}
	cmd.Println()
}

// monthInRange reports whether year/month falls within [startYear/startMonth, endYear/endMonth].
func monthInRange(year, month, startYear, endYear int, startMonth, endMonth time.Month) bool {
	if year < startYear || year > endYear {
		return false
	}
	if year == startYear && time.Month(month) < startMonth {
		return false
	}
	if year == endYear && time.Month(month) > endMonth {
		return false
	}
	return true
}

// maxInstrumentLen returns the length of the longest instrument name.
func maxInstrumentLen(instruments []string) int {
	max := 0
	for _, inst := range instruments {
		if len(inst) > max {
			max = len(inst)
		}
	}
	return max
}

// resolveValidateDefaults fills in missing instruments, fromStr, and toStr by
// scanning the store inventory for candle keys matching source and timeframe.
func resolveValidateDefaults(
	instruments []string,
	fromStr, toStr string,
	source string,
	tf types.Timeframe,
) (outInstruments []string, outFrom, outTo string, err error) {
	keys, err := datamanager.ListCandleKeys()
	if err != nil {
		return nil, "", "", fmt.Errorf("scan store: %w", err)
	}

	instSet := make(map[string]struct{})
	for _, inst := range instruments {
		instSet[inst] = struct{}{}
	}

	var minYear, minMonth, maxYear, maxMonth int

	for _, key := range keys {
		if key.Source != source {
			continue
		}
		if key.TF != tf {
			continue
		}

		if len(instruments) == 0 {
			instSet[key.Instrument] = struct{}{}
		}

		if fromStr == "" {
			if minYear == 0 || key.Year < minYear || (key.Year == minYear && key.Month < minMonth) {
				minYear, minMonth = key.Year, key.Month
			}
		}
		if toStr == "" {
			if key.Year > maxYear || (key.Year == maxYear && key.Month > maxMonth) {
				maxYear, maxMonth = key.Year, key.Month
			}
		}
	}

	if len(instSet) == 0 {
		return nil, "", "", fmt.Errorf("no %s %s candles found in store", source, tf)
	}

	outInstruments = make([]string, 0, len(instSet))
	for inst := range instSet {
		outInstruments = append(outInstruments, inst)
	}
	sort.Strings(outInstruments)

	outFrom = fromStr
	if outFrom == "" {
		if minYear == 0 {
			return nil, "", "", fmt.Errorf("could not determine start date from store")
		}
		outFrom = fmt.Sprintf("%04d-%02d", minYear, minMonth)
	}

	outTo = toStr
	if outTo == "" {
		if maxYear == 0 {
			return nil, "", "", fmt.Errorf("could not determine end date from store")
		}
		outTo = fmt.Sprintf("%04d-%02d", maxYear, maxMonth)
	}

	return outInstruments, outFrom, outTo, nil
}

// filterReportIssues returns a copy of report with every issue of the given
// kind removed, for display purposes only. The original report (passed to
// --repair and --report) is left untouched.
func filterReportIssues(report *datamanager.CandleValidationReport, kind string) *datamanager.CandleValidationReport {
	filtered := &datamanager.CandleValidationReport{
		Source:        report.Source,
		Timeframe:     report.Timeframe,
		IncludeRaw:    report.IncludeRaw,
		MonthsScanned: report.MonthsScanned,
	}
	for _, iss := range report.Issues {
		if iss.Kind == kind {
			continue
		}
		filtered.Issues = append(filtered.Issues, iss)
	}
	return filtered
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
