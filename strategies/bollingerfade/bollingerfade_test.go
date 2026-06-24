package bollingerfade

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
)

const scale = float64(market.PriceScale) // 100_000

// flat returns a candle where OHLC = close (flat bar at a given price).
func flat(close float64) *market.CandleTime {
	p := market.Price(close * scale)
	return &market.CandleTime{
		Candle: market.Candle{Open: p, High: p, Low: p, Close: p},
	}
}

// warmup feeds period flat candles at price 1.0 and asserts the strategy is ready.
func warmup(t *testing.T, f *Fade) {
	t.Helper()
	// BB(20) + ATR(14) need max(20, 15) bars; feed 25 to be safe.
	for range 25 {
		plan := f.Update(context.Background(), flat(1.0), nil)
		require.Empty(t, plan.Opens)
	}
	require.True(t, f.Ready())
}

// makeLot builds a minimal open lot snapshot for the given side.
func makeLot(side market.Side) *execution.LotBook {
	lb := &execution.LotBook{}
	tc := &execution.TradeCommon{ID: "test-lot"}
	tc.Side = side
	lb.Add(&execution.Lot{TradeCommon: tc, State: execution.LotOpen})
	return lb
}

// TestFade_NoOpensBeforeReady verifies warmup suppression.
func TestFade_NoOpensBeforeReady(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 5, Multiplier: 2.0, ATRPeriod: 3, ATRMult: 1.5})
	require.NoError(t, err)
	for range 4 {
		plan := f.Update(context.Background(), flat(1.0), nil)
		assert.Empty(t, plan.Opens, "no opens during warmup")
	}
}

// TestFade_LongEntryBelowLowerBand verifies a long signal when close < lower band.
func TestFade_LongEntryBelowLowerBand(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	// Push price sharply down to force close below lower band.
	// After 25 flat bars at 1.0 the BB middle ≈ 1.0, stddev ≈ 0, bands tight.
	// A move to 0.95 will be well outside the lower band.
	plan := f.Update(context.Background(), flat(0.95), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, market.Long, plan.Opens[0].Side)
	assert.NotZero(t, plan.Opens[0].Stop, "stop must be set")
	assert.True(t, plan.Opens[0].Stop < plan.Opens[0].Price,
		"long stop must be below entry price")
}

// TestFade_ShortEntryAboveUpperBand verifies a short signal when close > upper band.
func TestFade_ShortEntryAboveUpperBand(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	plan := f.Update(context.Background(), flat(1.05), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, market.Short, plan.Opens[0].Side)
	assert.NotZero(t, plan.Opens[0].Stop)
	assert.True(t, plan.Opens[0].Stop > plan.Opens[0].Price,
		"short stop must be above entry price")
}

// TestFade_NoEntryWithinBands verifies no signal when price is inside the bands.
func TestFade_NoEntryWithinBands(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	// Price stays at 1.0 — within bands (bands are near-zero width after flat warmup).
	plan := f.Update(context.Background(), flat(1.0), nil)
	assert.Empty(t, plan.Opens)
}

// TestFade_NoNewEntryWhenAlreadyOpen verifies the single-position guard.
func TestFade_NoNewEntryWhenAlreadyOpen(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(market.Long)}}
	// Even with a band-crossing price, no new open when a lot is already active.
	plan := f.Update(context.Background(), flat(0.95), run)
	assert.Empty(t, plan.Opens, "must not open when position already exists")
}

// TestFade_CloseLongAtMiddle verifies the mean-reversion exit for a long lot.
func TestFade_CloseLongAtMiddle(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	// Simulate an open long position. Middle band ≈ 1.0 after flat warmup.
	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(market.Long)}}

	// Price at 1.0 — at or above middle band → should close the long.
	plan := f.Update(context.Background(), flat(1.0), run)
	require.Len(t, plan.Closes, 1, "long must be closed when price returns to middle")
	assert.Equal(t, "bb-revert", plan.Closes[0].Reason)
}

// TestFade_CloseShortAtMiddle verifies the mean-reversion exit for a short lot.
func TestFade_CloseShortAtMiddle(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(market.Short)}}

	// Price at 1.0 — at or below middle band → should close the short.
	plan := f.Update(context.Background(), flat(1.0), run)
	require.Len(t, plan.Closes, 1, "short must be closed when price returns to middle")
	assert.Equal(t, "bb-revert", plan.Closes[0].Reason)
}

// TestFade_LongNotClosedBelowMiddle verifies no premature close of a long.
func TestFade_LongNotClosedBelowMiddle(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(market.Long)}}

	// Price still below middle — long should stay open.
	plan := f.Update(context.Background(), flat(0.97), run)
	assert.Empty(t, plan.Closes, "long must not close while price is below middle")
}

// TestFade_Name checks the strategy name format.
func TestFade_Name(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	assert.Equal(t, "BB-FADE(20,2.0,atr=14×1.5)", f.Name())
}

// TestFade_Reset verifies the strategy can be reset and re-warmed.
func TestFade_Reset(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 5, Multiplier: 2.0, ATRPeriod: 3, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)
	require.True(t, f.Ready())
	f.Reset()
	assert.False(t, f.Ready())
}
