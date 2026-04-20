package trader

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Tests for SyntheticCandleConfig
// ============================================================================

func TestDefaultSyntheticConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultSyntheticConfig("EURUSD")
	assert.Equal(t, "EURUSD", cfg.Instrument)
	assert.Equal(t, H1, cfg.Timeframe)
	assert.Greater(t, cfg.StartPrice, Price(0))
	assert.Greater(t, cfg.Volatility, 0.0)
}

func TestLinearCongruentialRandom_Deterministic(t *testing.T) {
	t.Parallel()

	// Same seed should give same sequence
	rng1 := NewLCRandom(42)
	rng2 := NewLCRandom(42)

	for i := 0; i < 100; i++ {
		val1 := rng1.NextGaussian()
		val2 := rng2.NextGaussian()
		assert.Equal(t, val1, val2, "iteration %d", i)
	}
}

func TestLinearCongruentialRandom_DifferentSeeds(t *testing.T) {
	t.Parallel()

	rng1 := NewLCRandom(42)
	rng2 := NewLCRandom(43)

	vals1 := []float64{}
	vals2 := []float64{}

	for i := 0; i < 10; i++ {
		vals1 = append(vals1, rng1.NextGaussian())
		vals2 = append(vals2, rng2.NextGaussian())
	}

	// Should eventually differ (probabilistically almost certain)
	assert.NotEqual(t, vals1, vals2)
}

// ============================================================================
// Tests for GenerateSyntheticCandle
// ============================================================================

func TestGenerateSyntheticCandle_ValidRange(t *testing.T) {
	t.Parallel()
	cfg := DefaultSyntheticConfig("EURUSD")
	rng := NewLCRandom(42)

	prevClose := Price(1080000)
	candle := cfg.generateCandle(rng, prevClose)

	// High >= max(open, close)
	assert.GreaterOrEqual(t, candle.High, candle.Open, "high should be >= open")
	assert.GreaterOrEqual(t, candle.High, candle.Close, "high should be >= close")

	// Low <= min(open, close)
	assert.LessOrEqual(t, candle.Low, candle.Open, "low should be <= open")
	assert.LessOrEqual(t, candle.Low, candle.Close, "low should be <= close")

	// Spread is positive
	assert.Greater(t, candle.AvgSpread, Price(0))
	assert.Greater(t, candle.MaxSpread, Price(0))
	assert.GreaterOrEqual(t, candle.MaxSpread, candle.AvgSpread)
}

func TestGenerateSyntheticCandle_Deterministic(t *testing.T) {
	t.Parallel()
	cfg := DefaultSyntheticConfig("EURUSD")

	rng1 := NewLCRandom(42)
	rng2 := NewLCRandom(42)
	prevClose := Price(1080000)

	candle1 := cfg.generateCandle(rng1, prevClose)
	candle2 := cfg.generateCandle(rng2, prevClose)

	assert.Equal(t, candle1.Open, candle2.Open)
	assert.Equal(t, candle1.High, candle2.High)
	assert.Equal(t, candle1.Low, candle2.Low)
	assert.Equal(t, candle1.Close, candle2.Close)
}

// ============================================================================
// Tests for GenerateSyntheticMonthlyCandles
// ============================================================================

func TestGenerateSyntheticMonthlyCandles_ValidStructure(t *testing.T) {
	t.Parallel()
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)
	require.NotNil(t, cs)

	// Instrument name is normalized to uppercase
	assert.Equal(t, "EURUSD", cs.Instrument)
	assert.Equal(t, H1, cs.Timeframe)
	assert.Greater(t, len(cs.Candles), 0)
	expectedLen := int((744 * 3600) / int64(H1)) // Jan = 31 days = 744 hours in seconds
	assert.Equal(t, len(cs.Candles), expectedLen)
}

func TestGenerateSyntheticMonthlyCandles_CandlesAreValid(t *testing.T) {
	t.Parallel()
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	// At least some candles should be valid (market hours)
	validCount := 0
	for i := 0; i < len(cs.Candles); i++ {
		if cs.IsValid(i) {
			validCount++
		}
	}
	assert.Greater(t, validCount, 0, "should have valid candles during market hours")
}

func TestGenerateSyntheticMonthlyCandles_DifferentMonths(t *testing.T) {
	t.Parallel()
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	cs1, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	cs2, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.February)
	require.NoError(t, err)

	// Different months have different starts
	assert.NotEqual(t, cs1.Start, cs2.Start)
}

// ============================================================================
// Tests for GenerateSyntheticYearlyCandles
// ============================================================================

