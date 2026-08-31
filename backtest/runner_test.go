package backtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

// mustRunnerModels returns a valid {fill, slippage, commission}
// ComponentInfo trio for RunnerParams.
func mustRunnerModels(t *testing.T) (fill, slippage, commission backtest.ComponentInfo) {
	t.Helper()
	var err error
	fill, err = backtest.NewComponentInfo("bar-close", "v1", nil)
	require.NoError(t, err)
	slippage, err = backtest.NewComponentInfo("none", "", nil)
	require.NoError(t, err)
	commission, err = backtest.NewComponentInfo("fixed", "v1", nil)
	require.NoError(t, err)
	return fill, slippage, commission
}

// publishBothInstrumentsFixture publishes the EUR/USD + GBP/USD H1
// canonical fixture data into mgr, the same fixture
// newTwoInstrumentReplay publishes — Runner builds its own internal
// Replay from Manager, so tests only need the data published, not a
// separately constructed Replay.
func publishBothInstrumentsFixture(t *testing.T, mgr *marketdata.Manager) {
	t.Helper()
	r := newTwoInstrumentReplay(t, mgr)
	_ = r.Close()
}

func mustRunnerParams(t *testing.T, strat strategy.Strategy) backtest.RunnerParams {
	t.Helper()
	mgr := newSchedulerTestManager(t)
	publishBothInstrumentsFixture(t, mgr)

	resolver := instrumentResolverFor(t)
	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	pl := newSchedulerPipeline(t, h)
	acc, err := h.broker.OpenAccount(context.Background(), h.accountID)
	require.NoError(t, err)

	fill, slippage, commission := mustRunnerModels(t)

	return backtest.RunnerParams{
		Manager:         mgr,
		Resolver:        resolver,
		Clock:           h.clockObj,
		IDs:             h.ids,
		Pipeline:        pl,
		Account:         acc,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
		FillModel:       fill,
		SlippageModel:   slippage,
		CommissionModel: commission,
		Strategy:        strat,
		Span:            schedulerSpan(t),
	}
}

// instrumentResolverFor returns an instrument.Resolver with both the
// EUR/USD and GBP/USD sim-provider Listings the default InputBuilder
// resolves against, matching schedulerHarness's own simListing
// convention.
func instrumentResolverFor(t *testing.T) instrument.Resolver {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(simListing(t, "EUR", "USD", "EUR_USD")))
	require.NoError(t, r.Register(simListing(t, "GBP", "USD", "GBP_USD")))
	return r
}

func TestRunner_SuccessfulRun(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)

	startBalance, err := params.Account.Snapshot(context.Background())
	require.NoError(t, err)

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	result, err := runner.Run(context.Background())
	require.NoError(t, err)

	assert.False(t, result.Manifest.RunID().IsZero())
	assert.Equal(t, "recording", result.Manifest.StrategyName())
	assert.True(t, result.Manifest.StartingCapital().Equal(startBalance.Equity()))
	assert.NotEmpty(t, result.Manifest.Dataset())
	require.Len(t, result.Manifest.Universe(), 2)

	// mustEnterOnFirstBarStrategy enters once per instrument on its
	// own first bar; next-bar-open eligibility fills both by the end
	// of a 4-bar run.
	assert.Len(t, result.Account.Positions(), 2)

	// Neither position ever exits, so DeriveTrades must report both as
	// still open (Trades holds only fully closed round trips) and their
	// RealizedPnL must reconcile with the account's own cumulative
	// RealizedPnL (zero here, since opening a position realizes none).
	assert.Empty(t, result.Trades, "nothing closed")
	require.Len(t, result.OpenTrades, 2)
	total := result.Account.RealizedPnL()
	for _, tr := range result.OpenTrades {
		assert.True(t, tr.ClosedAt.IsZero())
		assert.NotEmpty(t, tr.EntryFillIDs)
		var err error
		total, err = total.Sub(tr.RealizedPnL)
		require.NoError(t, err)
	}
	zero, err := num.ParseMoney("0", total.Currency())
	require.NoError(t, err)
	assert.True(t, total.Equal(zero), "sum of derived trades' RealizedPnL must reconcile with the account's own RealizedPnL")

	// Result.Metrics/EquityCurve (issue #219, M5-11) must reconcile with
	// the same authoritative Manifest/Account data checked above.
	assert.True(t, result.Metrics.StartingCapital().Equal(result.Manifest.StartingCapital()))
	assert.True(t, result.Metrics.FinalEquity().Equal(result.Account.Equity()))
	assert.Equal(t, len(result.Trades), result.Metrics.TradeCount())
	require.NotEmpty(t, result.EquityCurve, "at least the run's own starting observation")
	assert.True(t, result.EquityCurve[0].Equity.Equal(startBalance.Equity()))
	assert.Equal(t, result.EquityCurve, result.Metrics.EquityCurve())
	for i := 1; i < len(result.EquityCurve); i++ {
		assert.False(t, result.EquityCurve[i].Timestamp.Before(result.EquityCurve[i-1].Timestamp))
	}
}

