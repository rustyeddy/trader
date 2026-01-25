package indicators

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
)

func createTestCandles() []market.Candle {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return []market.Candle{
		{Open: 100, High: 105, Low: 99, Close: 102, Time: baseTime, Volume: 1000},
		{Open: 102, High: 107, Low: 101, Close: 105, Time: baseTime.Add(time.Hour), Volume: 1100},
		{Open: 105, High: 108, Low: 104, Close: 106, Time: baseTime.Add(2 * time.Hour), Volume: 1200},
		{Open: 106, High: 110, Low: 105, Close: 108, Time: baseTime.Add(3 * time.Hour), Volume: 1300},
		{Open: 108, High: 112, Low: 107, Close: 110, Time: baseTime.Add(4 * time.Hour), Volume: 1400},
		{Open: 110, High: 113, Low: 109, Close: 111, Time: baseTime.Add(5 * time.Hour), Volume: 1500},
		{Open: 111, High: 115, Low: 110, Close: 113, Time: baseTime.Add(6 * time.Hour), Volume: 1600},
		{Open: 113, High: 116, Low: 112, Close: 114, Time: baseTime.Add(7 * time.Hour), Volume: 1700},
		{Open: 114, High: 118, Low: 113, Close: 116, Time: baseTime.Add(8 * time.Hour), Volume: 1800},
		{Open: 116, High: 120, Low: 115, Close: 118, Time: baseTime.Add(9 * time.Hour), Volume: 1900},
	}
}

func TestMA(t *testing.T) {
	candles := createTestCandles()

	t.Run("valid period", func(t *testing.T) {
		ma, err := MA(candles, 5)
		assert.NoError(t, err)
		// Last 5 closes: 111, 113, 114, 116, 118 = 572 / 5 = 114.4
		assert.InDelta(t, 114.4, ma, 0.001)
	})

	t.Run("period of 1", func(t *testing.T) {
		ma, err := MA(candles, 1)
		assert.NoError(t, err)
		// Last close: 118
		assert.Equal(t, 118.0, ma)
	})

	t.Run("period equals candle count", func(t *testing.T) {
		ma, err := MA(candles, len(candles))
		assert.NoError(t, err)
		// All closes: 102+105+106+108+110+111+113+114+116+118 = 1103 / 10 = 110.3
		assert.InDelta(t, 110.3, ma, 0.001)
	})

	t.Run("not enough candles", func(t *testing.T) {
		_, err := MA(candles, 11)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not enough candles")
	})

	t.Run("empty candles", func(t *testing.T) {
		_, err := MA([]market.Candle{}, 1)
		assert.Error(t, err)
	})
}

func TestEMA(t *testing.T) {
	candles := createTestCandles()

	t.Run("valid period", func(t *testing.T) {
		ema, err := EMA(candles, 5)
		assert.NoError(t, err)
		
		// EMA calculation with period 5, multiplier = 2/(5+1) = 0.333...
		// Start with SMA of first 5: (102+105+106+108+110)/5 = 106.2
		// Then apply EMA formula for remaining candles
		multiplier := 2.0 / 6.0
		expectedEMA := 106.2
		expectedEMA = (111-expectedEMA)*multiplier + expectedEMA // 107.8
		expectedEMA = (113-expectedEMA)*multiplier + expectedEMA // 109.533...
		expectedEMA = (114-expectedEMA)*multiplier + expectedEMA // 111.022...
		expectedEMA = (116-expectedEMA)*multiplier + expectedEMA // 112.681...
		expectedEMA = (118-expectedEMA)*multiplier + expectedEMA // 114.454...
		
		assert.InDelta(t, expectedEMA, ema, 0.001)
	})

	t.Run("not enough candles", func(t *testing.T) {
		_, err := EMA(candles, 11)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not enough candles")
	})
}

