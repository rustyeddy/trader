package planner

import (
	"testing"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles -----------------------------------------------------------

type fakeRegime struct {
	ready    bool
	trending bool
	allow    map[market.Side]bool
}

func (f fakeRegime) Name() string           { return "fake-regime" }
func (f fakeRegime) Ready() bool            { return f.ready }
func (f fakeRegime) Tick(market.CandleTime) {}
func (f fakeRegime) Trending() bool         { return f.trending }
func (f fakeRegime) AllowSide(s market.Side) bool {
	if f.allow == nil {
		return true
	}
	return f.allow[s]
}

type fakeExit struct {
	ready bool
	stop  market.Price
}

func (f fakeExit) Name() string       { return "fake-exit" }
func (f fakeExit) Ready() bool        { return f.ready }
func (f fakeExit) Tick(market.Candle) {}
func (f fakeExit) InitialStop(market.Side, market.Price, market.Candle) market.Price {
	return f.stop
}
func (f fakeExit) UpdateStop(_ market.Side, cur, _, _ market.Price, _ market.Candle) market.Price {
	return cur
}

type testCtx struct {
	acct      *execution.Account
	exit      strategy.ExitStrategy
	regime    strategy.RegimeFilter
	candle    market.CandleTime
	slippage  market.Price
	maxSpread market.Price
}

func (c testCtx) Account() *execution.Account   { return c.acct }
func (c testCtx) Exit() strategy.ExitStrategy   { return c.exit }
func (c testCtx) Regime() strategy.RegimeFilter { return c.regime }
func (c testCtx) Candle() market.CandleTime     { return c.candle }
func (c testCtx) Slippage() market.Price        { return c.slippage }
func (c testCtx) MaxSpread() market.Price       { return c.maxSpread }

func openReq(id string, side market.Side, price, stop market.Price, units market.Units) *execution.OpenRequest {
	return &execution.OpenRequest{Request: execution.Request{
		TradeCommon: &execution.TradeCommon{ID: id, Instrument: "EURUSD", Side: side, Units: units, Stop: stop},
		Price:       price,
	}}
}

func candleTime(avgSpread market.Price) market.CandleTime {
	return market.CandleTime{Candle: market.Candle{Close: market.PriceFromFloat(1.10), AvgSpread: avgSpread}}
}

// --- tests ------------------------------------------------------------------

func TestDefaultPlanner_NilInputs(t *testing.T) {
	t.Parallel()
	var p DefaultPlanner

	got, stats, err := p.Plan(nil, testCtx{})
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, Stats{}, stats)

	plan := &strategy.StrategyPlan{}
	got, _, err = p.Plan(plan, nil)
	require.NoError(t, err)
	assert.Same(t, plan, got)
}

func TestDefaultPlanner_RegimeNotTrendingSuppressesOpens(t *testing.T) {
	t.Parallel()
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{openReq("o1", market.Long, market.PriceFromFloat(1.10), market.PriceFromFloat(1.09), 1)}}

	_, _, err := DefaultPlanner{}.Plan(plan, testCtx{
		regime: fakeRegime{ready: true, trending: false},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Opens)
}

func TestDefaultPlanner_RegimeAllowSideFiltersOpens(t *testing.T) {
	t.Parallel()
	longOp := openReq("long", market.Long, market.PriceFromFloat(1.10), market.PriceFromFloat(1.09), 1)
	shortOp := openReq("short", market.Short, market.PriceFromFloat(1.10), market.PriceFromFloat(1.11), 1)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{longOp, shortOp}}

	_, _, err := DefaultPlanner{}.Plan(plan, testCtx{
		regime: fakeRegime{ready: true, trending: true, allow: map[market.Side]bool{market.Long: true}},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, "long", plan.Opens[0].ID)
}

