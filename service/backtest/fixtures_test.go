package backtest_test

import (
	"context"
	"testing"
	"time"

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

// eurusdID returns EUR/USD's canonical instrument identity.
func eurusdID(t *testing.T) instrument.ID {
	t.Helper()
	return instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
}

func eurusdSpec(t *testing.T) instrument.Spec {
	t.Helper()
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	return spec
}

// eurusdOandaListing is the marketdata-side Listing (provider "oanda")
// the fixture manager's own Resolver uses to fetch bars.
func eurusdOandaListing(t *testing.T) instrument.Listing {
	t.Helper()
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   "oanda",
		Symbol:     "EURUSD",
		Spec:       eurusdSpec(t),
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// eurusdSimListing is the broker-side Listing (provider "sim") the
// fixture RunRequest's Resolver and simEnvironmentFactory's price
// source both key against, independent of the marketdata-side
// Listing above (ADR-016).
func eurusdSimListing(t *testing.T) instrument.Listing {
	t.Helper()
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   "sim",
		Symbol:     "EUR_USD",
		Spec:       eurusdSpec(t),
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// fixtureSpan is the span the committed testdata/raw/oanda fixture
// covers — matching backtest package's own schedulerSpan fixture.
func fixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 8, 4, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// zeroSpan is the Go zero TimeRange — Duration() <= 0, invalid as a
// RunRequest.Span.
func zeroSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	return marketdata.TimeRange{}
}

// noopStrategy never emits an intent — service-level tests exercise
// Service's own plumbing (validation, cancellation, factory wiring),
// not trading logic, which backtest's own tests already cover
// end-to-end.
type noopStrategy struct{}

func (noopStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{
		Name:    "noop",
		Version: "v1",
		Requirements: []strategy.DataRequirement{
			{Instrument: instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")), Interval: marketdata.H1},
		},
	}
}

func (noopStrategy) Start(ctx context.Context, env strategy.Environment) error { return nil }

func (noopStrategy) OnBar(ctx context.Context, event strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	return nil, nil
}

func validRunRequest(t *testing.T) svcbacktest.RunRequest {
	t.Helper()
	return svcbacktest.RunRequest{
		Strategy:        noopStrategy{},
		Span:            fixtureSpan(t),
		StartingCapital: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
		TraderVersion:   "test-v0",
	}
}

// newFixtureManager returns a *marketdata.Manager with the committed
// testdata/raw/oanda EUR/USD H1 fixture published for fixtureSpan.
func newFixtureManager(t *testing.T) *marketdata.Manager {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdOandaListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	ctx := context.Background()
	plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: marketdata.H1, Range: fixtureSpan(t)})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = mgr.Build(ctx, plan)
		require.NoError(t, err)
	}
	return mgr
}

// newFixtureResolver returns the instrument.Resolver a RunRequest's
// broker-side Listing resolution needs — sim listings, independent of
// the manager's own oanda resolver above.
func newFixtureResolver(t *testing.T) instrument.Resolver {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(eurusdSimListing(t)))
	return r
}

// fixedPriceSource is a minimal sim.FillPriceSource keyed by symbol.
type fixedPriceSource map[string]num.Price

func (f fixedPriceSource) Info() sim.ModelInfo {
	return sim.ModelInfo{Name: "fixedPriceSource", Version: "test"}
}

func (f fixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := f[listing.Symbol()]
	if !ok {
		return num.Price{}, sim.ErrInvalidConfig
	}
	return p, nil
}

// simEnvironmentFactory is a real (not stubbed) svcbacktest.
// EnvironmentFactory implementation, built the same way backtest's own
// scheduler/runner tests wire a fresh sim.Broker + pipeline per run —
// the composition-root role the package doc comment describes.
type simEnvironmentFactory struct{}

func (simEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := clock.NewSimulated(req.Span.Start())
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	if err != nil {
		return svcbacktest.Environment{}, err
	}

	b, err := sim.NewBroker("sim", sim.Deps{
		Clock:  c,
		IDs:    ids,
		Prices: fixedPriceSource{"EUR_USD": num.MustParsePrice("1.10000")},
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

	fill, err := backtest.NewComponentInfo("bar-close", "v1", nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	slippage, err := backtest.NewComponentInfo("none", "", nil)
	if err != nil {
		return svcbacktest.Environment{}, err
	}
	commission, err := backtest.NewComponentInfo("fixed", "v1", nil)
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

// fakeEnvironmentFactory is a test double letting a test control
// exactly what NewEnvironment returns and observe whether it was
// called at all — used to prove pre-factory cancellation never invokes
// it, and to test environment-factory/environment-validation error
// propagation without a real sim.Broker.
type fakeEnvironmentFactory struct {
	env    svcbacktest.Environment
	err    error
	called bool
}

func (f *fakeEnvironmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	f.called = true
	return f.env, f.err
}
