// Package review hosts the `trader review` CLI command: a multi-timeframe
// watchlist review that triages FX pairs into Watch/Hot/Tradeable buckets.
// Business logic lives in review/ and service/; this package parses flags,
// calls the service, and formats output.
package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/review"
	"github.com/rustyeddy/trader/service"
)

var (
	instrumentsCSV string
	showWatch      bool
	showHotlist    bool
	showTradeable  bool
	outputFormat   string
	token          string
	env            string
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
	cmd.Flags().StringVar(&outputFormat, "output", "table", "Output format: table|json|org")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (takes precedence over global config, OANDA_TOKEN env var, and ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live (takes precedence over global config)")
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
	case "table", "json", "org":
		return nil
	default:
		return fmt.Errorf("invalid --output %q: must be table, json, or org", format)
	}
}

func runReview(cmd *cobra.Command, rc *config.RootConfig) error {
	if err := validateOutputFormat(outputFormat); err != nil {
		return err
	}
	buckets := selectedBuckets()

	svc, err := buildService(cmd, rc)
	if err != nil {
		return err
	}

	resp, err := svc.ReviewWatchlist(context.Background(), service.ReviewRequest{
		Instruments: splitCSV(instrumentsCSV),
	})
	if err != nil {
		return err
	}

	filtered := make([]review.ReviewResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		if buckets[r.Bucket] {
			filtered = append(filtered, r)
		}
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

// sortByBucket stable-sorts results tradeable-first, then hot, then watch,
// preserving each bucket's original relative order.
func sortByBucket(results []review.ReviewResult) {
	sort.SliceStable(results, func(i, j int) bool {
		return bucketRank[results[i].Bucket] < bucketRank[results[j].Bucket]
	})
}

// reviewTableHeader/reviewTableRow keep the table and org renderers' column
// sets in sync.
var reviewTableHeader = []string{"PAIR", "BUCKET", "BIAS", "ADX", "CI", "EMA SEP", "ATR", "EMA DIST", "H4 ADX", "H4 CI", "WEEK%"}

func reviewTableRow(r review.ReviewResult) []string {
	return []string{
		r.Instrument,
		r.Bucket,
		r.Bias,
		fmt.Sprintf("%.1f", r.D1.ADX),
		fmt.Sprintf("%.1f", r.D1.CI),
		fmt.Sprintf("%+.1f", r.D1.EMASepATR),
		fmt.Sprintf("%.1fp", r.D1.ATRPips),
		fmt.Sprintf("%+.1f", r.D1.PriceEMA20ATR),
		fmt.Sprintf("%.1f", r.H4.ADX),
		fmt.Sprintf("%.1f", r.H4.CI),
		fmt.Sprintf("%.0f%%", r.W1.WeekUsedPct*100),
	}
}

// renderTable writes the human-readable aligned table, unchanged from prior
// behavior: a blank line separates each bucket group.
func renderTable(out io.Writer, results []review.ReviewResult) error {
	if len(results) == 0 {
		fmt.Fprintln(out, "No results.")
		return nil
	}

	// Render the whole table as one aligned tabwriter block first — a blank
	// line fed directly into the writer would break column alignment across
	// the gap — then splice in blank lines between bucket groups afterward.
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(reviewTableHeader, "\t"))
	underline := make([]string, len(reviewTableHeader))
	for i, h := range reviewTableHeader {
		underline[i] = strings.Repeat("-", len(h))
	}
	fmt.Fprintln(w, strings.Join(underline, "\t"))
	for _, r := range results {
		fmt.Fprintln(w, strings.Join(reviewTableRow(r), "\t"))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	fmt.Fprintln(out, lines[0]) // header
	fmt.Fprintln(out, lines[1]) // separator

	prevBucket := results[0].Bucket
	for i, line := range lines[2:] {
		if r := results[i]; r.Bucket != prevBucket {
			fmt.Fprintln(out)
			prevBucket = r.Bucket
		}
		fmt.Fprintln(out, line)
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
