//go:build fullarchive

// This file runs issue #365's own research protocol: test two
// re-entry-confirmation hypotheses for the SMA Long Hold playbook,
// following #361's own inconclusive N-bar breakout finding —
//
//   - Hypothesis A ("above-sma-slope-20"/"above-sma-slope-50",
//     strategy/smatrend/reentryrule.go's own smaSlopeReEntryRule):
//     require the long SMA itself to be rising before re-entering.
//   - Hypothesis B ("retrace-25"/"retrace-50", percentRetraceReEntryRule):
//     require a fixed fraction of recovery from the post-exit low
//     before re-entering.
//
// against the playbook's current reference re-entry rule
// ("reclaim-exit-price") — using full backtest results with real
// fills and state, never a trigger-date-only analysis (issue #365's
// own explicit guardrail, matching #361's).
//
// Exit behavior (ExitRuleName "probation-trend", InitialStopBelowSMA
// 0.01, TrailActivationGain 0.05, TrailingStopPercent 0.10) and every
// other reference parameter are held fixed across all five variants —
// the only intentional strategy variable is ReEntryRuleName.
//
// # Primary comparison now uses reference-price-exact full-notional sizing directly
//
// Issue #364 (risk.NewFullNotionalSizer, ADR-061) landed after #361's
// own walk-forward "approximately ~100%-notional" comparison was
// written, and changes this file's own protocol materially:
// backtest.resolverInputBuilder already sets pipeline.Input.
// ReferencePrice to event.Bar.Open on every single submission (issue
// #216, M5-08 review) — the real bar-open price at the actual moment
// of each entry, not a fold-anchored approximation. Swapping in
// risk.NewFullNotionalSizer() therefore sizes every entry exactly at
// 100% of available capital *at that reference price*, continuously,
// with no walk-forward folding needed at all to keep sizing from
// drifting within a test window (the entire reason #361's own
// walk-forward protocol existed in the first place).
// slopeRetraceFullNotionalEnvironmentFactory below is
// wfJournalEnvironmentFactory's own identical composition with
// exactly one changed line: the Sizer.
//
// "Reference-price-exact," not fill-price-exact (ADR-061's own
// explicit boundary; PR #372 review): the simulated broker's own Buy
// fill is rounded up to the listing's own tick size, which can differ
// from the unrounded ReferencePrice the Sizer computed the quantity
// from. This file's own results are exact at the supplied reference
// price, not a guarantee of exact notional at the real, tick-rounded
// fill — see risk.SizeInput.ReferencePrice's own doc comment for why
// this Sizer makes no stronger claim.
//
// A secondary, sizing-independent diagnostic run (the playbook's own
// established reference sizing: RiskFraction 0.01, fixed $20 adverse
// distance, via the existing runWFBacktestWithJournal helper) is also
// produced, for the whipsaw/named-episode analysis, mirroring #361's
// own Comparison 1 — its absolute CAGR/return must not be read as a
// portfolio-allocation answer.
//
// Lives under marketdata/ as package marketdata_test for the same
// reason every other fullarchive driver in this package does, reusing
// already-built helpers (setupEQS01WalkForwardInstrument,
// runWFBacktestWithJournal, weightedFillPrice, indexOfBarTime,
// cagrFromReturn, calmarRatio, mustParseFloatWF, fieldsFirst,
// firstBarAtOrAfter, lastBarBefore, mustParseDate) directly.
// classifyEpisode/renderNamedEpisodeChart follow
// smatrend_breakout_reentry_fullarchive_test.go's own identical
// shape, duplicated narrowly under a new name (this codebase's own
// established "one self-contained file per issue" convention for
// fullarchive drivers — no shared helper extraction across driver
// files).
//
// Results go to a local, private directory
// (fullArchiveSlopeRetraceOutputDir, edited locally, never
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
	"time"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/chart"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveSlopeRetraceOutputDir names the local directory this
// test writes its results into — empty by default; an operator edits
// this locally (for example to "/srv/trading/sweeps/smatrend-slope-retrace-reentry").
const fullArchiveSlopeRetraceOutputDir = ""

// slopeRetraceVariants is issue #365's own fixed, five-variant
// comparison set — baseline plus the two SMA-slope and two
// percent-retrace candidates. No broader sweep (issue #365's own
// guardrail).
var slopeRetraceVariants = []string{"reclaim-exit-price", "above-sma-slope-20", "above-sma-slope-50", "retrace-25", "retrace-50"}

// slopeRetraceFullNotionalEnvironmentFactory is
// wfJournalEnvironmentFactory's own identical composition (real M4
// pipeline, simulated broker, journal recording) with exactly one
// change: risk.NewFullNotionalSizer() instead of
// risk.NewFixedFractionSizer() — see this file's own doc comment for
// why that alone is now sufficient for reference-price-exact
// 100%-invested sizing,
// with no walk-forward folding required.
type slopeRetraceFullNotionalEnvironmentFactory struct {
	prices  map[time.Time]marketdata.Bar
	journal *wfMemoryRecorder
}

func (f slopeRetraceFullNotionalEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := clock.NewSimulated(req.Span.Start())
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	prices := &eqs01WFPriceSource{clock: c, bars: f.prices}
	b, err := simbroker.NewBroker("sim", simbroker.Deps{
		Clock:  c,
		IDs:    ids,
		Prices: prices,
	}, simbroker.AccountConfig{AccountID: accountID, StartingCash: req.StartingCapital})
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	acct, err := b.OpenAccount(ctx, accountID)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	planner, err := execution.NewPlanner(execution.Deps{Clock: c, IDs: ids})
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	engine, err := risk.NewEngine()
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	pl, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFullNotionalSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     ids,
	})
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	fill, err := backtest.NewComponentInfo(prices.Info().Name, prices.Info().Version, nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	none, err := backtest.NewComponentInfo("none", "", nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	return svcbacktest.Environment{
		Clock:           c,
		IDs:             ids,
		Account:         acct,
		Pipeline:        pl,
		Journal:         f.journal,
		FillModel:       fill,
		SlippageModel:   none,
		CommissionModel: none,
	}, nil
}

// runSlopeRetraceFullNotionalBacktest runs one continuous
// reference-price-exact full-notional backtest (see this file's own
// doc comment for why "reference-price-exact," not fill-price-exact).
// RiskFraction/AdverseDistance are passed
// only because svcbacktest.RunRequest's own shape still carries them
// (ADR-061 made both conditionally required per concrete Sizer, not
// removed from the request shape); risk.NewFullNotionalSizer()
// ignores both entirely, sizing instead from pipeline.Input.
// ReferencePrice — always event.Bar.Open (see this file's own doc
// comment) — so any nonzero placeholder value is fine here.
func runSlopeRetraceFullNotionalBacktest(ctx context.Context, mgr *marketdata.Manager, simResolver instrument.Resolver, simID instrument.ID,
	span marketdata.TimeRange, cfg smatrend.Config, startingCapital num.Money, priceByTime map[time.Time]marketdata.Bar) (svcbacktest.RunResponse, *wfMemoryRecorder, error) {

	rec := &wfMemoryRecorder{}
	factory := slopeRetraceFullNotionalEnvironmentFactory{prices: priceByTime, journal: rec}
	svc, err := svcbacktest.New(mgr, simResolver, factory, nil)
	if err != nil {
		return svcbacktest.RunResponse{}, nil, err
	}

	strat, err := smatrend.New(simID, marketdata.D1, cfg)
	if err != nil {
		return svcbacktest.RunResponse{}, nil, err
	}

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:           strat,
		Span:               span,
		StartingCapital:    startingCapital,
		RiskFraction:       num.MustParseRate("0.01"),     // ignored by risk.NewFullNotionalSizer()
		AdverseDistance:    num.MustParsePrice("1.00000"), // ignored by risk.NewFullNotionalSizer()
		StrategyParameters: cfg,                           // PR #372 review: without this, every variant's manifest/config digest is indistinguishable despite ReEntryRuleName differing.
	})
	return resp, rec, err
}

