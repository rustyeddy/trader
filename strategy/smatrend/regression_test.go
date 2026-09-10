// This file is issue #335 (EQS-01)'s own required horizontal-slice
// regression: one deterministic run proving the entire path — canonical
// bars -> SMA calculation -> cross-above detection -> order.Intent -> M4
// execution/risk -> simulated fill -> broker-side resting-stop trigger
// (ADR-026, issue #338) -> account state — works together, through the
// real public composition path (service/backtest.Service, the same seam
// strategy/emacross's own regression test exercises), not a mock that
// bypasses M3/M4/M5. It exercises, in one run: warm-up, a genuine
// cross-above entry, high-water-mark ratcheting, a normal intraday stop
// hit, no re-entry while price remains above the SMA after that stop,
// a fresh cross-above re-entry starting a new trailing episode, and a
// gap-through-open stop hit closing that second episode.
package smatrend_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// memoryRecorder is a minimal in-memory journal.Recorder, used only to
// assert on the actual journaled sequence, without needing a real
// storage adapter.
type memoryRecorder struct {
	records []journal.Record
}

func (r *memoryRecorder) Record(ctx context.Context, rec journal.Record) error {
	r.records = append(r.records, rec)
	return nil
}

func (r *memoryRecorder) Close() error { return nil }

func (r *memoryRecorder) kinds(kind journal.Kind) []journal.Record {
	var out []journal.Record
	for _, rec := range r.records {
		if rec.Kind == kind {
			out = append(out, rec)
		}
	}
	return out
}

func eurusdListing(t *testing.T, provider string) instrument.Listing {
	t.Helper()
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   provider,
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// barLookupPriceSource is a real (not fixed-value) simbroker.
// FillPriceSource: for whatever instant clock currently reports, it
// returns that instant's own canonical bar Open — the real next-bar-
// open price a market order (Enter) fills at, matching
// strategy/emacross's own identical fixture helper.
type barLookupPriceSource struct {
	clock *clock.Simulated
	bars  map[time.Time]marketdata.Bar
}

func (s *barLookupPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "next-bar-open-lookup", Version: "test"}
}

func (s *barLookupPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	now := s.clock.Now()
	bar, ok := s.bars[now]
	if !ok {
		return num.Price{}, fmt.Errorf("no canonical bar for %s at %s", listing.Symbol(), now)
	}
	return bar.Open, nil
}

func loadBarLookupPriceSource(t *testing.T, ctx context.Context, manager *marketdata.Manager, c *clock.Simulated, query marketdata.BarQuery) *barLookupPriceSource {
	t.Helper()
	reader, err := manager.Bars(ctx, query)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	byTime := make(map[time.Time]marketdata.Bar)
	for {
		bar, err := reader.Next(ctx)
		if err != nil {
			require.ErrorIs(t, err, io.EOF, "canonical read must fail clearly, not be silently treated as end of stream")
			break
		}
		byTime[bar.Time] = bar
	}
	return &barLookupPriceSource{clock: c, bars: byTime}
}

// smatrendEnvironmentFactory builds the real M4 pipeline (fixed-
// fraction sizing, execution.Planner, risk.Engine, simulated broker)
// against prices — no smatrend-specific execution wiring exists or is
// added here. The simulated broker's own account handle is what
// actually satisfies backtest.IntrabarAdvancer (issue #338): this
// factory adds nothing extra for the trailing stop to trigger.
type smatrendEnvironmentFactory struct {
	prices  *barLookupPriceSource
	journal *memoryRecorder
}

