package service

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/planner"
	"github.com/rustyeddy/trader/strategy"
)

// ── oandaCandleToCandleTime ───────────────────────────────────────────────────

func TestOandaCandleToCandleTime_MidPrice(t *testing.T) {
	t.Parallel()
	c := oanda.Candle{
		Time:    time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
		BidOpen: 1.09000, AskOpen: 1.09010,
		BidHigh: 1.09500, AskHigh: 1.09510,
		BidLow: 1.08900, AskLow: 1.08910,
		BidClose: 1.09200, AskClose: 1.09210,
		Complete: true,
	}
	ct := oandaCandleToCandleTime(c, "EURUSD")
	scale := float64(market.PriceScale)
	assert.InDelta(t, (1.09000+1.09010)/2*scale, float64(ct.Open), 1)
	assert.InDelta(t, (1.09200+1.09210)/2*scale, float64(ct.Close), 1)
	assert.InDelta(t, (1.09500+1.09510)/2*scale, float64(ct.High), 1)
	assert.InDelta(t, (1.08900+1.08910)/2*scale, float64(ct.Low), 1)
	assert.Equal(t, market.FromTime(c.Time), ct.Timestamp)
}

func TestOandaCandleToCandleTime_SpreadRecorded(t *testing.T) {
	t.Parallel()
	// 1-pip spread on a 5-decimal pair.
	c := oanda.Candle{
		BidClose: 1.10000, AskClose: 1.10010,
		BidOpen: 1.10000, AskOpen: 1.10010,
		BidHigh: 1.10000, AskHigh: 1.10010,
		BidLow: 1.10000, AskLow: 1.10010,
		Complete: true,
	}
	ct := oandaCandleToCandleTime(c, "EURUSD")
	// spread = ask - bid = 0.00010 * PriceScale = 10 price units = 1 pip
	assert.True(t, ct.AvgSpread > 0, "spread must be positive")
}

// ── barsBefore ───────────────────────────────────────────────────────────────

func TestBarsBefore_D1(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	from := barsBefore(now, "D", 100)
	diff := now.Sub(from)
	// 100 days × 1.4 buffer ≈ 140 days
	assert.True(t, diff >= 130*24*time.Hour && diff <= 150*24*time.Hour,
		"D1 barsBefore should be ~140 days before now, got %v", diff)
}

func TestBarsBefore_H1(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	from := barsBefore(now, "H1", 100)
	diff := now.Sub(from)
	// 100 hours × 1.4 ≈ 140 hours
	assert.True(t, diff >= 130*time.Hour && diff <= 150*time.Hour,
		"H1 barsBefore should be ~140 hours before now, got %v", diff)
}

// ── liveLotsTracker ──────────────────────────────────────────────────────────

func TestLiveLotsTracker_SyncAddsLots(t *testing.T) {
	t.Parallel()
	var lt liveLotsTracker
	trades := []LiveTrade{
		{ID: "100", Units: 1000, EntryPrice: 1.10},
		{ID: "101", Units: -500, EntryPrice: 1.11},
	}
	lt.sync(trades)
	lb := lt.toLotBook()
	require.Equal(t, 2, lb.Len())
}

func TestLiveLotsTracker_SyncRemovesClosedLots(t *testing.T) {
	t.Parallel()
	var lt liveLotsTracker
	lt.sync([]LiveTrade{{ID: "100", Units: 1000}})
	assert.Equal(t, 1, lt.toLotBook().Len())

	// Trade 100 no longer present — should be pruned.
	lt.sync([]LiveTrade{})
	assert.Equal(t, 0, lt.toLotBook().Len())
}

func TestLiveLotsTracker_SideFromUnits(t *testing.T) {
	t.Parallel()
	var lt liveLotsTracker
	lt.sync([]LiveTrade{
		{ID: "long", Units: 1000},
		{ID: "short", Units: -500},
	})
	lb := lt.toLotBook()
	var sides []market.Side
	_ = lb.Range(func(lot *execution.Lot) error {
		sides = append(sides, lot.Side)
		return nil
	})
	require.Len(t, sides, 2)
	assert.Contains(t, sides, market.Long)
	assert.Contains(t, sides, market.Short)
}

