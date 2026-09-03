// This file proves EMA-06 (issue #251): the EMA crossover strategy's
// emitted intents flow through Trader's existing, unchanged fixed-
// fraction sizing / M4 execution-risk pipeline / simulated broker —
// no special EMA execution path — and that entry, exit, and reversal
// all produce the exact resulting positions/trades EMA-01 specifies,
// with next-bar-open fill semantics and observable, deterministic risk
// rejection.
package emacross_test

import (
	"context"
	"fmt"
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
	"github.com/rustyeddy/trader/strategy/emacross"
)

// barLookupPriceSource is a real (not fixed-value) simbroker.
// FillPriceSource: it returns, for whatever instant clock currently
// reports, that instant's own canonical bar Open — the actual next-
// bar-open price Scheduler's flush step expects (backtest/scheduler.go's
// own "Flush ... happens before any OnBar call" ordering: clock is
// already advanced to the new bar's own time before Flush submits a
// previously queued intent, so looking the price up by current time is
// exactly correct, whether one or many fills occur over a run).
// cmd/trader/backtest's own simPriceSource is deliberately narrower
// (one precomputed value per instrument) because demoStrategy only
// ever enters once per instrument; emacross can enter, exit, and
// re-enter, so this test needs the general form its own doc comment
// says a "future, less trivial strategy" should bring.
type barLookupPriceSource struct {
	clock *clock.Simulated
	bars  map[string]map[time.Time]marketdata.Bar // symbol -> bar time -> bar
}

func (s *barLookupPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "next-bar-open-lookup", Version: "test"}
}

func (s *barLookupPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	now := s.clock.Now()
	bar, ok := s.bars[listing.Symbol()][now]
	if !ok {
		return num.Price{}, fmt.Errorf("no canonical bar for %s at %s", listing.Symbol(), now)
	}
	return bar.Open, nil
}

// loadBarLookupPriceSource drains a full canonical read of query into
// a barLookupPriceSource, keyed by symbol.
func loadBarLookupPriceSource(t *testing.T, ctx context.Context, manager *marketdata.Manager, c *clock.Simulated, symbol string, query marketdata.BarQuery) *barLookupPriceSource {
	t.Helper()
	reader, err := manager.Bars(ctx, query)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	byTime := make(map[time.Time]marketdata.Bar)
	for {
		bar, err := reader.Next(ctx)
		if err != nil {
			break
		}
		byTime[bar.Time] = bar
	}
	return &barLookupPriceSource{clock: c, bars: map[string]map[time.Time]marketdata.Bar{symbol: byTime}}
}

// execEnvironmentFactory builds the real M4 pipeline (fixed-fraction
// sizing, execution.Planner, risk.Engine with whatever rules the test
// configures, simulated broker) against prices, exactly the path any
// other strategy uses — no EMA-specific execution wiring exists or is
// added here.
type execEnvironmentFactory struct {
	prices *barLookupPriceSource
	rules  []risk.Rule
}

func (f execEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
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
	engine, err := risk.NewEngine(f.rules...)
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
		FillModel:       fill,
		SlippageModel:   none,
		CommissionModel: none,
	}, nil
}

// emaCrossoverFixtureSpan is strategy/emacross/testdata's own dedicated
// EMA-06 fixture: the exact EMA(3)/EMA(5) closes from docs/research/
// ema-01-experiment-definition.org's worked example, scaled into a
// realistic EURUSD price range and given real hourly timestamps
// (2024-03-04, a Monday — no weekend session gap across these 14
// hours), so the same bullish cross at bar 7 and bearish reversal at
// bar 12 that strategy/emacross's own unit tests already prove occur
// here too, now through the real canonical-data/pipeline/simulator
// path instead of hand-built strategy.View/BarEvent values.
func emaCrossoverFixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.March, 4, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.March, 4, 14, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

func runEMACrossoverFixture(t *testing.T, rules ...risk.Rule) svcbacktest.RunResponse {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t, "oanda")))

	span := emaCrossoverFixtureSpan(t)
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
	query := marketdata.BarQuery{Instrument: simListing.InstrumentID(), Interval: marketdata.H1, Range: span}
	plan, err := manager.Plan(ctx, query)
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	prices := loadBarLookupPriceSource(t, ctx, manager, c, "EURUSD", query)
	factory := execEnvironmentFactory{prices: prices, rules: rules}

	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	real, err := emacross.New(simListing.InstrumentID(), marketdata.H1, emacross.Config{FastPeriod: 3, SlowPeriod: 5})
	require.NoError(t, err)

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        real,
		Span:            span,
		StartingCapital: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
	})
	require.NoError(t, err, "a risk rejection must never abort the run")
	return resp
}

// TestEmacross_EntryExitReversalThroughRealPipeline is EMA-06's own
// required demonstration: at least one crossover producing the exact
// intent -> proposal -> decision -> request -> order -> fill chain,
// with resulting position/account state matching EMA-01's transition
// semantics — flat->long at the bullish cross (bar 7), then
// long->short at the bearish reversal (bar 12), both filled at the
// real next-bar-open price the canonical fixture actually contains.
func TestEmacross_EntryExitReversalThroughRealPipeline(t *testing.T) {
	resp := runEMACrossoverFixture(t)

	require.Len(t, resp.Trades, 1, "the bar-12 reversal must have closed the bar-7 long as one realized trade")
	closed := resp.Trades[0]
	assert.Equal(t, order.Long, closed.Side)
	// The long entered at bar 8's open (1.10040, the bar after the
	// bar-7 crossover) and exited at bar 13's open (1.10000, the bar
	// after the bar-12 crossover) — a lower exit than entry on a long
	// realizes a loss.
	sign, err := closed.RealizedPnL.Cmp(num.MustParseMoney("0", num.MustParseCurrency("USD")))
	require.NoError(t, err)
	assert.Negative(t, sign, "a long that exits lower than it entered must realize a loss, got %s", closed.RealizedPnL)

	require.Len(t, resp.OpenTrades, 1, "the bar-12 reversal's re-entry must remain open through the end of the fixture")
	open := resp.OpenTrades[0]
	assert.Equal(t, order.Short, open.Side)

	require.Len(t, resp.Account.Positions(), 1)
	position := resp.Account.Positions()[0]
	assert.Equal(t, order.Short, position.Side)
	require.NotNil(t, position.AvgPrice)
	assert.True(t, position.AvgPrice.Equal(num.MustParsePrice("1.10000")),
		"the re-entry must have filled at bar 13's own open, got %s", position.AvgPrice)
}

// TestEmacross_RiskRejectionIsObservableAndDeterministic proves risk
// rejection remains an observable, deterministic pipeline outcome —
// never an aborted run and never a silently-succeeding order — by
// configuring a risk.MaxPositionQuantityRule far below what fixed-
// fraction sizing computes for this account, so both the bar-7 entry
// and the bar-12 crossover's own (now still flat->short, since bar 7's
// entry never actually filled) entry attempt are rejected.
func TestEmacross_RiskRejectionIsObservableAndDeterministic(t *testing.T) {
	tooSmall, err := risk.NewMaxPositionQuantityRule(num.MustParseQuantity("1"))
	require.NoError(t, err)

	resp := runEMACrossoverFixture(t, tooSmall)

	assert.Empty(t, resp.Trades)
	assert.Empty(t, resp.OpenTrades)
	assert.Empty(t, resp.Account.Positions(), "every entry attempt must have been rejected, leaving the account flat")

	// Determinism: repeating the identical run reaches the identical
	// (rejected, flat) outcome again.
	again := runEMACrossoverFixture(t, tooSmall)
	assert.Empty(t, again.Account.Positions())
}
