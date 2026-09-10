//go:build fullarchive

// This file closes out issue #355's own two chart types that
// #354/PR #359 did not yet exercise against real data: the full-run
// overview chart (issue #355's "Required chart types" #1) and the
// equity-curve-vs-buy-and-hold chart (#3). Episode charts (#2) were
// already proven against real SPY data by
// smatrend_probation_episode_fullarchive_test.go; this file
// deliberately does not duplicate that work.
//
// Same continuous "probation-trend" backtest config
// smatrend_probation_episode_fullarchive_test.go and
// smatrend_probation_walkforward_fullarchive_test.go already
// established as this playbook's own reference run (SMAPeriod 200,
// InitialStopBelowSMA 0.01, TrailActivationGain 0.05,
// TrailingStopPercent 0.10, ReEntryRuleName "reclaim-exit-price"), run
// once, continuously, over SPY's full Stooq history — reusing this
// package's already-built helpers
// (setupEQS01WalkForwardInstrument, runWFBacktestWithJournal,
// weightedFillPrice, smaSeries) directly rather than duplicating them.
//
// Results go to a local, private directory
// (fullArchiveSMATrendOverviewOutputDir, edited locally, never
// committed), matching every other fullarchive driver's own
// convention. Writes into a charts/ subdirectory specifically
// (issue #355's own "Artifact layout" section: "charts should live
// beside the data that produced them") — a full results/<run-id>/
// convention with summary.csv/trades.csv/equity.csv is not
// implemented here, since the issue's own Acceptance Criteria do not
// require the exact layout, only that "generated files can be stored
// alongside a research run without modifying strategy behavior,"
// which this satisfies.
package marketdata_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/rustyeddy/trader/chart"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveSMATrendOverviewOutputDir names the local directory this
// test writes its charts/ subdirectory into — empty by default; an
// operator edits this locally (for example to
// "/srv/trading/sweeps/smatrend-episode-analysis", the same directory
// the episode driver already uses, so both sets of research artifacts
// for this reference run live together).
const fullArchiveSMATrendOverviewOutputDir = ""