func TestDefaultPlanner_RegimeNotReadyLeavesOpens(t *testing.T) {
	t.Parallel()
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{openReq("o1", market.Long, market.PriceFromFloat(1.10), market.PriceFromFloat(1.09), 1)}}

	_, _, err := DefaultPlanner{}.Plan(plan, testCtx{
		regime: fakeRegime{ready: false},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.Len(t, plan.Opens, 1)
}

func TestDefaultPlanner_MaxSpreadGate(t *testing.T) {
	t.Parallel()
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{openReq("o1", market.Long, market.PriceFromFloat(1.10), market.PriceFromFloat(1.09), 1)}}

	_, stats, err := DefaultPlanner{}.Plan(plan, testCtx{
		regime:    strategy.NoopRegime{},
		exit:      strategy.NoopExit{},
		candle:    candleTime(market.Price(30)),
		maxSpread: market.Price(20),
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, 1, stats.SpreadFiltered)
}

func TestDefaultPlanner_OpenFillAdjustAndStats(t *testing.T) {
	t.Parallel()
	entry := market.PriceFromFloat(1.10)
	avgSpread := market.Price(10)
	slippage := market.Price(5)

	// Long buys the ask: +spread+slippage. Units preset so sizing is skipped.
	longOp := openReq("long", market.Long, entry, market.PriceFromFloat(1.09), 1)
	// Short sells the bid: only -slippage.
	shortOp := openReq("short", market.Short, entry, market.PriceFromFloat(1.11), 1)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{longOp, shortOp}}

	_, stats, err := DefaultPlanner{}.Plan(plan, testCtx{
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
	op := openReq("o1", market.Long, market.PriceFromFloat(1.10), 0, 1) // no strategy stop
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{op}}
	exitStop := market.PriceFromFloat(1.085)

	_, _, err := DefaultPlanner{}.Plan(plan, testCtx{
		regime: strategy.NoopRegime{},
		exit:   fakeExit{ready: true, stop: exitStop},
		candle: candleTime(0),
	})
	require.NoError(t, err)
	assert.Equal(t, exitStop, op.Stop)
}

func TestDefaultPlanner_SizesWhenUnitsZero(t *testing.T) {
	t.Parallel()
	acct := execution.NewAccount("t", market.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance
	acct.RiskFraction = market.RateFromFloat(0.01)

	op := openReq("o1", market.Long, market.PriceFromFloat(1.10), market.PriceFromFloat(1.09), 0)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{op}}

	_, _, err := DefaultPlanner{}.Plan(plan, testCtx{
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
	acct := execution.NewAccount("t", market.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance
	acct.RiskFraction = market.RateFromFloat(0.01)

	// Entry == Stop is an invalid sizing request.
	op := openReq("o1", market.Long, market.PriceFromFloat(1.10), market.PriceFromFloat(1.10), 0)
	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{op}}

	_, _, err := DefaultPlanner{}.Plan(plan, testCtx{
		acct:   acct,
		regime: strategy.NoopRegime{},
		exit:   strategy.NoopExit{},
		candle: candleTime(0),
	})
	require.Error(t, err)
}

func TestDefaultPlanner_CloseFillAdjust(t *testing.T) {
	t.Parallel()
	avgSpread := market.Price(10)
	slippage := market.Price(5)
	base := market.PriceFromFloat(1.10)

	// Closing a short means buying the ask: +spread+slippage.
	shortClose := &execution.CloseRequest{
		Request: execution.Request{TradeCommon: &execution.TradeCommon{ID: "c1"}, Price: base},
		Lot:     &execution.Lot{TradeCommon: &execution.TradeCommon{ID: "c1", Side: market.Short}},
	}
	// Closing a long means selling the bid: only -slippage.
	longClose := &execution.CloseRequest{
		Request: execution.Request{TradeCommon: &execution.TradeCommon{ID: "c2"}, Price: base},
		Lot:     &execution.Lot{TradeCommon: &execution.TradeCommon{ID: "c2", Side: market.Long}},
	}
	plan := &strategy.StrategyPlan{Closes: []*execution.CloseRequest{shortClose, longClose}}

	_, _, err := DefaultPlanner{}.Plan(plan, testCtx{
		regime:   strategy.NoopRegime{},
		exit:     strategy.NoopExit{},
		candle:   candleTime(avgSpread),
		slippage: slippage,
	})
	require.NoError(t, err)
	assert.Equal(t, base+avgSpread+slippage, shortClose.Price)
	assert.Equal(t, base-slippage, longClose.Price)
}
