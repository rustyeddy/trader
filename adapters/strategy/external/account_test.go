package external_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestToWireAccountSnapshot(t *testing.T) {
	snap := testSnapshot(t)

	w, err := external.ToWireAccountSnapshot(snap)
	require.NoError(t, err)

	require.Equal(t, snap.AccountID().String(), w.GetAccountId())
	require.Equal(t, snap.Currency().String(), w.GetCurrency())
	require.Equal(t, snap.AsOf().UTC().UnixNano(), w.GetAsOfUnixNanos())
	require.Len(t, w.GetPositions(), 1)

	pos := snap.Positions()[0]
	wp := w.GetPositions()[0]
	require.Equal(t, pos.Listing.InstrumentID().String(), wp.GetInstrumentId())
	require.Equal(t, v1.PositionSide_POSITION_SIDE_LONG, wp.GetSide())
	require.Equal(t, pos.AvgPrice.String(), wp.GetAvgPrice())
}

// TestToWireAccountSnapshot_ZeroValueRejected is the review's blocking
// finding: account.Snapshot{} (or any value that skipped
// account.NewSnapshot's own validation) must not silently serialize
// as if it carried real empty/zero identity.
func TestToWireAccountSnapshot_ZeroValueRejected(t *testing.T) {
	_, err := external.ToWireAccountSnapshot(account.Snapshot{})
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestToWireAccountSnapshot_NoPositionsIsEmptySlice(t *testing.T) {
	flat := testFlatSnapshot(t)

	w, err := external.ToWireAccountSnapshot(flat)
	require.NoError(t, err)
	require.Empty(t, w.GetPositions())
}

// TestToWireAccountSnapshot_EveryPositionSide exercises Short and
// Flat as well as TestToWireAccountSnapshot's own Long case,
// including Flat's nil AvgPrice -> empty-string conversion.
func TestToWireAccountSnapshot_EveryPositionSide(t *testing.T) {
	acctID := mustAccountID(t)
	usd := num.MustParseCurrency("USD")
	zero := num.MustParseMoney("0", usd)
	avg := num.MustParsePrice("1.2500")

	short, err := order.NewPosition(order.Position{
		AccountID: acctID,
		Listing:   gbpUSDListing(t),
		Side:      order.Short,
		Quantity:  num.MustParseQuantity("200"),
		AvgPrice:  &avg,
	})
	require.NoError(t, err)

	flat, err := order.NewPosition(order.Position{
		AccountID: acctID,
		Listing:   usdJPYListing(t),
		Side:      order.Flat,
	})
	require.NoError(t, err)

	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       acctID,
		Broker:          "sim",
		Currency:        usd,
		AsOf:            time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
		Positions:       []order.Position{short, flat},
		Equity:          zero,
		BuyingPower:     zero,
		MarginUsed:      zero,
		MarginAvailable: zero,
		RealizedPnL:     zero,
		UnrealizedPnL:   zero,
		Fees:            zero,
		Financing:       zero,
	})
	require.NoError(t, err)

	w, err := external.ToWireAccountSnapshot(snap)
	require.NoError(t, err)
	require.Len(t, w.GetPositions(), 2)

	require.Equal(t, v1.PositionSide_POSITION_SIDE_SHORT, w.GetPositions()[0].GetSide())
	require.Equal(t, "1.25", w.GetPositions()[0].GetAvgPrice())

	require.Equal(t, v1.PositionSide_POSITION_SIDE_FLAT, w.GetPositions()[1].GetSide())
	require.Empty(t, w.GetPositions()[1].GetAvgPrice())
}
