package pipvalues

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipValues_DefaultsMajorPairs(t *testing.T) {
	result, err := (&Service{}).PipValues(context.Background(), PipValuesRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(100_000), result.Units)
	assert.Len(t, result.Rows, 7)
}

func TestPipValues_ScopesToRequestedInstruments(t *testing.T) {
	result, err := (&Service{}).PipValues(context.Background(), PipValuesRequest{
		Instruments: []string{"EURUSD", "USDJPY"},
	})
	require.NoError(t, err)
	assert.Len(t, result.Rows, 2)
}

func TestPipValues_NormalizesRateOverrideKeys(t *testing.T) {
	result, err := (&Service{}).PipValues(context.Background(), PipValuesRequest{
		// Pass underscore form; should still override the normalized "USDJPY" entry.
		Rates:       map[string]float64{"USD_JPY": 160.0},
		Instruments: []string{"USDJPY"},
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.InDelta(t, 160.0, result.Rows[0].RateUsed, 0.01)
}

func TestPipValues_SkipsNonPositiveRateOverrides(t *testing.T) {
	result, err := (&Service{}).PipValues(context.Background(), PipValuesRequest{
		Rates:       map[string]float64{"USDJPY": -1.0},
		Instruments: []string{"USDJPY"},
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	// Rate should fall back to the built-in default (~150), not the negative override.
	assert.Greater(t, result.Rows[0].RateUsed, 0.0)
}

func TestPipValues_NormalizesInstrumentKeyForRateLookup(t *testing.T) {
	// Pass underscore-form instrument; pip value should still be computed correctly.
	result, err := (&Service{}).PipValues(context.Background(), PipValuesRequest{
		Instruments: []string{"USD_JPY"},
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	// With rate ~150, 1 pip on 100k USDJPY ≈ $6.67; ensure it's non-zero.
	assert.Greater(t, result.Rows[0].Pips1, 0.0)
}

func TestPipValues_CustomUnits(t *testing.T) {
	result, err := (&Service{}).PipValues(context.Background(), PipValuesRequest{
		Units:       10_000, // mini lot
		Instruments: []string{"EURUSD"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(10_000), result.Units)
	require.Len(t, result.Rows, 1)
	// 1 pip on a mini lot of EURUSD ≈ $1.00; confirm it's non-zero.
	assert.Greater(t, result.Rows[0].Pips1, 0.0)
}

func TestPipValues_SkipsUnknownInstrument(t *testing.T) {
	result, err := (&Service{}).PipValues(context.Background(), PipValuesRequest{
		Instruments: []string{"EURUSD", "ZZZZZZ"},
	})
	require.NoError(t, err)
	// Only EURUSD should appear; unknown instrument is silently skipped.
	assert.Len(t, result.Rows, 1)
	assert.Equal(t, "EURUSD", result.Rows[0].Instrument)
}
