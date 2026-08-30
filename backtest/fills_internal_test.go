package backtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var fillsTestIDs = id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func fillsTestListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// fakeEventReader is a minimal broker.EventReader over a fixed slice
// of events, returning errAfter once exhausted instead of blocking —
// drainFills only needs to observe an error other than
// context.DeadlineExceeded to propagate it, so this fake does not need
// to reproduce EventReader's real blocking contract.
type fakeEventReader struct {
	events   []broker.Event
	idx      int
	errAfter error
}

func (r *fakeEventReader) Next(ctx context.Context) (broker.Event, error) {
	if r.idx < len(r.events) {
		e := r.events[r.idx]
		r.idx++
		return e, nil
	}
	return broker.Event{}, r.errAfter
}

func (r *fakeEventReader) Close() error { return nil }

func fillEvent(t *testing.T, seq uint64) broker.Event {
	t.Helper()
	fillID, err := id.GenerateFillID(fillsTestIDs)
	require.NoError(t, err)
	orderID, err := id.GenerateOrderID(fillsTestIDs)
	require.NoError(t, err)
	accountID, err := id.GenerateAccountID(fillsTestIDs)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(fillsTestIDs)
	require.NoError(t, err)

	fill, err := order.NewFill(order.Fill{
		FillID:    fillID,
		OrderID:   orderID,
		AccountID: accountID,
		Listing:   fillsTestListing(t),
		Side:      order.Buy,
		Price:     num.MustParsePrice("1.10000"),
		Quantity:  num.MustParseQuantity("1000"),
		Timestamp: time.Now(),
	})
	require.NoError(t, err)

	event, err := broker.NewEvent(broker.Event{
		Metadata:   id.Metadata{EventID: eventID, Timestamp: time.Now()},
		ObservedAt: time.Now(),
		Sequence:   seq,
		Kind:       broker.EventKindFill,
		Fill:       &fill,
	})
	require.NoError(t, err)
	return event
}

func TestDrainFills_StopsOnDeadlineExceeded(t *testing.T) {
	reader := &fakeEventReader{events: []broker.Event{fillEvent(t, 1), fillEvent(t, 2)}, errAfter: context.DeadlineExceeded}

	fills, err := drainFills(context.Background(), reader)
	require.NoError(t, err)
	assert.Len(t, fills, 2)
}

func TestDrainFills_PropagatesNonDeadlineError(t *testing.T) {
	wantErr := errors.New("fills_internal_test: injected reader failure")
	reader := &fakeEventReader{errAfter: wantErr}

	_, err := drainFills(context.Background(), reader)
	require.ErrorIs(t, err, wantErr)
}

func TestDrainFills_StopsOnAlreadyCanceledContext(t *testing.T) {
	reader := &fakeEventReader{events: []broker.Event{fillEvent(t, 1)}, errAfter: context.DeadlineExceeded}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := drainFills(ctx, reader)
	require.ErrorIs(t, err, context.Canceled)
}
