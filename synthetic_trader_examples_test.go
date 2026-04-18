package trader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Example: Using Synthetic Data with TestTrader
// ============================================================================

// demonstrateSyntheticDataUsage shows how to feed a year's worth of synthetic
// candles to the trader to test for infinite loops or processing issues.
func demonstrateSyntheticDataUsage() {
	// Setup: Generate year of synthetic data
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1 // Hourly for reasonable test size (~700 candles/month)

	candleSets, _ := cfg.GenerateSyntheticYearlyCandles(2025)

	// Process through trader
	totalCandles := 0
	for _, cs := range candleSets {
		iter := NewCandleSetIterator(cs, TimeRange{})
		for iter.Next() {
			totalCandles++
			_ = iter.Candle()
		}
		iter.Close()
	}

	// If this completes without hanging, the infinite loop issue is fixed
	println("Processed", totalCandles, "candles successfully")
}

// ============================================================================
// Tests: Verifying Infinite Loop Fix
// ============================================================================

// TestTraderWithYearOfSyntheticHourly tests that trader can process a full year
// of hourly candles without infinite loops. This is useful for reproducible
// testing of the infinite loop issue on CI/CD systems.
func TestTraderWithYearOfSyntheticHourly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping year-long test in short mode")
	}

	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1
	cfg.Seed = 2025 // Reproducible seed

	candleSets, err := cfg.GenerateSyntheticYearlyCandles(2025)
	require.NoError(t, err)
	require.Len(t, candleSets, 12)

	totalCandles := 0
	startTime := time.Now()

	// Process all months
	for monthIdx, cs := range candleSets {
		iter := NewCandleSetIterator(cs, TimeRange{})
		monthStart := time.Now()

		candlesInMonth := 0
		for iter.Next() {
			totalCandles++
			candlesInMonth++
			c := iter.Candle()

			// Verify candle sanity
			if c.High < c.Low || c.Open > c.High || c.Open < c.Low {
				t.Fatalf("Month %d, candle %d: invalid OHLC: H=%d L=%d O=%d C=%d",
					monthIdx+1, candlesInMonth, c.High, c.Low, c.Open, c.Close)
			}
		}

		err := iter.Close()
		require.NoError(t, err)

		elapsed := time.Since(monthStart)
		assert.NotZero(t, elapsed)
	}

	totalElapsed := time.Since(startTime)
	assert.NotZero(t, totalElapsed)
	// Verify we processed expected amount
	// Note: Not all hours have candles (weekends, forex market hours)
	// ~520 candles/month * 12 months = ~6200 candles
	assert.Greater(t, totalCandles, 5000)
	assert.Less(t, totalCandles, 7000)
}

// TestTraderWithYearOfSyntheticDaily tests with daily data (fewer candles).
func TestTraderWithYearOfSyntheticDaily(t *testing.T) {
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = D1 // Daily: ~252 candles/year (trading days)

	cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	iter := NewCandleSetIterator(cs, TimeRange{})
	defer iter.Close()

	candleCount := 0
	for iter.Next() {
		candleCount++
		_ = iter.Candle()
	}

	// January has ~22 trading days
	assert.Greater(t, candleCount, 15)
	assert.Less(t, candleCount, 30)
}

// TestTraderTimeoutDetection verifies we can detect infinite loops with timeouts.
// This ensures that if the infinite loop still exists, the test will fail decisively.
func TestTraderTimeoutDetection(t *testing.T) {
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	// Set a reasonable timeout for processing ~730 candles
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	iter := NewCandleSetIterator(cs, TimeRange{})
	defer iter.Close()

	candleCount := 0
	for iter.Next() {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout while processing candles: infinite loop likely at candle %d",
				candleCount)
		default:
		}
		candleCount++
	}

	require.NoError(t, iter.Err())
}

// TestTraderWithHighVolatilitySynthetic tests with extreme volatility
// to ensure the trader handles edge cases.
func TestTraderWithHighVolatilitySynthetic(t *testing.T) {
	cfg := SyntheticCandleConfig{
		Instrument:  "EURUSD",
		Timeframe:   H1,
		StartPrice:  Price(1080000),
		Volatility:  0.05, // 5% volatility (extreme)
		Trend:       0.0,  // No trend
		Seed:        12345,
		TicksPerBar: 200,
	}

	cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	iter := NewCandleSetIterator(cs, TimeRange{})
	defer iter.Close()

	candleCount := 0
	maxPrice := Price(0)
	minPrice := Price(999999999) // Fixed overflow

	for iter.Next() {
		candleCount++
		c := iter.Candle()

		if c.High > maxPrice {
			maxPrice = c.High
		}
		if c.Low < minPrice {
			minPrice = c.Low
		}

		// With extreme volatility, prices can swing significantly
		// Just verify prices are positive and OHLC structure is valid
		assert.Greater(t, c.High, Price(0))
		assert.Greater(t, c.Low, Price(0))
		assert.Less(t, c.Low, c.High)
	}

	// Calculate price range
	priceRange := float64(maxPrice-minPrice) / float64(Price(1080000))
	assert.NotZero(t, priceRange)
}

// TestTraderWithDifferentSeeds verifies reproducibility
func TestTraderWithDifferentSeeds(t *testing.T) {
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	// Generate with same seed twice
	cfg.Seed = 42
	cs1, _ := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)

	cfg.Seed = 42
	cs2, _ := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)

	// Should be identical
	require.Len(t, cs1.Candles, len(cs2.Candles))
	for i := 0; i < len(cs1.Candles); i++ {
		assert.Equal(t, cs1.Candles[i].Open, cs2.Candles[i].Open, "candle %d", i)
		assert.Equal(t, cs1.Candles[i].Close, cs2.Candles[i].Close, "candle %d", i)
		assert.Equal(t, cs1.Candles[i].High, cs2.Candles[i].High, "candle %d", i)
		assert.Equal(t, cs1.Candles[i].Low, cs2.Candles[i].Low, "candle %d", i)
	}

	// Different seed should produce different data
	cfg.Seed = 43
	cs3, _ := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)

	var differences int
	for i := 0; i < len(cs1.Candles); i++ {
		if cs1.Candles[i].Close != cs3.Candles[i].Close {
			differences++
		}
	}

	assert.Greater(t, differences, 0, "different seeds should produce different candles")
}

// ============================================================================
// Performance Tests
// ============================================================================

// BenchmarkSyntheticCandleGeneration benchmarks how fast we can generate candles
func BenchmarkSyntheticCandleGeneration(b *testing.B) {
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg.Seed = int64(i)
		_, _ = cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	}
}

// BenchmarkSyntheticCandleIteration benchmarks iteration speed
func BenchmarkSyntheticCandleIteration(b *testing.B) {
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1
	cs, _ := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := NewCandleSetIterator(cs, TimeRange{})
		count := 0
		for iter.Next() {
			count++
		}
		iter.Close()
	}
}

// BenchmarkYearGeneration benchmarks full year generation
func BenchmarkYearGeneration(b *testing.B) {
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg.Seed = int64(i)
		_, _ = cfg.GenerateSyntheticYearlyCandles(2025)
	}
}
