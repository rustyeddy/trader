// Package review hosts the `trader review` CLI command: a multi-timeframe
// watchlist review that triages FX pairs into Watch/Hot/Tradeable buckets.
// Business logic lives in review/ and service/; this package parses flags,
// calls the service, and formats output.
package review

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/review"
	"github.com/rustyeddy/trader/service"
)

// dateFlagLayout is the accepted format for --asof/--from/--to: a plain
// calendar date, parsed as UTC midnight.
const dateFlagLayout = "2006-01-02"

var (
	instrumentsCSV string
	showWatch      bool
	showHotlist    bool
	showTradeable  bool
	outputFormat   string
	token          string
	env            string
	asOfStr        string
	fromStr        string
	toStr          string
	interval       time.Duration
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review the watchlist across W1/D1/H4 and print triage buckets",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReview(cmd, rc)
		},
	}
	cmd.Flags().StringVar(&instrumentsCSV, "instruments", "", "Comma-separated pairs to review (default: every pair in the instrument registry)")
	cmd.Flags().BoolVar(&showWatch, "watch", false, "Print the watch bucket (default: all three buckets)")
	cmd.Flags().BoolVar(&showHotlist, "hotlist", false, "Print the hot bucket (default: all three buckets)")
	cmd.Flags().BoolVar(&showTradeable, "tradeable", false, "Print the tradeable bucket (default: all three buckets)")
	cmd.Flags().StringVar(&outputFormat, "output", "table", "Output format: table|json|org (table/org only for a single date; use json or csv for a multi-date sweep)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (takes precedence over global config, OANDA_TOKEN env var, and ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live (takes precedence over global config)")
	cmd.Flags().StringVar(&asOfStr, "asof", "", "Classify the watchlist as of this past date (YYYY-MM-DD) instead of now; cannot combine with --from/--to")
	cmd.Flags().StringVar(&fromStr, "from", "", "Historical sweep start date (YYYY-MM-DD, inclusive); requires --to")
	cmd.Flags().StringVar(&toStr, "to", "", "Historical sweep end date (YYYY-MM-DD, inclusive); requires --from")
	cmd.Flags().DurationVar(&interval, "interval", 24*time.Hour, "Step interval between sweep dates when --from and --to differ")
	return cmd
}

// buildService wires a market-data-only Service from flag values + global
// config + env fallbacks. The review endpoint needs no account resolution.
func buildService(cmd *cobra.Command, rc *config.RootConfig) (*service.Service, error) {
	tok := token
	if !cmd.Flags().Changed("token") {
		if rc != nil && rc.OANDAToken != "" {
			tok = rc.OANDAToken
		} else {
			tok = os.Getenv("OANDA_TOKEN")
		}
	}

	resolvedEnv := env
	if !cmd.Flags().Changed("env") && rc != nil && rc.OANDAEnv != "" {
		resolvedEnv = rc.OANDAEnv
	}

	return service.New(service.Config{
		Env:   resolvedEnv,
		Token: tok,
	})
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// selectedBuckets returns the set of triage buckets to print, honoring
// --watch/--hot/--tradeable. When none are set, all three buckets print,
// matching prior default behavior.
func selectedBuckets() map[string]bool {
	if !showWatch && !showHotlist && !showTradeable {
		return map[string]bool{"watch": true, "hot": true, "tradeable": true}
	}
	return map[string]bool{"watch": showWatch, "hot": showHotlist, "tradeable": showTradeable}
}

func validateOutputFormat(format string) error {
	switch format {
	case "table", "json", "org", "csv":
		return nil
	default:
		return fmt.Errorf("invalid --output %q: must be table, json, org, or csv", format)
	}
}

// parseHistoricalRange resolves --asof/--from/--to into a [from, to] date
// range and reports whether either was set at all (historical mode vs the
// live "now" path). --asof is sugar for from == to (a single-date sweep
// step); --from/--to must be set together. Dates are parsed as UTC
// midnight, matching the closed-bars-only convention in
// service.ReviewWatchlistRange: --asof 2026-06-15 means "as of the start of
// June 15", i.e. using data through June 14's close.
func parseHistoricalRange(cmd *cobra.Command) (from, to time.Time, historical bool, err error) {
	asOfSet := cmd.Flags().Changed("asof")
	fromSet := cmd.Flags().Changed("from")
	toSet := cmd.Flags().Changed("to")

	if asOfSet && (fromSet || toSet) {
		return time.Time{}, time.Time{}, false, fmt.Errorf("--asof cannot be combined with --from/--to")
	}
	if fromSet != toSet {
		return time.Time{}, time.Time{}, false, fmt.Errorf("--from and --to must be set together")
	}

	switch {
	case asOfSet:
		t, err := time.Parse(dateFlagLayout, asOfStr)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("invalid --asof %q: %w", asOfStr, err)
		}
		return t, t, true, nil
	case fromSet:
		from, err := time.Parse(dateFlagLayout, fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("invalid --from %q: %w", fromStr, err)
		}
		to, err := time.Parse(dateFlagLayout, toStr)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("invalid --to %q: %w", toStr, err)
		}
		if to.Before(from) {
			return time.Time{}, time.Time{}, false, fmt.Errorf("--to (%s) must not be before --from (%s)", toStr, fromStr)
		}
		return from, to, true, nil
	default:
		return time.Time{}, time.Time{}, false, nil
	}
}

