package trader

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsFXMarketClosed_BackCompatWrapper verifies expected behavior for this component.
func TestIsFXMarketClosed_BackCompatWrapper(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	// Wrapper should match canonical function behavior.
	tBerlin := time.Date(2024, 6, 8, 1, 0, 0, 0, berlin)
	assert.Equal(t, isForexMarketClosed(tBerlin), isFXMarketClosed(tBerlin))
}

// TestIsForexMarketClosed_NewYorkBoundaries verifies expected behavior for this component.
func TestIsForexMarketClosed_NewYorkBoundaries(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	assert.True(t, isForexMarketClosed(time.Date(2024, 6, 7, 17, 0, 0, 0, ny)))
	assert.False(t, isForexMarketClosed(time.Date(2024, 6, 9, 17, 0, 0, 0, ny)))
	assert.True(t, isForexMarketClosed(time.Date(2024, 12, 24, 13, 0, 0, 0, ny)))
}

// TestCandleSetAggregate_UsesCanonicalBitHelpers verifies expected behavior for this component.
func TestCandleSetAggregate_UsesCanonicalBitHelpers(t *testing.T) {
	t.Parallel()

	cs := &marketdata.CandleSet{
		Instrument: "EURUSD",
		Start:      Timestamp(1704067200),
		Timeframe:  M1,
		Scale:      PriceScale,
		Candles: []Candle{
			{Open: 100, High: 110, Low: 95, Close: 105, AvgSpread: 2, MaxSpread: 3, Ticks: 4},
			{},
			{Open: 106, High: 120, Low: 101, Close: 115, AvgSpread: 4, MaxSpread: 5, Ticks: 6},
			{},
		},
		Valid: make([]uint64, 1),
	}
	bitSet(cs.Valid, 0)
	bitSet(cs.Valid, 2)

	out, err := cs.Aggregate(Timeframe(300))
	require.NoError(t, err)
	require.Len(t, out.Candles, 1)
	assert.True(t, out.IsValid(0))
	assert.Equal(t, Candle{
		Open:      100,
		High:      120,
		Low:       95,
		Close:     115,
		AvgSpread: 3,
		MaxSpread: 5,
		Ticks:     10,
	}, out.Candles[0])
}
