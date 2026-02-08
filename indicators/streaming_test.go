package indicators

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/pricing"
	"github.com/stretchr/testify/assert"
)

func TestSimpleMAStreaming(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := []pricing.Candle{
		{Open: 100, High: 105, Low: 99, Close: 102, Time: baseTime, Volume: 1000},
		{Open: 102, High: 107, Low: 101, Close: 105, Time: baseTime.Add(time.Hour), Volume: 1100},
		{Open: 105, High: 108, Low: 104, Close: 106, Time: baseTime.Add(2 * time.Hour), Volume: 1200},
		{Open: 106, High: 110, Low: 105, Close: 108, Time: baseTime.Add(3 * time.Hour), Volume: 1300},
		{Open: 108, High: 112, Low: 107, Close: 110, Time: baseTime.Add(4 * time.Hour), Volume: 1400},
	}

	t.Run("basic functionality", func(t *testing.T) {
		ma := NewMA(3)
		assert.Equal(t, "MA(3)", ma.Name())
		assert.Equal(t, 3, ma.Warmup())
		assert.False(t, ma.Ready())
		assert.Equal(t, 0.0, ma.Value())

		// Update with first candle
		ma.Update(candles[0])
		assert.False(t, ma.Ready())

		// Update with second candle
		ma.Update(candles[1])
		assert.False(t, ma.Ready())

		// Update with third candle - should be ready now
		ma.Update(candles[2])
		assert.True(t, ma.Ready())
		expected := (102.0 + 105.0 + 106.0) / 3.0
		assert.InDelta(t, expected, ma.Value(), 0.001)

		// Update with fourth candle - should use last 3
		ma.Update(candles[3])
		assert.True(t, ma.Ready())
		expected = (105.0 + 106.0 + 108.0) / 3.0
		assert.InDelta(t, expected, ma.Value(), 0.001)
	})

	t.Run("reset functionality", func(t *testing.T) {
		ma := NewMA(2)
		ma.Update(candles[0])
		ma.Update(candles[1])
		assert.True(t, ma.Ready())

		ma.Reset()
		assert.False(t, ma.Ready())
		assert.Equal(t, 0.0, ma.Value())
	})

	t.Run("matches batch calculation", func(t *testing.T) {
		ma := NewMA(3)
		for _, c := range candles {
			ma.Update(c)
		}

		// Compare with batch function
		batchResult, _ := MA(candles, 3)
		assert.InDelta(t, batchResult, ma.Value(), 0.001)
	})
}

func TestExponentialMAStreaming(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := []pricing.Candle{
		{Close: 102, Time: baseTime},
		{Close: 105, Time: baseTime.Add(time.Hour)},
		{Close: 106, Time: baseTime.Add(2 * time.Hour)},
		{Close: 108, Time: baseTime.Add(3 * time.Hour)},
		{Close: 110, Time: baseTime.Add(4 * time.Hour)},
		{Close: 111, Time: baseTime.Add(5 * time.Hour)},
		{Close: 113, Time: baseTime.Add(6 * time.Hour)},
	}

	t.Run("basic functionality", func(t *testing.T) {
		ema := NewEMA(3)
		assert.Equal(t, "EMA(3)", ema.Name())
		assert.Equal(t, 3, ema.Warmup())
		assert.False(t, ema.Ready())
		assert.Equal(t, 0.0, ema.Value())

		// Update with first two candles
		ema.Update(candles[0])
		ema.Update(candles[1])
		assert.False(t, ema.Ready())

		// Update with third candle - should initialize with SMA
		ema.Update(candles[2])
		assert.True(t, ema.Ready())
		expectedSMA := (102.0 + 105.0 + 106.0) / 3.0
		assert.InDelta(t, expectedSMA, ema.Value(), 0.001)

		// Update with fourth candle - should apply EMA formula
		ema.Update(candles[3])
		assert.True(t, ema.Ready())
		// EMA formula: (close - prevEMA) * multiplier + prevEMA
		// multiplier = 2/(3+1) = 0.5
		expectedEMA := (108.0-expectedSMA)*0.5 + expectedSMA
		assert.InDelta(t, expectedEMA, ema.Value(), 0.001)
	})

	t.Run("reset functionality", func(t *testing.T) {
		ema := NewEMA(2)
		ema.Update(candles[0])
		ema.Update(candles[1])
		assert.True(t, ema.Ready())

		ema.Reset()
		assert.False(t, ema.Ready())
		assert.Equal(t, 0.0, ema.Value())
	})

	t.Run("matches batch calculation", func(t *testing.T) {
		ema := NewEMA(5)
		for _, c := range candles {
			ema.Update(c)
		}

		// Compare with batch function
		batchResult, _ := EMA(candles, 5)
		assert.InDelta(t, batchResult, ema.Value(), 0.001)
	})
}