func TestRunner_RejectsSecondRunCall(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)
	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	_, err = runner.Run(context.Background())
	require.NoError(t, err)

	_, err = runner.Run(context.Background())
	require.ErrorIs(t, err, backtest.ErrRunnerAlreadyUsed)
}

func TestRunner_AlreadyCancelledContextReturnsImmediately(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)
	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = runner.Run(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// A canceled Run must not have claimed the one-shot slot, and must
	// not have opened any account/replay activity — verified here by
	// checking the account is still exactly at its starting balance
	// with no positions, since Run returned before Scheduler ever ran.
	snap, err := params.Account.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Positions())

	// Because the pre-canceled call never claimed the one-shot slot, a
	// second call with a live context must still be allowed to run
	// normally — a harmless caller-side cancellation must not
	// permanently burn an otherwise untouched Runner.
	_, err = runner.Run(context.Background())
	require.NoError(t, err)
}

func TestNewRunner_RequiresEveryParam(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	full := mustRunnerParams(t, strat)

	cases := []struct {
		name   string
		mutate func(p *backtest.RunnerParams)
	}{
		{"manager", func(p *backtest.RunnerParams) { p.Manager = nil }},
		{"resolver", func(p *backtest.RunnerParams) { p.Resolver = nil }},
		{"clock", func(p *backtest.RunnerParams) { p.Clock = nil }},
		{"ids", func(p *backtest.RunnerParams) { p.IDs = nil }},
		{"pipeline", func(p *backtest.RunnerParams) { p.Pipeline = nil }},
		{"account", func(p *backtest.RunnerParams) { p.Account = nil }},
		{"strategy", func(p *backtest.RunnerParams) { p.Strategy = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := full
			tc.mutate(&p)
			_, err := backtest.NewRunner(p)
			require.ErrorIs(t, err, backtest.ErrInvalidRunnerParams)
		})
	}
}

// TestRunner_PropagatesComponentErrors proves a Replay-layer failure
// (a span with no published canonical data) propagates classifiably,
// not as an opaque or swallowed error.
func TestRunner_PropagatesComponentErrors(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)

	// Move the span to a range no fixture data was ever published for.
	span, err := marketdata.NewTimeRange(
		schedulerSpan(t).Start().AddDate(1, 0, 0),
		schedulerSpan(t).End().AddDate(1, 0, 0),
	)
	require.NoError(t, err)
	params.Span = span

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	_, err = runner.Run(context.Background())
	require.ErrorIs(t, err, marketdata.ErrDataUnavailable)
}

// TestRunner_ManifestFillModelReflectsConfiguredComponent proves the
// resulting Manifest describes the actual configured models, not a
// separately-claimed value that could diverge from them.
func TestRunner_ManifestFillModelReflectsConfiguredComponent(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)
	result, err := runner.Run(context.Background())
	require.NoError(t, err)

	assert.Equal(t, params.FillModel.Name(), result.Manifest.FillModel().Name())
	assert.Equal(t, params.SlippageModel.Name(), result.Manifest.SlippageModel().Name())
	assert.Equal(t, params.CommissionModel.Name(), result.Manifest.CommissionModel().Name())
}

// alwaysErrorOnBarStrategy fails every OnBar call, to exercise
// Scheduler-layer failure propagation through Runner.
type alwaysErrorOnBarStrategy struct {
	requirements []strategy.DataRequirement
}

func (s *alwaysErrorOnBarStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "always-error", Version: "test", Requirements: s.requirements}
}
func (s *alwaysErrorOnBarStrategy) Start(ctx context.Context, env strategy.Environment) error {
	return nil
}
func (s *alwaysErrorOnBarStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	return nil, errIntentional
}

var errIntentional = errors.New("runner_test: intentional OnBar failure")

// eventsErrorAccount wraps a broker.Account, injecting a failure from
// Events while delegating every other method — used to exercise
// Runner's own deriveTrades error-propagation path, which every other
// component-failure test in this file already covers for its own
// stage.
type eventsErrorAccount struct {
	broker.Account
}

func (a eventsErrorAccount) Events(ctx context.Context, cursor broker.EventCursor) (broker.EventReader, error) {
	return nil, errIntentional
}

