package backtest_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	sim "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	"github.com/rustyeddy/trader/strategy"
)

// gbpusdListing returns a tradable GBP/USD Listing under provider
// "oanda", symbol "GBPUSD" — matching backtest/testdata's own
// synthetic GBPUSD raw fixture — for tests that need a genuine second
// instrument alongside EUR/USD to exercise Scheduler's cross-
// instrument same-timestamp ordering.
func gbpusdListing(t *testing.T) instrument.Listing {
	t.Helper()
	gbpusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: gbpusd,
		Provider:   "oanda",
		Symbol:     "GBPUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func gbpusdID(t *testing.T) instrument.ID {
	t.Helper()
	return gbpusdListing(t).InstrumentID()
}

// schedulerSpan is a narrow, gap-free H1 span covering both the
// EUR/USD and the synthetic GBP/USD raw fixture's four shared hourly
// timestamps.
func schedulerSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 8, 4, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// newSchedulerTestManager is newTestManager plus a registered GBP/USD
// listing, for tests needing both instruments.
func newSchedulerTestManager(t *testing.T) *marketdata.Manager {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t)))
	require.NoError(t, resolver.Register(gbpusdListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      copyFixtureRaw(t),
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	return mgr
}

func publishFixtureFor(t *testing.T, mgr *marketdata.Manager, instID instrument.ID, interval marketdata.Interval, span marketdata.TimeRange) {
	t.Helper()
	ctx := context.Background()
	plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: instID, Interval: interval, Range: span})
	require.NoError(t, err)
	if len(plan.Actions) == 0 {
		return
	}
	_, err = mgr.Build(ctx, plan)
	require.NoError(t, err)
}

// simListing returns the broker-side (provider "sim") Listing for
// base/quote — distinct from the marketdata-side "oanda" Listing:
// Scheduler's InputBuilder needs a Listing suitable for order
// submission against the sim broker, resolved independently of the
// marketdata Listing used to fetch bars, exactly as ADR-016 intends.
func simListing(t *testing.T, base, quote, symbol string) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency(base), num.MustParseCurrency(quote))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     symbol,
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// fixedPriceSource is a deterministic sim.FillPriceSource keyed by
// listing symbol.
type fixedPriceSource map[string]num.Price

func (f fixedPriceSource) Info() sim.ModelInfo {
	return sim.ModelInfo{Name: "fixedPriceSource", Version: "test"}
}

func (f fixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := f[listing.Symbol()]
	if !ok {
		return num.Price{}, fmt.Errorf("fixedPriceSource: no price for %s", listing.Symbol())
	}
	return p, nil
}

// schedulerHarness bundles one deterministic sim.Broker/Pipeline/
// clock/id-generator set for a Scheduler test.
type schedulerHarness struct {
	broker    *sim.Broker
	accountID id.AccountID
	clockObj  *clock.Simulated
	ids       *id.Generator
	eurusd    instrument.Listing
	gbpusd    instrument.Listing
}

func newSchedulerHarness(t *testing.T, start time.Time) schedulerHarness {
	t.Helper()
	c := clock.NewSimulated(start)
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	require.NoError(t, err)

	b, err := sim.NewBroker("sim", sim.Deps{
		Clock: c,
		IDs:   ids,
		Prices: fixedPriceSource{
			"EUR_USD": num.MustParsePrice("1.10000"),
			"GBP_USD": num.MustParsePrice("1.27000"),
		},
	}, sim.AccountConfig{
		AccountID:    accountID,
		StartingCash: num.MustParseMoney("100000", num.MustParseCurrency("USD")),
	})
	require.NoError(t, err)

	return schedulerHarness{
		broker:    b,
		accountID: accountID,
		clockObj:  c,
		ids:       ids,
		eurusd:    simListing(t, "EUR", "USD", "EUR_USD"),
		gbpusd:    simListing(t, "GBP", "USD", "GBP_USD"),
	}
}

func newSchedulerPipeline(t *testing.T, h schedulerHarness) *pipeline.Pipeline {
	t.Helper()
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clockObj, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine()
	require.NoError(t, err)
	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  h.broker,
		IDs:     h.ids,
	})
	require.NoError(t, err)
	return p
}

// fixedInputBuilder is a deterministic, test-only backtest.InputBuilder:
// a fixed risk fraction/adverse distance per run, listing resolution
// by instrument.ID, and the triggering bar's own Close as the
// reference price — exactly the mechanical/policy split #213's design
// review settled on.
type fixedInputBuilder struct {
	listings        map[string]instrument.Listing
	riskFraction    num.Rate
	adverseDistance num.Price
}

