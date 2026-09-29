package risk

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// equityListing builds a USD-settled equity listing on the "sim"
// provider at venue (which may be empty).
func equityListing(t *testing.T, ticker, venue, settle string) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewEquity("ARCX", ticker)
	require.NoError(t, err)
	spec, err := instrument.NewSpec(num.MustParsePrice("0.01"), num.MustParseQuantity("1"), num.MustParseRate("1"), num.MustParseCurrency(settle))
	require.NoError(t, err)
	l, err := instrument.NewListing(instrument.ListingParams{Instrument: inst, Provider: "sim", Venue: venue, Symbol: ticker, Spec: spec, Tradable: true})
	require.NoError(t, err)
	return l
}

// held is one open position and its mark ("" for no mark).
type held struct {
	listing  instrument.Listing
	side     order.PositionSide
	quantity string
	mark     string
}

// marginSnapshot builds a USD snapshot on "sim" with the given equity
// and open positions, each marked as held specifies.
func marginSnapshot(t *testing.T, accountID id.AccountID, equity string, positions ...held) account.Snapshot {
	t.Helper()
	usd := num.MustParseCurrency("USD")
	money := func(s string) num.Money { return num.MustParseMoney(s, usd) }
	var ps []runtimeorder.Position
	var marks []account.PositionMark
	for _, h := range positions {
		ps = append(ps, mustPosition(t, accountID, h.listing, h.side, h.quantity))
		if h.mark != "" {
			marks = append(marks, account.PositionMark{Listing: account.KeyOf(h.listing), Price: num.MustParsePrice(h.mark), AsOf: testStart})
		}
	}
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID: accountID, Broker: "sim", Currency: usd, AsOf: testStart,
		CashBalances: []num.Money{money(equity)},
		Equity:       money(equity), BuyingPower: money(equity), MarginUsed: money("0"), MarginAvailable: money(equity),
		RealizedPnL: money("0"), UnrealizedPnL: money("0"), Fees: money("0"), Financing: money("0"),
		Positions: ps, Marks: marks,
	})
	require.NoError(t, err)
	return snap
}

func marginRule(t *testing.T, ratio string) Rule {
	t.Helper()
	r, err := NewAccountInitialMarginRule(num.MustParseRate(ratio))
	require.NoError(t, err)
	return r
}

func marginInput(t *testing.T, snap account.Snapshot, listing instrument.Listing, side order.Side, quantity, reference string) Input {
	t.Helper()
	in := Input{Proposal: mustProposalWith(t, snap.AccountID(), listing, side, quantity, false), Account: snap}
	if reference != "" {
		p := num.MustParsePrice(reference)
		in.ReferencePrice = &p
	}
	return in
}

// evaluate returns whether the rule admits in.
func evaluate(t *testing.T, r Rule, in Input) (bool, RuleResult) {
	t.Helper()
	res, err := r.Evaluate(context.Background(), in)
	require.NoError(t, err)
	return len(res.Violations) == 0, res
}

func TestNewAccountInitialMarginRule(t *testing.T) {
	r := marginRule(t, "0.5")
	assert.Equal(t, "account_initial_margin", r.Name())
	for _, bad := range []string{"0", "-1"} {
		_, err := NewAccountInitialMarginRule(num.MustParseRate(bad))
		assert.ErrorIs(t, err, ErrInvalidRule, bad)
	}
}

func TestAccountInitialMargin_MotivatingSPYOrder(t *testing.T) {
	aid := mustAccountID(t)
	spy := equityListing(t, "SPY", "", "USD")
	snap := marginSnapshot(t, aid, "10000")
	r := marginRule(t, "1")

	ok, res := evaluate(t, r, marginInput(t, snap, spy, order.Buy, "100", "463.08"))
	require.False(t, ok, "46308 of gross notional on 10000 of equity at 1.0")
	require.Len(t, res.Violations, 1)
	v := res.Violations[0]
	assert.Equal(t, "46308 USD", v.Measured)
	assert.Equal(t, "10000 USD", v.Limit)
	assert.Contains(t, v.Message, "gross notional 46308 USD")
	assert.Contains(t, v.Message, "up from 0 USD")
	assert.Contains(t, v.Message, "ratio 1")

	ok, _ = evaluate(t, r, marginInput(t, snap, spy, order.Buy, "21", "463.08"))
	assert.True(t, ok, "21 shares = 9724.68 fits")
}

