// Package emacross_test proves the EMA crossover strategy runs
// through the real, unchanged M5 path — marketdata.Manager -> Replay
// -> Scheduler -> Strategy.OnBar (issue #250, EMA-05) — using a real
// canonical fixture, not a synthetic in-memory bar slice. It composes
// the same environment shape examples/m5/privatestrategy_test.go
// already established for exactly this purpose (a real strategy driven
// through service/backtest.Service), scoped here to what EMA-05 itself
// checks: bars/timestamps reach the strategy in deterministic order,
// warm-up is honored, and requesting uncovered data fails clearly. No
// second bar-feed abstraction is introduced, and no direct provider/
// raw-storage access happens outside marketdata.
package emacross_test

import (
	"context"
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
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/strategy/emacross"
)

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

// recordingStrategy wraps a real strategy.Strategy and records every
// BarEvent it observes, purely to let this test assert on exactly
// what reached OnBar without adding any such bookkeeping to emacross
// itself.
type recordingStrategy struct {
	strategy.Strategy
	events []strategy.BarEvent
}

func (r *recordingStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	r.events = append(r.events, event)
	return r.Strategy.OnBar(ctx, event, view)
}

type fixedPriceSource map[string]num.Price

func (f fixedPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "fixed", Version: "test"}
}

func (f fixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := f[listing.Symbol()]
	if !ok {
		return num.Price{}, assert.AnError
	}
	return p, nil
}

// environmentFactory mirrors examples/m5/privatestrategy_test.go's own
// identical composition-root role: it builds the real M4 pipeline and
// simulated broker Scheduler requires (Scheduler.Run drives
// Pipeline.Submit for any intent a strategy emits, so there is no way
// to exercise Manager/Replay/Scheduler without one), even though
// EMA-05 itself only asserts on bar delivery, not trading outcomes —
// that is EMA-06's own scope.
type environmentFactory struct {
	prices fixedPriceSource
}

func (f environmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := clock.NewSimulated(req.Span.Start())
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

	fill, err := backtest.NewComponentInfo("fixed", "test", nil)
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
		FillModel:       fill,
		SlippageModel:   none,
		CommissionModel: none,
	}, nil
}

// TestEmacross_RunsThroughCanonicalMarketdataReplay drives the real
// EMA crossover strategy through Manager -> Replay -> Scheduler using
// a real, committed canonical H1 fixture (strategy/emacross/testdata,
// copied from examples/m5's own identical fixture) and the real
// reference periods (20/50, docs/research/ema-01-experiment-
// definition.org), and asserts on exactly what EMA-05 requires: every
// bar the fixture covers reaches OnBar exactly once, in strictly
// increasing time order. It proves "every bar exactly once" by
// comparing the full recorded timestamp sequence against an
// independent Manager.Bars read of the identical query — not a
// hardcoded count, since the fixture's own bar count depends on
// session-calendar gaps this test should not have to duplicate — so a
// dropped or duplicated bar in the middle of the run cannot pass
// unnoticed. Strategy-level warm-up/readiness (no intent returned
// before SlowPeriod bars) is EMA-04's own scope, already covered
// there; this test does not re-assert it.
func TestEmacross_RunsThroughCanonicalMarketdataReplay(t *testing.T) {
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t, "oanda")))

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.January, 20, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	simResolver := instrument.NewMemoryResolver()
	simListing := eurusdListing(t, "sim")
	require.NoError(t, simResolver.Register(simListing))

	// The fixture covers 2024-01-07T22:00Z through 2024-01-19T21:00Z;
	// request exactly that range so the expected bar count is known.
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 19, 22, 0, 0, 0, time.UTC), // half-open: excludes nothing real, includes the last bar at 21:00
	)
	require.NoError(t, err)

	ctx := context.Background()
	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: simListing.InstrumentID(), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	factory := environmentFactory{prices: fixedPriceSource{"EURUSD": num.MustParsePrice("1.13600")}}
	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	real, err := emacross.New(simListing.InstrumentID(), marketdata.H1, emacross.Config{FastPeriod: 20, SlowPeriod: 50})
	require.NoError(t, err)
	recorder := &recordingStrategy{Strategy: real}

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        recorder,
		Span:            span,
		StartingCapital: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
	})
	require.NoError(t, err)
	assert.Equal(t, emacross.Name, resp.Manifest.StrategyName())

	require.NotEmpty(t, recorder.events, "the strategy must have actually been called")

	// Every recorded bar belongs to the requested instrument/interval
	// and arrives in strictly increasing time order — no duplicate, no
	// out-of-order, and (no-lookahead) never before the previous bar.
	for i, event := range recorder.events {
		assert.Equal(t, simListing.InstrumentID(), event.Instrument)
		assert.Equal(t, marketdata.H1, event.Interval)
		if i > 0 {
			assert.True(t, event.Bar.Time.After(recorder.events[i-1].Bar.Time),
				"bar %d (%s) must be strictly after bar %d (%s)",
				i, event.Bar.Time, i-1, recorder.events[i-1].Bar.Time)
		}
	}

	assert.True(t, recorder.events[0].Bar.Time.Equal(time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC)))
	assert.True(t, recorder.events[len(recorder.events)-1].Bar.Time.Equal(time.Date(2024, time.January, 19, 21, 0, 0, 0, time.UTC)))

	// "Every bar exactly once": compare the full recorded sequence
	// against an independent Manager.Bars read of the identical query,
	// rather than a hardcoded count — a bar dropped or duplicated
	// anywhere in the middle would change strictly-increasing ordering
	// only if it also reordered timestamps, but a straight count/
	// sequence mismatch catches it unconditionally.
	reader, err := manager.Bars(ctx, marketdata.BarQuery{Instrument: simListing.InstrumentID(), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	var expected []time.Time
	for {
		bar, err := reader.Next(ctx)
		if err != nil {
			require.ErrorIs(t, err, io.EOF)
			break
		}
		expected = append(expected, bar.Time)
	}

	var recorded []time.Time
	for _, event := range recorder.events {
		recorded = append(recorded, event.Bar.Time)
	}
	assert.Equal(t, expected, recorded, "every canonical bar must reach OnBar exactly once, in order")
}

// TestEmacross_UncoveredRangeFailsClearly proves requesting a range
// this raw archive does not cover (EMA-05's own "fail clearly when
// canonical data coverage is unavailable" requirement) fails clearly
// — not with a partial, silently-wrong result — before any replay or
// strategy invocation is attempted. Plan itself succeeds (it reports a
// "missing" partition and a download-raw action, since planning what
// would need fetching is not itself a failure); the actual explicit
// failure this requirement cares about surfaces at Manager.Bars, the
// same read path Replay uses to drive Scheduler.
func TestEmacross_UncoveredRangeFailsClearly(t *testing.T) {
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t, "oanda")))

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	instID := eurusdListing(t, "oanda").InstrumentID()

	// February 2024 has no raw partition at all in this fixture.
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 2, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	ctx := context.Background()
	query := marketdata.BarQuery{Instrument: instID, Interval: marketdata.H1, Range: span}

	plan, err := manager.Plan(ctx, query)
	require.NoError(t, err, "planning a would-need-fetching action is not itself a failure")
	require.Len(t, plan.Coverage.Partitions, 1)
	assert.Equal(t, marketdata.PartitionCoverageMissing, plan.Coverage.Partitions[0].Status)
	require.Len(t, plan.Actions, 1)
	assert.Equal(t, marketdata.ActionDownloadRaw, plan.Actions[0].Kind)

	_, err = manager.Bars(ctx, query)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no coverage")
}