func (f smatrendEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := f.prices.clock
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	b, err := simbroker.NewBroker("sim", simbroker.Deps{
		Clock:  c,
		IDs:    ids,
		Prices: f.prices,
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
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     ids,
	})
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	fill, err := backtest.NewComponentInfo(f.prices.Info().Name, f.prices.Info().Version, nil)
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

// smatrendFixtureSpan covers strategy/smatrend/testdata's own dedicated
// 11-bar D1 EURUSD fixture (see the fixture's own doc comment in
// regression_test.go's header for the engineered price path).
func smatrendFixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 23, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// runSMATrendFixture runs the full fixture through the real
// service/backtest.Service composition path with SMAPeriod=3 (so an
// 11-bar fixture is enough to warm up and still leave room for two
// full trailing-stop episodes) and a 10% trailing stop, matching
// EQS-01's own reference TrailingStopPercent, using the default
// exit/re-entry rules (issue #347's own DefaultExitRuleName/
// DefaultReEntryRuleName).
func runSMATrendFixture(t *testing.T) (svcbacktest.RunResponse, *memoryRecorder) {
	t.Helper()
	return runSMATrendFixtureWithConfig(t, smatrend.Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})
}

// runSMATrendFixtureWithConfig is runSMATrendFixture generalized over
// the full smatrend.Config (issue #347), so a non-default
// ExitRuleName/ReEntryRuleName combination can be exercised through
// the exact same real M4/M5 composition path rather than a second,
// parallel fixture.
func runSMATrendFixtureWithConfig(t *testing.T, cfg smatrend.Config) (svcbacktest.RunResponse, *memoryRecorder) {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t, "oanda")))

	span := smatrendFixtureSpan(t)
	c := clock.NewSimulated(span.Start())

	manager, err := marketdata.New(marketdata.Config{
		Clock:        c,
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	simResolver := instrument.NewMemoryResolver()
	simListing := eurusdListing(t, "sim")
	require.NoError(t, simResolver.Register(simListing))

	ctx := context.Background()
	query := marketdata.BarQuery{Instrument: simListing.InstrumentID(), Interval: marketdata.D1, Range: span}
	plan, err := manager.Plan(ctx, query)
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	prices := loadBarLookupPriceSource(t, ctx, manager, c, query)
	rec := &memoryRecorder{}
	factory := smatrendEnvironmentFactory{prices: prices, journal: rec}

	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	strat, err := smatrend.New(simListing.InstrumentID(), marketdata.D1, cfg)
	require.NoError(t, err)

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        strat,
		Span:            span,
		StartingCapital: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
	})
	require.NoError(t, err, "a risk rejection must never abort the run")
	return resp, rec
}

