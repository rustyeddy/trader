package execution

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openLot(id string, entryTime market.Timestamp, units market.Units) *Lot {
	th := NewTradeHistory("EURUSD")
	th.ID = id
	th.Side = market.Long
	th.Units = units
	return &Lot{
		TradeCommon:    th.TradeCommon,
		EntryPrice:     market.PriceFromFloat(1.1000),
		EntryTime:      entryTime,
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          LotOpen,
	}
}

func TestFIFOMatcher_ExactUnits(t *testing.T) {
	t.Parallel()

	lot := openLot("a", 1000, 100_000)
	matches, err := FIFOMatcher{}.Match([]*Lot{lot}, 100_000)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, lot, matches[0].Lot)
	assert.Equal(t, market.Units(100_000), matches[0].Units)
}

func TestFIFOMatcher_PartialUnits(t *testing.T) {
	t.Parallel()

	lot := openLot("a", 1000, 100_000)
	matches, err := FIFOMatcher{}.Match([]*Lot{lot}, 50_000)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, market.Units(50_000), matches[0].Units)
}

func TestFIFOMatcher_MultipleLotsOldestFirst(t *testing.T) {
	t.Parallel()

	// older lot has lower EntryTime
	old := openLot("old", 1000, 60_000)
	new := openLot("new", 2000, 60_000)

	matches, err := FIFOMatcher{}.Match([]*Lot{new, old}, 60_000)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, old, matches[0].Lot, "oldest lot must be consumed first")
	assert.Equal(t, market.Units(60_000), matches[0].Units)
}

func TestFIFOMatcher_SpansMultipleLots(t *testing.T) {
	t.Parallel()

	a := openLot("a", 1000, 40_000)
	b := openLot("b", 2000, 40_000)
	c := openLot("c", 3000, 40_000)

	matches, err := FIFOMatcher{}.Match([]*Lot{c, a, b}, 80_000)
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, a, matches[0].Lot)
	assert.Equal(t, market.Units(40_000), matches[0].Units)
	assert.Equal(t, b, matches[1].Lot)
	assert.Equal(t, market.Units(40_000), matches[1].Units)
}

func TestFIFOMatcher_PartialLastLot(t *testing.T) {
	t.Parallel()

	a := openLot("a", 1000, 60_000)
	b := openLot("b", 2000, 60_000)

	// Request 80k: all of a (60k) + 20k from b
	matches, err := FIFOMatcher{}.Match([]*Lot{a, b}, 80_000)
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, market.Units(60_000), matches[0].Units)
	assert.Equal(t, market.Units(20_000), matches[1].Units)
}

func TestFIFOMatcher_InsufficientUnitsError(t *testing.T) {
	t.Parallel()

	lot := openLot("a", 1000, 50_000)
	_, err := FIFOMatcher{}.Match([]*Lot{lot}, 100_000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requested")
}

func TestFIFOMatcher_EmptyLotsError(t *testing.T) {
	t.Parallel()

	_, err := FIFOMatcher{}.Match([]*Lot{}, 1)
	require.Error(t, err)
}

func TestFIFOMatcher_SkipsNilLots(t *testing.T) {
	t.Parallel()

	lot := openLot("a", 1000, 100_000)
	matches, err := FIFOMatcher{}.Match([]*Lot{nil, lot, nil}, 100_000)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, lot, matches[0].Lot)
}

func TestFIFOMatcher_SkipsClosedAndZeroUnitLots(t *testing.T) {
	t.Parallel()

	closed := openLot("closed", 500, 100_000)
	closed.State = LotClosed

	zeroUnits := openLot("zero", 600, 0)
	zeroUnits.RemainingUnits = 0

	open := openLot("open", 1000, 100_000)

	matches, err := FIFOMatcher{}.Match([]*Lot{closed, zeroUnits, open}, 100_000)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, open, matches[0].Lot)
}
