//go:build fullarchive

// This file is a simple backtest comparison for strategy/smatrend's
// "probation-trend" ExitRule (issue #349, the SMA Long Hold playbook),
// requested directly rather than as part of the walk-forward sweep:
// run SPY history at TrailingStopPercent 0.10 and once at 0.20, every
// other playbook parameter fixed at its own reference value, and
// compare both against buy-and-hold — all three at genuinely matched
// ~100%-of-equity notional exposure, so the comparison is actually
// meaningful (a real user's own explicit correction after an earlier
// version of this comparison used mismatched sizing: "is this using
// the same size lots? that is did they both go in 100% or both 20%").
//
// Lives under marketdata/ as package marketdata_test for the same
// reason eqs01_walkforward_fullarchive_test.go does (it needs
// marketdata/internal/provider/stooq, only reachable from inside this
// directory tree) and reuses that file's own already-built helpers
// (setupEQS01WalkForwardInstrument, runEQS01WFBacktest,
// eqs01WFInstrument, cagrFromReturn, calmarRatio, equityCurveAt)
// directly rather than duplicating them.
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

// fullArchiveSMATrendSimpleSpanStart/End bound the comparison window
// (empty by default; an operator edits these locally, e.g. to
// "2007-01-01"/"2026-09-01"). SMAPeriod (200) worth of prior bars are
// additionally read as warmup before SpanStart, matching the
// walk-forward driver's own train-window convention.
const fullArchiveSMATrendSimpleSpanStart = ""
const fullArchiveSMATrendSimpleSpanEnd = ""

// fullArchiveSMATrendSimpleReanchorYears is how often the strategy's
// own AdverseDistance is recomputed from the then-current price,
// rather than frozen once for the entire comparison window.
// risk.FixedFractionSizer's AdverseDistance is an absolute price
// distance, not a percentage — RiskFraction=1/AdversePercent=1
// approximates ~100% notional deployment only as long as
// AdverseDistance still roughly tracks the current price (see
// eqs01WFSizingMode's own doc comment for the identical technique).
// Over a single, multi-year span with real price appreciation, an
// anchor frozen at the start drifts far enough from later prices to
// size positions wildly larger than 100% of equity by the end — this
// is exactly what running this comparison over SPY's full 2007-2026
// history without re-anchoring did before this fix, sizing a Buy so
// large it later overflowed the account into a short position when
// sold (issue #352, since fixed independently at the broker level,
// but re-anchoring is still needed here regardless, for the sizing to
// actually mean "~100% of equity" throughout rather than only at the
// very first entry).
const fullArchiveSMATrendSimpleReanchorYears = 1

// smatrendSimpleBacktestResult is one full-comparison-window backtest
// (or buy-and-hold baseline)'s own summary, all sized at matched
// ~100%-of-equity notional exposure.
type smatrendSimpleBacktestResult struct {
	Instrument                  string  `json:"instrument"`
	Strategy                    string  `json:"strategy"` // "probation-trend" trailing-stop percent, or "buy-and-hold"
	SpanStart                   string  `json:"span_start"`
	SpanEnd                     string  `json:"span_end"`
	Years                       float64 `json:"years"`
	NetReturn                   float64 `json:"net_return"`
	CAGR                        float64 `json:"cagr"`
	MaxDrawdown                 float64 `json:"max_drawdown"`
	MaxDrawdownIsPerPeriodFloor bool    `json:"max_drawdown_is_per_period_floor"` // see doc comment below
	Calmar                      float64 `json:"calmar"`
	Entries                     int     `json:"entries"`
	Exits                       int     `json:"exits"`
	OpenAtEnd                   bool    `json:"open_at_end"`
}

