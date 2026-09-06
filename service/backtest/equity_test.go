package backtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/broker/sim"
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
)

// TestService_Run_SPYEquityBacktest is issue #300 (EQ-07)'s central
// acceptance criterion: a real equity instrument (SPY, an ETF) runs
// end to end through the exact same service/backtest.Service ->
// backtest.Runner -> pipeline -> simulator path
// TestService_Run_SuccessfulRun already proves for EUR/USD, with no
// equity-specific Service, Runner, pipeline, or strategy interface.
// simEquityEnvironmentFactory below is simEnvironmentFactory's exact
// shape, generalized only in the price source it seeds sim.Broker
// with — every other component (execution.Planner, risk.Engine,
// risk.FixedFractionSizer, pipeline.Pipeline) is constructed
// identically and needed no equity-specific branch.
//
// See docs/research/eq-07-backtest-pipeline-note.org for the full
// per-risk-area findings this test's own passing is evidence for.
func TestService_Run_SPYEquityBacktest(t *testing.T) {
	svc, err := svcbacktest.New(newSPYFixtureManager(t), newSPYFixtureResolver(t), simEquityEnvironmentFactory{}, nil)
	require.NoError(t, err)

	req := svcbacktest.RunRequest{
		Strategy:        &spyEnterOnceStrategy{},
		Span:            spyFixtureSpan(t),
		StartingCapital: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("1.00"),
		TraderVersion:   "test-v0",
	}

	resp, err := svc.Run(context.Background(), req)
	require.NoError(t, err)

	assert.False(t, resp.Manifest.RunID().IsZero())
	assert.Equal(t, "spy-enter-once", resp.Manifest.StrategyName())

	// The strategy enters on the first bar (2020-05-01) and Scheduler's
	// next-bar-open fill-eligibility rule fills it at the *second*
	// bar's Open (2020-05-04, 280.34) — never the entry bar's own
	// price. This is the exact same rule EMA-07/#240 already
	// established for FX; proving it holds unchanged for an equity
	// instrument is this test's whole point.
	require.Len(t, resp.OpenTrades, 1, "the strategy must have one open position (no exit intent was ever emitted)")
	trade := resp.OpenTrades[0]
	assert.Equal(t, spyID(t), trade.Listing.InstrumentID())
	assert.Equal(t, order.Long, trade.Side)
	assert.True(t, trade.OpenedAt.Equal(time.Date(2020, time.May, 4, 0, 0, 0, 0, time.UTC)),
		"fill must land on the second bar (next-bar-open), not the entry bar")

	// Whole-share quantity sizing: RiskFraction 0.01 against $10,000
	// capital and a $1.00 AdverseDistance risk-sizes to 100 shares
	// (100 shares * $1.00 adverse move = 1% of $10,000) — an ordinary
	// integer share count, not a fractional FX lot size, proving
	// risk.FixedFractionSizer needs no equity-specific handling.
	positions := resp.Account.Positions()
	require.Len(t, positions, 1)
	pos := positions[0]
	assert.Equal(t, order.Long, pos.Side)
	assert.Equal(t, "100", pos.Quantity.String())
	require.NotNil(t, pos.AvgPrice)
	assert.Equal(t, "280.34", pos.AvgPrice.String())

	// PnL/equity is denominated in SPY's own settlement currency (USD)
	// throughout — nothing in the pipeline hardcodes EUR/USD's own
	// quote currency. This is also the exact-arithmetic proof issue
	// #300's own acceptance criterion asks for ("PnL and position
	// quantities are correct"), not merely a currency/denomination
	// check: the position is marked at the second (and last) bar's
	// Close ($285.34), $5.00/share above the $280.34 fill price, so
	// 100 shares produces exactly $500 unrealized profit and $10,500
	// equity from $10,000 starting capital — hand-checked, not just
	// asserted against the implementation's own output.
	assert.Equal(t, "USD", resp.Account.Equity().Currency().String())
	assert.Equal(t, "10500 USD", resp.Account.Equity().String())
	assert.Equal(t, "0 USD", resp.Account.RealizedPnL().String())
	assert.Equal(t, "500 USD", resp.Account.UnrealizedPnL().String())
	require.NotEmpty(t, resp.EquityCurve)
}

// spyID is SPY's canonical instrument identity (an ETF listed on NYSE
// Arca, per ADR-047's own exchange-identity requirement).
func spyID(t *testing.T) instrument.ID {
	t.Helper()
	return instrument.ETFID("ARCA", "SPY")
}

// spySpec is SPY's Spec: a one-cent tick and a whole-share quantity
// increment (ADR-047's Phase 1 default), unlike EUR/USD's 0.00001 tick
// and unit-lot increment.
func spySpec(t *testing.T) instrument.Spec {
	t.Helper()
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	return spec
}

