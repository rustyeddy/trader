package service

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// ── oandaCandleToCandleTime ───────────────────────────────────────────────────

func TestOandaCandleToCandleTime_MidPrice(t *testing.T) {
	t.Parallel()
	c := oanda.Candle{
		Time:     time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
		BidOpen:  1.09000, AskOpen:  1.09010,
		BidHigh:  1.09500, AskHigh:  1.09510,
		BidLow:   1.08900, AskLow:   1.08910,
		BidClose: 1.09200, AskClose: 1.09210,
		Complete: true,
	}
	ct := oandaCandleToCandleTime(c, "EURUSD")
	scale := float64(trader.PriceScale)
	assert.InDelta(t, (1.09000+1.09010)/2*scale, float64(ct.Open), 1)
	assert.InDelta(t, (1.09200+1.09210)/2*scale, float64(ct.Close), 1)
	assert.InDelta(t, (1.09500+1.09510)/2*scale, float64(ct.High), 1)
	assert.InDelta(t, (1.08900+1.08910)/2*scale, float64(ct.Low), 1)
	assert.Equal(t, trader.FromTime(c.Time), ct.Timestamp)
}

func TestOandaCandleToCandleTime_SpreadRecorded(t *testing.T) {
	t.Parallel()
	// 1-pip spread on a 5-decimal pair.
	c := oanda.Candle{
		BidClose: 1.10000, AskClose: 1.10010,
		BidOpen: 1.10000, AskOpen: 1.10010,
		BidHigh: 1.10000, AskHigh: 1.10010,
		BidLow:  1.10000, AskLow:  1.10010,
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
	trades := []trader.LiveTrade{
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
	lt.sync([]trader.LiveTrade{{ID: "100", Units: 1000}})
	assert.Equal(t, 1, lt.toLotBook().Len())

	// Trade 100 no longer present — should be pruned.
	lt.sync([]trader.LiveTrade{})
	assert.Equal(t, 0, lt.toLotBook().Len())
}

func TestLiveLotsTracker_SideFromUnits(t *testing.T) {
	t.Parallel()
	var lt liveLotsTracker
	lt.sync([]trader.LiveTrade{
		{ID: "long",  Units: 1000},
		{ID: "short", Units: -500},
	})
	lb := lt.toLotBook()
	var sides []trader.Side
	_ = lb.Range(func(lot *trader.Lot) error {
		sides = append(sides, lot.Side)
		return nil
	})
	require.Len(t, sides, 2)
	assert.Contains(t, sides, trader.Long)
	assert.Contains(t, sides, trader.Short)
}

// ── convertPlan ──────────────────────────────────────────────────────────────

func makeTestAdapter() *CandleStrategyAdapter {
	return &CandleStrategyAdapter{
		instNorm: "EURUSD",
		scale:    trader.PriceScale,
		regime:   trader.NoopRegime{},
	}
}

func TestConvertPlan_NilPlanReturnsNil(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()
	assert.Nil(t, a.convertPlan(nil, trader.CandleTime{}, trader.LivePrice{}))
}

func TestConvertPlan_EmptyPlanReturnsNil(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()
	assert.Nil(t, a.convertPlan(&trader.StrategyPlan{}, trader.CandleTime{}, trader.LivePrice{}))
}

func TestConvertPlan_OpenLongConverted(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()

	scale := float64(trader.PriceScale)
	close := trader.Price(math.Round(1.10000 * scale))
	stop := trader.Price(math.Round(1.09000 * scale)) // 100-pip stop

	tc := &trader.TradeCommon{}
	tc.Side = trader.Long
	open := trader.NewOpenRequest("EURUSD", &trader.CandleTime{
		Candle:    trader.Candle{Close: close},
		Timestamp: trader.FromTime(time.Now()),
	}, trader.Long, stop, 0, "test")

	plan := &trader.StrategyPlan{Opens: []*trader.OpenRequest{open}}
	ct := trader.CandleTime{Candle: trader.Candle{Close: close}}

	live := a.convertPlan(plan, ct, trader.LivePrice{})
	require.NotNil(t, live)
	require.NotNil(t, live.Open)
	assert.Equal(t, "long", live.Open.Side)
	assert.Greater(t, live.Open.StopPips, 0.0)
}

func TestConvertPlan_CloseIDsPopulated(t *testing.T) {
	t.Parallel()
	a := makeTestAdapter()

	tc := &trader.TradeCommon{ID: "oanda-trade-999"}
	lot := &trader.Lot{TradeCommon: tc, State: trader.LotOpen}
	cr := &trader.CloseRequest{
		Request: trader.Request{
			TradeCommon: tc,
			RequestType: trader.RequestClose,
		},
		Lot: lot,
	}

	plan := &trader.StrategyPlan{Closes: []*trader.CloseRequest{cr}}
	live := a.convertPlan(plan, trader.CandleTime{}, trader.LivePrice{})
	require.NotNil(t, live)
	assert.Equal(t, []string{"oanda-trade-999"}, live.CloseIDs)
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
