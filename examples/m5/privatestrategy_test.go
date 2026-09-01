// Package m5_test plays the role of a real application built on top
// of a private strategy (issue #225, M5-17): it imports m5 (the
// "private strategy repository") plus Trader's own public application
// service (service/backtest) and a concrete broker adapter
// (adapters/broker/sim) — exactly the composition-root role
// cmd/trader/backtest already plays for the CLI, demonstrated here
// without cmd/trader/backtest itself, proving the seam does not
// require any particular transport. This is legitimate composition
// code and is expected to import backtest/service/adapters/execution/
// risk/pipeline; the property this package exists to prove is that
// m5.PrivateStrategy itself does not need to (privatestrategy.go /
// boundary_test.go).
//
// This mirrors examples/m1's own "external consumer of Trader's
// public API" convention (issue #32), scoped to M5's own strategy
// runtime contract instead of M1's domain foundation.
package m5_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/examples/m5"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
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

// environmentFactory is this example's own composition-root
// svcbacktest.EnvironmentFactory implementation — the same role
// cmd/trader/backtest's own environmentFactory plays for the CLI
// (ADR-039), built here directly to prove no CLI/cmd package is
// required to drive a private strategy through the public
// service/backtest boundary.
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

	// FillModel describes f.prices itself — the actual configured fill-
	// price authority. SlippageModel/CommissionModel are both "none":
	// this Deps leaves Slippage/Commission nil, and simbroker.Deps' own
	// doc comment is explicit that nil means exactly that — no
	// slippage, no commission — not "fixed" (issue #243 review: the
	// manifest must describe the actual configured environment, not a
	// convenient shared placeholder).
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
		Account:         account,
		Pipeline:        pl,
		FillModel:       fill,
		SlippageModel:   none,
		CommissionModel: none,
	}, nil
}

// TestPrivateStrategy_RunsThroughPublicBacktestService is issue
// #225's own required demonstration: m5.PrivateStrategy — built using
// only the public strategy.Strategy contract (boundary_test.go
// mechanically confirms this) — runs successfully end to end through
// service/backtest.Service.Run, the same public application boundary
// cmd/trader/backtest itself uses, without this test importing
// cmd/trader/backtest or reaching into any backtest-internal type
// beyond what service/backtest.RunResponse already exposes.
func TestPrivateStrategy_RunsThroughPublicBacktestService(t *testing.T) {
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t, "oanda")))

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	simResolver := instrument.NewMemoryResolver()
	simListing := eurusdListing(t, "sim")
	require.NoError(t, simResolver.Register(simListing))

	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 8, 4, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	ctx := context.Background()
	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: simListing.InstrumentID(), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	factory := environmentFactory{prices: fixedPriceSource{"EURUSD": num.MustParsePrice("1.10000")}}

	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	privateStrategy := m5.NewPrivateStrategy(simListing.InstrumentID(), marketdata.H1)

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        privateStrategy,
		Span:            span,
		StartingCapital: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
	})
	require.NoError(t, err)

	assert.Equal(t, "private-example", resp.Manifest.StrategyName())
	assert.NotEmpty(t, resp.OpenTrades, "the private strategy must have actually entered a position through the real M4 pipeline and simulator")
	assert.Len(t, resp.Account.Positions(), 1)

	// The manifest must describe the environment actually configured
	// (issue #243 review): this environment never sets Slippage or
	// Commission, so the recorded models must say "none", not the same
	// "fixed" descriptor FillModel legitimately uses.
	assert.Equal(t, "fixed", resp.Manifest.FillModel().Name())
	assert.Equal(t, "none", resp.Manifest.SlippageModel().Name())
	assert.Equal(t, "none", resp.Manifest.CommissionModel().Name())
}