// slopeRetraceVariantResult is one re-entry variant's own full-history
// result under reference-price-exact full-notional sizing — issue
// #365's own required
// "report at minimum" metric set, plus buy-and-hold's own row and each
// named episode's own outcome, mirroring
// smatrend_breakout_reentry_fullarchive_test.go's own
// walkForwardVariantResult/variantResult shape (merged into one type
// here since a single continuous exact-notional run needs no separate
// walk-forward-vs-diagnostic split for the primary comparison the way
// the approximate #361 protocol did).
type slopeRetraceVariantResult struct {
	Variant      string  `json:"variant"`
	NetReturn    float64 `json:"net_return"`
	CAGR         float64 `json:"cagr"`
	MaxDrawdown  float64 `json:"max_drawdown"`
	Calmar       float64 `json:"calmar"`
	TradeCount   int     `json:"trade_count"`
	ReEntryCount int     `json:"reentry_count"`
	OpenAtEnd    bool    `json:"open_at_end"`
	ExposurePct  float64 `json:"exposure_pct"`
	IsBuyAndHold bool    `json:"is_buy_and_hold,omitempty"`
}

// slopeRetraceDiagnosticResult is one re-entry variant's own
// secondary, sizing-independent diagnostic result (RiskFraction 0.01,
// fixed $20 adverse distance) — mirrors
// smatrend_breakout_reentry_fullarchive_test.go's own variantResult
// exactly, including the whipsaw/good-defensive-exit split and named
// episode outcomes.
type slopeRetraceDiagnosticResult struct {
	Variant                string  `json:"variant"`
	NetReturn              float64 `json:"net_return"`
	CAGR                   float64 `json:"cagr"`
	MaxDrawdown            float64 `json:"max_drawdown"`
	Calmar                 float64 `json:"calmar"`
	TradeCount             int     `json:"trade_count"`
	ReEntryCount           int     `json:"reentry_count"`
	OpenAtEnd              bool    `json:"open_at_end"`
	ExposurePct            float64 `json:"exposure_pct"`
	WhipsawCount           int     `json:"whipsaw_count"`
	GoodDefensiveExitCount int     `json:"good_defensive_exit_count"`

	Episode2008 *slopeRetraceEpisodeOutcome `json:"episode_2008,omitempty"`
	Episode2022 *slopeRetraceEpisodeOutcome `json:"episode_2022,omitempty"`
}

// slopeRetraceEpisodeOutcome mirrors
// smatrend_breakout_reentry_fullarchive_test.go's own
// namedEpisodeOutcome exactly.
type slopeRetraceEpisodeOutcome struct {
	ExitDate          string  `json:"exit_date"`
	ExitPrice         float64 `json:"exit_price"`
	PostExitLow       float64 `json:"post_exit_low"`
	PostExitLowDate   string  `json:"post_exit_low_date"`
	ReEntryDate       string  `json:"reentry_date"`
	ReEntryPrice      float64 `json:"reentry_price"`
	DaysFlat          int     `json:"days_flat"`
	FurtherDeclinePct float64 `json:"further_decline_pct"`
	// ReboundBeforeReEntryPct is (reentryPrice-low)/low — a general
	// "how far above the low is the re-entry" figure, independent of
	// any hypothesis.
	ReboundBeforeReEntryPct float64 `json:"rebound_before_reentry_pct"`
	// RecoveryFractionAtReEntry is (reentryPrice-low)/(exitPrice-low)
	// — the exact quantity percentRetraceReEntryRule's own hypothesis
	// is defined in terms of (PR #372 review: ReboundBeforeReEntryPct
	// is a different metric and cannot verify where the 25%/50%
	// thresholds actually fired). 1.0 when exitPrice == low (no
	// decline occurred at all — the rule's own no-decline case, where
	// it enters unconditionally rather than computing a ratio).
	RecoveryFractionAtReEntry float64 `json:"recovery_fraction_at_reentry"`
	Classification            string  `json:"classification"`
	DownstreamStillOpenAtEnd  bool    `json:"downstream_still_open_at_end"`
	DownstreamExitDate        string  `json:"downstream_exit_date,omitempty"`
	DownstreamExitPrice       float64 `json:"downstream_exit_price,omitempty"`
	DownstreamReturnPct       float64 `json:"downstream_return_pct,omitempty"`
}