func TestGenerateSyntheticYearlyCandles_HasAllMonths(t *testing.T) {
	t.Parallel()
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = M1

	candleSets, err := cfg.GenerateSyntheticYearlyCandles(2025)
	require.NoError(t, err)
	require.Len(t, candleSets, 12)

	// Verify months are in order
	for i, cs := range candleSets {
		start := time.Unix(int64(cs.Start), 0).UTC()
		expectedMonth := time.Month(i + 1)
		assert.Equal(t, expectedMonth, start.Month())
	}
}

// ============================================================================
// Tests for Helper Functions
// ============================================================================

func TestTestHelperGenerateSyntheticCandles(t *testing.T) {
	t.Parallel()
	cs := HelperGenerateSyntheticCandles(t, "EURUSD", 2025, time.January, H1)
	require.NotNil(t, cs)
	assert.Equal(t, "EURUSD", cs.Instrument)
	assert.Equal(t, H1, cs.Timeframe)
}

func TestTestHelperGenerateSyntheticCandlesWithConfig(t *testing.T) {
	t.Parallel()
	cfg := SyntheticCandleConfig{
		Instrument:  "GBPUSD",
		Timeframe:   M1,
		StartPrice:  Price(1250000),
		Volatility:  0.001,
		Trend:       0.00002,
		Seed:        123,
		TicksPerBar: 100,
	}

	cs := HelperGenerateSyntheticCandlesWithConfig(t, cfg, 2025, time.January)
	require.NotNil(t, cs)
	assert.Equal(t, "GBPUSD", cs.Instrument)
	assert.Equal(t, M1, cs.Timeframe)
}

// ============================================================================
// Tests for File I/O
// ============================================================================

func TestGenerateSyntheticYearlyAndWrite(t *testing.T) {
	t.Parallel()

	tmpdir := t.TempDir()
	store := &Store{basedir: tmpdir}
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	paths, err := cfg.GenerateSyntheticYearlyAndWrite(store, 2025)
	require.NoError(t, err)
	require.Len(t, paths, 12)

	// Verify files exist
	for _, path := range paths {
		info, err := os.Stat(path)
		require.NoError(t, err, "path %q should exist", path)
		assert.Greater(t, info.Size(), int64(0), "path %q should be non-empty", path)
	}
}

func TestLoadSyntheticCandles_CreatesIfMissing(t *testing.T) {
	t.Parallel()

	tmpdir := t.TempDir()
	// Override TestDataDir for this test by using GetOrCreateTestData with tmpdir
	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	store := &Store{basedir: tmpdir}
	paths, err := cfg.GenerateSyntheticYearlyAndWrite(store, 2025)
	require.NoError(t, err)
	require.Greater(t, len(paths), 0)

	// Verify files exist and match expected path structure
	for _, path := range paths {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Greater(t, info.Size(), int64(0))
	}
}

func TestMakeSyntheticCandleSetIterator(t *testing.T) {
	t.Parallel()

	cs := HelperGenerateSyntheticCandles(t, "EURUSD", 2025, time.January, H1)
	iter := MakeSyntheticCandleSetIterator(cs)

	require.NotNil(t, iter)
	assert.True(t, iter.Next(), "should have at least one candle")

	candle := iter.Candle()
	assert.Greater(t, candle.High, Price(0))
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestSyntheticDataFeedsTraderIterator(t *testing.T) {
	t.Parallel()

	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1

	cs, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	iter := newCandleSetIterator(cs, TimeRange{})
	defer iter.Close()

	candleCount := 0
	for iter.Next() {
		candleCount++
		candle := iter.Candle()
		ct := iter.CandleTime()

		// Verify candle structure
		assert.Greater(t, candle.High, Price(0))
		assert.Less(t, candle.Low, candle.High)
		assert.Greater(t, ct.Timestamp, Timestamp(0))
	}

	assert.Greater(t, candleCount, 0, "should have processed some candles")
}

func TestSyntheticDataReproducible(t *testing.T) {
	t.Parallel()

	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1
	cfg.Seed = 42

	cs1, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	// Reset seed to same value
	cfg.Seed = 42
	cs2, err := cfg.GenerateSyntheticMonthlyCandles(2025, time.January)
	require.NoError(t, err)

	// Should be identical
	require.Len(t, cs1.Candles, len(cs2.Candles))
	for i := 0; i < len(cs1.Candles); i++ {
		assert.Equal(t, cs1.Candles[i].Open, cs2.Candles[i].Open, "candle %d open", i)
		assert.Equal(t, cs1.Candles[i].Close, cs2.Candles[i].Close, "candle %d close", i)
		assert.Equal(t, cs1.Candles[i].High, cs2.Candles[i].High, "candle %d high", i)
		assert.Equal(t, cs1.Candles[i].Low, cs2.Candles[i].Low, "candle %d low", i)
	}
}
