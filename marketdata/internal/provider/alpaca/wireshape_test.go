package alpaca

import (
	"math"
	"testing"
	"time"

	sdkmarketdata "github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordsFromSDKBars_ReanchorsToMidnightUTCOfTradingDate(t *testing.T) {
	bars := []sdkmarketdata.Bar{
		// 2024-01-02T05:00:00Z is 2024-01-02 00:00:00 in
		// America/New_York (EST, UTC-5) — same calendar date.
		{Timestamp: time.Date(2024, 1, 2, 5, 0, 0, 0, time.UTC), Open: 100, High: 101, Low: 99, Close: 100.50, Volume: 1000},
		// 2024-07-01T04:00:00Z is 2024-07-01 00:00:00 in
		// America/New_York (EDT, UTC-4) — exercises DST.
		{Timestamp: time.Date(2024, 7, 1, 4, 0, 0, 0, time.UTC), Open: 200, High: 201, Low: 199, Close: 200.50, Volume: 2000},
		// 2024-01-02T23:30:00Z is still 2024-01-02 18:30 EST — same
		// trading date despite being late in the UTC day.
		{Timestamp: time.Date(2024, 1, 2, 23, 30, 0, 0, time.UTC), Open: 300, High: 301, Low: 299, Close: 300.50, Volume: 3000},
	}

	records, err := recordsFromSDKBars(bars)
	require.NoError(t, err)
	require.Len(t, records, 3)

	assert.True(t, records[0].Time.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))
	assert.True(t, records[1].Time.Equal(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)))
	assert.True(t, records[2].Time.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))

	assert.Equal(t, "100", records[0].Open.String())
	assert.Equal(t, "101", records[0].High.String())
	assert.Equal(t, "99", records[0].Low.String())
	assert.Equal(t, "100.5", records[0].Close.String())
	assert.Equal(t, int64(1000), records[0].Volume)
}

func TestRecordsFromSDKBars_EmptyBarsIsEmptySlice(t *testing.T) {
	records, err := recordsFromSDKBars(nil)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestRecordsFromSDKBars_RejectsNonFinitePrice(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		bars := []sdkmarketdata.Bar{{Timestamp: time.Date(2024, 1, 2, 5, 0, 0, 0, time.UTC), Open: bad, High: 1, Low: 1, Close: 1}}
		_, err := recordsFromSDKBars(bars)
		assert.ErrorIs(t, err, ErrBadRequest, "value %v", bad)
	}
}

func TestRecordsFromSDKBars_RejectsVolumeOverflow(t *testing.T) {
	bars := []sdkmarketdata.Bar{{
		Timestamp: time.Date(2024, 1, 2, 5, 0, 0, 0, time.UTC),
		Open:      1, High: 1, Low: 1, Close: 1,
		Volume: uint64(math.MaxInt64) + 1,
	}}
	_, err := recordsFromSDKBars(bars)
	assert.ErrorIs(t, err, ErrBadRequest)
}

// TestQuantizedPriceFromFloat_RoundsToCentTickSize proves the
// deliberate quantization wireshape.go's own doc comment describes:
// adopting the official SDK (issue #323) means prices arrive as
// float64 with no original decimal text to recover, so this package
// rounds to the cent tick size (ADR-047's Phase 1 equity default)
// before constructing num.Price.
func TestQuantizedPriceFromFloat_RoundsToCentTickSize(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{100, "100"},
		{100.5, "100.5"},
		{100.567, "100.57"},
		{100.564, "100.56"},
		{0.005, "0.01"}, // rounds half up at the cent boundary
	}
	for _, tt := range tests {
		got, err := quantizedPriceFromFloat("c", tt.in)
		require.NoError(t, err, "input %v", tt.in)
		assert.Equal(t, tt.want, got.String(), "input %v", tt.in)
	}
}

func TestQuantizedPriceFromFloat_RejectsNonFinite(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := quantizedPriceFromFloat("c", bad)
		assert.ErrorIs(t, err, ErrBadRequest, "value %v", bad)
	}
}