// TestSMATrendSlopeRetraceReEntryComparison is issue #365's own
// primary comparison: one continuous backtest per variant, at exact
// full-notional sizing, over SPY's full Stooq history, plus a
// buy-and-hold benchmark row over the identical span.
func TestSMATrendSlopeRetraceReEntryComparison(t *testing.T) {
	if fullArchiveSlopeRetraceOutputDir == "" {
		t.Skip("fullArchiveSlopeRetraceOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	if err := os.MkdirAll(fullArchiveSlopeRetraceOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveSlopeRetraceOutputDir, err)
	}

	ctx := context.Background()
	inst := eqs01WFInstrument{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"}
	setup := setupEQS01WalkForwardInstrument(t, ctx, inst)
	bars := setup.bars

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	spanStart, spanEnd := bars[0].Time, bars[len(bars)-1].Time.AddDate(0, 0, 1)
	span, err := marketdata.NewTimeRange(spanStart, spanEnd)
	if err != nil {
		t.Fatalf("run span: %v", err)
	}
	years := bars[len(bars)-1].Time.Sub(bars[0].Time).Hours() / 24 / 365.25
	spanDays := spanEnd.Sub(spanStart).Hours() / 24

	var results []slopeRetraceVariantResult
	for _, variant := range slopeRetraceVariants {
		cfg := smatrend.Config{
			SMAPeriod:           200,
			ExitRuleName:        "probation-trend",
			ReEntryRuleName:     variant,
			InitialStopBelowSMA: num.MustParseRate("0.01"),
			TrailActivationGain: num.MustParseRate("0.05"),
			TrailingStopPercent: num.MustParseRate("0.10"),
		}

		resp, _, err := runSlopeRetraceFullNotionalBacktest(ctx, setup.mgr, setup.simResolver, setup.simID, span, cfg, startingCapital, setup.priceByTime)
		if err != nil {
			t.Fatalf("variant %s: backtest run: %v", variant, err)
		}

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

		reEntryCount := 0
		if len(all) > 0 {
			reEntryCount = len(all) - 1
		}
		result := slopeRetraceVariantResult{
			Variant:      variant,
			NetReturn:    netReturn,
			CAGR:         cagr,
			MaxDrawdown:  maxDD,
			Calmar:       calmarRatio(cagr, maxDD),
			TradeCount:   len(resp.Trades),
			ReEntryCount: reEntryCount,
			OpenAtEnd:    len(resp.OpenTrades) > 0,
			ExposurePct:  exposureDays / spanDays,
		}
		results = append(results, result)
		t.Logf("[full-notional] variant=%s return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f trades=%d reentries=%d exposure=%.4f",
			variant, netReturn, cagr, maxDD, result.Calmar, result.TradeCount, result.ReEntryCount, result.ExposurePct)
	}

	// Buy-and-hold benchmark, over the identical full span, computed
	// directly from Close prices (exactly 100% invested by
	// construction, no Sizer involved at all).
	bhReturn := bars[len(bars)-1].Close.Float64()/bars[0].Open.Float64() - 1
	bhCAGR := cagrFromReturn(bhReturn, years)
	bhMaxDD := 0.0
	peak := 0.0
	for _, b := range bars {
		c := b.Close.Float64()
		if c > peak {
			peak = c
		}
		if peak > 0 {
			if dd := (peak - c) / peak; dd > bhMaxDD {
				bhMaxDD = dd
			}
		}
	}
	results = append(results, slopeRetraceVariantResult{
		Variant:      "buy-and-hold",
		NetReturn:    bhReturn,
		CAGR:         bhCAGR,
		MaxDrawdown:  bhMaxDD,
		Calmar:       calmarRatio(bhCAGR, bhMaxDD),
		ExposurePct:  1.0,
		IsBuyAndHold: true,
	})
	t.Logf("[full-notional] buy-and-hold return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f", bhReturn, bhCAGR, bhMaxDD, calmarRatio(bhCAGR, bhMaxDD))

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal results: %v", err)
	}
	outPath := filepath.Join(fullArchiveSlopeRetraceOutputDir, "slope-retrace-reentry-fullnotional-comparison.json")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", outPath, err)
	}
	t.Logf("wrote %s", outPath)
	fmt.Println(string(out))
}