// TestSMATrendProbationTrendOverviewAndEquityCharts renders issue
// #355's full-run overview chart (Close, SMA200, every entry/exit/
// re-entry marker across the whole run) and equity-curve-vs-
// buy-and-hold chart, in both PNG and SVG, from one real, continuous
// backtest of strategy/smatrend's "probation-trend" ExitRule against
// SPY's full Stooq history.
func TestSMATrendProbationTrendOverviewAndEquityCharts(t *testing.T) {
	if fullArchiveSMATrendOverviewOutputDir == "" {
		t.Skip("fullArchiveSMATrendOverviewOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	chartsDir := filepath.Join(fullArchiveSMATrendOverviewOutputDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", chartsDir, err)
	}

	ctx := context.Background()
	inst := eqs01WFInstrument{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"}
	setup := setupEQS01WalkForwardInstrument(t, ctx, inst)
	bars := setup.bars

	cfg := smatrend.Config{
		SMAPeriod:           200,
		ExitRuleName:        "probation-trend",
		ReEntryRuleName:     "reclaim-exit-price",
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0.05"),
		TrailingStopPercent: num.MustParseRate("0.10"),
	}
	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	riskFraction := num.MustParseRate("0.01")
	adverseDistance := num.MustParsePrice("20.00000")

	span, err := marketdata.NewTimeRange(bars[0].Time, bars[len(bars)-1].Time.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("run span: %v", err)
	}

	resp, rec, err := runWFBacktestWithJournal(ctx, setup.mgr, setup.simResolver, setup.simID, span, cfg, startingCapital, riskFraction, adverseDistance, setup.priceByTime)
	if err != nil {
		t.Fatalf("backtest run: %v", err)
	}
	fills := rec.fillsByID()
	t.Logf("run produced %d closed trades, %d open at end, %d equity points", len(resp.Trades), len(resp.OpenTrades), len(resp.EquityCurve))

	sma200 := smaSeries(bars, 200)

	// --- Overview chart: Close + SMA200 + every entry/exit/re-entry
	// marker across the whole run (issue #355's chart type #1).
	var smaOverlay []chart.LevelPoint
	for i, v := range sma200 {
		if !math.IsNaN(v) {
			smaOverlay = append(smaOverlay, chart.LevelPoint{Time: bars[i].Time, Price: num.MustParsePrice(fmt.Sprintf("%.4f", v))})
		}
	}

	all := make([]order.Trade, 0, len(resp.Trades)+len(resp.OpenTrades))
	all = append(all, resp.Trades...)
	all = append(all, resp.OpenTrades...)
	sort.Slice(all, func(i, j int) bool { return all[i].OpenedAt.Before(all[j].OpenedAt) })

	var markers []chart.Marker
	for i, tr := range all {
		entryPrice, _, err := weightedFillPrice(t, tr.EntryFillIDs, fills)
		if err != nil {
			t.Fatalf("trade %d: reconstructing entry price: %v", i, err)
		}
		kind := chart.MarkerEntry
		if i > 0 {
			kind = chart.MarkerReentry
		}
		markers = append(markers, chart.Marker{Time: tr.OpenedAt, Price: entryPrice, Kind: kind})

		if !tr.ClosedAt.IsZero() {
			exitPrice, _, err := weightedFillPrice(t, tr.ExitFillIDs, fills)
			if err != nil {
				t.Fatalf("trade %d: reconstructing exit price: %v", i, err)
			}
			markers = append(markers, chart.Marker{Time: tr.ClosedAt, Price: exitPrice, Kind: chart.MarkerExit})
		}
	}
	t.Logf("overview chart: %d bars, %d SMA points, %d markers", len(bars), len(smaOverlay), len(markers))

	overviewIn := chart.OverviewInput{
		Title:   "SPY probation-trend (10% trailing, reclaim-exit-price) full run",
		Bars:    bars,
		SMA:     smaOverlay,
		Markers: markers,
	}
	renderBoth(t, chartsDir, "overview", func(w *os.File, format chart.Format) error {
		return chart.RenderOverview(w, overviewIn, format)
	})

	// --- Equity chart: strategy equity curve vs. buy-and-hold over
	// the identical span, both at matched ~100%-of-equity notional
	// sizing (issue #355's chart type #3). This deliberately does NOT
	// reuse resp.EquityCurve from the run above: that run is sized
	// conservatively (RiskFraction 0.01, fixed $20 adverse distance —
	// fine for the overview chart, which only ever needs entry/exit
	// *dates and prices*, unaffected by position size) and plotting it
	// against an unsized buy-and-hold curve would repeat exactly the
	// mismatched-sizing mistake already corrected earlier in this
	// research thread (PR #353's own "are you sizing buy and hold at
	// 100%?" correction) — the strategy would look like it captured
	// almost none of buy-and-hold's return, purely as a sizing
	// artifact having nothing to do with the strategy's real
	// risk-adjusted performance.
	//
	// A second, separately sized run builds the real equity curve:
	// the comparison window is split into 1-year periods (mirroring
	// smatrend_probation_simple_fullarchive_test.go's own chaining
	// technique), each run independently at full notional
	// (RiskFraction=1, AdverseDistance=100% of that period's own
	// opening price — re-anchored every period for the same reason
	// recorded there), and each period's own equity-curve points are
	// rescaled to continue from the running equity the *previous*
	// period ended at, producing one genuine continuous, honestly-
	// comparable equity curve rather than only a final chained return
	// ratio.
	riskFractionFull := num.MustParseRate("1")
	startingCapitalF := mustParseFloatWF(t, fieldsFirst(startingCapital.String()))

	type period struct{ start, end time.Time }
	var periods []period
	spanEnd := bars[len(bars)-1].Time.AddDate(0, 0, 1)
	for cur := bars[0].Time; cur.Before(spanEnd); cur = cur.AddDate(1, 0, 0) {
		end := cur.AddDate(1, 0, 0)
		if end.After(spanEnd) {
			end = spanEnd
		}
		periods = append(periods, period{start: cur, end: end})
	}

	runningEquity := startingCapitalF
	var equityPts []chart.EquityPoint
	for i, p := range periods {
		// Clamped to the dataset's own start (PR #359-style pattern,
		// but a real edge case here unlike that file's own driver:
		// this file's own periods begin at bars[0].Time itself, so
		// the very first period's naive 1-year warmup prefix would
		// underflow before any data exists at all).
		warmupStart := p.start.AddDate(-1, 0, 0)
		if warmupStart.Before(bars[0].Time) {
			warmupStart = bars[0].Time
		}
		anchorBar, ok := firstBarAtOrAfter(bars, warmupStart)
		if !ok {
			t.Fatalf("period %d: no bar found at or after warmup start %s", i, warmupStart.Format("2006-01-02"))
		}
		periodAdverseDistance, err := anchorBar.Open.MulRate(riskFractionFull) // 100% of price: full notional
		if err != nil {
			t.Fatalf("period %d: computing adverse distance: %v", i, err)
		}
		runSpan, err := marketdata.NewTimeRange(warmupStart, p.end)
		if err != nil {
			t.Fatalf("period %d: run span: %v", i, err)
		}
		periodResp, err := runEQS01WFBacktest(ctx, setup.mgr, setup.simResolver, setup.simID, runSpan, cfg, startingCapital, riskFractionFull, periodAdverseDistance, setup.priceByTime)
		if err != nil {
			t.Fatalf("period %d [%s, %s): backtest run: %v", i, p.start.Format("2006-01-02"), p.end.Format("2006-01-02"), err)
		}

		periodStartBar, ok := firstBarAtOrAfter(bars, p.start)
		if !ok {
			t.Fatalf("period %d: no bar found at or after period start %s", i, p.start.Format("2006-01-02"))
		}
		baselineEquity, ok := equityCurveAt(periodResp.EquityCurve, periodStartBar.Time)
		if !ok {
			t.Fatalf("period %d: could not locate equity-curve point at period start %s", i, periodStartBar.Time.Format("2006-01-02"))
		}
		scale := runningEquity / baselineEquity

		for _, ep := range periodResp.EquityCurve {
			if ep.Timestamp.Before(p.start) || !ep.Timestamp.Before(p.end) {
				continue // warmup-prefix or next-period's own boundary point; each period contributes only its own [start, end) span
			}
			v := mustParseFloatWF(t, fieldsFirst(ep.Equity.String()))
			equityPts = append(equityPts, chart.EquityPoint{Time: ep.Timestamp, Value: v * scale})
		}

		finalBar := lastBarBefore(bars, p.end)
		finalEquity, ok := equityCurveAt(periodResp.EquityCurve, finalBar.Time)
		if !ok {
			t.Fatalf("period %d: could not locate equity-curve point at period end %s", i, finalBar.Time.Format("2006-01-02"))
		}
		runningEquity = finalEquity * scale
	}

	baseClose := bars[0].Close.Float64()
	var buyHoldPts []chart.EquityPoint
	for _, b := range bars {
		buyHoldPts = append(buyHoldPts, chart.EquityPoint{Time: b.Time, Value: startingCapitalF * (b.Close.Float64() / baseClose)})
	}
	t.Logf("equity chart: %d strategy points (matched 100%% notional, %d periods), %d buy-and-hold points, final strategy equity %.2f", len(equityPts), len(periods), len(buyHoldPts), runningEquity)

	equityIn := chart.EquityInput{
		Title:      "SPY probation-trend (10% trailing) vs buy-and-hold, matched 100% notional sizing",
		Equity:     equityPts,
		BuyAndHold: buyHoldPts,
	}
	renderBoth(t, chartsDir, "equity", func(w *os.File, format chart.Format) error {
		return chart.RenderEquity(w, equityIn, format)
	})
}

// renderBoth writes name.png and name.svg into dir via render,
// failing the test on any error — issue #355's own "PNG output
// works" / "SVG output works" acceptance criteria, exercised for
// both chart types this file renders.
func renderBoth(t *testing.T, dir, name string, render func(*os.File, chart.Format) error) {
	t.Helper()
	for _, format := range []chart.Format{chart.FormatPNG, chart.FormatSVG} {
		path := filepath.Join(dir, name+"."+string(format))
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
		if err := render(f, format); err != nil {
			t.Fatalf("render %s: %v", path, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
		t.Logf("wrote %s", path)
	}
}
