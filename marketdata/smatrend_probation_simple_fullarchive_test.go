//go:build fullarchive

// This file is a simple, single-run (not walk-forward) backtest
// comparison for strategy/smatrend's "probation-trend" ExitRule
// (issue #349, the SMA Long Hold playbook), requested directly rather
// than as part of the walk-forward sweep: run the full available SPY
// history once at TrailingStopPercent 0.10 and once at 0.20, holding
// every other playbook parameter fixed at its own reference value.
//
// Lives under marketdata/ as package marketdata_test for the same
// reason eqs01_walkforward_fullarchive_test.go does (it needs
// marketdata/internal/provider/stooq, only reachable from inside this
// directory tree) and reuses that file's own already-built helpers
// (setupEQS01WalkForwardInstrument, runEQS01WFBacktest,
// eqs01WFInstrument, cagrFromReturn, calmarRatio) directly rather than
// duplicating them.
//
// Results go to a local, private directory
// (fullArchiveSMATrendSimpleOutputDir, edited locally, never
// committed) — never anywhere under this repository, per the same
// decision recorded in eqs01_walkforward_fullarchive_test.go's own
// doc comment.
package marketdata_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveSMATrendSimpleOutputDir names the local directory this
// test writes its results into — empty by default; an operator edits
// this locally (for example to
// "/srv/trading/sweeps/smatrend-probation-simple").
const fullArchiveSMATrendSimpleOutputDir = ""

// smatrendSimpleBacktestResult is one full-history, single-run
// backtest's own summary.
type smatrendSimpleBacktestResult struct {
	Instrument          string  `json:"instrument"`
	TrailingStopPercent string  `json:"trailing_stop_percent"`
	SpanStart           string  `json:"span_start"`
	SpanEnd             string  `json:"span_end"`
	Years               float64 `json:"years"`
	NetReturn           float64 `json:"net_return"`
	CAGR                float64 `json:"cagr"`
	MaxDrawdown         float64 `json:"max_drawdown"`
	Calmar              float64 `json:"calmar"`
	Entries             int     `json:"entries"`
	Exits               int     `json:"exits"`
	OpenAtEnd           bool    `json:"open_at_end"`
	BuyAndHoldReturn    float64 `json:"buy_and_hold_return"`
	BuyAndHoldCAGR      float64 `json:"buy_and_hold_cagr"`
}

// fullArchiveSMATrendSimpleSpanStart/End bound the window this test
// actually runs (empty by default; an operator edits these locally
// too, e.g. to "2015-01-01"/"2025-01-01"). A shorter, more recent
// window is used deliberately, not the full available history:
// issue #352 (found running this exact test against SPY's full
// 2005-2026 history) documents a real adapters/broker/sim gap where a
// resting protective stop left un-canceled by a same-position direct
// exit can double-fill and flip the account short if both trigger
// around the same bar — reproduced there on 2006-06-08, early in the
// dataset. Restricting to a window that avoids that particular
// collision lets this comparison run today without waiting on #352's
// own fix; it does not fix the underlying gap, and a different window
// (or QQQ, or a different SMA period) could in principle hit it again.
const fullArchiveSMATrendSimpleSpanStart = ""
const fullArchiveSMATrendSimpleSpanEnd = ""