// TestSMATrendSlopeRetraceReEntryDiagnostic is issue #365's own
// secondary, sizing-independent diagnostic: the playbook's own
// established reference sizing (RiskFraction 0.01, fixed $20 adverse
// distance), producing the whipsaw/good-defensive-exit split and
// named-episode outcomes charted below — mirroring
// smatrend_breakout_reentry_fullarchive_test.go's own
// TestSMATrendBreakoutReEntryComparison exactly, generalized over
// slopeRetraceVariants.
func TestSMATrendSlopeRetraceReEntryDiagnostic(t *testing.T) {
	if fullArchiveSlopeRetraceOutputDir == "" {
		t.Skip("fullArchiveSlopeRetraceOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	if err := os.MkdirAll(fullArchiveSlopeRetraceOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveSlopeRetraceOutputDir, err)
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

	chartsDir := filepath.Join(fullArchiveSlopeRetraceOutputDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", chartsDir, err)
	}

	var results []slopeRetraceDiagnosticResult
	for _, variant := range slopeRetraceVariants {
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
		var episode2008, episode2022 *slopeRetraceEpisodeOutcome
		for i := 0; i < len(all); i++ {
			tr := all[i]
			if tr.ClosedAt.IsZero() || i+1 >= len(all) {
				continue
			}
			next := all[i+1]
			outcome, err := slopeRetraceClassifyEpisode(t, bars, tr, next, fills)
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

		reEntryCount := 0
		if len(all) > 0 {
			reEntryCount = len(all) - 1
		}
		result := slopeRetraceDiagnosticResult{
			Variant:                variant,
			NetReturn:              netReturn,
			CAGR:                   cagr,
			MaxDrawdown:            maxDD,
			Calmar:                 calmarRatio(cagr, maxDD),
			TradeCount:             len(resp.Trades),
			ReEntryCount:           reEntryCount,
			OpenAtEnd:              len(resp.OpenTrades) > 0,
			ExposurePct:            exposureDays / spanDays,
			WhipsawCount:           whipsawCount,
			GoodDefensiveExitCount: goodCount,
			Episode2008:            episode2008,
			Episode2022:            episode2022,
		}
		results = append(results, result)
		t.Logf("[diagnostic] variant=%s return=%.4f cagr=%.4f maxDD=%.4f calmar=%.4f trades=%d reentries=%d exposure=%.4f whipsaws=%d/%d",
			variant, netReturn, cagr, maxDD, result.Calmar, result.TradeCount, result.ReEntryCount, result.ExposurePct, whipsawCount, whipsawCount+goodCount)

		slopeRetraceRenderEpisodeChart(t, chartsDir, bars, variant, "2008", episode2008)
		slopeRetraceRenderEpisodeChart(t, chartsDir, bars, variant, "2022", episode2022)
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal results: %v", err)
	}
	outPath := filepath.Join(fullArchiveSlopeRetraceOutputDir, "slope-retrace-reentry-diagnostic.json")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", outPath, err)
	}
	t.Logf("wrote %s", outPath)
	fmt.Println(string(out))
}

// slopeRetraceClassifyEpisode mirrors
// smatrend_breakout_reentry_fullarchive_test.go's own classifyEpisode
// exactly.
func slopeRetraceClassifyEpisode(t *testing.T, bars []marketdata.Bar, tr, next order.Trade, fills map[id.FillID]order.Fill) (*slopeRetraceEpisodeOutcome, error) {
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

	// low/lowIdx track the minimum Low (and the bar it first occurred
	// on) across [exitIdx, reentryIdx) — the identical window
	// percentRetraceReEntryRule's own postExitLow tracks live (issue
	// #365's own hypothesis definition: "the lowest Low observed
	// since exit"), including the exit bar itself.
	low := bars[exitIdx].Low
	lowIdx := exitIdx
	for j := exitIdx + 1; j < reentryIdx; j++ {
		if bars[j].Low.Cmp(low) < 0 {
			low = bars[j].Low
			lowIdx = j
		}
	}
	lowF := low.Float64()

	furtherDeclinePct := (exitPriceF - lowF) / exitPriceF
	reboundPct := 0.0
	if lowF != 0 {
		reboundPct = (reentryPriceF - lowF) / lowF
	}
	// RecoveryFractionAtReEntry is the exact quantity
	// percentRetraceReEntryRule's own hypothesis is defined in terms
	// of — (reentryPrice-low)/(exitPrice-low) — not
	// ReboundBeforeReEntryPct's own different (reentryPrice-low)/low
	// (PR #372 review). 1.0 when exitPriceF == lowF: no decline below
	// the exit price ever occurred, the rule's own no-decline case,
	// where it enters unconditionally rather than computing a ratio
	// at all.
	recoveryFraction := 1.0
	if exitPriceF != lowF {
		recoveryFraction = (reentryPriceF - lowF) / (exitPriceF - lowF)
	}
	classification := "good-defensive-exit"
	if furtherDeclinePct <= 0.01 {
		classification = "whipsaw"
	}

	outcome := &slopeRetraceEpisodeOutcome{
		ExitDate:                  tr.ClosedAt.Format("2006-01-02"),
		ExitPrice:                 exitPriceF,
		PostExitLow:               lowF,
		PostExitLowDate:           bars[lowIdx].Time.Format("2006-01-02"),
		ReEntryDate:               next.OpenedAt.Format("2006-01-02"),
		ReEntryPrice:              reentryPriceF,
		DaysFlat:                  int(next.OpenedAt.Sub(tr.ClosedAt).Hours() / 24),
		FurtherDeclinePct:         furtherDeclinePct,
		ReboundBeforeReEntryPct:   reboundPct,
		RecoveryFractionAtReEntry: recoveryFraction,
		Classification:            classification,
	}

	if next.ClosedAt.IsZero() {
		outcome.DownstreamStillOpenAtEnd = true
	} else {
		downstreamExitPrice, _, err := weightedFillPrice(t, next.ExitFillIDs, fills)
		if err != nil {
			return nil, fmt.Errorf("reconstructing downstream exit price: %w", err)
		}
		downstreamExitPriceF := downstreamExitPrice.Float64()
		outcome.DownstreamExitDate = next.ClosedAt.Format("2006-01-02")
		outcome.DownstreamExitPrice = downstreamExitPriceF
		outcome.DownstreamReturnPct = (downstreamExitPriceF - reentryPriceF) / reentryPriceF
	}

	return outcome, nil
}

// slopeRetraceRenderEpisodeChart mirrors
// smatrend_breakout_reentry_fullarchive_test.go's own
// renderNamedEpisodeChart exactly.
func slopeRetraceRenderEpisodeChart(t *testing.T, dir string, bars []marketdata.Bar, variant, label string, outcome *slopeRetraceEpisodeOutcome) {
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