func TestAccountInitialMargin_ExactLimit(t *testing.T) {
	aid := mustAccountID(t)
	spy := equityListing(t, "SPY", "", "USD")
	snap := marginSnapshot(t, aid, "10000")
	r := marginRule(t, "1")

	ok, _ := evaluate(t, r, marginInput(t, snap, spy, order.Buy, "100", "100"))
	assert.True(t, ok, "exactly equal to equity is admitted")
	ok, _ = evaluate(t, r, marginInput(t, snap, spy, order.Buy, "101", "100"))
	assert.False(t, ok, "one unit over")
}

func TestAccountInitialMargin_AggregatesAcrossPositions(t *testing.T) {
	aid := mustAccountID(t)
	spy, qqq := equityListing(t, "SPY", "", "USD"), equityListing(t, "QQQ", "", "USD")
	r := marginRule(t, "1")

	t.Run("individually acceptable positions cannot collectively exceed", func(t *testing.T) {
		snap := marginSnapshot(t, aid, "10000", held{qqq, order.Long, "50", "100"}) // 5000
		ok, _ := evaluate(t, r, marginInput(t, snap, spy, order.Buy, "50", "100"))
		assert.True(t, ok, "5000 + 5000 = 10000")
		ok, _ = evaluate(t, r, marginInput(t, snap, spy, order.Buy, "51", "100"))
		assert.False(t, ok, "5000 + 5100 exceeds 10000, though 5100 alone would fit")
	})
	t.Run("short positions count at gross value", func(t *testing.T) {
		snap := marginSnapshot(t, aid, "10000", held{qqq, order.Short, "60", "100"}) // 6000 gross
		ok, _ := evaluate(t, r, marginInput(t, snap, spy, order.Buy, "50", "100"))
		assert.False(t, ok, "6000 + 5000 gross exceeds 10000, though net would be -1000")
	})
	t.Run("other positions valued at their marks", func(t *testing.T) {
		// QQQ's AvgPrice is 1.10 (mustPosition), its mark is 150: 50 × 150 = 7500.
		snap := marginSnapshot(t, aid, "10000", held{qqq, order.Long, "50", "150"})
		ok, _ := evaluate(t, r, marginInput(t, snap, spy, order.Buy, "26", "100"))
		assert.False(t, ok, "7500 + 2600 exceeds 10000")
	})
}

func TestAccountInitialMargin_RatioHalfPermitsTwoX(t *testing.T) {
	aid := mustAccountID(t)
	spy := equityListing(t, "SPY", "", "USD")
	snap := marginSnapshot(t, aid, "10000")
	r := marginRule(t, "0.5")

	ok, _ := evaluate(t, r, marginInput(t, snap, spy, order.Buy, "200", "100"))
	assert.True(t, ok, "20000 gross × 0.5 = 10000")
	ok, _ = evaluate(t, r, marginInput(t, snap, spy, order.Buy, "201", "100"))
	assert.False(t, ok)
}

func TestAccountInitialMargin_DeRiskingOnOverLimitAccount(t *testing.T) {
	aid := mustAccountID(t)
	spy := equityListing(t, "SPY", "", "USD")
	// 300 SPY at 100 = 30000 gross on 10000 equity: over the limit.
	// No marks and no ReferencePrice: de-risking needs neither.
	snap := marginSnapshot(t, aid, "10000", held{spy, order.Long, "300", ""})
	r := marginRule(t, "1")

	for _, tc := range []struct {
		name     string
		side     order.Side
		quantity string
	}{
		{"partial reduction", order.Sell, "100"},
		{"full close", order.Sell, "300"},
		{"reversal that doesn't grow", order.Sell, "500"}, // 200 short
		{"reversal to equal size", order.Sell, "600"},     // 300 short
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, _ := evaluate(t, r, marginInput(t, snap, spy, tc.side, tc.quantity, ""))
			assert.True(t, ok)
		})
	}
	t.Run("reduce-only larger than the position", func(t *testing.T) {
		in := Input{Proposal: mustProposalWith(t, aid, spy, order.Sell, "1000", true), Account: snap}
		ok, _ := evaluate(t, r, in)
		assert.True(t, ok, "clamps to flat")
	})
	t.Run("reversal that grows is evaluated", func(t *testing.T) {
		_, err := r.Evaluate(context.Background(), marginInput(t, snap, spy, order.Sell, "700", "")) // 400 short
		assert.ErrorIs(t, err, ErrInsufficientRuleInput, "needs a price once it increases exposure")
		ok, _ := evaluate(t, r, marginInput(t, snap, spy, order.Sell, "700", "100"))
		assert.False(t, ok, "400 short × 100 = 40000 exceeds 10000")
	})
}

