package execution

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/require"
)

var testStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func testDeps() Deps {
	c := clock.NewSimulated(testStart)
	return Deps{Clock: c, IDs: id.NewGenerator(c, id.NewDeterministic(1, 2))}
}

func mustEurUsdListing(t *testing.T) instrument.Listing {
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

func mustGbpUsdInstrumentID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	return inst.ID()
}

func mustAccountID(t *testing.T, gen *id.Generator) id.AccountID {
	t.Helper()
	aid, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	return aid
}

func mustIntentID(t *testing.T, gen *id.Generator) id.IntentID {
	t.Helper()
	iid, err := id.GenerateIntentID(gen)
	require.NoError(t, err)
	return iid
}

func mustEventID(t *testing.T, gen *id.Generator) id.EventID {
	t.Helper()
	eid, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	return eid
}

func mustCorrelationID(t *testing.T, gen *id.Generator) id.CorrelationID {
	t.Helper()
	cid, err := id.GenerateCorrelationID(gen)
	require.NoError(t, err)
	return cid
}

// mustSnapshot builds a minimal, valid account.Snapshot for accountID
// carrying positions (may be empty), using listing.Provider() as the
// snapshot's own Broker so every position's own Listing.Provider
// consistency check passes.
func mustSnapshot(t *testing.T, accountID id.AccountID, listing instrument.Listing, positions ...order.Position) account.Snapshot {
	t.Helper()
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          listing.Provider(),
		Currency:        num.MustParseCurrency("USD"),
		AsOf:            testStart,
		CashBalances:    []num.Money{num.MustParseMoney("10000", num.MustParseCurrency("USD"))},
		Equity:          num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		BuyingPower:     num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		MarginUsed:      num.MustParseMoney("0", num.MustParseCurrency("USD")),
		MarginAvailable: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		RealizedPnL:     num.MustParseMoney("0", num.MustParseCurrency("USD")),
		UnrealizedPnL:   num.MustParseMoney("0", num.MustParseCurrency("USD")),
		Fees:            num.MustParseMoney("0", num.MustParseCurrency("USD")),
		Financing:       num.MustParseMoney("0", num.MustParseCurrency("USD")),
		Positions:       positions,
	})
	require.NoError(t, err)
	return snap
}

func mustPosition(t *testing.T, accountID id.AccountID, listing instrument.Listing, side order.PositionSide, quantity string) order.Position {
	t.Helper()
	q := num.MustParseQuantity(quantity)
	avg := num.MustParsePrice("1.10000")
	p, err := order.NewPosition(order.Position{
		AccountID: accountID,
		Listing:   listing,
		Side:      side,
		Quantity:  q,
		AvgPrice:  &avg,
	})
	require.NoError(t, err)
	return p
}

func mustEnterIntent(t *testing.T, gen *id.Generator, instID instrument.ID, side order.Side) order.Intent {
	t.Helper()
	corrID := mustCorrelationID(t, gen)
	in, err := order.NewIntent(order.Intent{
		IntentID:   mustIntentID(t, gen),
		Kind:       order.IntentEnter,
		Instrument: instID,
		Side:       side,
		Metadata:   id.Metadata{EventID: mustEventID(t, gen), CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

func mustExitIntent(t *testing.T, gen *id.Generator, instID instrument.ID) order.Intent {
	t.Helper()
	corrID := mustCorrelationID(t, gen)
	in, err := order.NewIntent(order.Intent{
		IntentID:   mustIntentID(t, gen),
		Kind:       order.IntentExit,
		Instrument: instID,
		Metadata:   id.Metadata{EventID: mustEventID(t, gen), CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

func mustTargetExposureIntent(t *testing.T, gen *id.Generator, instID instrument.ID, side order.Side, quantity string) order.Intent {
	t.Helper()
	q := num.MustParseQuantity(quantity)
	corrID := mustCorrelationID(t, gen)
	in, err := order.NewIntent(order.Intent{
		IntentID:   mustIntentID(t, gen),
		Kind:       order.IntentTargetExposure,
		Instrument: instID,
		Side:       side,
		Quantity:   &q,
		Metadata:   id.Metadata{EventID: mustEventID(t, gen), CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

func mustAdjustStopIntent(t *testing.T, gen *id.Generator, instID instrument.ID) order.Intent {
	t.Helper()
	corrID := mustCorrelationID(t, gen)
	sp := num.MustParsePrice("1.05000")
	in, err := order.NewIntent(order.Intent{
		IntentID:   mustIntentID(t, gen),
		Kind:       order.IntentAdjustStop,
		Instrument: instID,
		StopPrice:  &sp,
		Metadata:   id.Metadata{EventID: mustEventID(t, gen), CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

func qty(t *testing.T, s string) *num.Quantity {
	t.Helper()
	q := num.MustParseQuantity(s)
	return &q
}