// TestSMATrend_EndToEndRegression is EQS-01's own required
// demonstration, exercising most of its acceptance criteria in one
// real run:
//
//   - warm-up (SMAPeriod=3): no intent from bars 1-3.
//   - a genuine cross-above entry (bar 4's close, filled at bar 5's
//     open) — not merely "close above SMA."
//   - a high-water-mark ratchet (bar 5's stop at 90% of its own High,
//     ratcheted higher by bar 6's own higher High).
//   - a normal intraday stop hit on bar 7 (Open above the resting
//     stop, Low below it: an intrabar touch, filling at the stop
//     price itself, per ADR-026).
//   - no re-entry on bars 7-8 while price remains above the SMA after
//     that stop exit (EQS-01's own central re-entry rule).
//   - a fresh cross-above re-entry on bar 9 (filled at bar 10's open),
//     starting a brand-new trailing episode — proving no stale
//     high-water mark/stop carried over from the first episode.
//   - a gap-through-open stop hit on bar 11 (this bar's own Open
//     already at/through the resting stop): fills at that Open, never
//     at the requested stop price.
func TestSMATrend_EndToEndRegression(t *testing.T) {
	resp, rec := runSMATrendFixture(t)

	// The actual intent -> proposal -> decision -> request -> order ->
	// fill chain, not inferred from final account state alone.
	require.NotEmpty(t, rec.kinds(journal.KindIntent))
	require.NotEmpty(t, rec.kinds(journal.KindProposal))
	require.NotEmpty(t, rec.kinds(journal.KindDecision))
	require.NotEmpty(t, rec.kinds(journal.KindRequest))
	require.NotEmpty(t, rec.kinds(journal.KindOrder))
	// Four fills: bar-5 entry, bar-7 stop exit, bar-10 re-entry, bar-11
	// gap-through stop exit.
	require.Len(t, rec.kinds(journal.KindFill), 4)

	// Decision evidence: exactly two "enter-long" signals (bar 4 and
	// bar 9's own crosses) and at least two "adjust-stop" signals (the
	// initial placements on bar 5 and bar 10 — the ratchet on bar 6
	// makes a third).
	signals := rec.kinds(journal.KindSignal)
	var enters, adjustStops int
	for _, s := range signals {
		switch s.Signal.Values["action"] {
		case "enter-long":
			enters++
		case "adjust-stop":
			adjustStops++
		}
		assert.Equal(t, smatrend.Name, s.Signal.Strategy)
	}
	assert.Equal(t, 2, enters, "exactly two genuine cross-above entries")
	assert.Equal(t, 3, adjustStops, "bar 5 initial stop, bar 6 ratchet, bar 10 initial stop")

	bar5Fill := time.Date(2024, time.January, 12, 22, 0, 0, 0, time.UTC)
	bar7Fill := time.Date(2024, time.January, 16, 22, 0, 0, 0, time.UTC) // the stop-hit bar's own timestamp
	bar10Fill := time.Date(2024, time.January, 19, 22, 0, 0, 0, time.UTC)
	bar11Fill := time.Date(2024, time.January, 22, 22, 0, 0, 0, time.UTC)

	require.Len(t, resp.Trades, 2, "both trailing-stop episodes must have closed as realized trades")

	first := resp.Trades[0]
	assert.Equal(t, order.Long, first.Side)
	assert.True(t, first.OpenedAt.Equal(bar5Fill), "got %s", first.OpenedAt)
	assert.True(t, first.ClosedAt.Equal(bar7Fill), "the normal intraday stop hit must close at bar 7's own timestamp, got %s", first.ClosedAt)
	firstCmp, err := first.RealizedPnL.Cmp(num.MustParseMoney("0", num.MustParseCurrency("USD")))
	require.NoError(t, err)
	assert.True(t, firstCmp > 0, "bought at 1.115, stopped out at 1.17: must be profitable, got %s", first.RealizedPnL)

	second := resp.Trades[1]
	assert.Equal(t, order.Long, second.Side)
	assert.True(t, second.OpenedAt.Equal(bar10Fill), "got %s", second.OpenedAt)
	assert.True(t, second.ClosedAt.Equal(bar11Fill), "the gap-through stop hit must close at bar 11's own timestamp, got %s", second.ClosedAt)
	secondCmp, err := second.RealizedPnL.Cmp(num.MustParseMoney("0", num.MustParseCurrency("USD")))
	require.NoError(t, err)
	assert.True(t, secondCmp < 0, "bought at 1.21, gapped out at 1.05: must be a loss, got %s", second.RealizedPnL)

	assert.Empty(t, resp.OpenTrades, "the gap-through stop must have closed the second episode's position, not left it open")
	assert.Empty(t, resp.Account.Positions())
}

// TestSMATrend_BreakoutReEntryChangesRealOutcome is issue #347's own
// required end-to-end proof that a non-default rule combination
// actually changes behavior through the real pipeline, not merely in
// an isolated unit test. It runs the identical fixture
// TestSMATrend_EndToEndRegression uses, with ReEntryRuleName:
// "breakout" instead of the default "fresh-cross".
//
// On the real fixture, the bar-7 stop exit seeds the breakout rule's
// since-exit high at bar 7's own High (1.26, oanda bid_h) — higher
// than every subsequent bar's Close through the end of the fixture
// (bars 8-11 close at 1.05, 1.20, 1.20, and 1.02). The default
// fresh-cross rule instead re-enters on bar 9 (a genuine cross back
// above the SMA) and is stopped out again by bar 11's gap-through
// open, producing two closed trades. The breakout rule, with the
// exact same market data, never re-enters at all: exactly one closed
// trade and a flat account at the end of the run.
func TestSMATrend_BreakoutReEntryChangesRealOutcome(t *testing.T) {
	resp, rec := runSMATrendFixtureWithConfig(t, smatrend.Config{
		SMAPeriod:           3,
		TrailingStopPercent: num.MustParseRate("0.10"),
		ReEntryRuleName:     "breakout",
	})

	// Exactly one enter-long signal: the bar-4 cross. No second
	// enter-long, unlike the default rule's bar-9 re-entry.
	signals := rec.kinds(journal.KindSignal)
	var enters int
	for _, s := range signals {
		if s.Signal.Values["action"] == "enter-long" {
			enters++
		}
		assert.Equal(t, "breakout", s.Signal.Values["reentry_rule"])
	}
	assert.Equal(t, 1, enters, "the breakout rule must never find a close above the post-exit high in this fixture")

	// Only the first episode's entry and stop-exit fill — two fills
	// total, not TestSMATrend_EndToEndRegression's four.
	require.Len(t, rec.kinds(journal.KindFill), 2)

	require.Len(t, resp.Trades, 1, "the breakout rule must leave only the first trailing-stop episode closed")
	trade := resp.Trades[0]
	tradeCmp, err := trade.RealizedPnL.Cmp(num.MustParseMoney("0", num.MustParseCurrency("USD")))
	require.NoError(t, err)
	assert.True(t, tradeCmp > 0, "bought at 1.115, stopped out at 1.17: must be profitable, got %s", trade.RealizedPnL)

	assert.Empty(t, resp.OpenTrades, "no re-entry ever occurred, so no open trade can exist at the end of the run")
	assert.Empty(t, resp.Account.Positions(), "the account must remain flat for the rest of the fixture")
}

