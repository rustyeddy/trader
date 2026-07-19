package bollingerfade

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

const scale = float64(types.PriceScale) // 100_000

// flat returns a candle where OHLC = close (flat bar at a given price).
func flat(close float64) *market.Candle {
	p := types.Price(close * scale)
	return &market.Candle{Open: p, High: p, Low: p, Close: p}
}

// warmup feeds period flat candles at price 1.0 and asserts the strategy is ready.
func warmup(t *testing.T, f *Fade) {
	t.Helper()
	for range 25 {
		sig := f.Update(context.Background(), flat(1.0), nil)
		require.Equal(t, types.Flat, sig.Side)
	}
	require.True(t, f.Ready())
}

// makeLot builds a minimal open lot snapshot for the given side.
func makeLot(side types.Side) *account.LotBook {
	lb := &account.LotBook{}
	tc := &account.TradeCommon{ID: "test-lot"}
	tc.Side = side
	lb.Add(&account.Lot{TradeCommon: tc, State: account.LotOpen})
	return lb
}

// TestFade_NoOpensBeforeReady verifies warmup suppression.
func TestFade_NoOpensBeforeReady(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 5, Multiplier: 2.0, ATRPeriod: 3, ATRMult: 1.5})
	require.NoError(t, err)
	for range 4 {
		sig := f.Update(context.Background(), flat(1.0), nil)
		assert.Equal(t, types.Flat, sig.Side, "no opens during warmup")
	}
}

// TestFade_LongEntryBelowLowerBand verifies a long signal when close < lower band.
func TestFade_LongEntryBelowLowerBand(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	sig := f.Update(context.Background(), flat(0.95), nil)
	assert.Equal(t, types.Long, sig.Side)
}

// TestFade_ShortEntryAboveUpperBand verifies a short signal when close > upper band.
func TestFade_ShortEntryAboveUpperBand(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	sig := f.Update(context.Background(), flat(1.05), nil)
	assert.Equal(t, types.Short, sig.Side)
}

// TestFade_NoEntryWithinBands verifies no signal when price is inside the bands.
func TestFade_NoEntryWithinBands(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	sig := f.Update(context.Background(), flat(1.0), nil)
	assert.Equal(t, types.Flat, sig.Side)
}

// TestFade_NoNewEntryWhenAlreadyOpen verifies the single-position guard.
func TestFade_NoNewEntryWhenAlreadyOpen(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(types.Long)}}
	sig := f.Update(context.Background(), flat(0.95), run)
	assert.Equal(t, types.Flat, sig.Side, "must not open when position already exists")
}

// TestFade_CloseLongAtMiddle verifies the mean-reversion exit for a long lot.
func TestFade_CloseLongAtMiddle(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(types.Long)}}

	sig := f.Update(context.Background(), flat(1.0), run)
	assert.True(t, sig.CloseAll, "long must be closed when price returns to middle")
	assert.Equal(t, "bb-revert", sig.Reason)
}

// TestFade_CloseShortAtMiddle verifies the mean-reversion exit for a short lot.
func TestFade_CloseShortAtMiddle(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(types.Short)}}

	sig := f.Update(context.Background(), flat(1.0), run)
	assert.True(t, sig.CloseAll, "short must be closed when price returns to middle")
	assert.Equal(t, "bb-revert", sig.Reason)
}

// TestFade_LongNotClosedBelowMiddle verifies no premature close of a long.
func TestFade_LongNotClosedBelowMiddle(t *testing.T) {
	t.Parallel()
	f, err := New(Config{Period: 20, Multiplier: 2.0, ATRPeriod: 14, ATRMult: 1.5})
	require.NoError(t, err)
	warmup(t, f)

	run := &backtest.Backtest{State: &backtest.BacktestRun{Lots: makeLot(types.Long)}}

	sig := f.Update(context.Background(), flat(0.97), run)
	assert.False(t, sig.CloseAll, "long must not close while price is below middle")
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