func runReview(cmd *cobra.Command, rc *config.RootConfig) error {
	if err := validateOutputFormat(outputFormat); err != nil {
		return err
	}
	from, to, historical, err := parseHistoricalRange(cmd)
	if err != nil {
		return err
	}
	// A multi-date sweep needs a Date column the table/org bucket-grouped
	// layout doesn't have room for; a single date (live, or --asof with
	// from == to) reuses the existing bucket-grouped renderers unchanged.
	multiStep := historical && !from.Equal(to)
	if multiStep && outputFormat != "json" && outputFormat != "csv" {
		return fmt.Errorf("--output %q not supported for a multi-date sweep (--from/--to differ): use json or csv", outputFormat)
	}

	buckets := selectedBuckets()

	svc, err := buildService(cmd, rc)
	if err != nil {
		return err
	}

	var results []review.ReviewResult
	if historical {
		resp, err := svc.ReviewWatchlistRange(context.Background(), service.ReviewRangeRequest{
			Instruments: splitCSV(instrumentsCSV),
			From:        from,
			To:          to,
			Interval:    interval,
		})
		if err != nil {
			return err
		}
		results = resp.Results
	} else {
		resp, err := svc.ReviewWatchlist(context.Background(), service.ReviewRequest{
			Instruments: splitCSV(instrumentsCSV),
		})
		if err != nil {
			return err
		}
		results = resp.Results
	}

	filtered := make([]review.ReviewResult, 0, len(results))
	for _, r := range results {
		if buckets[r.Bucket] {
			filtered = append(filtered, r)
		}
	}

	if multiStep {
		sortByInstrumentThenDate(filtered)
		if outputFormat == "json" {
			return renderJSON(os.Stdout, filtered)
		}
		return renderCSV(os.Stdout, filtered)
	}

	sortByBucket(filtered)
	switch outputFormat {
	case "json":
		return renderJSON(os.Stdout, filtered)
	case "org":
		return renderOrg(os.Stdout, filtered)
	default:
		return renderTable(os.Stdout, filtered)
	}
}

// bucketRank orders triage buckets tradeable, then hot, then watch.
var bucketRank = map[string]int{"tradeable": 0, "hot": 1, "watch": 2}

// sortByBucket stable-sorts results tradeable-first, then hot, then watch;
// within each bucket, pairs are ordered by D1 ADX descending (per
// docs/Review.org's "Default sort" rule) so the strongest trends surface
// first.
func sortByBucket(results []review.ReviewResult) {
	sort.SliceStable(results, func(i, j int) bool {
		bi, bj := bucketRank[results[i].Bucket], bucketRank[results[j].Bucket]
		if bi != bj {
			return bi < bj
		}
		return results[i].D1.ADX > results[j].D1.ADX
	})
}

// sortByInstrumentThenDate orders a multi-date sweep's results so a single
// pair's bucket transitions read as a time series: grouped by instrument,
// oldest date first within each group.
func sortByInstrumentThenDate(results []review.ReviewResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Instrument != results[j].Instrument {
			return results[i].Instrument < results[j].Instrument
		}
		return results[i].ScannedAt.Before(results[j].ScannedAt)
	})
}

// reviewTableHeader/reviewTableRow keep the table and org renderers' column
// sets in sync.
var reviewTableHeader = []string{"PAIR", "BUCKET", "BIAS", "ADX", "CI", "EMA SEP", "ATR(p)", "EMA DIST", "H4 ADX", "H4 CI", "H4 EMA DIST", "Squeeze", "W1 Bias", "WEEK%"}