func TestAverageTrueRangeStreaming(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := []pricing.Candle{
		{High: 10, Low: 8, Close: 9, Time: baseTime},
		{High: 11, Low: 9, Close: 10, Time: baseTime.Add(time.Hour)},
		{High: 12, Low: 10, Close: 11, Time: baseTime.Add(2 * time.Hour)},
		{High: 11, Low: 9, Close: 10, Time: baseTime.Add(3 * time.Hour)},
		{High: 12, Low: 10, Close: 11, Time: baseTime.Add(4 * time.Hour)},
		{High: 13, Low: 11, Close: 12, Time: baseTime.Add(5 * time.Hour)},
	}

	t.Run("basic functionality", func(t *testing.T) {
		atr := NewATR(3)
		assert.Equal(t, "ATR(3)", atr.Name())
		assert.Equal(t, 4, atr.Warmup()) // period + 1
		assert.False(t, atr.Ready())
		assert.Equal(t, 0.0, atr.Value())

		// First candle just stores for reference
		atr.Update(candles[0])
		assert.False(t, atr.Ready())

		// Second and third candles accumulate
		atr.Update(candles[1])
		assert.False(t, atr.Ready())
		atr.Update(candles[2])
		assert.False(t, atr.Ready())

		// Fourth candle completes warmup
		atr.Update(candles[3])
		assert.True(t, atr.Ready())
		// Average of 3 TRs (each should be 2.0 for this test data)
		assert.InDelta(t, 2.0, atr.Value(), 0.001)
	})

	t.Run("reset functionality", func(t *testing.T) {
		atr := NewATR(2)
		atr.Update(candles[0])
		atr.Update(candles[1])
		atr.Update(candles[2])
		assert.True(t, atr.Ready())

		atr.Reset()
		assert.False(t, atr.Ready())
		assert.Equal(t, 0.0, atr.Value())
	})

	t.Run("matches batch calculation", func(t *testing.T) {
		atr := NewATR(3)
		for _, c := range candles {
			atr.Update(c)
		}

		// Compare with batch function
		batchResult, _ := ATRFunc(candles, 3)
		assert.InDelta(t, batchResult, atr.Value(), 0.001)
	})
}

func TestIndicatorInterface(t *testing.T) {
	// Test that all indicators implement the Indicator interface
	var _ Indicator = &SimpleMA{}
	var _ Indicator = &ExponentialMA{}
	var _ Indicator = &ATR{}

	t.Run("all indicators have consistent interface", func(t *testing.T) {
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		candles := []pricing.Candle{
			{High: 105, Low: 99, Close: 102, Time: baseTime},
			{High: 107, Low: 101, Close: 105, Time: baseTime.Add(time.Hour)},
			{High: 108, Low: 104, Close: 106, Time: baseTime.Add(2 * time.Hour)},
			{High: 110, Low: 105, Close: 108, Time: baseTime.Add(3 * time.Hour)},
			{High: 112, Low: 107, Close: 110, Time: baseTime.Add(4 * time.Hour)},
		}

		indicators := []Indicator{
			NewMA(3),
			NewEMA(3),
			NewATR(2),
		}

		for _, ind := range indicators {
			// All should start not ready
			assert.False(t, ind.Ready(), "indicator %s should not be ready initially", ind.Name())

			// Feed candles
			for _, c := range candles {
				ind.Update(c)
			}

			// All should be ready after sufficient candles
			assert.True(t, ind.Ready(), "indicator %s should be ready after warmup", ind.Name())

			// Value should be non-zero
			assert.Greater(t, ind.Value(), 0.0, "indicator %s should have positive value", ind.Name())

			// Reset should make them not ready
			ind.Reset()
			assert.False(t, ind.Ready(), "indicator %s should not be ready after reset", ind.Name())
		}
	})
}

func TestStreamingVsBatchConsistency(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := []pricing.Candle{
		{High: 105, Low: 99, Close: 102, Time: baseTime},
		{High: 107, Low: 101, Close: 105, Time: baseTime.Add(time.Hour)},
		{High: 108, Low: 104, Close: 106, Time: baseTime.Add(2 * time.Hour)},
		{High: 110, Low: 105, Close: 108, Time: baseTime.Add(3 * time.Hour)},
		{High: 112, Low: 107, Close: 110, Time: baseTime.Add(4 * time.Hour)},
		{High: 113, Low: 109, Close: 111, Time: baseTime.Add(5 * time.Hour)},
		{High: 115, Low: 110, Close: 113, Time: baseTime.Add(6 * time.Hour)},
		{High: 116, Low: 112, Close: 114, Time: baseTime.Add(7 * time.Hour)},
		{High: 118, Low: 113, Close: 116, Time: baseTime.Add(8 * time.Hour)},
		{High: 120, Low: 115, Close: 118, Time: baseTime.Add(9 * time.Hour)},
	}

	t.Run("MA streaming matches batch", func(t *testing.T) {
		ma := NewMA(5)
		for _, c := range candles {
			ma.Update(c)
		}
		batchResult, _ := MA(candles, 5)
		assert.InDelta(t, batchResult, ma.Value(), 0.001)
	})

	t.Run("EMA streaming matches batch", func(t *testing.T) {
		ema := NewEMA(5)
		for _, c := range candles {
			ema.Update(c)
		}
		batchResult, _ := EMA(candles, 5)
		assert.InDelta(t, batchResult, ema.Value(), 0.001)
	})

	t.Run("ATR streaming matches batch", func(t *testing.T) {
		atr := NewATR(5)
		for _, c := range candles {
			atr.Update(c)
		}
		batchResult, _ := ATRFunc(candles, 5)
		assert.InDelta(t, batchResult, atr.Value(), 0.001)
	})
}
