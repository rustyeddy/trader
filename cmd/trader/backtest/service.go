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
// run" uses, keyed by listing symbol — one entry per requested
// instrument (issue #224 extended this from a single symbol/price
// pair to a map so a multi-instrument run can configure each
// instrument's own price independently, matching the shape
// backtest's own test-only fixedPriceSource fixture already uses).
//
// demoStrategy emits its one Enter intent per instrument on that
// instrument's own first eligible bar, and Scheduler's next-bar-open
// fill-eligibility rule (issue #214) means that intent is not
// submitted until the *following* bar arrives, filling at that bar's
// own Open — never the entry bar's Close (PR #240 review: an earlier
// version of this file fixed the price to the entry bar's Close,
// silently reintroducing the exact causal pricing mismatch #214 was
// designed to remove). run.go computes each instrument's one,
// analytically known fill price before calling Service.Run (see
// nextBarOpenAfterEntry) and configures them all here as fixed
// values — not a live per-bar feed, since backtest.Runner drives the
// full replay internally and exposes no per-bar hook a composition
// root could otherwise update a price source from, and demoStrategy's
// own single, deterministic entry per instrument means exactly one
// fill per instrument ever needs a price, so one precomputed value
// each is both sufficient and correct.
//
// This is deliberately narrow: it is one precomputed next-bar-open
// fill price per instrument for this provisional single-entry demo
// strategy specifically, never a general multi-bar portfolio fill
// model (issue #224 review, point 4). A future, less trivial CLI
// strategy needing a genuine per-bar price feed should get its own
// FillPriceSource, not a stretched version of this one.
type simPriceSource map[string]num.Price

func (s simPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "cli-demo-next-bar-open", Version: "v1"}
}

func (s simPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := s[listing.Symbol()]
	if !ok {
		return num.Price{}, fmt.Errorf("no price configured for %s", listing.Symbol())
	}
	return p, nil
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
	prices simPriceSource
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

	// FillModel describes f.prices itself — the actual configured fill-
	// price authority — rather than a hardcoded literal, so the
	// resulting Manifest can never claim a fill model other than the
	// one that actually ran (PR #240 review, matching #215/#216's own
	// "descriptors travel with the actual configured environment"
	// principle). SlippageModel/CommissionModel are both "none": this
	// environment's simbroker.Deps sets neither Slippage nor
	// Commission, and simbroker.Deps' own doc comment is explicit that
	// leaving either nil means exactly that — no slippage, no
	// commission — not a "fixed" fee model this CLI never configured.
	priceInfo := f.prices.Info()
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