// ── convertPlan ──────────────────────────────────────────────────────────────

func makeTestAdapter() *CandleStrategyAdapter {
	return &CandleStrategyAdapter{
		instNorm: "EURUSD",
		scale:    market.PriceScale,
		regime:   strategy.NoopRegime{},
		exit:     strategy.NoopExit{},
		log:      slog.Default(),
	}
}

func TestConvertPlan_NilPlanReturnsNil(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()
	assert.Nil(t, a.convertPlan(nil, LivePrice{}))
}

func TestConvertPlan_EmptyPlanReturnsNil(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()
	assert.Nil(t, a.convertPlan(&strategy.StrategyPlan{}, LivePrice{}))
}

func TestConvertPlan_OpenLongConverted(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()

	scale := float64(market.PriceScale)
	close := market.Price(math.Round(1.10000 * scale))
	stop := market.Price(math.Round(1.09000 * scale)) // 100-pip stop

	open := execution.NewOpenRequest("EURUSD", &market.CandleTime{
		Candle:    market.Candle{Close: close},
		Timestamp: market.FromTime(time.Now()),
	}, market.Long, stop, 0, "test")

	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{open}}

	lp := a.convertPlan(plan, LivePrice{})
	require.NotNil(t, lp)
	require.NotNil(t, lp.Open)
	assert.Equal(t, "long", lp.Open.Side)
	assert.Greater(t, lp.Open.StopPips, 0.0)
}

func TestConvertPlan_OpenWithNoStopSkipped(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()

	scale := float64(market.PriceScale)
	close := market.Price(math.Round(1.10000 * scale))

	// Stop == 0: PlanSignal would normally set it; if it reaches convertPlan
	// with stop=0 still, the open must be skipped.
	open := execution.NewOpenRequest("EURUSD", &market.CandleTime{
		Candle:    market.Candle{Close: close},
		Timestamp: market.FromTime(time.Now()),
	}, market.Long, 0 /*stop*/, 0, "test")

	plan := &strategy.StrategyPlan{Opens: []*execution.OpenRequest{open}}

	lp := a.convertPlan(plan, LivePrice{})
	// Plan has no closes either, so result must be nil.
	assert.Nil(t, lp)
}

func TestConvertPlan_CloseIDsPopulated(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()

	tc := &execution.TradeCommon{ID: "oanda-trade-999"}
	lot := &execution.Lot{TradeCommon: tc, State: execution.LotOpen}
	cr := &execution.CloseRequest{
		Request: execution.Request{
			TradeCommon: tc,
			RequestType: execution.RequestClose,
		},
		Lot: lot,
	}

	plan := &strategy.StrategyPlan{Closes: []*execution.CloseRequest{cr}}
	lp := a.convertPlan(plan, LivePrice{})
	require.NotNil(t, lp)
	assert.Equal(t, []string{"oanda-trade-999"}, lp.CloseIDs)
}

func TestLivePlanContext_ImplementsPlanContext(t *testing.T) {
	t.Parallel()
	// Compile-time interface check: livePlanContext must satisfy planner.PlanContext.
	var _ planner.PlanContext = livePlanContext{}
}

// ── oandaGranToTF ─────────────────────────────────────────────────────────────

func TestOandaGranToTF(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want market.Timeframe
	}{
		{"H1", market.H1},
		{"h1", market.H1},
		{"D", market.D1},
		{"D1", market.D1},
		{"M1", market.M1},
		{"unknown", market.H1},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, oandaGranToTF(tc.in), "input %q", tc.in)
	}
}

// ── warmupFromLocalData ───────────────────────────────────────────────────────

