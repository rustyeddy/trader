package stress

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

func ct(close float64) *market.CandleTime {
	p := types.PriceFromFloat(close)
	return &market.CandleTime{
		Candle: market.Candle{Open: p, High: p, Low: p, Close: p},
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
	sig := s.Update(context.Background(), nil, nil)
	assert.Equal(t, types.Flat, sig.Side)
}

func TestUpdate_TradeEvery1_OpensOnFirstBar(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "long"})
	sig := s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Long, sig.Side)
}

func TestUpdate_TradeEvery3_WaitsBeforeOpening(t *testing.T) {
	s, _ := New(Config{TradeEvery: 3, StopBps: 20, Side: "long"})

	sig := s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Flat, sig.Side, "candle 1: should wait")

	sig = s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Flat, sig.Side, "candle 2: should wait")

	sig = s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Long, sig.Side, "candle 3: should open")
}

func TestUpdate_SkipsWhenInPosition(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "long"})

	run := &backtest.Backtest{
		Request: &backtest.BacktestRequest{Instrument: "EURUSD"},
		State:   &backtest.BacktestRun{},
	}

	// First call should signal long.
	sig := s.Update(context.Background(), ct(1.1000), run)
	assert.Equal(t, types.Long, sig.Side)

	// Simulate position open by adding a lot.
	lb := &execution.LotBook{}
	lb.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "test-lot-1"}})
	run.State.Lots = lb

	// Subsequent calls skip because a position is open.
	sig = s.Update(context.Background(), ct(1.1000), run)
	assert.Equal(t, types.Flat, sig.Side)
	assert.Equal(t, "in position", sig.Reason)
}

func TestUpdate_SideLong(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "long"})
	sig := s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Long, sig.Side)
}

func TestUpdate_SideShort(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "short"})
	sig := s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Short, sig.Side)
}

func TestUpdate_SideAlternate(t *testing.T) {
	s, _ := New(Config{TradeEvery: 1, StopBps: 20, Side: "alternate"})

	sig := s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Long, sig.Side, "first trade should be Long")

	sig = s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Short, sig.Side, "second trade should be Short")

	sig = s.Update(context.Background(), ct(1.1000), nil)
	assert.Equal(t, types.Long, sig.Side, "third trade should be Long again")
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
