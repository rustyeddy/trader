//go:build fullarchive

// This file runs rustyeddy/trading#2's own out-of-sample validation
// protocol for the SMA Long Hold re-entry candidates Trader issue
// #365/PR #372 found PROMISING against the same full history used to
// formulate them: above-sma-slope-20, above-sma-slope-50, retrace-25,
// retrace-50, compared against the reference reclaim-exit-price
// baseline and a buy-and-hold benchmark.
//
// # OOS split (frozen before any result was inspected)
//
// Genuinely unseen SPY data beyond this checkout's own Stooq export
// (through 2026-09-04) amounts to only a handful of days — nowhere
// near enough for a useful long-horizon holdout. Per trading#2's own
// explicit fallback ("if the currently available unseen period is too
// short ... document that limitation explicitly and use a frozen
// historical holdout chosen without tuning against its results"),
// this file instead splits the existing history at a single fixed
// calendar boundary, chosen and recorded here before any variant was
// run for this issue:
//
//	development: [2005-02-25, 2020-01-01)
//	OOS holdout: [2020-01-01, last available bar + 1 day)
//
// This holdout includes a full bear/recovery cycle never analyzed as
// a named episode in #365/#372 (the 2020 COVID crash) plus the
// already-examined 2022 episode. The 2022 overlap with #365/#372's
// own qualitative review is a real, disclosed limitation — the four
// candidates and their fixed thresholds were specified in issue #365
// before any backtest of them was run at all, so there is no
// parameter-tuning leakage, but the qualitative "this looks promising"
// narrative was partly formed by looking at exactly this period. No
// candidate definition, threshold, or lookback was changed after
// inspecting the OOS numbers this file produces.
//
// # Sizing
//
// Reference-price-exact full-notional (risk.NewFullNotionalSizer(),
// ADR-061) — identical to trader#372's own primary comparison; see
// that file's own doc comment for why "reference-price-exact," not
// fill-price-exact.
//
// Reuses runSlopeRetraceFullNotionalBacktest and
// slopeRetraceClassifyEpisode directly from
// smatrend_slope_retrace_reentry_fullarchive_test.go (same package,
// same build tag): one continuous full-history backtest per variant
// — identical to #372's own primary run — with this file's own
// contribution being the OOS-window extraction, buy-and-hold
// benchmark, and artifact generation, not a second execution path.
//
// Results go to a local path (fullArchiveOOSReentryOutputDir, edited
// locally, never committed here) — per rustyeddy/trading#2, this
// points at the *trading* repository's own results tree
// (backtests/equities/sma-long-hold/oos/reentry-validation/), not
// this repository. trader owns the driver code below; trading owns
// the committed results/report it produces.
package marketdata_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/rustyeddy/trader/chart"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveOOSReentryOutputDir names the local directory this test
// writes its results into — empty by default; an operator edits this
// locally to point at the trading repository's own results tree, for
// example "/srv/trading/backtests/equities/sma-long-hold/oos/reentry-validation".
const fullArchiveOOSReentryOutputDir = ""

// oosReentrySplit is the frozen development/holdout boundary — see
// this file's own doc comment for why this exact date was chosen and
// the disclosed limitation of including the already-examined 2022
// episode.
var oosReentrySplit = time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)

// oosChartedExitDates restricts rendered charts to the OOS window's
// two genuinely important, shared crisis episodes (the exit-rule is
// identical across all five variants, so the exit date itself — not
// the re-entry date — is what identifies "the same real episode" seen
// by every variant): the 2020-02-27 COVID crash exit and the
// 2022-01-24 2022-bear-market exit, mirroring the
// reentry/breakout-2-3 exemplar's own "episode-2008/episode-2022"
// convention of one chart set per named crisis rather than one chart
// per every individual re-entry across every variant (which produced
// over 150 largely redundant PNGs on a first, uncurated pass).
var oosChartedExitDates = map[string]bool{
	"2020-02-27": true,
	"2022-01-24": true,
}

