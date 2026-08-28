package backtest_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	sim "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
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
	ref := event.Bar.Close
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
	mu      sync.Mutex
	calls   []strategy.BarEvent
	intents strategy.IntentFactory
	emit    func(strategy.IntentFactory, strategy.BarEvent) ([]order.Intent, error)
	onEach  func(strategy.BarEvent)
}

func (s *recordingStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "recording", Version: "test"}
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
	strat := &recordingStrategy{}
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
		strat := &recordingStrategy{}
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
	mu          sync.Mutex
	positions   []int
	instruments []instrument.ID
	intents     strategy.IntentFactory
	enterOnce   map[string]bool
}

func (s *viewObservingStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "view-observing", Version: "test"}
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
// batching-semantics guarantee: EUR/USD's own Enter fill at T0 (it is
// evaluated and submitted first, per canonical order) must not be
// visible to GBP/USD's View at the very same T0 — both calls in the
// first batch must observe zero positions.
func TestScheduler_ViewIsFrozenWithinABatch(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	strat := &viewObservingStrategy{}
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
	strat := &recordingStrategy{}
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
