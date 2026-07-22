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
	"github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/review"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/view"
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

	// Threshold flags override config-file/default review.Thresholds
	// fields for ad-hoc sweep tuning (GitHub issue #165). Zero (unset)
	// means "don't override" — see review.MergeThresholds.
	hotADXFloor        float64
	hotCICeiling       float64
	tradeableCICeiling float64
	h4ADXFloor         float64
	h4MinEMASep        float64
	demotionADXFloor   float64
	demotionCICeiling  float64
	weekUsedCaution    float64
	valueZoneMin       float64
	valueZoneMax       float64
	h4SqueezeWidthATR  float64
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
	cmd.Flags().StringVar(&outputFormat, "output", "table", "Output format: table|json|org|csv (table/org only for a single date; use json or csv for a multi-date sweep)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (takes precedence over global config, OANDA_TOKEN env var, and ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live (takes precedence over global config)")
	cmd.Flags().StringVar(&asOfStr, "asof", "", "Classify the watchlist as of this past date (YYYY-MM-DD) instead of now; cannot combine with --from/--to")
	cmd.Flags().StringVar(&fromStr, "from", "", "Historical sweep start date (YYYY-MM-DD, inclusive); requires --to")
	cmd.Flags().StringVar(&toStr, "to", "", "Historical sweep end date (YYYY-MM-DD, inclusive); requires --from")
	cmd.Flags().DurationVar(&interval, "interval", 24*time.Hour, "Step interval between sweep dates when --from and --to differ")

	cmd.Flags().Float64Var(&hotADXFloor, "hot-adx-floor", 0, "Hot gate: D1 ADX floor (default: config, else 25.0)")
	cmd.Flags().Float64Var(&hotCICeiling, "hot-ci-ceiling", 0, "Hot gate: D1 CI ceiling (default: config, else 55.0)")
	cmd.Flags().Float64Var(&tradeableCICeiling, "tradeable-ci-ceiling", 0, "Tradeable gate: H4 CI ceiling (default: config, else 60.0)")
	cmd.Flags().Float64Var(&h4ADXFloor, "h4-adx-floor", 0, "Tradeable gate: H4 ADX floor (default: config, else 20.0)")
	cmd.Flags().Float64Var(&h4MinEMASep, "h4-min-ema-sep", 0, "Tradeable gate: H4 EMA20/50 separation floor, in ATR multiples (default: config, else 0.3)")
	cmd.Flags().Float64Var(&demotionADXFloor, "demotion-adx-floor", 0, "Demotion note: D1 ADX floor (default: config, else 20.0)")
	cmd.Flags().Float64Var(&demotionCICeiling, "demotion-ci-ceiling", 0, "Demotion note: D1 CI ceiling (default: config, else 65.0)")
	cmd.Flags().Float64Var(&weekUsedCaution, "week-used-caution", 0, "Demotion note: weekly ATR-budget-used caution threshold (default: config, else 0.90)")
	cmd.Flags().Float64Var(&valueZoneMin, "value-zone-min", 0, "Setup gate: H4 price-vs-EMA20 value-zone lower bound, in ATR multiples (default: config, else 0.5)")
	cmd.Flags().Float64Var(&valueZoneMax, "value-zone-max", 0, "Setup gate: H4 price-vs-EMA20 value-zone upper bound, in ATR multiples (default: config, else 1.5)")
	cmd.Flags().Float64Var(&h4SqueezeWidthATR, "h4-squeeze-width-atr", 0, "H4 Bollinger squeeze threshold, in ATR multiples (default: config, else 2.0)")
	return cmd
}

