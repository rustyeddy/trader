package external_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func mustFillID(t *testing.T) id.FillID {
	t.Helper()
	v, err := id.GenerateFillID(testIDs)
	require.NoError(t, err)
	return v
}

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	v, err := id.GenerateOrderID(testIDs)
	require.NoError(t, err)
	return v
}

func mustEventID(t *testing.T) id.EventID {
	t.Helper()
	v, err := id.GenerateEventID(testIDs)
	require.NoError(t, err)
	return v
}

func mustCorrelationID(t *testing.T) id.CorrelationID {
	t.Helper()
	v, err := id.GenerateCorrelationID(testIDs)
	require.NoError(t, err)
	return v
}

func testFill(t *testing.T) runtimeorder.Fill {
	t.Helper()
	corr := mustCorrelationID(t)
	caus := mustEventID(t)
	f, err := runtimeorder.NewFill(runtimeorder.Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   eurUSDListing(t),
		Side:      order.Buy,
		Price:     num.MustParsePrice("1.1005"),
		Quantity:  num.MustParseQuantity("1000"),
		Timestamp: time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
		Metadata:  id.Metadata{EventID: mustEventID(t), CorrelationID: corr, CausationID: caus},
	})
	require.NoError(t, err)
	return f
}

func TestToWireFillEvent(t *testing.T) {
	f := testFill(t)
	snap := testSnapshot(t)

	w, err := external.ToWireFillEvent(7, strategy.FillEvent{Fill: f}, snap)
	require.NoError(t, err)

	require.Equal(t, uint64(7), w.GetSequence())
	require.Equal(t, f.OrderID.String(), w.GetOrderId())
	require.Equal(t, f.Listing.InstrumentID().String(), w.GetInstrumentId())
	require.Equal(t, v1.Side_SIDE_BUY, w.GetSide())
	require.Equal(t, "1.1005", w.GetPrice())
	require.Equal(t, "1000", w.GetQuantity())
	require.Equal(t, f.Metadata.CorrelationID.String(), w.GetCorrelationId())
	require.Equal(t, f.Metadata.CausationID.String(), w.GetCausationId())
	require.NotNil(t, w.GetAccount())
}

func TestToWireFillEvent_SellSide(t *testing.T) {
	f := testFill(t)
	f.Side = order.Sell
	snap := testSnapshot(t)

	w, err := external.ToWireFillEvent(1, strategy.FillEvent{Fill: f}, snap)
	require.NoError(t, err)
	require.Equal(t, v1.Side_SIDE_SELL, w.GetSide())
}

func TestToWireFillEvent_InvalidSideRejected(t *testing.T) {
	f := testFill(t)
	f.Side = order.Side(99) // bypasses order.NewFill's own validation
	snap := testSnapshot(t)

	_, err := external.ToWireFillEvent(1, strategy.FillEvent{Fill: f}, snap)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestToWireFillEvent_ZeroCausationIDIsEmptyString(t *testing.T) {
	f := testFill(t)
	f.Metadata.CausationID = id.EventID{} // first event in a workflow
	snap := testSnapshot(t)

	w, err := external.ToWireFillEvent(1, strategy.FillEvent{Fill: f}, snap)
	require.NoError(t, err)
	require.Empty(t, w.GetCausationId())
}