// TestSMATrend_ProbationEntryBarIntrabarGapIsAKnownLimitation documents
// a known limitation of the "probation-trend" ExitRule raised in PR
// #350 review, using the exact same real EURUSD bars
// TestSMATrend_EndToEndRegression exercises: bar 5 (2024-01-12, the
// entry fill bar) has Low 1.10000 and SMA(bar3,4,5)=1.11667, so the
// intended probation stop — 1.11667 * (1-0.01) = 1.1055 — is breached
// intrabar on this very bar, yet its Close (1.15000) recovers back
// above the SMA.
//
// The playbook's own intent is that this should stop the position out
// intrabar. It does not, for a real sequencing reason rather than a
// bug in the stop math itself: Strategy only ever observes a fresh
// Long position — and therefore only ever computes and emits its
// first protective stop — on the bar *after* the entry decision (the
// fill bar itself), but backtest.Scheduler's own broker-side
// resting-order machinery has already resolved that fill bar's own
// intrabar price action *before* OnBar ever runs for it (see
// Strategy.OnBar's own doc comment). The AdjustStop intent bar 5's own
// OnBar call emits only becomes a resting order checked against
// intrabar price action starting bar 6 onward. Every other ExitRule
// in this package has the identical gap; "probation-trend" simply
// makes it matter most, since its whole purpose is a *tight* stop
// immediately after entry.
//
// Solving this needs an execution/pipeline capability this codebase
// does not have yet — submitting a protective stop atomically with
// the entry order itself, rather than one bar later — which is out of
// scope for issue #349's current implementation pass. This test
// exists so the gap is proven and regression-locked rather than
// silently assumed away: if a future change to Strategy or the
// pipeline closes this gap, this test's own assertion (that the
// position survives bar 5 unprotected) will fail and must be updated
// deliberately, not accidentally.
func TestSMATrend_ProbationEntryBarIntrabarGapIsAKnownLimitation(t *testing.T) {
	resp, _ := runSMATrendFixtureWithConfig(t, smatrend.Config{
		SMAPeriod:           3,
		ExitRuleName:        "probation-trend",
		ReEntryRuleName:     "fresh-cross",
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0.05"),
		TrailingStopPercent: num.MustParseRate("0.10"),
	})

	bar5Fill := time.Date(2024, time.January, 12, 22, 0, 0, 0, time.UTC)
	require.NotEmpty(t, resp.Trades, "the position must have closed eventually for this run to produce a trade at all")
	first := resp.Trades[0]
	assert.True(t, first.OpenedAt.Equal(bar5Fill), "got %s", first.OpenedAt)
	assert.True(t, first.ClosedAt.After(bar5Fill),
		"known limitation: the position must survive bar 5's own intrabar Low (1.10000), which breaches the 1.1055 probation stop that same bar's SMA implies — the resting stop is not active until the following bar")
}