// TestSMATrendProbationTrendSimpleBacktest runs strategy/smatrend's
// "probation-trend" ExitRule once over
// [fullArchiveSMATrendSimpleSpanStart, fullArchiveSMATrendSimpleSpanEnd)
// at TrailingStopPercent 0.10 and once at 0.20 — SMAPeriod (200),
// InitialStopBelowSMA (0.01), TrailActivationGain (0.05), and
// ReEntryRuleName ("reclaim-exit-price", the playbook's own
// "PreviousExit" mode) held fixed at the playbook's own reference
// values throughout. Sizing is EQS-01's own original risk-managed
// reference configuration (RiskFraction 1%, AdverseDistance 5% of the
// span's own starting price) — see fullArchiveSMATrendSimpleSpanStart's
// own doc comment for why this test runs a bounded, recent window
// rather than the full available history.
func TestSMATrendProbationTrendSimpleBacktest(t *testing.T) {
	if fullArchiveSMATrendSimpleOutputDir == "" {
		t.Skip("fullArchiveSMATrendSimpleOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	if fullArchiveSMATrendSimpleSpanStart == "" || fullArchiveSMATrendSimpleSpanEnd == "" {
		t.Skip("fullArchiveSMATrendSimpleSpanStart/End are empty; edit the constants in this file to point at a local date range to run this test")
	}
	if err := os.MkdirAll(fullArchiveSMATrendSimpleOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveSMATrendSimpleOutputDir, err)
	}

	ctx := context.Background()
	inst := eqs01WFInstrument{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"}
	setup := setupEQS01WalkForwardInstrument(t, ctx, inst)

	spanStart, err := time.Parse("2006-01-02", fullArchiveSMATrendSimpleSpanStart)
	if err != nil {
		t.Fatalf("parse span start: %v", err)
	}
	spanEnd, err := time.Parse("2006-01-02", fullArchiveSMATrendSimpleSpanEnd)
	if err != nil {
		t.Fatalf("parse span end: %v", err)
	}
	// SMAPeriod (200) worth of bars must exist before spanStart, or
	// the SMA is never ready inside the run window at all — warm up
	// from a year earlier, matching the walk-forward driver's own
	// train-window convention, then only evaluate/report from
	// spanStart onward.
	warmupStart := spanStart.AddDate(-1, 0, 0)

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	riskFraction := num.MustParseRate("0.01")

	var anchorBar marketdata.Bar
	for _, b := range setup.bars {
		if !b.Time.Before(warmupStart) {
			anchorBar = b
			break
		}
	}
	adverseDistance, err := anchorBar.Open.MulRate(num.MustParseRate("0.05"))
	if err != nil {
		t.Fatalf("computing adverse distance: %v", err)
	}

	runSpan, err := marketdata.NewTimeRange(warmupStart, spanEnd)
	if err != nil {
		t.Fatalf("run span: %v", err)
	}

	var reportFirst, reportLast marketdata.Bar
	for _, b := range setup.bars {
		if b.Time.Before(spanStart) || !b.Time.Before(spanEnd) {
			continue
		}
		if reportFirst.Time.IsZero() {
			reportFirst = b
		}
		reportLast = b
	}
	if reportFirst.Time.IsZero() {
		t.Fatalf("no bars found in report window [%s, %s)", fullArchiveSMATrendSimpleSpanStart, fullArchiveSMATrendSimpleSpanEnd)
	}
	years := reportLast.Time.Sub(reportFirst.Time).Hours() / 24 / 365.25

	bhReturn := reportLast.Close.Float64()/reportFirst.Open.Float64() - 1
	bhCAGR := cagrFromReturn(bhReturn, years)

	var results []smatrendSimpleBacktestResult
	for _, tsp := range []string{"0.10", "0.20"} {
		cfg := smatrend.Config{
			SMAPeriod:           200,
			ExitRuleName:        "probation-trend",
			ReEntryRuleName:     "reclaim-exit-price",
			InitialStopBelowSMA: num.MustParseRate("0.01"),
			TrailActivationGain: num.MustParseRate("0.05"),
			TrailingStopPercent: num.MustParseRate(tsp),
		}
		resp, err := runEQS01WFBacktest(ctx, setup.mgr, setup.simResolver, setup.simID, runSpan, cfg, startingCapital, riskFraction, adverseDistance, setup.priceByTime)
		if err != nil {
			t.Fatalf("tsp=%s: backtest run: %v (if this is \"unexpected short position\", see issue #352 — pick a different span)", tsp, err)
		}

		netReturn := mustParseFloatWF(t, resp.Metrics.NetReturn().String())
		maxDD := mustParseFloatWF(t, resp.Metrics.MaxDrawdown().String())
		cagr := cagrFromReturn(netReturn, years)

		result := smatrendSimpleBacktestResult{
			Instrument:          inst.Symbol,
			TrailingStopPercent: tsp,
			SpanStart:           fullArchiveSMATrendSimpleSpanStart,
			SpanEnd:             fullArchiveSMATrendSimpleSpanEnd,
			Years:               years,
			NetReturn:           netReturn,
			CAGR:                cagr,
			MaxDrawdown:         maxDD,
			Calmar:              calmarRatio(cagr, maxDD),
			Entries:             len(resp.Trades),
			Exits:               len(resp.Trades),
			OpenAtEnd:           len(resp.OpenTrades) > 0,
			BuyAndHoldReturn:    bhReturn,
			BuyAndHoldCAGR:      bhCAGR,
		}
		if result.OpenAtEnd {
			result.Entries = len(resp.Trades) + len(resp.OpenTrades)
		}
		results = append(results, result)

		t.Logf("SPY probation-trend tsp=%s: return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f trades=%d openAtEnd=%v",
			tsp, netReturn, cagr, maxDD, result.Calmar, result.Entries, result.OpenAtEnd)
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal results: %v", err)
	}
	outPath := filepath.Join(fullArchiveSMATrendSimpleOutputDir, "spy-probation-trend-10-vs-20.json")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", outPath, err)
	}
	t.Logf("wrote %s", outPath)
	fmt.Println(string(out))
}