func TestWarmupFromLocalData_PrimesExitStrategy(t *testing.T) {
	t.Parallel()

	// Build a chandelier exit that needs 3 bars to be ready.
	exit, err := strategy.NewChandelierExit(3, 2.0, market.PriceScale)
	require.NoError(t, err)
	require.False(t, exit.Ready())

	// Write 5 synthetic H1 candles into a temp store. Place them at the end of
	// the current month so they fall within the barsBefore(now, "H1", 200) window.
	tmpDir := t.TempDir()
	s := marketdata.NewStoreAt(tmpDir)
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// hoursElapsed = how many H1 slots have passed since start of month.
	hoursElapsed := int(now.Sub(monthStart).Hours())
	// Place 5 real candles in the last 6 hours; pad everything before with zeros.
	startSlot := hoursElapsed - 6
	if startSlot < 0 {
		startSlot = 0
	}
	real := []market.Candle{
		{Open: 110000, High: 111000, Low: 109500, Close: 110500},
		{Open: 110500, High: 112000, Low: 110000, Close: 111500},
		{Open: 111500, High: 113000, Low: 111000, Close: 112000},
		{Open: 112000, High: 113500, Low: 111500, Close: 113000},
		{Open: 113000, High: 114000, Low: 112500, Close: 113500},
	}
	candles := make([]market.Candle, startSlot+len(real))
	copy(candles[startSlot:], real)

	require.NoError(t, s.WriteMonthlyCandles("oanda", "EURUSD", market.H1, monthStart, candles))

	// Swap the global store to point at our temp dir and restore after.
	restore := marketdata.SwapStore(s)
	defer restore()

	a := &CandleStrategyAdapter{
		instNorm:        "EURUSD",
		granularity:     "H1",
		localWarmupBars: 200,
		scale:           market.PriceScale,
		regime:          strategy.NoopRegime{},
		exit:            exit,
		strategy:        &noopStrategy{},
		log:             slog.Default(),
	}

	err = a.warmupFromLocalData(context.Background())
	require.NoError(t, err)

	assert.True(t, exit.Ready(), "exit strategy should be ready after local warmup")
	assert.False(t, a.lastBarTime.IsZero(), "lastBarTime should be set after local warmup")
}

func TestWarmupFromLocalData_NoDataNoError(t *testing.T) {
	t.Parallel()

	// Empty temp store — no candle files at all.
	restore := marketdata.SwapStore(marketdata.NewStoreAt(t.TempDir()))
	defer restore()

	a := &CandleStrategyAdapter{
		instNorm:        "EURUSD",
		granularity:     "H1",
		localWarmupBars: 100,
		scale:           market.PriceScale,
		regime:          strategy.NoopRegime{},
		exit:            strategy.NoopExit{},
		strategy:        &noopStrategy{},
		log:             slog.Default(),
	}

	// Should return nil (missing months silently skipped).
	assert.NoError(t, a.warmupFromLocalData(context.Background()))
}

// noopStrategy is a minimal Strategy for tests that records no state.
type noopStrategy struct{}

func (n *noopStrategy) Name() string            { return "noop" }
func (n *noopStrategy) Reset()                  {}
func (n *noopStrategy) Ready() bool             { return true }
func (n *noopStrategy) StopDescription() string { return "" }
func (n *noopStrategy) Update(_ context.Context, _ *market.CandleTime, _ strategy.StrategyContext) strategy.Signal {
	return strategy.Hold("noop")
}

// ── portfolio config ──────────────────────────────────────────────────────────

func TestLoadPortfolioConfig_Defaults(t *testing.T) {
	t.Parallel()
	// Write a minimal config to a temp file.
	content := []byte(`instruments: []`)
	f := t.TempDir() + "/p.yml"
	require.NoError(t, writeFile(f, content))

	cfg, err := LoadPortfolioConfig(f)
	require.NoError(t, err)
	assert.Equal(t, "practice", cfg.Env)
	assert.Equal(t, 1.0, cfg.RiskPct)
	assert.Equal(t, 10.0, cfg.DrawdownCircuitPct)
}

func TestLoadPortfolioConfig_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := LoadPortfolioConfig("/nonexistent/path.yml")
	require.Error(t, err)
}

// ── circuit breaker ───────────────────────────────────────────────────────────

func TestDrawdownCircuitBreaker_ZeroLimitAlwaysAllows(t *testing.T) {
	t.Parallel()
	cb := &drawdownCircuitBreaker{limitPct: 0}
	assert.True(t, cb.allowOpen(context.Background()))
}

// writeFile is a test helper to write bytes to a path.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
