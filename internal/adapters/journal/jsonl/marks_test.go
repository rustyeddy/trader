package jsonl_test

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/journal"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustGbpUsdVenueListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(num.MustParsePrice("0.00001"), num.MustParseQuantity("1"), num.MustParseRate("1"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	l, err := instrument.NewListing(instrument.ListingParams{Instrument: inst, Provider: "sim", Venue: "ECN", Symbol: "GBP_USD", Spec: spec, Tradable: true})
	require.NoError(t, err)
	return l
}

// TestWriterReaderRoundTripsAccountMarks proves a snapshot's
// per-position marks, each with its own AsOf, survive a JSONL write and
// read exactly (ADR-066).
func TestWriterReaderRoundTripsAccountMarks(t *testing.T) {
	usd := num.MustParseCurrency("USD")
	accountID := mustAccountID(t)
	asOf := time.Date(2026, 3, 2, 21, 0, 0, 0, time.UTC)
	listings := []instrument.Listing{mustEurUsdListing(t), mustGbpUsdVenueListing(t)}

	var positions []runtimeorder.Position
	for _, l := range listings {
		avg := num.MustParsePrice("1.10000")
		p, err := runtimeorder.NewPosition(runtimeorder.Position{AccountID: accountID, Listing: l, Side: order.Long, Quantity: num.MustParseQuantity("100"), AvgPrice: &avg})
		require.NoError(t, err)
		positions = append(positions, p)
	}
	marks := []account.PositionMark{
		{Listing: account.KeyOf(listings[0]), Price: num.MustParsePrice("1.12345"), AsOf: asOf},
		{Listing: account.KeyOf(listings[1]), Price: num.MustParsePrice("1.25000"), AsOf: asOf.Add(-time.Hour)},
	}
	zero := num.MustParseMoney("0", usd)
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID: accountID, Broker: "sim", Currency: usd, AsOf: asOf,
		CashBalances: []num.Money{num.MustParseMoney("10000", usd)},
		Equity:       num.MustParseMoney("10000", usd), BuyingPower: zero, MarginUsed: zero, MarginAvailable: zero,
		RealizedPnL: zero, UnrealizedPnL: zero, Fees: zero, Financing: zero,
		Positions: positions, Marks: marks,
	})
	require.NoError(t, err)

	w, path := mustWriter(t)
	require.NoError(t, w.Record(context.Background(), journal.Record{RunID: mustRunID(t), Metadata: id.Metadata{Timestamp: asOf}, Kind: journal.KindAccount, Account: &snap}))
	require.NoError(t, w.Close())

	entries := readAll(t, path)
	require.Len(t, entries, 1)
	got := entries[0].Account.Marks()
	require.Len(t, got, 2)
	for i, want := range snap.Marks() {
		assert.Equal(t, want.Listing, got[i].Listing)
		assert.True(t, want.Price.Equal(got[i].Price))
		assert.True(t, want.AsOf.Equal(got[i].AsOf), "mark %d keeps its own AsOf", i)
	}
}

// TestWriterReaderRoundTripsCancelReason proves a broker-initiated
// cancel's reason (ADR-066) survives a JSONL write and read.
func TestWriterReaderRoundTripsCancelReason(t *testing.T) {
	o := mustWorkingOrderFor(t, mustAccountID(t))
	req, err := runtimeorder.NewCancelRequest(runtimeorder.CancelRequest{OrderID: o.Request.OrderID, Metadata: id.Metadata{EventID: mustEventID(t), Timestamp: time.Now()}})
	require.NoError(t, err)
	pending, err := runtimeorder.ApplyCancelRequest(o, req)
	require.NoError(t, err)
	result, err := runtimeorder.NewCancelResult(runtimeorder.CancelResult{OrderID: o.Request.OrderID, Status: runtimeorder.StatusCanceled, Metadata: id.Metadata{CausationID: req.Metadata.EventID, Timestamp: time.Now()}})
	require.NoError(t, err)
	canceled, err := runtimeorder.ApplyCancelResult(pending, result)
	require.NoError(t, err)
	canceled.CancelReason = &runtimeorder.Rejection{Reason: runtimeorder.ReasonInsufficientMargin, Detail: "required margin exceeds equity", BrokerCode: "sim"}

	w, path := mustWriter(t)
	require.NoError(t, w.Record(context.Background(), journal.Record{RunID: mustRunID(t), Metadata: id.Metadata{Timestamp: time.Now()}, Kind: journal.KindOrder, Order: &canceled}))
	require.NoError(t, w.Close())

	entries := readAll(t, path)
	require.Len(t, entries, 1)
	got := entries[0].Order
	assert.Equal(t, runtimeorder.StatusCanceled, got.Status)
	require.NotNil(t, got.CancelReason)
	assert.Equal(t, *canceled.CancelReason, *got.CancelReason)
	assert.Nil(t, got.Rejection)
}

// TestWriterReaderRoundTripsNoAction (ADR-067).
func TestWriterReaderRoundTripsNoAction(t *testing.T) {
	na := journal.NoAction{IntentID: mustIntentID(t), Reason: "pipeline: nothing to do: execution: no open position to exit"}
	w, path := mustWriter(t)
	require.NoError(t, w.Record(context.Background(), journal.Record{RunID: mustRunID(t), Metadata: id.Metadata{Timestamp: time.Now()}, Kind: journal.KindNoAction, NoAction: &na}))
	require.NoError(t, w.Close())

	entries := readAll(t, path)
	require.Len(t, entries, 1)
	assert.Equal(t, journal.KindNoAction, entries[0].Kind)
	require.NotNil(t, entries[0].NoAction)
	assert.Equal(t, na, *entries[0].NoAction)
}