func TestATR(t *testing.T) {
	candles := createTestCandles()

	t.Run("valid period", func(t *testing.T) {
		atr, err := ATR(candles, 5)
		assert.NoError(t, err)
		
		// ATR should be positive and reasonable for the test data
		assert.Greater(t, atr, 0.0)
		assert.Less(t, atr, 20.0) // Sanity check
	})

	t.Run("detailed calculation", func(t *testing.T) {
		// Using first few candles for manual verification
		testCandles := []market.Candle{
			{High: 10, Low: 8, Close: 9},
			{High: 11, Low: 9, Close: 10},
			{High: 12, Low: 10, Close: 11},
			{High: 11, Low: 9, Close: 10},
			{High: 12, Low: 10, Close: 11},
			{High: 13, Low: 11, Close: 12},
		}
		
		atr, err := ATR(testCandles, 3)
		assert.NoError(t, err)
		
		// True ranges:
		// TR1 = max(11-9, |11-9|, |9-9|) = max(2, 2, 0) = 2
		// TR2 = max(12-10, |12-10|, |10-10|) = max(2, 2, 0) = 2
		// TR3 = max(11-9, |11-11|, |9-11|) = max(2, 0, 2) = 2
		// TR4 = max(12-10, |12-10|, |10-10|) = max(2, 2, 0) = 2
		// TR5 = max(13-11, |13-11|, |11-11|) = max(2, 2, 0) = 2
		// Initial ATR = (2+2+2)/3 = 2
		// ATR after TR4: (2*2 + 2)/3 = 2
		// ATR after TR5: (2*2 + 2)/3 = 2
		
		assert.InDelta(t, 2.0, atr, 0.001)
	})

	t.Run("not enough candles", func(t *testing.T) {
		_, err := ATR(candles, 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not enough candles")
	})

	t.Run("minimum candles for period 1", func(t *testing.T) {
		shortCandles := candles[:2]
		atr, err := ATR(shortCandles, 1)
		assert.NoError(t, err)
		assert.Greater(t, atr, 0.0)
	})
}

func TestTrueRange(t *testing.T) {
	t.Run("high-low is maximum", func(t *testing.T) {
		current := market.Candle{High: 110, Low: 100, Close: 105}
		previous := market.Candle{Close: 104}
		
		tr := trueRange(current, previous)
		assert.Equal(t, 10.0, tr)
	})

	t.Run("high-previous close is maximum", func(t *testing.T) {
		current := market.Candle{High: 110, Low: 108, Close: 109}
		previous := market.Candle{Close: 100}
		
		tr := trueRange(current, previous)
		assert.Equal(t, 10.0, tr)
	})

	t.Run("low-previous close is maximum", func(t *testing.T) {
		current := market.Candle{High: 102, Low: 100, Close: 101}
		previous := market.Candle{Close: 110}
		
		tr := trueRange(current, previous)
		assert.Equal(t, 10.0, tr)
	})
}

func TestIndicatorsWithRealWorldData(t *testing.T) {
	// Simulating more realistic price movement
	candles := []market.Candle{
		{Close: 50.00}, {Close: 50.50}, {Close: 51.00}, {Close: 51.25},
		{Close: 51.50}, {Close: 51.75}, {Close: 52.00}, {Close: 52.25},
		{Close: 52.50}, {Close: 52.75}, {Close: 53.00}, {Close: 53.25},
	}

	t.Run("MA trends upward with rising prices", func(t *testing.T) {
		ma5, _ := MA(candles[:8], 5)
		ma10, _ := MA(candles, 5)
		
		// MA should increase as prices rise
		assert.Greater(t, ma10, ma5)
	})

	t.Run("EMA reacts faster than MA", func(t *testing.T) {
		// For a quick price change, EMA should be more responsive
		risingCandles := []market.Candle{
			{Close: 50}, {Close: 50}, {Close: 50}, {Close: 50},
			{Close: 50}, {Close: 60}, {Close: 60}, {Close: 60},
		}
		
		ma, _ := MA(risingCandles, 5)
		ema, _ := EMA(risingCandles, 5)
		
		// EMA should be closer to recent prices (60) than MA
		assert.Greater(t, ema, ma)
	})
}