// TestSMATrendProbationTrendSimpleBacktest runs strategy/smatrend's
// "probation-trend" ExitRule over
// [fullArchiveSMATrendSimpleSpanStart, fullArchiveSMATrendSimpleSpanEnd)
// at TrailingStopPercent 0.10 and once at 0.20 — SMAPeriod (200),
// InitialStopBelowSMA (0.01), TrailActivationGain (0.05), and
// ReEntryRuleName ("reclaim-exit-price", the playbook's own
// "PreviousExit" mode) held fixed at the playbook's own reference
// values throughout — and compares both against buy-and-hold over the
// identical window, all three at matched ~100%-of-equity notional
// sizing (RiskFraction=1, AdverseDistance=100% of price, re-anchored
// every fullArchiveSMATrendSimpleReanchorYears — see that constant's
// own doc comment for why re-anchoring is necessary at all).
//
// The comparison window is split into
// fullArchiveSMATrendSimpleReanchorYears-year periods, each run
// independently (with its own SMAPeriod-year warmup prefix) via
// runEQS01WFBacktest, mirroring the walk-forward driver's own
// fold-chaining technique (see runEQS01WalkForwardFolds): each
// period's own return ratio (its ending equity-curve point divided by
// its starting one) is extracted and chained multiplicatively across
// every period — mathematically exact for the chained return/CAGR,
// without needing true continuous dollar capital carried between
// independent per-period broker/account instances.
//
// MaxDrawdown for the strategy is NOT a true continuous peak-to-trough
// across the whole chained window (MaxDrawdownIsPerPeriodFloor is
// true): it is the worst of each period's own independently-reported
// max drawdown, which can only ever understate a real drawdown that
// spans a period boundary — the same limitation
// eqs01WFFoldResult.TestMaxDrawdown already has and the walk-forward
// driver's own summary never aggregates into one continuous figure
// either. Buy-and-hold's own MaxDrawdown has no such limitation: it is
// computed directly, in one pass, from the real daily Close series
// over the whole comparison window.
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

	// Period boundaries: [spanStart, spanStart+1yr), [spanStart+1yr,
	// spanStart+2yr), ... clipped to spanEnd.
	type period struct {
		start, end time.Time
	}
	var periods []period
	for cur := spanStart; cur.Before(spanEnd); cur = cur.AddDate(fullArchiveSMATrendSimpleReanchorYears, 0, 0) {
		end := cur.AddDate(fullArchiveSMATrendSimpleReanchorYears, 0, 0)
		if end.After(spanEnd) {
			end = spanEnd
		}
		periods = append(periods, period{start: cur, end: end})
	}

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	riskFraction := num.MustParseRate("1") // full notional: see fullArchiveSMATrendSimpleReanchorYears's own doc comment

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

	// Buy-and-hold: real price return and a true, single-pass
	// peak-to-trough max drawdown over the Close series — no chaining
	// approximation needed at all, since it is not a risk-sized
	// product.
	bhReturn := reportLast.Close.Float64()/reportFirst.Open.Float64() - 1
	bhCAGR := cagrFromReturn(bhReturn, years)
	bhMaxDD := 0.0
	peak := 0.0
	for _, b := range setup.bars {
		if b.Time.Before(spanStart) || !b.Time.Before(spanEnd) {
			continue
		}
		c := b.Close.Float64()
		if c > peak {
			peak = c
		}
		if peak > 0 {
			dd := (peak - c) / peak
			if dd > bhMaxDD {
				bhMaxDD = dd
			}
		}
	}

	var results []smatrendSimpleBacktestResult
	results = append(results, smatrendSimpleBacktestResult{
		Instrument:  inst.Symbol,
		Strategy:    "buy-and-hold",
		SpanStart:   fullArchiveSMATrendSimpleSpanStart,
		SpanEnd:     fullArchiveSMATrendSimpleSpanEnd,
		Years:       years,
		NetReturn:   bhReturn,
		CAGR:        bhCAGR,
		MaxDrawdown: bhMaxDD,
		Calmar:      calmarRatio(bhCAGR, bhMaxDD),
	})
	t.Logf("SPY buy-and-hold: return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f", bhReturn, bhCAGR, bhMaxDD, calmarRatio(bhCAGR, bhMaxDD))

	for _, tsp := range []string{"0.10", "0.20"} {
		cfg := smatrend.Config{
			SMAPeriod:           200,
			ExitRuleName:        "probation-trend",
			ReEntryRuleName:     "reclaim-exit-price",
			InitialStopBelowSMA: num.MustParseRate("0.01"),
			TrailActivationGain: num.MustParseRate("0.05"),
			TrailingStopPercent: num.MustParseRate(tsp),
		}

		chainedReturn := 1.0
		worstPeriodMaxDD := 0.0
		totalEntries, totalExits := 0, 0
		openAtEnd := false

		for i, p := range periods {
			warmupStart := p.start.AddDate(-1, 0, 0)
			anchorBar, ok := firstBarAtOrAfter(setup.bars, warmupStart)
			if !ok {
				t.Fatalf("tsp=%s period %d: no bar found at or after warmup start %s", tsp, i, warmupStart.Format("2006-01-02"))
			}
			adverseDistance, err := anchorBar.Open.MulRate(num.MustParseRate("1")) // 100% of price: full notional
			if err != nil {
				t.Fatalf("tsp=%s period %d: computing adverse distance: %v", tsp, i, err)
			}
			runSpan, err := marketdata.NewTimeRange(warmupStart, p.end)
			if err != nil {
				t.Fatalf("tsp=%s period %d: run span: %v", tsp, i, err)
			}

			resp, err := runEQS01WFBacktest(ctx, setup.mgr, setup.simResolver, setup.simID, runSpan, cfg, startingCapital, riskFraction, adverseDistance, setup.priceByTime)
			if err != nil {
				t.Fatalf("tsp=%s period %d [%s, %s): backtest run: %v", tsp, i, p.start.Format("2006-01-02"), p.end.Format("2006-01-02"), err)
			}

			// p.start/p.end are calendar boundaries (AddDate) and are
			// not guaranteed to land on an actual trading day, unlike
			// the walk-forward driver's own bar-index-derived fold
			// boundaries — resolved to the nearest real bar time
			// before looking up the equity curve, which only ever has
			// points at real bar timestamps.
			periodStartBar, ok := firstBarAtOrAfter(setup.bars, p.start)
			if !ok {
				t.Fatalf("tsp=%s period %d: no bar found at or after period start %s", tsp, i, p.start.Format("2006-01-02"))
			}
			periodEndBar := lastBarBefore(setup.bars, p.end)
			baselineEquity, ok1 := equityCurveAt(resp.EquityCurve, periodStartBar.Time)
			finalEquity, ok2 := equityCurveAt(resp.EquityCurve, periodEndBar.Time)
			if !ok1 || !ok2 {
				t.Fatalf("tsp=%s period %d: could not locate equity-curve points at period boundaries", tsp, i)
			}
			periodReturn := finalEquity/baselineEquity - 1
			chainedReturn *= 1 + periodReturn

			periodMaxDD := mustParseFloatWF(t, resp.Metrics.MaxDrawdown().String())
			if periodMaxDD > worstPeriodMaxDD {
				worstPeriodMaxDD = periodMaxDD
			}

			totalEntries += len(resp.Trades) + len(resp.OpenTrades)
			totalExits += len(resp.Trades)
			openAtEnd = len(resp.OpenTrades) > 0 // only the last period's own value matters
		}

		netReturn := chainedReturn - 1
		cagr := cagrFromReturn(netReturn, years)

		result := smatrendSimpleBacktestResult{
			Instrument:                  inst.Symbol,
			Strategy:                    "probation-trend-" + tsp,
			SpanStart:                   fullArchiveSMATrendSimpleSpanStart,
			SpanEnd:                     fullArchiveSMATrendSimpleSpanEnd,
			Years:                       years,
			NetReturn:                   netReturn,
			CAGR:                        cagr,
			MaxDrawdown:                 worstPeriodMaxDD,
			MaxDrawdownIsPerPeriodFloor: true,
			Calmar:                      calmarRatio(cagr, worstPeriodMaxDD),
			Entries:                     totalEntries,
			Exits:                       totalExits,
			OpenAtEnd:                   openAtEnd,
		}
		results = append(results, result)

		t.Logf("SPY probation-trend tsp=%s (full notional, %d periods): return=%.4f cagr=%.4f maxDD>=%.4f calmar=%.4f trades=%d openAtEnd=%v",
			tsp, len(periods), netReturn, cagr, worstPeriodMaxDD, result.Calmar, totalEntries, openAtEnd)
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal results: %v", err)
	}
	outPath := filepath.Join(fullArchiveSMATrendSimpleOutputDir, "spy-probation-trend-10-vs-20-vs-buyhold.json")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", outPath, err)
	}
	t.Logf("wrote %s", outPath)
	fmt.Println(string(out))
}

// lastBarBefore returns the last bar in bars strictly before cutoff —
// used to locate the equity-curve point at the end of a period whose
// own end boundary (cutoff) is an exclusive, possibly-non-trading
// date.
func lastBarBefore(bars []marketdata.Bar, cutoff time.Time) marketdata.Bar {
	var last marketdata.Bar
	for _, b := range bars {
		if !b.Time.Before(cutoff) {
			break
		}
		last = b
	}
	return last
}

// firstBarAtOrAfter returns the first bar in bars at or after from,
// and false (a zero Bar) if no such bar exists — used to resolve a
// calendar period boundary (which may fall on a non-trading day) to
// the real bar time the equity curve actually has a point at. Never
// silently falls back to some other bar (for example the dataset's
// own last one) when from is out of range: a caller anchoring
// sizing or an equity-curve lookup on the wrong bar would fail
// silently instead of loudly (PR #353 review).
func firstBarAtOrAfter(bars []marketdata.Bar, from time.Time) (marketdata.Bar, bool) {
	for _, b := range bars {
		if !b.Time.Before(from) {
			return b, true
		}
	}
	return marketdata.Bar{}, false
}