// oosVariantResult is one variant's own OOS-only summary — the
// required metric set from trading#2, computed by restricting the
// identical full-history backtest #372 already ran to the OOS window.
type oosVariantResult struct {
	Variant           string  `json:"variant"`
	OOSReturn         float64 `json:"oos_return"`
	CAGR              float64 `json:"cagr"`
	MaxDrawdown       float64 `json:"max_drawdown"`
	Calmar            float64 `json:"calmar"`
	TradeCount        int     `json:"trade_count"`
	ReEntryCount      int     `json:"reentry_count"`
	OpenAtEnd         bool    `json:"open_at_end"`
	ExposurePct       float64 `json:"exposure_pct"`
	WhipsawCount      int     `json:"whipsaw_count"`
	GoodDefensiveExit int     `json:"good_defensive_exit_count"`
	ReturnVsBuyHold   float64 `json:"return_vs_buy_and_hold"`  // OOSReturn - buy-and-hold OOS return
	MaxDDVsBuyHold    float64 `json:"max_dd_vs_buy_and_hold"`  // MaxDrawdown - buy-and-hold OOS max drawdown
	IsBuyAndHold      bool    `json:"is_buy_and_hold,omitempty"`
	Classification    string  `json:"classification,omitempty"` // VALIDATED / MIXED / REJECTED, baseline/buy-and-hold left blank
}