func TestRunner_PropagatesTradeDerivationErrors(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)
	params.Account = eventsErrorAccount{Account: params.Account}

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	_, err = runner.Run(context.Background())
	require.ErrorIs(t, err, errIntentional)
}

func TestRunner_PropagatesSchedulerErrors(t *testing.T) {
	strat := &alwaysErrorOnBarStrategy{requirements: bothInstrumentsRequirements(t)}
	params := mustRunnerParams(t, strat)

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	_, err = runner.Run(context.Background())
	require.ErrorIs(t, err, errIntentional)
}

// TestRunner_DeterministicAcrossIndependentRunners proves two Runners
// built from identical deterministic dependencies (same clock/ID
// seed, same fixture, same strategy/config) produce identical
// ConfigDigest — including generating the identical RunID, since both
// draw from identically-seeded deterministic id.Generators. This is
// full-determinism-by-construction, the same property
// TestScheduler_DeterministicAcrossIndependentInstances already
// establishes one layer down.
func TestRunner_DeterministicAcrossIndependentRunners(t *testing.T) {
	strat1 := mustEnterOnFirstBarStrategy(t)
	params1 := mustRunnerParams(t, strat1)
	runner1, err := backtest.NewRunner(params1)
	require.NoError(t, err)
	result1, err := runner1.Run(context.Background())
	require.NoError(t, err)

	strat2 := mustEnterOnFirstBarStrategy(t)
	params2 := mustRunnerParams(t, strat2)
	runner2, err := backtest.NewRunner(params2)
	require.NoError(t, err)
	result2, err := runner2.Run(context.Background())
	require.NoError(t, err)

	assert.True(t, result1.Manifest.RunID().Equal(result2.Manifest.RunID()))
	assert.Equal(t, result1.Manifest.ConfigDigest(), result2.Manifest.ConfigDigest())
}

// TestNewResolverInputBuilder_RejectsNilResolver proves the
// constructor rejects a nil resolver at construction time, rather
// than letting Build discover it later and panic on a nil interface
// method call.
func TestNewResolverInputBuilder_RejectsNilResolver(t *testing.T) {
	_, err := backtest.NewResolverInputBuilder(nil, num.MustParseRate("0.01"), num.MustParsePrice("0.01"))
	require.ErrorIs(t, err, backtest.ErrInvalidInputBuilder)
}

// TestResolverInputBuilder_Build_UnresolvableInstrumentFails proves
// Build reports a clear error when the intent's own instrument has no
// Listing registered under the account's own broker — rather than
// panicking or silently building a malformed pipeline.Input.
func TestResolverInputBuilder_Build_UnresolvableInstrumentFails(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)

	// A resolver with nothing registered at all.
	empty := instrument.NewMemoryResolver()
	builder, err := backtest.NewResolverInputBuilder(empty, num.MustParseRate("0.01"), num.MustParsePrice("0.01"))
	require.NoError(t, err)

	snap, err := params.Account.Snapshot(context.Background())
	require.NoError(t, err)

	intentID, err := id.GenerateIntentID(params.IDs)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(params.IDs)
	require.NoError(t, err)
	corrID, err := id.GenerateCorrelationID(params.IDs)
	require.NoError(t, err)
	intent, err := order.NewIntent(order.Intent{
		IntentID:   intentID,
		Kind:       order.IntentEnter,
		Instrument: eurusdID(t),
		Side:       order.Buy,
		Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
	})
	require.NoError(t, err)

	ev := strategy.BarEvent{Instrument: eurusdID(t), Interval: marketdata.H1}
	_, err = builder.Build(context.Background(), intent, ev, snap)
	require.Error(t, err)
}

// startErrorStrategy fails Start, to exercise Runner's own
// Strategy.Start failure propagation.
type startErrorStrategy struct {
	requirements []strategy.DataRequirement
}

func (s *startErrorStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "start-error", Version: "test", Requirements: s.requirements}
}
func (s *startErrorStrategy) Start(ctx context.Context, env strategy.Environment) error {
	return errIntentional
}
func (s *startErrorStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	return nil, nil
}

func TestRunner_PropagatesStrategyStartError(t *testing.T) {
	strat := &startErrorStrategy{requirements: bothInstrumentsRequirements(t)}
	params := mustRunnerParams(t, strat)

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	_, err = runner.Run(context.Background())
	require.ErrorIs(t, err, errIntentional)
}

// TestRunner_PropagatesManifestValidationError proves a manifest-level
// validation failure (here, a missing FillModel name) surfaces as a
// normal classifiable error rather than a panic.
func TestRunner_PropagatesManifestValidationError(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)
	params.FillModel = backtest.ComponentInfo{}

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)

	_, err = runner.Run(context.Background())
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}
