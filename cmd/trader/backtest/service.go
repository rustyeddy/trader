package backtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
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
// model (issue #224 review, point 4). The EMA crossover strategy run
// via --config (issue #252, EMA-07) enters, exits, and re-enters at
// run-dependent bars, so it uses nextBarOpenPriceSource below instead
// — the general per-bar-lookup form this doc comment used to say a
// "future, less trivial strategy" would need.
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

// nextBarOpenPriceSource is a general simbroker.FillPriceSource used
// by the --config/EMA crossover path (issue #252, EMA-07): it returns,
// for whatever instant its clock currently reports, that instant's own
// canonical bar Open for the given symbol — the actual next-bar-open
// price Scheduler's flush step expects, since Flush always advances
// Clock to the new bar's own time before submitting a previously
// queued intent (backtest/scheduler.go's own documented ordering). A
// strategy that enters, exits, and re-enters at run-dependent bars and
// prices (unlike demoStrategy's own single, deterministic entry) needs
// exactly this general form; simPriceSource above stays as it is for
// demoStrategy rather than being stretched to cover both.
//
// clock is intentionally not set at construction: run.go builds and
// loads this price source's canonical bar data before Service.Run
// exists a *clock.Simulated to observe, so environmentFactory sets it
// via setClock once it constructs that clock, immediately before
// building the simulated broker.
type nextBarOpenPriceSource struct {
	clock *clock.Simulated
	bars  map[string]map[time.Time]marketdata.Bar // symbol -> bar time -> bar
}

func newNextBarOpenPriceSource() *nextBarOpenPriceSource {
	return &nextBarOpenPriceSource{bars: make(map[string]map[time.Time]marketdata.Bar)}
}

func (s *nextBarOpenPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "next-bar-open", Version: "v1"}
}

func (s *nextBarOpenPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	if s.clock == nil {
		return num.Price{}, fmt.Errorf("next-bar-open price source: clock not set")
	}
	now := s.clock.Now()
	bar, ok := s.bars[listing.Symbol()][now]
	if !ok {
		return num.Price{}, fmt.Errorf("no canonical bar for %s at %s", listing.Symbol(), now)
	}
	return bar.Open, nil
}

func (s *nextBarOpenPriceSource) setClock(c *clock.Simulated) {
	s.clock = c
}

// load drains a full canonical read of query into s, keyed by symbol —
// every bar this run could ever need a fill price for, read once
// before the run starts, exactly the same "load the whole series up
// front" approach strategy/emacross's own EMA-05/EMA-06 tests already
// established for the identical problem.
func (s *nextBarOpenPriceSource) load(ctx context.Context, manager *marketdata.Manager, symbol string, query marketdata.BarQuery) error {
	reader, err := manager.Bars(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	byTime := make(map[time.Time]marketdata.Bar)
	for {
		bar, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		byTime[bar.Time] = bar
	}
	s.bars[symbol] = byTime
	return nil
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
	prices simbroker.FillPriceSource
	// journal is an optional durable record destination for this run
	// (--journal), passed straight through to svcbacktest.Environment.
	// A nil journal is accepted and treated as journal.Discard by
	// backtest.Runner (backtest/runner.go) — the common case, since
	// most invocations do not pass --journal.
	journal journal.Recorder
}

func (f environmentFactory) NewEnvironment(ctx context.Context, req svcbacktest.EnvironmentRequest) (svcbacktest.Environment, error) {
	c := clock.NewSimulated(req.Span.Start())
	// nextBarOpenPriceSource is loaded with canonical bar data before
	// this factory is ever called (run.go), but has no clock to
	// resolve "now" against until this call constructs one — hand it
	// over now, before the broker that will query Price() exists.
	if aware, ok := f.prices.(*nextBarOpenPriceSource); ok {
		aware.setClock(c)
	}
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
		Journal:         f.journal,
		FillModel:       fill,
		SlippageModel:   slippage,
		CommissionModel: commission,
	}, nil
}