func (b fixedInputBuilder) Build(ctx context.Context, intent order.Intent, event strategy.BarEvent, snap account.Snapshot) (pipeline.Input, error) {
	listing, ok := b.listings[intent.Instrument.String()]
	if !ok {
		return pipeline.Input{}, fmt.Errorf("fixedInputBuilder: no listing for %s", intent.Instrument)
	}
	adverse := b.adverseDistance
	// event is the bar that made intent eligible (its own requirement's
	// next bar after the one that triggered it), per Scheduler's
	// next-bar-open fill semantics — Open is the honest reference price
	// here, not Close.
	ref := event.Bar.Open
	return pipeline.Input{
		Intent:          intent,
		Listing:         listing,
		Account:         snap,
		RiskFraction:    b.riskFraction,
		AdverseDistance: &adverse,
		ReferencePrice:  &ref,
	}, nil
}

// recordingStrategy is a strategy.Strategy fake that records every
// OnBar call it receives, in the order Scheduler delivers them, and
// optionally emits an intent per call via emit. onEach, if set, runs
// after recording each call — tests use it to cancel a context mid-run
// to exercise Scheduler's cancellation behavior.
type recordingStrategy struct {
	mu           sync.Mutex
	calls        []strategy.BarEvent
	intents      strategy.IntentFactory
	emit         func(strategy.IntentFactory, strategy.BarEvent) ([]order.Intent, error)
	onEach       func(strategy.BarEvent)
	requirements []strategy.DataRequirement
}

func (s *recordingStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "recording", Version: "test", Requirements: s.requirements}
}

func (s *recordingStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

func (s *recordingStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	s.mu.Lock()
	s.calls = append(s.calls, ev)
	s.mu.Unlock()

	if s.onEach != nil {
		s.onEach(ev)
	}
	if s.emit == nil {
		return nil, nil
	}
	return s.emit(s.intents, ev)
}

func (s *recordingStrategy) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func mustEnterOnFirstBarStrategy(t *testing.T) *recordingStrategy {
	t.Helper()
	entered := make(map[string]bool)
	var mu sync.Mutex
	return &recordingStrategy{
		requirements: bothInstrumentsRequirements(t),
		emit: func(f strategy.IntentFactory, ev strategy.BarEvent) ([]order.Intent, error) {
			mu.Lock()
			already := entered[ev.Instrument.String()]
			entered[ev.Instrument.String()] = true
			mu.Unlock()
			if already {
				return nil, nil
			}
			in, err := f.Enter(ev.Instrument, order.Buy)
			if err != nil {
				return nil, err
			}
			return []order.Intent{in}, nil
		},
	}
}

func newSchedulerDeps(t *testing.T, replay *backtest.Replay, strat strategy.Strategy, h schedulerHarness) backtest.SchedulerDeps {
	t.Helper()
	ctx := context.Background()
	acc, err := h.broker.OpenAccount(ctx, h.accountID)
	require.NoError(t, err)
	marketObserver, ok := acc.(backtest.MarketObserver)
	require.True(t, ok, "sim account must implement backtest.MarketObserver")

	ids2 := id.NewGenerator(h.clockObj, id.NewDeterministic(3, 4))
	factory := strategy.NewIntentFactory(h.clockObj, ids2, id.Source("scheduler-test"))
	require.NoError(t, strat.Start(ctx, strategy.Environment{
		Clock:   h.clockObj,
		Intents: factory,
		Logger:  logging.Discard(),
	}))

	return backtest.SchedulerDeps{
		Replay:   replay,
		Strategy: strat,
		Clock:    h.clockObj,
		Pipeline: newSchedulerPipeline(t, h),
		Account:  acc,
		Builder: fixedInputBuilder{
			listings: map[string]instrument.Listing{
				h.eurusd.InstrumentID().String(): h.eurusd,
				h.gbpusd.InstrumentID().String(): h.gbpusd,
			},
			riskFraction:    num.MustParseRate("0.01"),
			adverseDistance: num.MustParsePrice("0.01000"),
		},
		Journal:        journal.Discard(),
		RunID:          mustSchedulerRunID(t, ids2),
		MarketObserver: marketObserver,
	}
}

func mustSchedulerRunID(t *testing.T, gen *id.Generator) id.RunID {
	t.Helper()
	runID, err := id.GenerateRunID(gen)
	require.NoError(t, err)
	return runID
}

// bothInstrumentsRequirements returns the EUR/USD H1 + GBP/USD H1
// DataRequirement pair matching newTwoInstrumentReplay's own Replay
// construction — the declared set most scheduler tests give their
// strategy fake's Describe(), now that Scheduler validates every
// Replay event's own (instrument, interval) against it.
func bothInstrumentsRequirements(t *testing.T) []strategy.DataRequirement {
	t.Helper()
	return []strategy.DataRequirement{
		{Instrument: eurusdID(t), Interval: marketdata.H1},
		{Instrument: gbpusdID(t), Interval: marketdata.H1},
	}
}

func newTwoInstrumentReplay(t *testing.T, mgr *marketdata.Manager) *backtest.Replay {
	t.Helper()
	span := schedulerSpan(t)
	publishFixtureFor(t, mgr, eurusdID(t), marketdata.H1, span)
	publishFixtureFor(t, mgr, gbpusdID(t), marketdata.H1, span)

	r, err := backtest.NewReplay(context.Background(), mgr,
		[]strategy.DataRequirement{
			{Instrument: eurusdID(t), Interval: marketdata.H1},
			{Instrument: gbpusdID(t), Interval: marketdata.H1},
		}, span)
	require.NoError(t, err)
	return r
}

// TestScheduler_SameTimestampMultiInstrumentOrdering proves #213's own
// acceptance criteria: at every shared timestamp, Scheduler invokes
// OnBar for every instrument's bar, in Replay's own canonical
// (timestamp, instrument ID, interval) order — fx:EUR/USD sorts before
// fx:GBP/USD — never in some other, incidental order.
func TestScheduler_SameTimestampMultiInstrumentOrdering(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &recordingStrategy{requirements: bothInstrumentsRequirements(t)}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.Len(t, strat.calls, 8, "4 shared H1 timestamps x 2 instruments")
	for i := 0; i < len(strat.calls); i += 2 {
		eur, gbp := strat.calls[i], strat.calls[i+1]
		require.True(t, eur.Instrument.Equal(eurusdID(t)))
		require.True(t, gbp.Instrument.Equal(gbpusdID(t)))
		require.True(t, eur.Bar.Time.Equal(gbp.Bar.Time), "both calls in a pair must share one timestamp")
	}
	for i := 1; i < len(strat.calls); i++ {
		require.False(t, strat.calls[i].Bar.Time.Before(strat.calls[i-1].Bar.Time), "timestamps must never move backward across calls")
	}
}

// TestScheduler_DeterministicAcrossIndependentInstances proves #213's
// "same inputs yield identical event ordering" acceptance criterion:
// two independently constructed Schedulers, built from identical
// fixtures/deps shapes, produce an identical OnBar call sequence.
func TestScheduler_DeterministicAcrossIndependentInstances(t *testing.T) {
	run := func() []strategy.BarEvent {
		mgr := newSchedulerTestManager(t)
		replay := newTwoInstrumentReplay(t, mgr)
		t.Cleanup(func() { _ = replay.Close() })

		h := newSchedulerHarness(t, schedulerSpan(t).Start())
		strat := &recordingStrategy{requirements: bothInstrumentsRequirements(t)}
		deps := newSchedulerDeps(t, replay, strat, h)

		sched, err := backtest.NewScheduler(deps)
		require.NoError(t, err)
		require.NoError(t, sched.Run(context.Background()))
		return strat.calls
	}

	first := run()
	second := run()
	require.Equal(t, first, second)
}

// viewObservingStrategy records the account-position count View exposes
// at the moment each OnBar call runs, alongside the triggering
// instrument.
type viewObservingStrategy struct {
	mu           sync.Mutex
	positions    []int
	instruments  []instrument.ID
	intents      strategy.IntentFactory
	enterOnce    map[string]bool
	requirements []strategy.DataRequirement
}

func (s *viewObservingStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "view-observing", Version: "test", Requirements: s.requirements}
}

