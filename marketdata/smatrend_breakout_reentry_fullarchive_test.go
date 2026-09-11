//go:build fullarchive

// This file runs issue #361's own research protocol: compare the SMA
// Long Hold playbook's current reference re-entry rule
// ("reclaim-exit-price") against the two new N-bar breakout
// candidates ("breakout-2", "breakout-3", strategy/smatrend/
// reentryrule.go's own nBarBreakoutReEntryRule) issue #354/PR #359's
// own diagnostic identified as the most plausible next experiment —
// using full backtest results with real fills and state, never a
// trigger-date-only analysis (issue #361's own explicit guardrail).
//
// Exit behavior (ExitRuleName "probation-trend",
// InitialStopBelowSMA 0.01, TrailActivationGain 0.05,
// TrailingStopPercent 0.10) and every other reference parameter are
// held fixed across all three variants — the only intentional
// strategy variable is ReEntryRuleName. Each variant runs one
// continuous backtest over SPY's full Stooq history at this
// playbook's own established reference sizing (RiskFraction 0.01,
// fixed $20 adverse distance — the same sizing #354/#356's own
// diagnostic already used), so relative comparisons between the three
// variants are fair (identical sizing convention throughout) even
// though, per PR #360's own review finding, this sizing is not
// matched to buy-and-hold and this file makes no such comparison.
//
// Lives under marketdata/ as package marketdata_test for the same
// reason every other fullarchive driver in this package does (it
// needs marketdata/internal/provider/stooq) and reuses already-built
// helpers (setupEQS01WalkForwardInstrument, runWFBacktestWithJournal,
// weightedFillPrice, smaSeries, indexOfBarTime, cagrFromReturn,
// calmarRatio, mustParseFloatWF, fieldsFirst) directly.
//
// Results go to a local, private directory
// (fullArchiveBreakoutReEntryOutputDir, edited locally, never
// committed), matching every other fullarchive driver's own
// convention.
package marketdata_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/rustyeddy/trader/chart"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveBreakoutReEntryOutputDir names the local directory this
// test writes its results into — empty by default; an operator edits
// this locally (for example to
// "/srv/trading/sweeps/smatrend-breakout-reentry").
const fullArchiveBreakoutReEntryOutputDir = ""

// breakoutReEntryVariants is issue #361's own fixed, three-variant
// comparison set — baseline plus the two N-bar breakout candidates.
// No broader sweep (issue #361's own guardrail).
var breakoutReEntryVariants = []string{"reclaim-exit-price", "breakout-2", "breakout-3"}

// variantResult is one re-entry variant's own full-history result —
// issue #361's own required "report at minimum" metric set, plus the
// whipsaw/good-defensive-exit split #354's diagnostic already
// established, and each of the two named long-flat episodes' own
// outcome under this variant.
type variantResult struct {
	Variant                string  `json:"variant"`
	NetReturn              float64 `json:"net_return"`
	CAGR                   float64 `json:"cagr"`
	MaxDrawdown            float64 `json:"max_drawdown"`
	Calmar                 float64 `json:"calmar"`
	TradeCount             int     `json:"trade_count"`
	OpenAtEnd              bool    `json:"open_at_end"`
	ExposurePct            float64 `json:"exposure_pct"` // fraction of the full span spent holding a position
	WhipsawCount           int     `json:"whipsaw_count"`
	GoodDefensiveExitCount int     `json:"good_defensive_exit_count"`

	Episode2008 *namedEpisodeOutcome `json:"episode_2008,omitempty"`
	Episode2022 *namedEpisodeOutcome `json:"episode_2022,omitempty"`
}

// namedEpisodeOutcome is what actually happened, under one variant,
// to whichever completed exit/re-entry episode's own exit falls
// inside the named crisis window — issue #361's own required "record
// when it would actually re-enter under the real strategy and what
// happened afterward" for the 2008 and 2022 episodes specifically.
// Exact exit/re-entry dates may differ slightly between variants:
// once re-entry timing changes, every subsequent entry/exit in the
// sequence can cascade to a different date, which is itself part of
// what this comparison is meant to surface honestly rather than force
// into artificial alignment.
type namedEpisodeOutcome struct {
	ExitDate                string  `json:"exit_date"`
	ExitPrice               float64 `json:"exit_price"`
	ReEntryDate             string  `json:"reentry_date"`
	ReEntryPrice            float64 `json:"reentry_price"`
	DaysFlat                int     `json:"days_flat"`
	FurtherDeclinePct       float64 `json:"further_decline_pct"`
	ReboundBeforeReEntryPct float64 `json:"rebound_before_reentry_pct"`
	Classification          string  `json:"classification"`
}