// resolveThresholds layers CLI-flag overrides over rc.ReviewThresholds
// (itself already resolved from global config, falling back to
// review.DefaultThresholds() — see cmd/main.go and config.GlobalReviewConfig).
// Only flags the user actually passed participate in the override, so an
// unset flag never clobbers a configured value with 0.
func resolveThresholds(cmd *cobra.Command, rc *config.RootConfig) review.Thresholds {
	base := rc.ReviewThresholds
	if base == (review.Thresholds{}) {
		base = review.DefaultThresholds()
	}

	var override review.Thresholds
	flags := cmd.Flags()
	if flags.Changed("hot-adx-floor") {
		override.HotD1ADXFloor = hotADXFloor
	}
	if flags.Changed("hot-ci-ceiling") {
		override.HotD1CICeiling = hotCICeiling
	}
	if flags.Changed("tradeable-ci-ceiling") {
		override.TradeableH4CICeiling = tradeableCICeiling
	}
	if flags.Changed("h4-adx-floor") {
		override.H4ADXFloor = h4ADXFloor
	}
	if flags.Changed("h4-min-ema-sep") {
		override.H4MinEMASep = h4MinEMASep
	}
	if flags.Changed("demotion-adx-floor") {
		override.DemotionD1ADXFloor = demotionADXFloor
	}
	if flags.Changed("demotion-ci-ceiling") {
		override.DemotionD1CICeiling = demotionCICeiling
	}
	if flags.Changed("week-used-caution") {
		override.WeekUsedCaution = weekUsedCaution
	}
	if flags.Changed("value-zone-min") {
		override.ValueZoneMin = valueZoneMin
	}
	if flags.Changed("value-zone-max") {
		override.ValueZoneMax = valueZoneMax
	}
	if flags.Changed("h4-squeeze-width-atr") {
		override.H4SqueezeWidthATR = h4SqueezeWidthATR
	}

	return review.MergeThresholds(base, override)
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
		Log:   log.L,
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
	th := resolveThresholds(cmd, rc)

	var results []review.ReviewResult
	if historical {
		resp, err := svc.ReviewWatchlistRange(context.Background(), service.ReviewRangeRequest{
			Instruments: splitCSV(instrumentsCSV),
			From:        from,
			To:          to,
			Interval:    interval,
			Thresholds:  th,
		})
		if err != nil {
			return err
		}
		results = resp.Results
	} else {
		resp, err := svc.ReviewWatchlist(context.Background(), service.ReviewRequest{
			Instruments: splitCSV(instrumentsCSV),
			Thresholds:  th,
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

	return renderResults(os.Stdout, outputFormat, filtered, multiStep)
}

// renderResults sorts and writes filtered per format, choosing between the
// sweep-oriented path (instrument+date sort; json/csv only, since a
// multi-date sweep needs a Date column the bucket-grouped table/org layout
// has no room for) and the single-date path (bucket sort; table/json/org/csv
// all apply, since it's still one row per instrument). Factored out of
// runReview so the format-to-renderer mapping — every one of table/json/
// org/csv must actually be reachable for a single date, not just accepted
// by validateOutputFormat — is directly unit-testable without a live
// Service.
func renderResults(out io.Writer, format string, filtered []review.ReviewResult, multiStep bool) error {
	if multiStep {
		sortByInstrumentThenDate(filtered)
		if format == "json" {
			return renderJSON(out, filtered)
		}
		return renderCSV(out, filtered)
	}

	sortByBucket(filtered)
	switch format {
	case "json":
		return renderJSON(out, filtered)
	case "org":
		return renderOrg(out, filtered)
	case "csv":
		return renderCSV(out, filtered)
	default:
		return renderTable(out, filtered)
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
var reviewTableHeader = []string{"PAIR", "BUCKET", "BIAS", "ADX", "CI", "EMA SEP", "ATR(p)", "EMA DIST", "H4 ADX", "H4 CI", "H4 EMA DIST", "Squeeze", "W1 Bias", "WEEK%", "H1 Align", "H1 EMA DIST"}

func reviewTableRow(r review.ReviewResult) []string {
	// H1 is only ever computed for tradeable pairs (see
	// review.EnrichWithH1); other buckets show "–" rather than a
	// misleading zero value, distinguishing "never attempted" from a
	// tradeable pair whose H1 genuinely came back unavailable.
	h1Align, h1Dist := "–", "–"
	if r.Bucket == "tradeable" {
		h1Align = boolGlyph(r.Setup.H1Aligned)
		h1Dist = fmt.Sprintf("%+.3f", r.Setup.H1EntryDist)
	}

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
		h1Align,
		h1Dist,
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

// boolGlyph renders a glance-able glyph for a plain two-state bool, as
// opposed to alignmentGlyph's tristate Alignment — used for H1Aligned,
// which (like H4Aligned) has no neutral state.
func boolGlyph(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}

// reviewTableNumericCol flags which reviewTableHeader columns hold numeric
// values; those are right-justified so decimal points and "%" signs line up
// down the column. PAIR/BUCKET/BIAS are text and stay left-justified.
var reviewTableNumericCol = []bool{false, false, false, true, true, true, true, true, true, true, true, false, false, true, false, true}

// buildReviewTable turns filtered results into a view.Table: one row per
// result via reviewTableRow, numeric columns right-justified per
// reviewTableNumericCol, grouped by bucket so RenderASCII/RenderOrg insert a
// blank line/hline between bucket groups. Width computation and padding
// happen inside view.Table, not here.
func buildReviewTable(results []review.ReviewResult) *view.Table {
	tbl := view.NewTable(reviewTableHeader...)
	var rightCols []int
	for i, numeric := range reviewTableNumericCol {
		if numeric {
			rightCols = append(rightCols, i)
		}
	}
	tbl.SetRight(rightCols...)

	if len(results) == 0 {
		return tbl
	}
	prevBucket := results[0].Bucket
	for _, r := range results {
		if r.Bucket != prevBucket {
			tbl.AddGroup()
			prevBucket = r.Bucket
		}
		tbl.AddRow(reviewTableRow(r)...)
	}
	return tbl
}

// renderTable writes the human-readable aligned table: text columns
// left-justified, numeric columns right-justified so decimals/percents line
// up, with a blank line separating each bucket group.
func renderTable(out io.Writer, results []review.ReviewResult) error {
	if len(results) == 0 {
		fmt.Fprintln(out, "No results.")
		return nil
	}
	return buildReviewTable(results).RenderASCII(out)
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
	return buildReviewTable(results).RenderOrg(out)
}

// renderCSV writes a multi-date sweep's results as CSV with a leading DATE
// column (RFC3339, since --interval can be sub-daily), one row per
// (date, instrument) — the output shape docs/archive/asof-review-sweep-spec.md §4.3
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