// spyStooqListing is the marketdata-side Listing (provider "stooq")
// the fixture manager's Resolver uses to fetch bars.
func spyStooqListing(t *testing.T) instrument.Listing {
	t.Helper()
	spy, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: spy,
		Provider:   "stooq",
		Venue:      "ARCA",
		Symbol:     "SPY",
		Spec:       spySpec(t),
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// spySimListing is the broker-side Listing (provider "sim") the
// RunRequest's Resolver and the environment factory's price source key
// against, independent of the marketdata-side Listing above (ADR-016)
// — the same split eurusdSimListing already establishes for FX.
func spySimListing(t *testing.T) instrument.Listing {
	t.Helper()
	spy, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: spy,
		Provider:   "sim",
		Venue:      "ARCA",
		Symbol:     "SPY",
		Spec:       spySpec(t),
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// spyFixtureSpan is the span the committed
// testdata/raw/stooq/SPY/2020/05 fixture covers: two real SPY D1 bars,
// 2020-05-01 and 2020-05-04 (the same real excerpt already used
// elsewhere this milestone — see
// marketdata/internal/provider/stooq/testdata/spy_us_d_sample.csv).
func spyFixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2020, time.May, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, time.May, 5, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// newSPYFixtureManager returns a *marketdata.Manager with the
// committed testdata/raw/stooq SPY D1 fixture published for
// spyFixtureSpan, mirroring newFixtureManager's identical FX-side
// construction with provider "stooq" instead of "oanda" — proving
// Manager/Plan/Build need no equity-specific caller-side handling
// (ADR-047's internal provider seam already made this transparent).
func newSPYFixtureManager(t *testing.T) *marketdata.Manager {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(spyStooqListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2020, time.June, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/stooq",
		Resolver:     resolver,
		ProviderName: "stooq",
	})
	require.NoError(t, err)

	ctx := context.Background()
	plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: spyID(t), Interval: marketdata.D1, Range: spyFixtureSpan(t)})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = mgr.Build(ctx, plan)
		require.NoError(t, err)
	}
	return mgr
}

// newSPYFixtureResolver returns the instrument.Resolver a RunRequest's
// broker-side Listing resolution needs.
func newSPYFixtureResolver(t *testing.T) instrument.Resolver {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(spySimListing(t)))
	return r
}

// spyFixedPriceSource is fixedPriceSource's SPY-keyed twin.
type spyFixedPriceSource map[string]num.Price

func (f spyFixedPriceSource) Info() sim.ModelInfo {
	return sim.ModelInfo{Name: "spyFixedPriceSource", Version: "test"}
}

func (f spyFixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := f[listing.Symbol()]
	if !ok {
		return num.Price{}, sim.ErrInvalidConfig
	}
	return p, nil
}

// simEquityEnvironmentFactory is simEnvironmentFactory's exact shape
// (fixtures_test.go), generalized only in the price-source seed (SPY's
// own fill price instead of EUR_USD's). Kept as its own type rather
// than parameterizing simEnvironmentFactory itself, to leave the
// existing FX fixture untouched — this test only needs to prove the
// equity path works, not refactor the FX one.
type simEquityEnvironmentFactory struct{}

func (simEquityEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := clock.NewSimulated(req.Span.Start())
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	prices := spyFixedPriceSource{"SPY": num.MustParsePrice("280.34")}
	b, err := sim.NewBroker("sim", sim.Deps{
		Clock:  c,
		IDs:    ids,
		Prices: prices,
	}, sim.AccountConfig{AccountID: accountID, StartingCash: req.StartingCapital})
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	account, err := b.OpenAccount(ctx, accountID)
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

	// FillModel is derived from the sim.Deps.Prices source actually
	// configured above, not a hardcoded label unrelated to what ran —
	// Deps.Prices is spyFixedPriceSource, not a real bar-close model,
	// so the manifest must say so (Copilot's and Rusty's PR #309
	// review). Deps.Commission is left nil (no commission model
	// configured), so CommissionModel records "none", the same
	// convention SlippageModel already uses for "not configured" —
	// claiming "fixed" here would describe a commission model that
	// never actually ran.
	priceInfo := prices.Info()
	fill, err := backtest.NewComponentInfo(priceInfo.Name, priceInfo.Version, nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	slippage, err := backtest.NewComponentInfo("none", "", nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	commission, err := backtest.NewComponentInfo("none", "", nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	return svcbacktest.Environment{
		Clock:           c,
		IDs:             ids,
		Account:         account,
		Pipeline:        pl,
		FillModel:       fill,
		SlippageModel:   slippage,
		CommissionModel: commission,
	}, nil
}

// spyEnterOnceStrategy emits one Enter intent for SPY on the first
// bar, then never again — demoStrategy's shape
// (cmd/trader/backtest/demo_strategy.go), reduced to exactly what this
// test needs. It lives here, rather than reusing that package's own
// unexported demoStrategy (which this package cannot import), to keep
// this test's dependency on the real strategy.Strategy contract, not
// on another package's internal composition-root fixture.
type spyEnterOnceStrategy struct {
	intents strategy.IntentFactory
	entered bool
}

func (s *spyEnterOnceStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{
		Name:    "spy-enter-once",
		Version: "v1",
		Requirements: []strategy.DataRequirement{
			{Instrument: instrument.ETFID("ARCA", "SPY"), Interval: marketdata.D1},
		},
	}
}

func (s *spyEnterOnceStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

func (s *spyEnterOnceStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	if s.entered {
		return nil, nil
	}
	s.entered = true
	in, err := s.intents.Enter(event.Instrument, order.Buy)
	if err != nil {
		return nil, err
	}
	return []order.Intent{in}, nil
}