func TestSMATrendBreakoutReEntryComparison(t *testing.T) {
	if fullArchiveBreakoutReEntryOutputDir == "" {
		t.Skip("fullArchiveBreakoutReEntryOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	if err := os.MkdirAll(fullArchiveBreakoutReEntryOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveBreakoutReEntryOutputDir, err)
	}

	ctx := context.Background()
	inst := eqs01WFInstrument{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"}
	setup := setupEQS01WalkForwardInstrument(t, ctx, inst)
	bars := setup.bars

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	riskFraction := num.MustParseRate("0.01")
	adverseDistance := num.MustParsePrice("20.00000")
	spanStart, spanEnd := bars[0].Time, bars[len(bars)-1].Time.AddDate(0, 0, 1)
	span, err := marketdata.NewTimeRange(spanStart, spanEnd)
	if err != nil {
		t.Fatalf("run span: %v", err)
	}
	years := bars[len(bars)-1].Time.Sub(bars[0].Time).Hours() / 24 / 365.25
	spanDays := spanEnd.Sub(spanStart).Hours() / 24

	chartsDir := filepath.Join(fullArchiveBreakoutReEntryOutputDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", chartsDir, err)
	}

	var results []variantResult
	for _, variant := range breakoutReEntryVariants {
		cfg := smatrend.Config{
			SMAPeriod:           200,
			ExitRuleName:        "probation-trend",
			ReEntryRuleName:     variant,
			InitialStopBelowSMA: num.MustParseRate("0.01"),
			TrailActivationGain: num.MustParseRate("0.05"),
			TrailingStopPercent: num.MustParseRate("0.10"),
		}

		resp, rec, err := runWFBacktestWithJournal(ctx, setup.mgr, setup.simResolver, setup.simID, span, cfg, startingCapital, riskFraction, adverseDistance, setup.priceByTime)
		if err != nil {
			t.Fatalf("variant %s: backtest run: %v", variant, err)
		}
		fills := rec.fillsByID()

		netReturn := mustParseFloatWF(t, fieldsFirst(resp.Metrics.NetReturn().String()))
		maxDD := mustParseFloatWF(t, fieldsFirst(resp.Metrics.MaxDrawdown().String()))
		cagr := cagrFromReturn(netReturn, years)

		all := make([]order.Trade, 0, len(resp.Trades)+len(resp.OpenTrades))
		all = append(all, resp.Trades...)
		all = append(all, resp.OpenTrades...)
		sort.Slice(all, func(i, j int) bool { return all[i].OpenedAt.Before(all[j].OpenedAt) })

		exposureDays := 0.0
		for _, tr := range all {
			end := tr.ClosedAt
			if end.IsZero() {
				end = spanEnd
			}
			exposureDays += end.Sub(tr.OpenedAt).Hours() / 24
		}

		whipsawCount, goodCount := 0, 0
		var episode2008, episode2022 *namedEpisodeOutcome
		for i := 0; i < len(all); i++ {
			tr := all[i]
			if tr.ClosedAt.IsZero() || i+1 >= len(all) {
				continue
			}
			next := all[i+1]
			outcome, err := classifyEpisode(t, bars, tr, next, fills)
			if err != nil {
				t.Fatalf("variant %s: classifying episode: %v", variant, err)
			}
			if outcome.Classification == "whipsaw" {
				whipsawCount++
			} else {
				goodCount++
			}

			exitYear := tr.ClosedAt.Year()
			if exitYear == 2007 || exitYear == 2008 || exitYear == 2009 {
				if episode2008 == nil || outcome.FurtherDeclinePct > episode2008.FurtherDeclinePct {
					episode2008 = outcome
				}
			}
			if exitYear == 2022 {
				if episode2022 == nil || outcome.FurtherDeclinePct > episode2022.FurtherDeclinePct {
					episode2022 = outcome
				}
			}
		}

		result := variantResult{
			Variant:                variant,
			NetReturn:              netReturn,
			CAGR:                   cagr,
			MaxDrawdown:            maxDD,
			Calmar:                 calmarRatio(cagr, maxDD),
			TradeCount:             len(all),
			OpenAtEnd:              len(resp.OpenTrades) > 0,
			ExposurePct:            exposureDays / spanDays,
			WhipsawCount:           whipsawCount,
			GoodDefensiveExitCount: goodCount,
			Episode2008:            episode2008,
			Episode2022:            episode2022,
		}
		results = append(results, result)
		t.Logf("variant=%s return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f trades=%d exposure=%.4f whipsaws=%d/%d",
			variant, netReturn, cagr, maxDD, result.Calmar, result.TradeCount, result.ExposurePct, whipsawCount, whipsawCount+goodCount)

		renderNamedEpisodeChart(t, chartsDir, bars, variant, "2008", episode2008)
		renderNamedEpisodeChart(t, chartsDir, bars, variant, "2022", episode2022)
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal results: %v", err)
	}
	outPath := filepath.Join(fullArchiveBreakoutReEntryOutputDir, "breakout-reentry-comparison.json")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", outPath, err)
	}
	t.Logf("wrote %s", outPath)
	fmt.Println(string(out))
}

// classifyEpisode computes one exit/re-entry episode's own outcome —
// the same further-decline/rebound/classification logic
// smatrend_probation_episode_fullarchive_test.go's own main loop
// established for issue #354, reused here (not duplicated in full:
// only the pieces this file's own summary actually needs) so issue
// #361's own whipsaw-frequency and named-episode requirements stay
// directly comparable to #354's own baseline findings.
func classifyEpisode(t *testing.T, bars []marketdata.Bar, tr, next order.Trade, fills map[id.FillID]order.Fill) (*namedEpisodeOutcome, error) {
	t.Helper()
	exitIdx := indexOfBarTime(bars, tr.ClosedAt)
	reentryIdx := indexOfBarTime(bars, next.OpenedAt)
	if exitIdx < 0 || reentryIdx < 0 || reentryIdx <= exitIdx {
		return nil, fmt.Errorf("could not resolve exit bar (%s -> idx %d) / reentry bar (%s -> idx %d)", tr.ClosedAt, exitIdx, next.OpenedAt, reentryIdx)
	}

	exitPrice, _, err := weightedFillPrice(t, tr.ExitFillIDs, fills)
	if err != nil {
		return nil, fmt.Errorf("reconstructing exit price: %w", err)
	}
	reentryPrice, _, err := weightedFillPrice(t, next.EntryFillIDs, fills)
	if err != nil {
		return nil, fmt.Errorf("reconstructing reentry price: %w", err)
	}
	exitPriceF, reentryPriceF := exitPrice.Float64(), reentryPrice.Float64()

	low := bars[exitIdx].Low
	for j := exitIdx + 1; j < reentryIdx; j++ {
		if bars[j].Low.Cmp(low) < 0 {
			low = bars[j].Low
		}
	}
	lowF := low.Float64()

	furtherDeclinePct := (exitPriceF - lowF) / exitPriceF
	reboundPct := 0.0
	if lowF != 0 {
		reboundPct = (reentryPriceF - lowF) / lowF
	}
	classification := "good-defensive-exit"
	if furtherDeclinePct <= 0.01 {
		classification = "whipsaw"
	}

	return &namedEpisodeOutcome{
		ExitDate:                tr.ClosedAt.Format("2006-01-02"),
		ExitPrice:               exitPriceF,
		ReEntryDate:             next.OpenedAt.Format("2006-01-02"),
		ReEntryPrice:            reentryPriceF,
		DaysFlat:                int(next.OpenedAt.Sub(tr.ClosedAt).Hours() / 24),
		FurtherDeclinePct:       furtherDeclinePct,
		ReboundBeforeReEntryPct: reboundPct,
		Classification:          classification,
	}, nil
}

// renderNamedEpisodeChart renders one chart.RenderEpisode PNG for
// outcome (if non-nil) — issue #361's own "Charts from the chart
// package should be generated for those episodes if practical."
func renderNamedEpisodeChart(t *testing.T, dir string, bars []marketdata.Bar, variant, label string, outcome *namedEpisodeOutcome) {
	t.Helper()
	if outcome == nil {
		return
	}
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
		Title:   fmt.Sprintf("%s episode: %s, exit %s -> reentry %s (%s)", label, variant, outcome.ExitDate, outcome.ReEntryDate, outcome.Classification),
		Bars:    window,
		Markers: markers,
	}
	path := filepath.Join(dir, fmt.Sprintf("episode-%s-%s.png", label, variant))
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
