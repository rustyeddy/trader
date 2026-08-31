package backtest

import (
	"context"
	"fmt"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
)

// simPriceSource is the simbroker.FillPriceSource "trader backtest
// run" uses: demoStrategy places its one market order on the
// requested span's own first bar, so the fill price is fixed at
// construction time to that same first bar's close (run.go reads it
// via Manager.Bars before calling Service.Run) — the identical
// reference-price convention backtest's own test fixtures use
// (a fixed price, not a live per-bar feed), since backtest.Runner
// drives the full replay internally and exposes no per-bar hook a
// composition root could otherwise update a price source from.
type simPriceSource struct {
	symbol string
	price  num.Price
}

func (s *simPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "cli-first-bar-close", Version: "v0"}
}

func (s *simPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	if listing.Symbol() != s.symbol {
		return num.Price{}, fmt.Errorf("no price configured for %s", listing.Symbol())
	}
	return s.price, nil
}

// environmentFactory is this CLI's own concrete svcbacktest.
// EnvironmentFactory implementation (ADR-039): it builds a fresh
// sim.Broker/execution.Planner/risk.Engine/pipeline.Pipeline stack per
// call, exactly like every existing cmd/trader command family's own
// buildService already does for its own use case — "trader backtest"
// is not special in this regard, it simply plugs that same
// construction into the injected-factory seam service/backtest
// defines rather than calling a service constructor directly.
type environmentFactory struct {
	listing instrument.Listing
	prices  *simPriceSource
}

func (f environmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := clock.NewSimulated(req.Span.Start())
	ids := id.NewGenerator(c, id.Random{})
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
