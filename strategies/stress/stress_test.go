package stress

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

func ct(close float64) *trader.CandleTime {
	p := trader.PriceFromFloat(close)
	return &trader.CandleTime{
		Candle: trader.Candle{Open: p, High: p, Low: p, Close: p},
	}
}

func TestNew_Defaults(t *testing.T) {
	s, err := New(Config{})
	require.NoError(t, err)
	assert.Equal(t, 1, s.cfg.TradeEvery)
	assert.Equal(t, 20, s.cfg.StopBps)
	assert.Equal(t, "long", s.cfg.Side)
}

func TestNew_Explicit(t *testing.T) {
	s, err := New(Config{TradeEvery: 5, StopBps: 50, Side: "short"})
	require.NoError(t, err)
	assert.Equal(t, 5, s.cfg.TradeEvery)
	assert.Equal(t, 50, s.cfg.StopBps)
	assert.Equal(t, "short", s.cfg.Side)
}

func TestName(t *testing.T) {
	s, _ := New(Config{TradeEvery: 3, StopBps: 20, Side: "long"})
	assert.Contains(t, s.Name(), "STRESS")
	assert.Contains(t, s.Name(), "every=3")
	assert.Contains(t, s.Name(), "long")
	assert.Contains(t, s.Name(), "20bps")
}

func TestReady_AlwaysTrue(t *testing.T) {
	s, _ := New(Config{})
	assert.True(t, s.Ready())
}

func TestReset(t *testing.T) {
	s, _ := New(Config{TradeEvery: 3})
	s.candleN = 2
	s.sideTurn = 5
	s.Reset()
	assert.Equal(t, 0, s.candleN)
	assert.Equal(t, 0, s.sideTurn)
}

func TestUpdate_NilCandleTime(t *testing.T) {
	s, _ := New(Config{})
	plan := s.Update(context.Background(), nil, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
}

func TestUpdate_TradeEvery1_OpensOnFirstBar(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "long"})
	plan := s.Update(context.Background(), ct(1.1000), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
}

func TestUpdate_TradeEvery3_WaitsBeforeOpening(t *testing.T) {
	s, _ := New(Config{TradeEvery: 3, StopBps: 20, Side: "long"})

	plan := s.Update(context.Background(), ct(1.1000), nil)
	assert.Empty(t, plan.Opens, "candle 1: should wait")

	plan = s.Update(context.Background(), ct(1.1000), nil)
	assert.Empty(t, plan.Opens, "candle 2: should wait")

	plan = s.Update(context.Background(), ct(1.1000), nil)
	require.Len(t, plan.Opens, 1, "candle 3: should open")
}

func TestUpdate_SkipsWhenInPosition(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "long"})

	run := &trader.Backtest{
		Request: &trader.BacktestRequest{Instrument: "EURUSD"},
		State:   &trader.BacktestRun{},
	}

	// First call opens a position.
	plan := s.Update(context.Background(), ct(1.1000), run)
	require.Len(t, plan.Opens, 1)

	// Simulate position open by adding a lot.
	lb := &trader.LotBook{}
	lb.Add(&trader.Lot{TradeCommon: &trader.TradeCommon{ID: "test-lot-1"}})
	run.State.Lots = lb

	// Subsequent calls skip because a position is open.
	plan = s.Update(context.Background(), ct(1.1000), run)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "in position", plan.Reason)
}

func TestUpdate_SideLong_StopBelowClose(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "long"})
	plan := s.Update(context.Background(), ct(1.1000), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
	assert.Less(t, plan.Opens[0].Stop, trader.PriceFromFloat(1.1000))
}

func TestUpdate_SideShort_StopAboveClose(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "short"})
	plan := s.Update(context.Background(), ct(1.1000), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
	assert.Greater(t, plan.Opens[0].Stop, trader.PriceFromFloat(1.1000))
}

func TestUpdate_SideAlternate(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "alternate"})

	plan := s.Update(context.Background(), ct(1.1000), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side, "first trade should be Long")

	plan = s.Update(context.Background(), ct(1.1000), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side, "second trade should be Short")

	plan = s.Update(context.Background(), ct(1.1000), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side, "third trade should be Long again")
}

func TestUpdate_InstrumentFromRun(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "long"})
	run := &trader.Backtest{
		Request: &trader.BacktestRequest{Instrument: "USDJPY"},
		State:   &trader.BacktestRun{},
	}
	plan := s.Update(context.Background(), ct(150.00), run)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, "USDJPY", plan.Opens[0].Instrument)
}

// TestCalcStop_Precision verifies integer-only stop math at a known price.
// close=1.1000 (110000 in Price), StopBps=20 → dist=110000*20/10000=220
// long stop = 110000 - 220 = 109780 = 1.0978
func TestCalcStop_Precision(t *testing.T) {
	s, _ := New(Config{StopBps: 20, Side: "long"})
	candle := ct(1.1000)
	stop := s.calcStop(candle, trader.Long)
	expected := trader.PriceFromFloat(1.1000) - trader.Price(int64(trader.PriceFromFloat(1.1000))*20/10000)
	assert.Equal(t, expected, stop)
}

// TestBuild_ConvertsPctToBps verifies the float→bps boundary conversion.
// stop_pct: 0.2 → 0.2% → 20 bps
// stop_pct: 1.5 → 1.5% → 150 bps
func TestBuild_ConvertsPctToBps(t *testing.T) {
	cases := []struct {
		pct     float64
		wantBps int
	}{
		{0.2, 20},
		{1.5, 150},
		{0.5, 50},
	}
	for _, tc := range cases {
		strat, err := build(map[string]any{"stop_pct": tc.pct})
		require.NoError(t, err)
		assert.Equal(t, tc.wantBps, strat.(*Strategy).cfg.StopBps,
			"stop_pct %.1f should produce %d bps", tc.pct, tc.wantBps)
	}
}

func TestBuild_ValidParams(t *testing.T) {
	params := map[string]any{
		"trade_every": 5,
		"stop_pct":    0.3,
		"side":        "short",
	}
	s, err := build(params)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestBuild_EmptyParams_UsesDefaults(t *testing.T) {
	s, err := build(map[string]any{})
	require.NoError(t, err)
	require.NotNil(t, s)
}
