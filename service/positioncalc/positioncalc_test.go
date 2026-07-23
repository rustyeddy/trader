package positioncalc

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestPositionCalc_UnknownInstrumentReturnsError(t *testing.T) {
	_, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "ZZZZZZ",
		Price:      1.0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown instrument")
}

func TestPositionCalc_NotionalBasedSizing(t *testing.T) {
	// EURUSD: base=EUR so notional = units * price; 10000 USD at 1.08 ≈ 9259 units.
	result, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
		Notional:   10_000,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "custom", result.Rows[0].Label)
	assert.Greater(t, result.Rows[0].Units, int64(0))
}

func TestPositionCalc_NotionalBasedSizingUSDBase(t *testing.T) {
	// USDJPY: base=USD so notional = units (price-independent).
	result, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "USDJPY",
		Price:      150.0,
		Notional:   5_000,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, int64(5_000), result.Rows[0].Units)
}

func TestPositionCalc_PipPLInResult(t *testing.T) {
	result, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      1.08,
		Units:      100_000,
		Pips:       10,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Greater(t, result.Rows[0].PipsPL, 0.0)
	assert.Equal(t, 10.0, result.Pips)
}

func TestPositionCalc_NotionalUSDBaseZeroMidPrice(t *testing.T) {
	// posUnitsForNotional with midPrice=0 for a non-USD-base should return 0 units.
	// EURUSD at price 0 should give 0 units.
	result, err := (&Service{}).PositionCalc(context.Background(), PositionCalcRequest{
		Instrument: "EURUSD",
		Price:      0.0001, // near-zero but positive to pass validation
		Notional:   1_000,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Greater(t, result.Rows[0].Units, int64(0))
}