func (s *viewObservingStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	s.enterOnce = make(map[string]bool)
	return nil
}

func (s *viewObservingStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	s.mu.Lock()
	s.positions = append(s.positions, len(view.Account().Positions()))
	s.instruments = append(s.instruments, ev.Instrument)
	already := s.enterOnce[ev.Instrument.String()]
	s.enterOnce[ev.Instrument.String()] = true
	s.mu.Unlock()

	if already {
		return nil, nil
	}
	in, err := s.intents.Enter(ev.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// TestScheduler_ViewIsFrozenWithinABatch is the sharp version of the
// batching-semantics guarantee: both EUR/USD's and GBP/USD's own View
// at the very first batch (T0) must observe zero positions — no fill
// from either instrument's own intent has happened yet (next-bar-open
// eligibility means nothing fills until T1 in any case), and no T0
// call's View reflects another T0 call's account effects even once
// fills do start happening at later batches.
func TestScheduler_ViewIsFrozenWithinABatch(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &viewObservingStrategy{requirements: bothInstrumentsRequirements(t)}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.GreaterOrEqual(t, len(strat.positions), 2)
	require.Equal(t, 0, strat.positions[0], "EUR/USD's own View at T0 sees no positions yet")
	require.Equal(t, 0, strat.positions[1], "GBP/USD's View at the same T0 must not see EUR/USD's own T0 fill")
}

// TestScheduler_RunPropagatesCancellation proves Run stops promptly on
// a canceled context, leaving whatever was already processed in place
// (a canceled Run is a defined partial run, not a rollback).
func TestScheduler_RunPropagatesCancellation(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	ctx, cancel := context.WithCancel(context.Background())
	strat := &recordingStrategy{
		requirements: bothInstrumentsRequirements(t),
		onEach: func(ev strategy.BarEvent) {
			cancel()
		},
	}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)

	err = sched.Run(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, strat.callCount(), "Run must stop before the next OnBar call once ctx is canceled")
}

// TestScheduler_AlreadyCancelledContextStopsImmediately proves Run
// checks ctx before doing any work at all.
func TestScheduler_AlreadyCancelledContextStopsImmediately(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &recordingStrategy{requirements: bothInstrumentsRequirements(t)}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = sched.Run(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, strat.callCount())
}

// TestScheduler_RiskRejectionDoesNotAbortRun proves a risk rejection
// (pipeline.ErrRejected) is a normal outcome, not a Run failure:
// Scheduler continues processing the remaining batches.
func TestScheduler_RiskRejectionDoesNotAbortRun(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())

	// alwaysRejectRule rejects every proposal, so every submitted
	// intent hits pipeline.ErrRejected.
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clockObj, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine(alwaysRejectRule{})
	require.NoError(t, err)
	pl, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  h.broker,
		IDs:     h.ids,
	})
	require.NoError(t, err)

	strat := mustEnterOnFirstBarStrategy(t)
	deps := newSchedulerDeps(t, replay, strat, h)
	deps.Pipeline = pl

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()), "a risk rejection must not abort Run")

	require.Len(t, strat.calls, 8, "Run must still process every remaining bar after a rejection")

	acc, err := h.broker.OpenAccount(context.Background(), h.accountID)
	require.NoError(t, err)
	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err)
	require.Empty(t, snap.Positions(), "every proposal was rejected; nothing should have filled")
}

