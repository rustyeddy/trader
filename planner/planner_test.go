package planner

import (
	"testing"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles -----------------------------------------------------------

type fakeRegime struct {
	ready    bool
	trending bool
	allow    map[types.Side]bool
}

func (f fakeRegime) Name() string           { return "fake-regime" }
func (f fakeRegime) Ready() bool            { return f.ready }
func (f fakeRegime) Tick(market.CandleTime) {}
func (f fakeRegime) Trending() bool         { return f.trending }
func (f fakeRegime) AllowSide(s types.Side) bool {
	if f.allow == nil {
		return true
	}
	return f.allow[s]
}

type fakeExit struct {
	ready bool
	stop  types.Price
}

func (f fakeExit) Name() string       { return "fake-exit" }
func (f fakeExit) Ready() bool        { return f.ready }
func (f fakeExit) Tick(market.Candle) {}
func (f fakeExit) InitialStop(types.Side, types.Price, market.Candle) types.Price {
	return f.stop
}
func (f fakeExit) UpdateStop(_ types.Side, cur, _, _ types.Price, _ market.Candle) types.Price {
	return cur
}

type testCtx struct {
	instrument string
	acct       *execution.Account
	exit       strategy.ExitStrategy
	regime     strategy.RegimeFilter
	candle     market.CandleTime
	slippage   types.Price
	maxSpread  types.Price
}

func (c testCtx) Instrument() string            { return c.instrument }
func (c testCtx) Account() *execution.Account   { return c.acct }
func (c testCtx) Exit() strategy.ExitStrategy   { return c.exit }
func (c testCtx) Regime() strategy.RegimeFilter { return c.regime }
func (c testCtx) Candle() market.CandleTime     { return c.candle }
func (c testCtx) Slippage() types.Price         { return c.slippage }
func (c testCtx) MaxSpread() types.Price        { return c.maxSpread }
func (c testCtx) DefaultStopPips() types.Pips   { return 0 }

func openReq(id string, side types.Side, price, stop types.Price, units types.Units) *execution.OpenRequest {
	return &execution.OpenRequest{Request: execution.Request{
		TradeCommon: &execution.TradeCommon{ID: id, Instrument: "EURUSD", Side: side, Units: units, Stop: stop},
		Price:       price,
	}}
}

func candleTime(avgSpread types.Price) market.CandleTime {
	return market.CandleTime{Candle: market.Candle{Close: types.PriceFromFloat(1.10), AvgSpread: avgSpread}}
}

// --- tests ------------------------------------------------------------------

func TestDefaultPlanner_NilInputs(t *testing.T) {
	t.Parallel()
	var p DefaultPlanner

	got, stats, err := p.finalize(nil, testCtx{})
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, Stats{}, stats)

	plan := &strategy.StrategyPlan{}
	got, _, err = p.finalize(plan, nil)
	require.NoError(t, err)
	assert.Same(t, plan, got)
}

func TestDefaultPlanner_RegimeNotTrendingSuppressesOpens(t *testing.T) {
	t.Parallel()
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{openReq("o1", types.Long, types.PriceFromFloat(1.10), types.PriceFromFloat(1.09), 1)}}

	_, _, err := DefaultPlanner{}.finalize(plan, testCtx{
		regime: fakeRegime{ready: true, trending: false},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Opens)
}

func TestDefaultPlanner_RegimeAllowSideFiltersOpens(t *testing.T) {
	t.Parallel()
	longOp := openReq("long", types.Long, types.PriceFromFloat(1.10), types.PriceFromFloat(1.09), 1)
	shortOp := openReq("short", types.Short, types.PriceFromFloat(1.10), types.PriceFromFloat(1.11), 1)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{longOp, shortOp}}

	_, _, err := DefaultPlanner{}.finalize(plan, testCtx{
		regime: fakeRegime{ready: true, trending: true, allow: map[types.Side]bool{types.Long: true}},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, "long", plan.Opens[0].ID)
}

func TestDefaultPlanner_RegimeNotReadyLeavesOpens(t *testing.T) {
	t.Parallel()
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{openReq("o1", types.Long, types.PriceFromFloat(1.10), types.PriceFromFloat(1.09), 1)}}

	_, _, err := DefaultPlanner{}.finalize(plan, testCtx{
		regime: fakeRegime{ready: false},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.Len(t, plan.Opens, 1)
}

func TestDefaultPlanner_MaxSpreadGate(t *testing.T) {
	t.Parallel()
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{openReq("o1", types.Long, types.PriceFromFloat(1.10), types.PriceFromFloat(1.09), 1)}}

	_, stats, err := DefaultPlanner{}.finalize(plan, testCtx{
		regime:    strategy.NoopRegime{},
		exit:      strategy.NoopExit{},
		candle:    candleTime(types.Price(30)),
		maxSpread: types.Price(20),
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, 1, stats.SpreadFiltered)
}

func TestDefaultPlanner_OpenFillAdjustAndStats(t *testing.T) {
	t.Parallel()
	entry := types.PriceFromFloat(1.10)
	avgSpread := types.Price(10)
	slippage := types.Price(5)

	// Long buys the ask: +spread+slippage. Units preset so sizing is skipped.
	longOp := openReq("long", types.Long, entry, types.PriceFromFloat(1.09), 1)
	// Short sells the bid: only -slippage.
	shortOp := openReq("short", types.Short, entry, types.PriceFromFloat(1.11), 1)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{longOp, shortOp}}

	_, stats, err := DefaultPlanner{}.finalize(plan, testCtx{
		regime:   strategy.NoopRegime{},
		exit:     strategy.NoopExit{},
		candle:   candleTime(avgSpread),
		slippage: slippage,
	})
	require.NoError(t, err)
	assert.Equal(t, entry+avgSpread+slippage, longOp.Price)
	assert.Equal(t, entry-slippage, shortOp.Price)
	assert.Equal(t, 2, stats.SpreadOpened)
	assert.Equal(t, avgSpread*2, stats.SpreadSum)
}

