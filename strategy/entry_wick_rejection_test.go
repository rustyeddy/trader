package strategy

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wrPx(f float64) types.Price { return types.PriceFromFloat(f) }

func wrQuiet(close float64) market.Candle {
	return market.Candle{Open: wrPx(close), High: wrPx(close + 0.5), Low: wrPx(close - 0.5), Close: wrPx(close)}
}

func wrBullishRejection() market.Candle {
	return market.Candle{Open: wrPx(100), High: wrPx(101), Low: wrPx(90), Close: wrPx(100.5)}
}

func newTestWickRejectionEntry(t *testing.T) *WickRejectionEntry {
	t.Helper()
	e, err := NewWickRejectionEntry(0.5, 0.3, 0.5, 1, 2, types.PriceScale)
	require.NoError(t, err)
	return e
}

func TestWickRejectionEntry_NotReadyBeforeWarmup(t *testing.T) {
	t.Parallel()
	e := newTestWickRejectionEntry(t)
	assert.False(t, e.Ready())
	e.Tick(wrQuiet(100))
	assert.False(t, e.Ready())
}

func TestWickRejectionEntry_TriggeredOnMatchingBar(t *testing.T) {
	t.Parallel()
	e := newTestWickRejectionEntry(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		e.Tick(wrQuiet(c))
	}
	require.True(t, e.Ready())
	assert.False(t, e.Triggered(types.Long, time.Now(), market.Candle{}), "no rejection bar ticked yet")

	e.Tick(wrBullishRejection())
	assert.True(t, e.Triggered(types.Long, time.Now(), market.Candle{}))
	assert.False(t, e.Triggered(types.Short, time.Now(), market.Candle{}), "wrong side must not trigger")
}

func TestWickRejectionEntry_ResetClearsWindow(t *testing.T) {
	t.Parallel()
	e := newTestWickRejectionEntry(t)
	for _, c := range []float64{100, 100.2, 100.1} {
		e.Tick(wrQuiet(c))
	}
	e.Tick(wrBullishRejection())
	require.True(t, e.Triggered(types.Long, time.Now(), market.Candle{}))

	e.Reset()
	assert.False(t, e.Ready(), "reset must drop the lookback window")
	assert.False(t, e.Triggered(types.Long, time.Now(), market.Candle{}))
}

func TestWickRejectionEntry_Name(t *testing.T) {
	t.Parallel()
	e := newTestWickRejectionEntry(t)
	assert.Equal(t, "rejection-candle(wick-rejection)", e.Name())
}

func TestNewWickRejectionEntry_PropagatesConstructorError(t *testing.T) {
	t.Parallel()
	_, err := NewWickRejectionEntry(5.0, 0.3, 0.5, 1, 2, types.PriceScale)
	assert.ErrorContains(t, err, "min-wick-ratio")
}

func TestNewWickRejectionEntry_InvalidLookbackErrors(t *testing.T) {
	t.Parallel()
	// lookback validation is owned by candlepattern.NewWickRejection;
	// confirm the error surfaces through WickRejectionEntry unmodified.
	e, err := NewWickRejectionEntry(0.5, 0.3, 0.5, 0, 2, types.PriceScale)
	require.Error(t, err)
	assert.Nil(t, e)
}