// alwaysRejectRule is a minimal risk.Rule that always reports a
// violation, used to exercise Scheduler's risk-rejection handling
// without depending on any concrete production Rule's own numeric
// thresholds (mirrors pipeline_test.go's identical rejectingRule).
type alwaysRejectRule struct{}

func (alwaysRejectRule) Name() string { return "always_reject" }
func (alwaysRejectRule) Evaluate(ctx context.Context, in risk.Input) (risk.RuleResult, error) {
	return risk.RuleResult{Violations: []risk.Violation{{Message: "test: always rejects"}}}, nil
}

// TestNewScheduler_RequiresEveryDep proves NewScheduler validates its
// dependencies rather than deferring to a nil-pointer panic at Run
// time.
func TestNewScheduler_RequiresEveryDep(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &recordingStrategy{}
	full := newSchedulerDeps(t, replay, strat, h)

	cases := []struct {
		name   string
		mutate func(d *backtest.SchedulerDeps)
	}{
		{"replay", func(d *backtest.SchedulerDeps) { d.Replay = nil }},
		{"strategy", func(d *backtest.SchedulerDeps) { d.Strategy = nil }},
		{"clock", func(d *backtest.SchedulerDeps) { d.Clock = nil }},
		{"pipeline", func(d *backtest.SchedulerDeps) { d.Pipeline = nil }},
		{"account", func(d *backtest.SchedulerDeps) { d.Account = nil }},
		{"builder", func(d *backtest.SchedulerDeps) { d.Builder = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := full
			tc.mutate(&d)
			_, err := backtest.NewScheduler(d)
			require.ErrorIs(t, err, backtest.ErrInvalidSchedulerDeps)
		})
	}
}

// erroringInputBuilder always fails, to exercise Scheduler's handling
// of an InputBuilder.Build failure.
// failOnKindRecorder wraps journal.Discard(), failing only the first
// Record call for the given Kind and succeeding for everything else —
// used to prove a mid-drain journal failure does not leave Scheduler's
// own watermark/fill bookkeeping claiming an event was recorded when
// it was not (issue #236 review).
type failOnKindRecorder struct {
	journal.Recorder
	failOn journal.Kind
	failed bool
}

func (f *failOnKindRecorder) Record(ctx context.Context, rec journal.Record) error {
	if !f.failed && rec.Kind == f.failOn {
		f.failed = true
		return errIntentional
	}
	return f.Recorder.Record(ctx, rec)
}

// TestScheduler_JournalFailureDuringDrainDoesNotAdvanceWatermarkOrFills
// proves drainAndJournal only commits an event to lastBrokerSeq/fills
// once it has actually been journaled successfully: if the Fill entry
// itself fails to record, Run aborts and Fills() must not report a
// fill Scheduler never durably recorded.
func TestScheduler_JournalFailureDuringDrainDoesNotAdvanceWatermarkOrFills(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := mustEnterOnFirstBarStrategy(t)
	deps := newSchedulerDeps(t, replay, strat, h)
	deps.Journal = &failOnKindRecorder{Recorder: journal.Discard(), failOn: journal.KindFill}

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)

	err = sched.Run(context.Background())
	require.ErrorIs(t, err, errIntentional)
	assert.Empty(t, sched.Fills(), "a fill must not be collected unless its journal record succeeded")
}

