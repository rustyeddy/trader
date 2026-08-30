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

// fakeFiniteEventReader is a broker.FiniteEventReader over a fixed
// slice of events. AtEnd reports idx >= len(events) unless
// forceNotAtEnd overrides it — used to exercise drainFills' behavior
// when Next itself fails or blocks on a genuinely canceled ctx, cases
// AtEnd's real definition would never actually produce on its own.
type fakeFiniteEventReader struct {
	events        []broker.Event
	idx           int
	nextErr       error
	forceNotAtEnd bool
}

func (r *fakeFiniteEventReader) Next(ctx context.Context) (broker.Event, error) {
	select {
	case <-ctx.Done():
		return broker.Event{}, ctx.Err()
	default:
	}
	if r.idx < len(r.events) {
		e := r.events[r.idx]
		r.idx++
		return e, nil
	}
	return broker.Event{}, r.nextErr
}

func (r *fakeFiniteEventReader) Close() error { return nil }

func (r *fakeFiniteEventReader) AtEnd() bool {
	if r.forceNotAtEnd {
		return false
	}
	return r.idx >= len(r.events)
}

var _ broker.FiniteEventReader = (*fakeFiniteEventReader)(nil)

// nonFiniteEventReader implements only broker.EventReader, not
// broker.FiniteEventReader — used to prove drainFills rejects a reader
// that cannot support deterministic draining rather than silently
// falling back to a timing guess.
type nonFiniteEventReader struct{}

func (nonFiniteEventReader) Next(ctx context.Context) (broker.Event, error) {
	return broker.Event{}, errors.New("nonFiniteEventReader: Next should never be called")
}
func (nonFiniteEventReader) Close() error { return nil }

var _ broker.EventReader = nonFiniteEventReader{}

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

func TestDrainFills_StopsAtEnd(t *testing.T) {
	reader := &fakeFiniteEventReader{events: []broker.Event{fillEvent(t, 1), fillEvent(t, 2)}}

	fills, err := drainFills(context.Background(), reader)
	require.NoError(t, err)
	assert.Len(t, fills, 2)
}

func TestDrainFills_RejectsNonFiniteReader(t *testing.T) {
	_, err := drainFills(context.Background(), nonFiniteEventReader{})
	require.ErrorIs(t, err, ErrEventReaderNotFinite)
}

func TestDrainFills_PropagatesNextError(t *testing.T) {
	wantErr := errors.New("fills_internal_test: injected reader failure")
	reader := &fakeFiniteEventReader{forceNotAtEnd: true, nextErr: wantErr}

	_, err := drainFills(context.Background(), reader)
	require.ErrorIs(t, err, wantErr)
}

func TestDrainFills_PropagatesContextCancellation(t *testing.T) {
	reader := &fakeFiniteEventReader{forceNotAtEnd: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := drainFills(ctx, reader)
	require.ErrorIs(t, err, context.Canceled)
}