func TestDefaultPlanner_InitialStopOverride(t *testing.T) {
	t.Parallel()
	op := openReq("o1", types.Long, types.PriceFromFloat(1.10), 0, 1) // no strategy stop
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{op}}
	exitStop := types.PriceFromFloat(1.085)

	_, _, err := DefaultPlanner{}.finalize(plan, testCtx{
		regime: strategy.NoopRegime{},
		exit:   fakeExit{ready: true, stop: exitStop},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.Equal(t, exitStop, op.Stop)
}

func TestDefaultPlanner_SizesWhenUnitsZero(t *testing.T) {
	t.Parallel()
	acct := execution.NewAccount("t", types.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance
	acct.RiskFraction = types.RateFromFloat(0.01)

	op := openReq("o1", types.Long, types.PriceFromFloat(1.10), types.PriceFromFloat(1.09), 0)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{op}}

	_, _, err := DefaultPlanner{}.finalize(plan, testCtx{
		acct:   acct,
		regime: strategy.NoopRegime{},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.NotZero(t, op.Units, "planner should size the position")
}

func TestDefaultPlanner_SizingErrorPropagates(t *testing.T) {
	t.Parallel()
	acct := execution.NewAccount("t", types.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance
	acct.RiskFraction = types.RateFromFloat(0.01)

	// Entry == Stop is an invalid sizing request.
	op := openReq("o1", types.Long, types.PriceFromFloat(1.10), types.PriceFromFloat(1.10), 0)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{op}}

	_, _, err := DefaultPlanner{}.finalize(plan, testCtx{
		acct:   acct,
		regime: strategy.NoopRegime{},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.Error(t, err)
}

// --- PlanSignal tests -------------------------------------------------------

func TestPlanSignal_FlatHolds(t *testing.T) {
	t.Parallel()
	plan, stats, err := DefaultPlanner{}.PlanSignal(strategy.Hold("no signal"), testCtx{
		regime: strategy.NoopRegime{},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.Equal(t, Stats{}, stats)
	assert.True(t, plan.Empty(), "flat signal should produce no opens/closes")
	assert.Equal(t, "no signal", plan.Reason)
}

func TestPlanSignal_LongOpensPosition(t *testing.T) {
	t.Parallel()
	entry := types.PriceFromFloat(1.10)
	avgSpread := types.Price(10)

	plan, stats, err := DefaultPlanner{}.PlanSignal(strategy.Signal{Side: types.Long, Reason: "test-long"}, testCtx{
		instrument: "EURUSD",
		regime:     strategy.NoopRegime{},
		exit:       strategy.NoopExit{},
		candle:     market.CandleTime{Candle: market.Candle{Close: entry, AvgSpread: avgSpread}},
	})
	require.NoError(t, err)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, types.Long, plan.Opens[0].Side)
	assert.Equal(t, "EURUSD", plan.Opens[0].Instrument)
	// Fill-price adjust: long buys at ask (close + spread).
	assert.Equal(t, entry+avgSpread, plan.Opens[0].Price)
	assert.Equal(t, 1, stats.SpreadOpened)
	assert.Empty(t, plan.Closes)
}

func TestPlanSignal_ReversalClosesOpposingSide(t *testing.T) {
	t.Parallel()
	// Build a small account just to host open lots; RiskFraction left at zero so
	// the planner skips sizing (stop is also 0 — this test focuses on close logic).
	acct := execution.NewAccount("t", types.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance

	// Plant an open short lot.
	shortLot := &execution.Lot{
		TradeCommon: &execution.TradeCommon{ID: "s1", Instrument: "EURUSD", Side: types.Short, Stop: types.PriceFromFloat(1.12), Units: 1000},
		State:       execution.LotOpen,
	}
	require.NoError(t, acct.Lots.Add(shortLot))

	// Pre-size the open so the planner does not attempt sizing (avoids stop=0 error).
	entry := types.PriceFromFloat(1.10)
	plan, _, err := DefaultPlanner{}.PlanSignal(strategy.Signal{Side: types.Long, Reason: "flip"}, testCtx{
		instrument: "EURUSD",
		acct:       acct,
		regime:     strategy.NoopRegime{},
		exit:       fakeExit{ready: true, stop: types.PriceFromFloat(1.09)},
		candle:     market.CandleTime{Candle: market.Candle{Close: entry, AvgSpread: 0}},
	})
	require.NoError(t, err)
	require.Len(t, plan.Closes, 1, "opposing short lot must be closed")
	assert.Equal(t, "s1", plan.Closes[0].Lot.ID)
	require.Len(t, plan.Opens, 1, "new long position must be opened")
	assert.Equal(t, types.Long, plan.Opens[0].Side)
}

func TestPlanSignal_SameSideNotClosed(t *testing.T) {
	t.Parallel()
	acct := execution.NewAccount("t", types.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance

	// Plant an open long lot — same side as the incoming signal.
	longLot := &execution.Lot{
		TradeCommon: &execution.TradeCommon{ID: "l1", Instrument: "EURUSD", Side: types.Long, Stop: types.PriceFromFloat(1.08), Units: 1000},
		State:       execution.LotOpen,
	}
	require.NoError(t, acct.Lots.Add(longLot))

	// Use an exit strategy that provides a stop so sizing can proceed without error.
	entry := types.PriceFromFloat(1.10)
	plan, _, err := DefaultPlanner{}.PlanSignal(strategy.Signal{Side: types.Long, Reason: "add-long"}, testCtx{
		instrument: "EURUSD",
		acct:       acct,
		regime:     strategy.NoopRegime{},
		exit:       fakeExit{ready: true, stop: types.PriceFromFloat(1.09)},
		candle:     market.CandleTime{Candle: market.Candle{Close: entry, AvgSpread: 0}},
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Closes, "same-side lot must not be closed")
	require.Len(t, plan.Opens, 1)
}

func TestPlanSignal_RegimeSuppressesOpen(t *testing.T) {
	t.Parallel()
	plan, _, err := DefaultPlanner{}.PlanSignal(strategy.Signal{Side: types.Long, Reason: "long"}, testCtx{
		instrument: "EURUSD",
		regime:     fakeRegime{ready: true, trending: false},
		exit:       strategy.NoopExit{},
		candle:     candleTime(0),
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Opens, "regime gate must suppress the open")
}

func TestPlanSignal_ExitStrategyOverridesStop(t *testing.T) {
	t.Parallel()
	entry := types.PriceFromFloat(1.10)
	exitStop := types.PriceFromFloat(1.085)

	plan, _, err := DefaultPlanner{}.PlanSignal(strategy.Signal{Side: types.Long, Reason: "long"}, testCtx{
		instrument: "EURUSD",
		regime:     strategy.NoopRegime{},
		exit:       fakeExit{ready: true, stop: exitStop},
		candle:     market.CandleTime{Candle: market.Candle{Close: entry, AvgSpread: 0}},
	})
	require.NoError(t, err)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, exitStop, plan.Opens[0].Stop)
}

func TestPlanSignal_SizesWhenAccountPresent(t *testing.T) {
	t.Parallel()
	acct := execution.NewAccount("t", types.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance
	acct.RiskFraction = types.RateFromFloat(0.01)

	entry := types.PriceFromFloat(1.10)
	exitStop := types.PriceFromFloat(1.09)

	plan, _, err := DefaultPlanner{}.PlanSignal(strategy.Signal{Side: types.Long, Reason: "long"}, testCtx{
		instrument: "EURUSD",
		acct:       acct,
		regime:     strategy.NoopRegime{},
		exit:       fakeExit{ready: true, stop: exitStop},
		candle:     market.CandleTime{Candle: market.Candle{Close: entry, AvgSpread: 0}},
	})
	require.NoError(t, err)
	require.Len(t, plan.Opens, 1)
	assert.NotZero(t, plan.Opens[0].Units, "planner must size the position")
}

func TestDefaultPlanner_CloseFillAdjust(t *testing.T) {
	t.Parallel()
	avgSpread := types.Price(10)
	slippage := types.Price(5)
	base := types.PriceFromFloat(1.10)

	// Closing a short means buying the ask: +spread+slippage.
	shortClose := &execution.CloseRequest{
		Request: execution.Request{TradeCommon: &execution.TradeCommon{ID: "c1"}, Price: base},
		Lot:     &execution.Lot{TradeCommon: &execution.TradeCommon{ID: "c1", Side: types.Short}},
	}
	// Closing a long means selling the bid: only -slippage.
	longClose := &execution.CloseRequest{
		Request: execution.Request{TradeCommon: &execution.TradeCommon{ID: "c2"}, Price: base},
		Lot:     &execution.Lot{TradeCommon: &execution.TradeCommon{ID: "c2", Side: types.Long}},
	}
	plan := &strategy.StrategyPlan{Closes: []*execution.CloseRequest{shortClose, longClose}}

	_, _, err := DefaultPlanner{}.finalize(plan, testCtx{
		regime:   strategy.NoopRegime{},
		exit:     strategy.NoopExit{},
		candle:   candleTime(avgSpread),
		slippage: slippage,
	})
	require.NoError(t, err)
	assert.Equal(t, base+avgSpread+slippage, shortClose.Price)
	assert.Equal(t, base-slippage, longClose.Price)
}