func reviewTableRow(r review.ReviewResult) []string {
	return []string{
		r.Instrument,
		r.Bucket,
		r.Bias,
		fmt.Sprintf("%.1f", r.D1.ADX),
		fmt.Sprintf("%.1f", r.D1.CI),
		fmt.Sprintf("%+.1f", r.D1.EMASepATR),
		fmt.Sprintf("%.1f", r.D1.ATRPips),
		fmt.Sprintf("%-.1f", r.D1.PriceEMA20ATR),
		fmt.Sprintf("%.1f", r.H4.ADX),
		fmt.Sprintf("%.1f", r.H4.CI),
		fmt.Sprintf("%+.3f", r.H4.PriceEMA20ATR),
		fmt.Sprintf("%t", r.H4.Squeeze),
		alignmentGlyph(r.Setup.W1Alignment),
		fmt.Sprintf("%.0f%%", r.W1.WeekUsedPct*100),
	}
}

// alignmentGlyph renders a glance-able glyph for the tristate W1/D1
// alignment rather than spelling out "aligned"/"neutral"/"conflict".
func alignmentGlyph(a review.Alignment) string {
	switch a {
	case review.Aligned:
		return "✓"
	case review.Conflict:
		return "✗"
	default:
		return "·"
	}
}

// reviewTableNumericCol flags which reviewTableHeader columns hold numeric
// values; those are right-justified so decimal points and "%" signs line up
// down the column. PAIR/BUCKET/BIAS are text and stay left-justified.
var reviewTableNumericCol = []bool{false, false, false, true, true, true, true, true, true, true, true, false, false, true}

// renderTable writes the human-readable aligned table: text columns
// left-justified, numeric columns right-justified so decimals/percents line
// up, with a blank line separating each bucket group.
func renderTable(out io.Writer, results []review.ReviewResult) error {
	if len(results) == 0 {
		fmt.Fprintln(out, "No results.")
		return nil
	}

	rows := make([][]string, len(results))
	for i, r := range results {
		rows[i] = reviewTableRow(r)
	}

	widths := make([]int, len(reviewTableHeader))
	for i, h := range reviewTableHeader {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	formatRow := func(cells []string) string {
		parts := make([]string, len(cells))
		for i, cell := range cells {
			if reviewTableNumericCol[i] {
				parts[i] = fmt.Sprintf("%*s", widths[i], cell)
			} else {
				parts[i] = fmt.Sprintf("%-*s", widths[i], cell)
			}
		}
		return strings.Join(parts, "  ")
	}

	underline := make([]string, len(reviewTableHeader))
	for i, h := range reviewTableHeader {
		underline[i] = strings.Repeat("-", len(h))
	}
	fmt.Fprintln(out, strings.TrimRight(formatRow(reviewTableHeader), " "))
	fmt.Fprintln(out, strings.TrimRight(formatRow(underline), " "))

	prevBucket := results[0].Bucket
	for i, row := range rows {
		if r := results[i]; r.Bucket != prevBucket {
			fmt.Fprintln(out)
			prevBucket = r.Bucket
		}
		fmt.Fprintln(out, strings.TrimRight(formatRow(row), " "))
	}
	return nil
}

// renderJSON writes results as an indented JSON array.
func renderJSON(out io.Writer, results []review.ReviewResult) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// renderOrg writes results as an Emacs org-mode table, with an hline
// separating each bucket group (org tables don't tolerate blank lines
// without ending the table).
func renderOrg(out io.Writer, results []review.ReviewResult) error {
	if len(results) == 0 {
		fmt.Fprintln(out, "No results.")
		return nil
	}

	fmt.Fprintf(out, "| %s |\n", strings.Join(reviewTableHeader, " | "))
	fmt.Fprintln(out, "|-")

	prevBucket := results[0].Bucket
	for _, r := range results {
		if r.Bucket != prevBucket {
			fmt.Fprintln(out, "|-")
			prevBucket = r.Bucket
		}
		fmt.Fprintf(out, "| %s |\n", strings.Join(reviewTableRow(r), " | "))
	}
	return nil
}

// renderCSV writes a multi-date sweep's results as CSV with a leading DATE
// column (RFC3339, since --interval can be sub-daily), one row per
// (date, instrument) — the output shape docs/asof-review-sweep-spec.md §4.3
// recommends for the sweep, since it's the easiest to load into external
// tooling for later threshold-tuning/grading work.
func renderCSV(out io.Writer, results []review.ReviewResult) error {
	w := csv.NewWriter(out)
	defer w.Flush()

	if err := w.Write(append([]string{"DATE"}, reviewTableHeader...)); err != nil {
		return err
	}
	for _, r := range results {
		row := append([]string{r.ScannedAt.UTC().Format(time.RFC3339)}, reviewTableRow(r)...)
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}
