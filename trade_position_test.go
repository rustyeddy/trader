package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstrumentPositionsNilLotBook(t *testing.T) {
	t.Parallel()

	assert.Nil(t, InstrumentPositions(nil))
}

func TestInstrumentPositionsAggregatesBySide(t *testing.T) {
	t.Parallel()

	var lb LotBook
	require.NoError(t, lb.Add(&Lot{
		TradeCommon:    &TradeCommon{ID: "l1", Instrument: "EURUSD", Side: Long},
		EntryPrice:     PriceFromFloat(1.1000),
		EntryTime:      1,
		OriginalUnits:  100,
		RemainingUnits: 100,
		State:          LotOpen,
	}))
	require.NoError(t, lb.Add(&Lot{
		TradeCommon:    &TradeCommon{ID: "l2", Instrument: "EURUSD", Side: Long},
		EntryPrice:     PriceFromFloat(1.2000),
		EntryTime:      2,
		OriginalUnits:  50,
		RemainingUnits: 50,
		State:          LotOpen,
	}))
	require.NoError(t, lb.Add(&Lot{
		TradeCommon:    &TradeCommon{ID: "s1", Instrument: "EURUSD", Side: Short},
		EntryPrice:     PriceFromFloat(1.3000),
		EntryTime:      3,
		OriginalUnits:  30,
		RemainingUnits: 30,
		State:          LotOpen,
	}))
	require.NoError(t, lb.Add(&Lot{
		TradeCommon:    &TradeCommon{ID: "closed", Instrument: "EURUSD", Side: Long},
		EntryPrice:     PriceFromFloat(1.4000),
		EntryTime:      4,
		OriginalUnits:  25,
		RemainingUnits: 25,
		State:          LotClosed,
	}))
	require.NoError(t, lb.Add(&Lot{
		TradeCommon:    &TradeCommon{ID: "zero", Instrument: "EURUSD", Side: Long},
		EntryPrice:     PriceFromFloat(1.5000),
		EntryTime:      5,
		OriginalUnits:  25,
		RemainingUnits: 0,
		State:          LotOpen,
	}))
	require.NoError(t, lb.Add(&Lot{
		TradeCommon:    &TradeCommon{ID: "gbp", Instrument: "GBPUSD", Side: Short},
		EntryPrice:     PriceFromFloat(1.2500),
		EntryTime:      6,
		OriginalUnits:  40,
		RemainingUnits: 40,
		State:          LotOpen,
	}))

	got := InstrumentPositions(&lb)
	require.Len(t, got, 2)

	assert.Equal(t, Position{
		Instrument:         "EURUSD",
		LongUnits:          150,
		LongAvgEntryPrice:  Price(113333),
		ShortUnits:         30,
		ShortAvgEntryPrice: Price(130000),
		NetUnits:           120,
	}, got["EURUSD"])

	assert.Equal(t, Position{
		Instrument:         "GBPUSD",
		ShortUnits:         40,
		ShortAvgEntryPrice: PriceFromFloat(1.2500),
		NetUnits:           -40,
	}, got["GBPUSD"])
}
