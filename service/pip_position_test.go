package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── PipValues ────────────────────────────────────────────────────────────────

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

// ── PositionCalc ─────────────────────────────────────────────────────────────

func TestPositionCalc_RejectsNegativePrice(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      -1.0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "price must be >= 0")
}

func TestPositionCalc_RejectsNaNPrice(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      math.NaN(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid number")
}

func TestPositionCalc_RejectsNegativeUnits(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
		Units:      -1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "units must be >= 0")
}

func TestPositionCalc_RejectsNegativeNotional(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
		Notional:   -100,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notional must be >= 0")
}

func TestPositionCalc_RejectsNegativePips(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
		Pips:       -5,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pips must be >= 0")
}

func TestPositionCalc_RejectsBothUnitsAndNotional(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
		Units:      10_000,
		Notional:   5_000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "specify units or notional, not both")
}

func TestPositionCalc_ReturnsTableWhenNoSizeGiven(t *testing.T) {
	result, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
	})
	require.NoError(t, err)
	assert.Equal(t, "EURUSD", result.Instrument)
	assert.InDelta(t, 1.08, result.Price, 0.001)
	assert.Len(t, result.Rows, 3) // micro / mini / standard
}

func TestPositionCalc_ReturnsSingleRowForSpecificUnits(t *testing.T) {
	result, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
		Units:      50_000,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, int64(50_000), result.Rows[0].Units)
}

func TestPositionCalc_RequiresPriceWhenOANDAAbsent(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      0, // triggers OANDA fetch, but svc.OANDA is nil
	})
	require.Error(t, err)
}