func TestSMATrendReEntryOOSValidation(t *testing.T) {
	if fullArchiveOOSReentryOutputDir == "" {
		t.Skip("fullArchiveOOSReentryOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	if err := os.MkdirAll(fullArchiveOOSReentryOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveOOSReentryOutputDir, err)
	}
	chartsDir := filepath.Join(fullArchiveOOSReentryOutputDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", chartsDir, err)
	}

	ctx := context.Background()
	inst := eqs01WFInstrument{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"}
	setup := setupEQS01WalkForwardInstrument(t, ctx, inst)
	bars := setup.bars

	oosStart := oosReentrySplit
	spanStart, spanEnd := bars[0].Time, bars[len(bars)-1].Time.AddDate(0, 0, 1)
	span, err := marketdata.NewTimeRange(spanStart, spanEnd)
	if err != nil {
		t.Fatalf("run span: %v", err)
	}
	oosDays := spanEnd.Sub(oosStart).Hours() / 24
	oosYears := oosDays / 365.25

	// oosStart itself (2020-01-01) is a market holiday, and equity-
	// curve points only exist at actual bar timestamps, so the
	// baseline point for OOS-return/drawdown measurement is the first
	// real bar at or after the frozen boundary, not the boundary
	// instant itself.
	oosStartBar, ok := firstBarAtOrAfter(bars, oosStart)
	if !ok {
		t.Fatalf("no bar found at or after OOS start %s", oosStart.Format("2006-01-02"))
	}
	oosStartAnchor := oosStartBar.Time

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))

	type tradeRow struct {
		Variant  string
		Side     string
		OpenedAt time.Time
		ClosedAt time.Time
		Open     bool
	}
	type episodeRow struct {
		Variant string
		*slopeRetraceEpisodeOutcome
	}
	var allTrades []tradeRow
	var allEpisodes []episodeRow
	equityByVariant := map[string][]struct {
		t time.Time
		v float64
	}{}

	var results []oosVariantResult
	for _, variant := range slopeRetraceVariants {
		cfg := smatrend.Config{
			SMAPeriod:           200,
			ExitRuleName:        "probation-trend",
			ReEntryRuleName:     variant,
			InitialStopBelowSMA: num.MustParseRate("0.01"),
			TrailActivationGain: num.MustParseRate("0.05"),
			TrailingStopPercent: num.MustParseRate("0.10"),
		}

		resp, rec, err := runSlopeRetraceFullNotionalBacktest(ctx, setup.mgr, setup.simResolver, setup.simID, span, cfg, startingCapital, setup.priceByTime)
		if err != nil {
			t.Fatalf("variant %s: backtest run: %v", variant, err)
		}
		fills := rec.fillsByID()

		baselineEquity, ok1 := equityCurveAt(resp.EquityCurve, oosStartAnchor)
		finalEquity, ok2 := equityCurveAt(resp.EquityCurve, bars[len(bars)-1].Time)
		if !ok1 || !ok2 {
			t.Fatalf("variant %s: could not locate equity-curve points at OOS boundaries", variant)
		}
		oosReturn := finalEquity/baselineEquity - 1
		maxDD := maxDrawdownSince(resp.EquityCurve, oosStartAnchor)
		cagr := cagrFromReturn(oosReturn, oosYears)

		for _, p := range resp.EquityCurve {
			if p.Timestamp.Before(oosStart) {
				continue
			}
			v, err := strconv.ParseFloat(fieldsFirst(p.Equity.String()), 64)
			if err != nil {
				continue
			}
			equityByVariant[variant] = append(equityByVariant[variant], struct {
				t time.Time
				v float64
			}{p.Timestamp, v})
		}

		all := make([]order.Trade, 0, len(resp.Trades)+len(resp.OpenTrades))
		all = append(all, resp.Trades...)
		all = append(all, resp.OpenTrades...)
		sort.Slice(all, func(i, j int) bool { return all[i].OpenedAt.Before(all[j].OpenedAt) })

		oosTradeCount, oosReEntryCount := 0, 0
		exposureDays := 0.0
		for i, tr := range all {
			end := tr.ClosedAt
			if end.IsZero() {
				end = spanEnd
			}
			if end.Before(oosStart) {
				continue
			}
			// trade_count is the count of round trips actually closed
			// at or after the OOS boundary — a still-open position
			// (ClosedAt zero) is reported separately via OpenAtEnd and
			// must not inflate this count (PR #374 review).
			if !tr.ClosedAt.IsZero() {
				oosTradeCount++
			}
			// A re-entry is any trade other than the account's very
			// first trade overall (i==0 in the full, sorted all slice
			// — never a re-entry by definition) whose own OpenedAt
			// falls at or after the OOS boundary; a position merely
			// carried open across the boundary from development is
			// not a fresh OOS re-entry. Indexing into the full
			// (not OOS-filtered) all slice — rather than tracking
			// "first OOS trade seen" — correctly handles an earlier
			// trade that both opened and closed entirely before
			// oosStart, which this loop's own oosStart filter above
			// skips over without ever seeing it (PR #374 review).
			if i > 0 && !tr.OpenedAt.Before(oosStart) {
				oosReEntryCount++
			}
			overlapStart := tr.OpenedAt
			if overlapStart.Before(oosStart) {
				overlapStart = oosStart
			}
			exposureDays += end.Sub(overlapStart).Hours() / 24

			allTrades = append(allTrades, tradeRow{
				Variant: variant, Side: tr.Side.String(),
				OpenedAt: tr.OpenedAt, ClosedAt: tr.ClosedAt, Open: tr.ClosedAt.IsZero(),
			})
		}

		whipsawCount, goodCount := 0, 0
		for i := 0; i < len(all); i++ {
			tr := all[i]
			if tr.ClosedAt.IsZero() || tr.ClosedAt.Before(oosStart) || i+1 >= len(all) {
				continue
			}
			next := all[i+1]
			outcome, err := slopeRetraceClassifyEpisode(t, bars, tr, next, fills)
			if err != nil {
				t.Fatalf("variant %s: classifying OOS episode: %v", variant, err)
			}
			if outcome.Classification == "whipsaw" {
				whipsawCount++
			} else {
				goodCount++
			}
			allEpisodes = append(allEpisodes, episodeRow{Variant: variant, slopeRetraceEpisodeOutcome: outcome})
			if oosChartedExitDates[outcome.ExitDate] {
				oosRenderEpisodeChart(t, chartsDir, bars, variant, outcome)
			}
		}

		result := oosVariantResult{
			Variant:           variant,
			OOSReturn:         oosReturn,
			CAGR:              cagr,
			MaxDrawdown:       maxDD,
			Calmar:            calmarRatio(cagr, maxDD),
			TradeCount:        oosTradeCount,
			ReEntryCount:      oosReEntryCount,
			OpenAtEnd:         len(resp.OpenTrades) > 0,
			ExposurePct:       exposureDays / oosDays,
			WhipsawCount:      whipsawCount,
			GoodDefensiveExit: goodCount,
		}
		results = append(results, result)
		t.Logf("[OOS %s -> %s] variant=%s return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f trades=%d reentries=%d exposure=%.4f whipsaws=%d/%d",
			oosStart.Format("2006-01-02"), spanEnd.Format("2006-01-02"), variant, oosReturn, cagr, maxDD, result.Calmar,
			result.TradeCount, result.ReEntryCount, result.ExposurePct, whipsawCount, whipsawCount+goodCount)
	}

	// Buy-and-hold benchmark over the identical OOS span, computed
	// directly from Close prices (exactly 100% invested by
	// construction, no Sizer involved).
	bhStartBar := oosStartBar
	bhEndBar := lastBarBefore(bars, spanEnd)
	// The strategy's own OOS baseline equity point (oosStartAnchor,
	// above) is the scheduler's mark-to-market value as of that bar's
	// own Close (backtest.Scheduler.equityCurve's own doc comment):
	// it already reflects that bar's full open-to-close move. Anchor
	// the benchmark at the same Close, not Open, so both sides of the
	// comparison start from the identical boundary instant instead of
	// silently giving buy-and-hold one extra bar's worth of return
	// the strategy side does not get (PR #374 review).
	bhReturn := bhEndBar.Close.Float64()/bhStartBar.Close.Float64() - 1
	bhCAGR := cagrFromReturn(bhReturn, oosYears)
	bhMaxDD := 0.0
	peak := 0.0
	for _, b := range bars {
		if b.Time.Before(oosStart) || !b.Time.Before(spanEnd) {
			continue
		}
		c := b.Close.Float64()
		if c > peak {
			peak = c
		}
		if peak > 0 {
			if dd := (peak - c) / peak; dd > bhMaxDD {
				bhMaxDD = dd
			}
		}
		equityByVariant["buy-and-hold"] = append(equityByVariant["buy-and-hold"], struct {
			t time.Time
			v float64
		}{b.Time, c / bhStartBar.Close.Float64()})
	}
	bhResult := oosVariantResult{
		Variant:      "buy-and-hold",
		OOSReturn:    bhReturn,
		CAGR:         bhCAGR,
		MaxDrawdown:  bhMaxDD,
		Calmar:       calmarRatio(bhCAGR, bhMaxDD),
		ExposurePct:  1.0,
		IsBuyAndHold: true,
	}
	results = append(results, bhResult)
	t.Logf("[OOS %s -> %s] buy-and-hold return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f",
		oosStart.Format("2006-01-02"), spanEnd.Format("2006-01-02"), bhReturn, bhCAGR, bhMaxDD, bhResult.Calmar)

	// Classification (VALIDATED/MIXED/REJECTED) against buy-and-hold
	// and the reclaim-exit-price baseline, frozen decision criteria
	// per trading#2: VALIDATED if the candidate's own OOS CAGR and
	// Calmar are both at or above baseline's; REJECTED if both are
	// below; MIXED otherwise (matches trading#2's own three-way
	// definition applied mechanically, not narratively).
	var baseline oosVariantResult
	for _, r := range results {
		if r.Variant == "reclaim-exit-price" {
			baseline = r
		}
	}
	var bhFinal oosVariantResult
	for _, r := range results {
		if r.IsBuyAndHold {
			bhFinal = r
		}
	}
	for i := range results {
		results[i].ReturnVsBuyHold = results[i].OOSReturn - bhFinal.OOSReturn
		results[i].MaxDDVsBuyHold = results[i].MaxDrawdown - bhFinal.MaxDrawdown
		if results[i].Variant == "reclaim-exit-price" || results[i].IsBuyAndHold {
			continue
		}
		switch {
		case results[i].CAGR >= baseline.CAGR && results[i].Calmar >= baseline.Calmar:
			results[i].Classification = "VALIDATED"
		case results[i].CAGR < baseline.CAGR && results[i].Calmar < baseline.Calmar:
			results[i].Classification = "REJECTED"
		default:
			results[i].Classification = "MIXED"
		}
	}

	// summary.json / summary.csv
	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fullArchiveOOSReentryOutputDir, "summary.json"), out, 0o644); err != nil {
		t.Fatalf("write summary.json: %v", err)
	}
	writeCSV(t, filepath.Join(fullArchiveOOSReentryOutputDir, "summary.csv"),
		[]string{"variant", "oos_return", "cagr", "max_drawdown", "calmar", "trades", "reentries", "exposure_pct", "whipsaws", "good_defensive_exits", "return_vs_buy_and_hold", "max_dd_vs_buy_and_hold", "classification"},
		func(w *csv.Writer) {
			for _, r := range results {
				_ = w.Write([]string{
					r.Variant, f4(r.OOSReturn), f4(r.CAGR), f4(r.MaxDrawdown), f4(r.Calmar),
					strconv.Itoa(r.TradeCount), strconv.Itoa(r.ReEntryCount), f4(r.ExposurePct),
					strconv.Itoa(r.WhipsawCount), strconv.Itoa(r.GoodDefensiveExit),
					f4(r.ReturnVsBuyHold), f4(r.MaxDDVsBuyHold), r.Classification,
				})
			}
		})

	// trades.csv
	writeCSV(t, filepath.Join(fullArchiveOOSReentryOutputDir, "trades.csv"),
		[]string{"variant", "side", "opened_at", "closed_at", "still_open"},
		func(w *csv.Writer) {
			for _, tr := range allTrades {
				closed := ""
				if !tr.ClosedAt.IsZero() {
					closed = tr.ClosedAt.Format("2006-01-02")
				}
				_ = w.Write([]string{tr.Variant, tr.Side, tr.OpenedAt.Format("2006-01-02"), closed, strconv.FormatBool(tr.Open)})
			}
		})

	// episodes.csv
	writeCSV(t, filepath.Join(fullArchiveOOSReentryOutputDir, "episodes.csv"),
		[]string{"variant", "exit_date", "exit_price", "post_exit_low", "post_exit_low_date", "reentry_date", "reentry_price", "days_flat", "further_decline_pct", "recovery_fraction_at_reentry", "classification", "downstream_exit_date", "downstream_return_pct", "downstream_still_open"},
		func(w *csv.Writer) {
			for _, e := range allEpisodes {
				_ = w.Write([]string{
					e.Variant, e.ExitDate, f4(e.ExitPrice), f4(e.PostExitLow), e.PostExitLowDate,
					e.ReEntryDate, f4(e.ReEntryPrice), strconv.Itoa(e.DaysFlat), f4(e.FurtherDeclinePct),
					f4(e.RecoveryFractionAtReEntry), e.Classification, e.DownstreamExitDate, f4(e.DownstreamReturnPct),
					strconv.FormatBool(e.DownstreamStillOpenAtEnd),
				})
			}
		})

	// equity.csv: one column per variant (normalized to 1.0 at OOS
	// start for the strategy variants; buy-and-hold is already
	// normalized to its own OOS-start Open above), outer-joined on
	// every distinct timestamp observed across all variants.
	writeEquityCSV(t, filepath.Join(fullArchiveOOSReentryOutputDir, "equity.csv"), slopeRetraceVariants, equityByVariant, baseline)

	t.Logf("wrote OOS validation artifacts to %s", fullArchiveOOSReentryOutputDir)
}