// TestScheduler_EquityCurveOnePointPerBatchInChronologicalOrder proves
// EquityCurve retains exactly one authoritative, mark-to-market
// observation per batch, in the same chronological order Replay itself
// yields batches (issue #219, M5-11), and that the final observation
// matches the account's own final snapshot — the same post-flush
// snapshot Phase 3 already takes for the strategy's own View, not a
// second one.
func TestScheduler_EquityCurveOnePointPerBatchInChronologicalOrder(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := mustEnterOnFirstBarStrategy(t)
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	curve := sched.EquityCurve()
	require.Len(t, curve, 4, "one point per batch: the fixture's 4 shared hourly timestamps")

	for i := 1; i < len(curve); i++ {
		assert.True(t, curve[i].Timestamp.After(curve[i-1].Timestamp))
	}

	finalSnap, err := deps.Account.Snapshot(context.Background())
	require.NoError(t, err)
	assert.True(t, curve[len(curve)-1].Equity.Equal(finalSnap.Equity()))
}

// TestScheduler_EquityCurveIsGenuinelyMarkToMarketBetweenFills proves
// ObserveMark actually revalues open positions from each batch's own
// bar close, not merely retains whatever mark a fill last set (issue
// #219 review): mustEnterOnFirstBarStrategy fills both instruments at
// batch 1 (next-bar-open eligibility) and never trades again, yet the
// fixture's own EUR/USD and GBP/USD closes keep rising through batches
// 2 and 3 with no further fills — so if marks were stale, equity would
// stay flat from batch 1 onward. It must not.
func TestScheduler_EquityCurveIsGenuinelyMarkToMarketBetweenFills(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := mustEnterOnFirstBarStrategy(t)
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	curve := sched.EquityCurve()
	require.Len(t, curve, 4)

	// Batch 0: both positions still flat (queued, not yet eligible) —
	// no unrealized P&L to speak of.
	// Batches 1-3: both fills already happened by batch 1's own flush;
	// EUR/USD and GBP/USD both keep rising through batch 3 with no
	// further fills, so equity must strictly increase each batch as
	// ObserveMark revalues the open positions from each new bar close.
	for i := 2; i <= 3; i++ {
		cmp, err := curve[i].Equity.Cmp(curve[i-1].Equity)
		require.NoError(t, err)
		assert.Greater(t, cmp, 0, "batch %d equity must exceed batch %d: marks must move with the fixture's own rising closes even without a new fill", i, i-1)
	}

	finalSnap, err := deps.Account.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Len(t, finalSnap.Positions(), 2, "no further fills occurred after batch 1 — the equity change is from revaluation alone")
}

type erroringInputBuilder struct{}

func (erroringInputBuilder) Build(ctx context.Context, intent order.Intent, event strategy.BarEvent, snap account.Snapshot) (pipeline.Input, error) {
	return pipeline.Input{}, fmt.Errorf("erroringInputBuilder: deliberate failure")
}

// TestScheduler_BuilderErrorAbortsRun proves an InputBuilder.Build
// failure is fatal to Run — unlike a risk rejection, it signals a
// broken translation, not a normal pipeline outcome.
func TestScheduler_BuilderErrorAbortsRun(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := mustEnterOnFirstBarStrategy(t)
	deps := newSchedulerDeps(t, replay, strat, h)
	deps.Builder = erroringInputBuilder{}

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)

	err = sched.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "erroringInputBuilder")
}

// batchBarsObservingStrategy records, for each OnBar call, whether its
// View exposes the backtest.BatchBars capability and, if so, how many
// bars and which instruments are visible in the current batch.
type batchBarsObservingStrategy struct {
	mu           sync.Mutex
	sawCapable   []bool
	batchSizes   []int
	instruments  [][]string
	requirements []strategy.DataRequirement
}

func (s *batchBarsObservingStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "batch-bars-observing", Version: "test", Requirements: s.requirements}
}

func (s *batchBarsObservingStrategy) Start(ctx context.Context, env strategy.Environment) error {
	return nil
}

func (s *batchBarsObservingStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	bb, ok := view.(backtest.BatchBars)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sawCapable = append(s.sawCapable, ok)
	if ok {
		bars := bb.Bars()
		s.batchSizes = append(s.batchSizes, len(bars))
		var insts []string
		for _, b := range bars {
			insts = append(insts, b.Instrument.String())
		}
		s.instruments = append(s.instruments, insts)
	} else {
		s.batchSizes = append(s.batchSizes, 0)
		s.instruments = append(s.instruments, nil)
	}
	return nil, nil
}

// TestScheduler_BatchBarsMakesCrossInstrumentVisibilityReal is issue
// #213's second review round: View must expose every bar in the
// current same-timestamp batch, not only the one that triggered a
// given OnBar call — so EURUSD's own callback can see GBPUSD's bar at
// the identical T, and vice versa, even though only one of them
// triggered any particular call.
func TestScheduler_BatchBarsMakesCrossInstrumentVisibilityReal(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &batchBarsObservingStrategy{requirements: bothInstrumentsRequirements(t)}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.Len(t, strat.sawCapable, 8)
	for i, capable := range strat.sawCapable {
		require.True(t, capable, "View must implement backtest.BatchBars")
		require.Equal(t, 2, strat.batchSizes[i], "both instruments' bars at this timestamp must be visible")
		require.Contains(t, strat.instruments[i], eurusdID(t).String())
		require.Contains(t, strat.instruments[i], gbpusdID(t).String())
	}
}

