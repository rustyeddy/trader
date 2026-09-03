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
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	"github.com/rustyeddy/trader/strategy/emacross"
)

// memoryRecorder is a minimal in-memory journal.Recorder: it exists
// only so this test can assert on the actual intent -> proposal ->
// decision -> request -> order -> fill sequence Scheduler/Pipeline
// produce, rather than inferring that chain occurred from final
// account state alone (PR #262 review).
type memoryRecorder struct {
	records []journal.Record
}

func (r *memoryRecorder) Record(ctx context.Context, rec journal.Record) error {
	r.records = append(r.records, rec)
	return nil
}

func (r *memoryRecorder) Close() error { return nil }

func (r *memoryRecorder) kinds(kind journal.Kind) []journal.Record {
	var out []journal.Record
	for _, rec := range r.records {
		if rec.Kind == kind {
			out = append(out, rec)
		}
	}
	return out
}

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
			require.ErrorIs(t, err, io.EOF, "canonical read must fail clearly, not be silently treated as end of stream")
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
	prices  *barLookupPriceSource
	rules   []risk.Rule
	journal *memoryRecorder
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
		Journal:         f.journal,
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

func runEMACrossoverFixture(t *testing.T, rules ...risk.Rule) (svcbacktest.RunResponse, *memoryRecorder) {
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
	rec := &memoryRecorder{}
	factory := execEnvironmentFactory{prices: prices, rules: rules, journal: rec}

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
	return resp, rec
}

// TestEmacross_EntryExitReversalThroughRealPipeline is EMA-06's own
// required demonstration: at least one crossover producing the exact
// intent -> proposal -> decision -> request -> order -> fill chain,
// with resulting position/account state matching EMA-01's transition
// semantics — flat->long at the bullish cross (bar 7), then
// long->short at the bearish reversal (bar 12), both filled at the
// real next-bar-open price the canonical fixture actually contains.
func TestEmacross_EntryExitReversalThroughRealPipeline(t *testing.T) {
	resp, rec := runEMACrossoverFixture(t)

	// The actual intent -> proposal -> decision -> request -> order ->
	// fill chain, not inferred from final account state: every stage
	// must have produced at least one record, and at least one
	// Decision must have actually been Allowed (a Decision record
	// exists for every proposal regardless of outcome, per
	// journal.KindDecision's own doc comment).
	require.NotEmpty(t, rec.kinds(journal.KindIntent), "no Intent was journaled")
	require.NotEmpty(t, rec.kinds(journal.KindProposal), "no Proposal was journaled")
	decisions := rec.kinds(journal.KindDecision)
	require.NotEmpty(t, decisions, "no Decision was journaled")
	var sawAllowed bool
	for _, d := range decisions {
		if d.Decision.Allowed {
			sawAllowed = true
		}
	}
	assert.True(t, sawAllowed, "at least one Decision must have been Allowed")
	require.NotEmpty(t, rec.kinds(journal.KindRequest), "no Request was journaled")
	require.NotEmpty(t, rec.kinds(journal.KindOrder), "no Order was journaled")
	fills := rec.kinds(journal.KindFill)
	require.Len(t, fills, 3, "three fills are expected: the bar-7 entry, the bar-12 exit, and the bar-12 re-entry")

	// Decision evidence (issue #253, EMA-08) reaches the very same
	// journal, via the real backtest.Runner -> strategy.Environment
	// wiring — not just the strategy-level unit tests in
	// decision_evidence_test.go, which call OnBar directly. Exactly two
	// signals: one per crossover (bar 7, bar 12) — a signal is recorded
	// only at a decision boundary, never on an ordinary no-crossover
	// bar.
	signals := rec.kinds(journal.KindSignal)
	require.Len(t, signals, 2)
	var sawBullish, sawBearish bool
	for _, s := range signals {
		switch s.Signal.Values["cross"] {
		case "bullish":
			sawBullish = true
			assert.Equal(t, "enter-long", s.Signal.Values["action"])
		case "bearish":
			sawBearish = true
			assert.Equal(t, "reverse", s.Signal.Values["action"])
		}
		assert.Equal(t, emacross.Name, s.Signal.Strategy)
	}
	assert.True(t, sawBullish, "the bar-7 bullish cross must have been journaled")
	assert.True(t, sawBearish, "the bar-12 bearish cross must have been journaled")

	bar8Open := time.Date(2024, time.March, 4, 7, 0, 0, 0, time.UTC)   // the bar after the bar-7 crossover
	bar13Open := time.Date(2024, time.March, 4, 12, 0, 0, 0, time.UTC) // the bar after the bar-12 crossover

	require.Len(t, resp.Trades, 1, "the bar-12 reversal must have closed the bar-7 long as one realized trade")
	closed := resp.Trades[0]
	assert.Equal(t, order.Long, closed.Side)
	assert.True(t, closed.OpenedAt.Equal(bar8Open), "the long must have opened at bar 8's own next-bar-open time, got %s", closed.OpenedAt)
	assert.True(t, closed.ClosedAt.Equal(bar13Open), "the long must have closed at bar 13's own next-bar-open time, got %s", closed.ClosedAt)
	// The long entered at bar 8's open (1.10040) and exited at bar
	// 13's open (1.10000) — a lower exit than entry on a long realizes
	// a loss.
	sign, err := closed.RealizedPnL.Cmp(num.MustParseMoney("0", num.MustParseCurrency("USD")))
	require.NoError(t, err)
	assert.Negative(t, sign, "a long that exits lower than it entered must realize a loss, got %s", closed.RealizedPnL)

	require.Len(t, resp.OpenTrades, 1, "the bar-12 reversal's re-entry must remain open through the end of the fixture")
	open := resp.OpenTrades[0]
	assert.Equal(t, order.Short, open.Side)
	assert.True(t, open.OpenedAt.Equal(bar13Open), "the re-entry must have opened at bar 13's own next-bar-open time, got %s", open.OpenedAt)

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

	resp, rec := runEMACrossoverFixture(t, tooSmall)

	assert.Empty(t, resp.Trades)
	assert.Empty(t, resp.OpenTrades)
	assert.Empty(t, resp.Account.Positions(), "every entry attempt must have been rejected, leaving the account flat")
	assert.Empty(t, rec.kinds(journal.KindFill), "a rejected proposal must never reach a fill")

	// Prove the account stayed flat *because* max_position_quantity
	// actually rejected every entry attempt, not merely because no
	// entry was ever submitted.
	decisions := rec.kinds(journal.KindDecision)
	require.NotEmpty(t, decisions, "no Decision was journaled")
	var rejectedByRule int
	for _, d := range decisions {
		if d.Decision.Allowed {
			continue
		}
		for _, v := range d.Decision.Violations {
			if v.Rule == "max_position_quantity" {
				rejectedByRule++
			}
		}
	}
	assert.Equal(t, 2, rejectedByRule, "both the bar-7 entry and the bar-12 entry attempt must have been rejected by max_position_quantity")

	// Determinism: repeating the identical run reaches the identical
	// (rejected, flat) outcome again.
	again, _ := runEMACrossoverFixture(t, tooSmall)
	assert.Empty(t, again.Account.Positions())
}