// oosRenderEpisodeChart mirrors slopeRetraceRenderEpisodeChart but
// names the output uniquely per episode (not just per variant) since
// an OOS window can contain more than one episode per variant.
func oosRenderEpisodeChart(t *testing.T, dir string, bars []marketdata.Bar, variant string, outcome *slopeRetraceEpisodeOutcome) {
	t.Helper()
	exitIdx := indexOfBarTime(bars, mustParseDate(t, outcome.ExitDate))
	reentryIdx := indexOfBarTime(bars, mustParseDate(t, outcome.ReEntryDate))
	if exitIdx < 0 || reentryIdx < 0 {
		return
	}
	from, to := exitIdx-30, reentryIdx+30
	if from < 0 {
		from = 0
	}
	if to >= len(bars) {
		to = len(bars) - 1
	}
	window := bars[from : to+1]

	markers := []chart.Marker{
		{Time: bars[exitIdx].Time, Price: num.MustParsePrice(fmt.Sprintf("%.4f", outcome.ExitPrice)), Kind: chart.MarkerExit, Label: fmt.Sprintf("exit %s", outcome.Classification)},
		{Time: bars[reentryIdx].Time, Price: num.MustParsePrice(fmt.Sprintf("%.4f", outcome.ReEntryPrice)), Kind: chart.MarkerReentry, Label: variant},
	}
	in := chart.EpisodeInput{
		Title:   fmt.Sprintf("OOS episode: %s, exit %s -> reentry %s (%s)", variant, outcome.ExitDate, outcome.ReEntryDate, outcome.Classification),
		Bars:    window,
		Markers: markers,
	}
	path := filepath.Join(dir, fmt.Sprintf("episode-%s-%s.png", outcome.ExitDate, variant))
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := chart.RenderEpisode(f, in, chart.FormatPNG); err != nil {
		t.Fatalf("render %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
}

func f4(f float64) string { return strconv.FormatFloat(f, 'f', 6, 64) }

func writeCSV(t *testing.T, path string, header []string, body func(*csv.Writer)) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	w := csv.NewWriter(f)
	_ = w.Write(header)
	body(w)
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
}

// writeEquityCSV outer-joins every variant's own OOS equity series
// (each already normalized to 1.0 at its own OOS-start value) on the
// union of every timestamp any variant observed, forward-filling a
// variant's own last-known value at any timestamp it has none of its
// own for (a normal occurrence: different variants' own equity points
// only advance on bars where that variant's own account state
// actually changed... in practice here every variant shares the
// identical bar series, so this is mostly exact rather than sparse).
func writeEquityCSV(t *testing.T, path string, variants []string, equityByVariant map[string][]struct {
	t time.Time
	v float64
}, baseline oosVariantResult) {
	t.Helper()
	allVariants := append([]string{}, variants...)
	allVariants = append(allVariants, "buy-and-hold")

	normalized := map[string]map[time.Time]float64{}
	timestamps := map[time.Time]struct{}{}
	for _, v := range allVariants {
		series := equityByVariant[v]
		m := make(map[time.Time]float64, len(series))
		if len(series) > 0 {
			base := series[0].v
			for _, p := range series {
				val := p.v
				if v != "buy-and-hold" && base != 0 {
					val = p.v / base
				}
				m[p.t] = val
				timestamps[p.t] = struct{}{}
			}
		}
		normalized[v] = m
	}

	sorted := make([]time.Time, 0, len(timestamps))
	for ts := range timestamps {
		sorted = append(sorted, ts)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	w := csv.NewWriter(f)
	header := append([]string{"date"}, allVariants...)
	_ = w.Write(header)

	last := make(map[string]float64, len(allVariants))
	for _, ts := range sorted {
		row := []string{ts.Format("2006-01-02")}
		for _, v := range allVariants {
			if val, ok := normalized[v][ts]; ok {
				last[v] = val
			}
			row = append(row, f4(last[v]))
		}
		_ = w.Write(row)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
}
