package risk

import (
	"context"
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
var testClock = clock.NewSimulated(testStart)

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

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	gen := id.NewGenerator(testClock, id.NewDeterministic(1, 2))
	aid, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	return aid
}

// mustGbpUsdListing builds a GBP/USD instrument.Listing — a distinct
// instrument from mustEurUsdListing, for tests exercising multiple
// positions/instruments (unlike mustListingWithSpec, which always
// builds a EUR/USD instrument regardless of its own parameters).
func mustGbpUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
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
		Symbol:     "GBP_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func mustOtherAccountID(t *testing.T) id.AccountID {
	t.Helper()
	gen := id.NewGenerator(testClock, id.NewDeterministic(5, 6))
	aid, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	return aid
}

// mustEurUsdListingForProvider is mustEurUsdListing with an overridden
// Provider, for tests exercising the Listing.Provider()/Account.Broker()
// consistency check.
func mustEurUsdListingForProvider(t *testing.T, provider string) instrument.Listing {
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
		Provider:   provider,
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func mustSnapshot(t *testing.T, accountID id.AccountID, listing instrument.Listing) account.Snapshot {
	t.Helper()
	return mustSnapshotWithEquity(t, accountID, listing.Provider(), "USD", "10000")
}

// mustSnapshotWithEquity builds a minimal, valid account.Snapshot with
// an explicit broker, equity currency, and equity amount — for sizing
// tests that need to control account size or currency independent of
// mustEurUsdListing's own fixed USD/10000 defaults.
func mustSnapshotWithEquity(t *testing.T, accountID id.AccountID, broker, currency, equity string) account.Snapshot {
	t.Helper()
	cur := num.MustParseCurrency(currency)
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          broker,
		Currency:        cur,
		AsOf:            testStart,
		CashBalances:    []num.Money{num.MustParseMoney(equity, cur)},
		Equity:          num.MustParseMoney(equity, cur),
		BuyingPower:     num.MustParseMoney(equity, cur),
		MarginUsed:      num.MustParseMoney("0", cur),
		MarginAvailable: num.MustParseMoney(equity, cur),
		RealizedPnL:     num.MustParseMoney("0", cur),
		UnrealizedPnL:   num.MustParseMoney("0", cur),
		Fees:            num.MustParseMoney("0", cur),
		Financing:       num.MustParseMoney("0", cur),
	})
	require.NoError(t, err)
	return snap
}

// mustSnapshotWithPositions is mustSnapshotWithEquity plus open
// positions, for per-trade-loss tests exercising existing exposure.
func mustSnapshotWithPositions(t *testing.T, accountID id.AccountID, broker, currency, equity string, positions ...order.Position) account.Snapshot {
	t.Helper()
	cur := num.MustParseCurrency(currency)
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       accountID,
		Broker:          broker,
		Currency:        cur,
		AsOf:            testStart,
		CashBalances:    []num.Money{num.MustParseMoney(equity, cur)},
		Equity:          num.MustParseMoney(equity, cur),
		BuyingPower:     num.MustParseMoney(equity, cur),
		MarginUsed:      num.MustParseMoney("0", cur),
		MarginAvailable: num.MustParseMoney(equity, cur),
		RealizedPnL:     num.MustParseMoney("0", cur),
		UnrealizedPnL:   num.MustParseMoney("0", cur),
		Fees:            num.MustParseMoney("0", cur),
		Financing:       num.MustParseMoney("0", cur),
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

// mustListingWithSpec builds an EUR/USD instrument.Listing with a
// fully parameterized Spec, for sizing tests exercising the contract
// multiplier, quantity increment, and settlement currency.
func mustListingWithSpec(t *testing.T, provider, tickSize, quantityIncrement, multiplier, settlementCurrency string) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice(tickSize),
		num.MustParseQuantity(quantityIncrement),
		num.MustParseRate(multiplier),
		num.MustParseCurrency(settlementCurrency),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   provider,
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func mustProposal(t *testing.T, accountID id.AccountID, listing instrument.Listing) order.Proposal {
	t.Helper()
	p, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	return p
}

// mustProposalWith builds a Market/GTC Proposal with an explicit
// Side/Quantity/ReduceOnly, for per-trade-loss tests exercising
// existing-position scenarios mustProposal's fixed Buy/1000/false
// defaults can't express.
func mustProposalWith(t *testing.T, accountID id.AccountID, listing instrument.Listing, side order.Side, quantity string, reduceOnly bool) order.Proposal {
	t.Helper()
	p, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        side,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity(quantity),
		ReduceOnly:  reduceOnly,
		Metadata:    id.Metadata{EventID: mustEventID(t)},
	})
	require.NoError(t, err)
	return p
}

func mustEventID(t *testing.T) id.EventID {
	t.Helper()
	gen := id.NewGenerator(testClock, id.NewDeterministic(3, 4))
	eid, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	return eid
}

// fakeRule is a test-double Rule whose behavior is fully controlled by
// its fields, used to prove Engine's own composition/aggregation/
// ordering behavior without depending on any real trading policy.
type fakeRule struct {
	name   string
	result RuleResult
	err    error
	calls  *[]string // if non-nil, appends name on every Evaluate call
}

func (f *fakeRule) Name() string { return f.name }

func (f *fakeRule) Evaluate(ctx context.Context, in Input) (RuleResult, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name)
	}
	if f.err != nil {
		return RuleResult{}, f.err
	}
	r := f.result
	r.Rule = f.name
	return r, nil
}

func passingRule(name string) *fakeRule {
	return &fakeRule{name: name, result: RuleResult{}}
}

func violatingRule(name, message string) *fakeRule {
	return &fakeRule{name: name, result: RuleResult{
		Violations: []Violation{{Rule: name, Message: message}},
	}}
}

func warningRule(name, message string) *fakeRule {
	return &fakeRule{name: name, result: RuleResult{
		Warnings: []Warning{{Rule: name, Message: message}},
	}}
}