// enterIfFlatStrategy emits an Enter intent for its own event's
// instrument on every call where View.Account() shows no existing
// position for it — so, unlike recordingStrategy's one-shot emit
// fixtures, it keeps retrying every bar until a fill has actually
// landed, making it suitable for testing how long that takes to
// happen under warm-up gating and next-bar-open fill eligibility.
type enterIfFlatStrategy struct {
	requirements []strategy.DataRequirement
	intents      strategy.IntentFactory
}

func (s *enterIfFlatStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "enter-if-flat", Version: "test", Requirements: s.requirements}
}

func (s *enterIfFlatStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

func (s *enterIfFlatStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	for _, p := range view.Account().Positions() {
		if p.Listing.InstrumentID().Equal(ev.Instrument) {
			return nil, nil
		}
	}
	in, err := s.intents.Enter(ev.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// TestScheduler_NextBarOpenFillEligibility proves #214's fill-timing
// rule directly: an intent emitted from bar N is not submitted during
// bar N's own batch — it is only submitted once bar N+1 (the same
// requirement's own next bar) begins processing, before that bar's own
// OnBar calls run.
func TestScheduler_NextBarOpenFillEligibility(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &viewObservingStrategy{requirements: bothInstrumentsRequirements(t)}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.Len(t, strat.positions, 8)
	// Batch 0 (calls 0,1): EUR/USD then GBP/USD, both still flat —
	// their own bar-0 intents were only just queued, not yet eligible.
	require.Equal(t, 0, strat.positions[0])
	require.Equal(t, 0, strat.positions[1])
	// Batch 1 (calls 2,3): bar 0's queued intents for both instruments
	// were flushed at the start of batch 1, before any batch-1 OnBar
	// call — so both calls in batch 1 already see both fills.
	require.Equal(t, 2, strat.positions[2])
	require.Equal(t, 2, strat.positions[3])
}

// enterOnNthCallStrategy emits an Enter intent for its own event's
// instrument only on that instrument's Nth OnBar call (1-indexed),
// letting a test target, for example, the very last bar of a fixture's
// replayed data.
type enterOnNthCallStrategy struct {
	mu           sync.Mutex
	n            int
	counts       map[string]int
	requirements []strategy.DataRequirement
	intents      strategy.IntentFactory
}

func (s *enterOnNthCallStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "enter-on-nth-call", Version: "test", Requirements: s.requirements}
}

func (s *enterOnNthCallStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	s.counts = make(map[string]int)
	return nil
}

func (s *enterOnNthCallStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	s.mu.Lock()
	s.counts[ev.Instrument.String()]++
	count := s.counts[ev.Instrument.String()]
	s.mu.Unlock()

	if count != s.n {
		return nil, nil
	}
	in, err := s.intents.Enter(ev.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}

// TestScheduler_LastBarIntentsAreNeverSubmitted proves the documented
// boundary consequence of next-bar-open semantics: an intent emitted
// from the very last bar of a requirement's replayed data has no next
// bar to become eligible against, and Run completes having never
// submitted it.
func TestScheduler_LastBarIntentsAreNeverSubmitted(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &enterOnNthCallStrategy{n: 4, requirements: bothInstrumentsRequirements(t)} // schedulerSpan yields exactly 4 H1 bars per instrument
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	acc, err := h.broker.OpenAccount(context.Background(), h.accountID)
	require.NoError(t, err)
	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err)
	require.Empty(t, snap.Positions(), "an intent from the last bar has no next bar to become eligible against")
}

// recordingInputBuilder wraps another InputBuilder, recording the
// event.Bar.Time each Build call received.
type recordingInputBuilder struct {
	inner     backtest.InputBuilder
	clock     clock.Clock
	mu        sync.Mutex
	eventTime []time.Time
	clockNow  []time.Time
}

func (b *recordingInputBuilder) Build(ctx context.Context, intent order.Intent, event strategy.BarEvent, snap account.Snapshot) (pipeline.Input, error) {
	b.mu.Lock()
	b.eventTime = append(b.eventTime, event.Bar.Time)
	if b.clock != nil {
		b.clockNow = append(b.clockNow, b.clock.Now())
	}
	b.mu.Unlock()
	return b.inner.Build(ctx, intent, event, snap)
}

// TestScheduler_InputBuilderReceivesEligibilityEventNotTriggeringEvent
// proves InputBuilder.Build's event parameter is the bar that made the
// intent eligible (the triggering requirement's own next bar), not the
// bar whose OnBar call actually emitted the intent.
func TestScheduler_InputBuilderReceivesEligibilityEventNotTriggeringEvent(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := mustEnterOnFirstBarStrategy(t) // triggers on each instrument's own bar 0
	deps := newSchedulerDeps(t, replay, strat, h)
	rec := &recordingInputBuilder{inner: deps.Builder}
	deps.Builder = rec

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.Len(t, rec.eventTime, 2, "one Build call per instrument, once each queued intent is flushed")
	triggeringBarTime := schedulerSpan(t).Start()
	for _, et := range rec.eventTime {
		require.False(t, et.Equal(triggeringBarTime),
			"Build must receive the eligibility bar (bar 1), not the triggering bar (bar 0)")
	}
}

// TestScheduler_ClockReflectsEligibilityBatchDuringFlush is the
// regression Rusty's #214 review asked for: flushing (submitting)
// intents that just became eligible at T's open must happen *after*
// clock.AdvanceTo(T), not before — so InputBuilder, Pipeline, and the
// broker/account all observe T, the batch whose open just made the
// submission eligible, not the previous batch's own time.
func TestScheduler_ClockReflectsEligibilityBatchDuringFlush(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := mustEnterOnFirstBarStrategy(t) // triggers on each instrument's own bar 0
	deps := newSchedulerDeps(t, replay, strat, h)
	rec := &recordingInputBuilder{inner: deps.Builder, clock: h.clockObj}
	deps.Builder = rec

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.Len(t, rec.clockNow, 2, "one flush-time Build call per instrument")
	bar1Time := schedulerSpan(t).Start().Add(time.Hour) // the eligibility batch (bar 1), not bar 0
	for i, now := range rec.clockNow {
		require.True(t, now.Equal(bar1Time),
			"Clock.Now() during flush must equal the eligibility batch's own timestamp (%s), got %s at call %d", bar1Time, now, i)
		require.True(t, now.Equal(rec.eventTime[i]), "Clock.Now() during flush must agree with the eligibility event's own Bar.Time")
	}
}

// TestScheduler_WarmupIsRunWideAcrossDeclaredRequirements proves #214's
// run-wide warm-up rule: EUR/USD's own WarmupBars is satisfied first
// (WarmupBars: 0), but its intents must still be discarded until
// GBP/USD's own higher WarmupBars also clears — one declared
// requirement warming up first does not entitle its own intents
// through early, and warm-up readiness applies uniformly to the whole
// batch that clears it, not just the event that happened to clear it.
// See the assertion below for the exact resulting fill count this
// fixture produces once warm-up and next-bar-open eligibility are both
// accounted for.
func TestScheduler_WarmupIsRunWideAcrossDeclaredRequirements(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &enterIfFlatStrategy{
		requirements: []strategy.DataRequirement{
			{Instrument: eurusdID(t), Interval: marketdata.H1, WarmupBars: 0},
			{Instrument: gbpusdID(t), Interval: marketdata.H1, WarmupBars: 2},
		},
	}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	// GBP/USD needs barsSeen > 2: bar 3 is the first bar where every
	// declared requirement's own bars-seen count exceeds its own
	// WarmupBars (EUR/USD's own WarmupBars of 0 was already satisfied
	// from bar 1 onward). Warm-up readiness is decided once for the
	// whole bar-3 batch, after both instruments' own bars-seen counts
	// for bar 3 have been incremented — so both EUR/USD's and GBP/USD's
	// own bar-3 intents are queued together, uniformly, not just
	// GBP/USD's. Both are then flushed at bar 4 (each requirement's own
	// next bar) — exactly one fill each.
	acc, err := h.broker.OpenAccount(context.Background(), h.accountID)
	require.NoError(t, err)
	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err)

	positions := snap.Positions()
	require.Len(t, positions, 2, "bar 3 is the first batch where run-wide warm-up clears, and it clears uniformly for the whole batch")
	instIDs := []string{positions[0].Listing.InstrumentID().String(), positions[1].Listing.InstrumentID().String()}
	require.Contains(t, instIDs, eurusdID(t).String())
	require.Contains(t, instIDs, gbpusdID(t).String())
}

// historyProbeStrategy records, for its own tracked (instID, interval),
// how many bars strategy.History exposes on each call, and stashes the
// View received on its second call for a later, out-of-band re-query.
type historyProbeStrategy struct {
	instID   instrument.ID
	interval marketdata.Interval
	// otherRequirements declares any additional DataRequirements
	// Replay will produce events for, beyond (instID, interval) — every
	// such event must still be declared for Scheduler's own
	// undeclared-event validation to pass.
	otherRequirements []strategy.DataRequirement

	mu       sync.Mutex
	calls    int
	counts   []int
	oks      []bool
	retained strategy.View
}

func (s *historyProbeStrategy) Describe() strategy.Descriptor {
	reqs := append([]strategy.DataRequirement{{Instrument: s.instID, Interval: s.interval}}, s.otherRequirements...)
	return strategy.Descriptor{Name: "history-probe", Version: "test", Requirements: reqs}
}

func (s *historyProbeStrategy) Start(ctx context.Context, env strategy.Environment) error { return nil }

func (s *historyProbeStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	if !ev.Instrument.Equal(s.instID) || ev.Interval != s.interval {
		return nil, nil
	}

	hv, ok := view.(strategy.History)
	var bars []marketdata.Bar
	var hok bool
	if ok {
		bars, hok = hv.HistoryBars(s.instID, s.interval, 10)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.counts = append(s.counts, len(bars))
	s.oks = append(s.oks, hok)
	if s.calls == 2 {
		s.retained = view
	}
	return nil, nil
}

// TestScheduler_HistoryExcludesCurrentBatchAndGrowsEachBar proves
// History returns strictly-prior bars only, growing by exactly one per
// call, and that a View retained from an earlier call never gains
// visibility into bars that arrived after it was constructed — even
// though the underlying buffer keeps growing.
func TestScheduler_HistoryExcludesCurrentBatchAndGrowsEachBar(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	probe := &historyProbeStrategy{instID: eurusdID(t), interval: marketdata.H1, otherRequirements: []strategy.DataRequirement{{Instrument: gbpusdID(t), Interval: marketdata.H1}}}
	deps := newSchedulerDeps(t, replay, probe, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.Equal(t, []int{0, 1, 2, 3}, probe.counts)
	for _, ok := range probe.oks {
		require.True(t, ok, "EUR/USD H1 is a declared requirement")
	}

	require.NotNil(t, probe.retained)
	hv, ok := probe.retained.(strategy.History)
	require.True(t, ok)
	bars, ok := hv.HistoryBars(eurusdID(t), marketdata.H1, 10)
	require.True(t, ok)
	require.Len(t, bars, 1, "a retained View's own visibility cutoff is permanent, regardless of replay having since advanced")
}

// TestScheduler_HistoryReportsFalseForUndeclaredRequirement proves
// History never makes arbitrary replayed data queryable merely because
// the runtime happens to have it — only a strategy's own declared
// DataRequirements are answerable.
func TestScheduler_HistoryReportsFalseForUndeclaredRequirement(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	// otherRequirements must match what Replay actually produces
	// (GBP/USD H1), since Scheduler now rejects any replayed event for
	// an undeclared requirement outright. The "undeclared" case this
	// test targets is therefore a pair neither declared nor ever
	// replayed — EUR/USD D1 — not GBP/USD H1.
	probe := &historyProbeStrategy{instID: eurusdID(t), interval: marketdata.H1, otherRequirements: []strategy.DataRequirement{{Instrument: gbpusdID(t), Interval: marketdata.H1}}}
	deps := newSchedulerDeps(t, replay, probe, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))

	require.NotNil(t, probe.retained)
	hv, ok := probe.retained.(strategy.History)
	require.True(t, ok)

	_, ok = hv.HistoryBars(eurusdID(t), marketdata.D1, 5)
	require.False(t, ok, "EUR/USD D1 was never declared by history-probe's own Descriptor.Requirements")
}

// TestNewScheduler_RejectsDuplicateDeclaredRequirement proves
// NewScheduler validates Strategy.Describe().Requirements rather than
// silently letting a duplicate (instrument, interval) pair produce an
// ambiguous warm-up/History key.
func TestNewScheduler_RejectsDuplicateDeclaredRequirement(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &recordingStrategy{
		requirements: []strategy.DataRequirement{
			{Instrument: eurusdID(t), Interval: marketdata.H1, WarmupBars: 0},
			{Instrument: eurusdID(t), Interval: marketdata.H1, WarmupBars: 1},
		},
	}
	deps := newSchedulerDeps(t, replay, strat, h)

	_, err := backtest.NewScheduler(deps)
	require.ErrorIs(t, err, backtest.ErrInvalidSchedulerDeps)
}

// TestScheduler_RunRejectsEventForUndeclaredRequirement is Rusty's own
// #214 review follow-up: Replay producing an event for an
// (instrument, interval) Strategy never declared must fail Run
// outright — such an event would otherwise reach OnBar and be able to
// emit tradable intents while remaining invisible to History/warm-up
// bookkeeping, which key strictly off the declared set.
func TestScheduler_RunRejectsEventForUndeclaredRequirement(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr) // produces EUR/USD H1 and GBP/USD H1
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	// Declares nothing at all — every replayed event is undeclared.
	strat := &recordingStrategy{}
	deps := newSchedulerDeps(t, replay, strat, h)

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err, "NewScheduler itself does not cross-validate against Replay")

	err = sched.Run(context.Background())
	require.ErrorIs(t, err, backtest.ErrInvalidSchedulerDeps)
	require.Equal(t, 0, strat.callCount(), "an undeclared event must be rejected before OnBar is ever called for it")
}