func TestAccountInitialMargin_ListingLevelPositions(t *testing.T) {
	aid := mustAccountID(t)
	arca, bats := equityListing(t, "SPY", "ARCA", "USD"), equityListing(t, "SPY", "BATS", "USD")
	require.True(t, arca.InstrumentID().Equal(bats.InstrumentID()))
	snap := marginSnapshot(t, aid, "10000", held{arca, order.Long, "50", "100"}) // 5000
	r := marginRule(t, "1")

	// A buy of 50 in BATS opens a separate BATS position: the resulting
	// BATS position is 50, not the 100 an instrument-level match would
	// produce by adding the existing 50-share ARCA position.
	ok, _ := evaluate(t, r, marginInput(t, snap, bats, order.Buy, "50", "100"))
	assert.True(t, ok, "5000 (ARCA) + 5000 (BATS)")
	ok, _ = evaluate(t, r, marginInput(t, snap, bats, order.Buy, "51", "100"))
	assert.False(t, ok)

	// A sell in BATS opens a BATS short; it does not reduce the ARCA
	// long, so it increases gross exposure and needs a price.
	_, err := r.Evaluate(context.Background(), marginInput(t, snap, bats, order.Sell, "10", ""))
	assert.ErrorIs(t, err, ErrInsufficientRuleInput)
	ok, _ = evaluate(t, r, marginInput(t, snap, bats, order.Sell, "51", "100"))
	assert.False(t, ok, "5000 long + 5100 short gross")
}

func TestAccountInitialMargin_InputErrors(t *testing.T) {
	aid := mustAccountID(t)
	spy, qqq := equityListing(t, "SPY", "", "USD"), equityListing(t, "QQQ", "", "USD")
	r := marginRule(t, "1")

	t.Run("missing ReferencePrice for an increase", func(t *testing.T) {
		_, err := r.Evaluate(context.Background(), marginInput(t, marginSnapshot(t, aid, "10000"), spy, order.Buy, "1", ""))
		assert.ErrorIs(t, err, ErrInsufficientRuleInput)
	})
	t.Run("zero ReferencePrice", func(t *testing.T) {
		_, err := r.Evaluate(context.Background(), marginInput(t, marginSnapshot(t, aid, "10000"), spy, order.Buy, "1", "0"))
		assert.ErrorIs(t, err, ErrInsufficientRuleInput)
	})
	t.Run("missing mark for another position", func(t *testing.T) {
		snap := marginSnapshot(t, aid, "10000", held{qqq, order.Long, "1", ""})
		_, err := r.Evaluate(context.Background(), marginInput(t, snap, spy, order.Buy, "1", "100"))
		assert.ErrorIs(t, err, ErrInsufficientRuleInput)
	})
	t.Run("currency mismatch", func(t *testing.T) {
		eurSettled := equityListing(t, "SAP", "", "EUR")
		_, err := r.Evaluate(context.Background(), marginInput(t, marginSnapshot(t, aid, "10000"), eurSettled, order.Buy, "1", "100"))
		assert.ErrorIs(t, err, ErrInsufficientRuleInput)
	})
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := r.Evaluate(ctx, marginInput(t, marginSnapshot(t, aid, "10000"), spy, order.Buy, "1", "100"))
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestAccountInitialMargin_ThroughEngine(t *testing.T) {
	aid := mustAccountID(t)
	spy := equityListing(t, "SPY", "", "USD")
	engine, err := NewEngine(marginRule(t, "1"))
	require.NoError(t, err)

	d, err := engine.Evaluate(context.Background(), marginInput(t, marginSnapshot(t, aid, "10000"), spy, order.Buy, "100", "463.08"))
	require.NoError(t, err)
	assert.False(t, d.Allowed)
	require.Len(t, d.Violations, 1)
	assert.Equal(t, "account_initial_margin", d.Violations[0].Rule)
}
