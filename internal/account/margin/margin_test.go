package margin

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var usd = num.MustParseCurrency("USD")

func money(s string) num.Money     { return num.MustParseMoney(s, usd) }
func price(s string) num.Price     { return num.MustParsePrice(s) }
func qty(s string) num.Quantity    { return num.MustParseQuantity(s) }
func rate(s string) num.Rate       { return num.MustParseRate(s) }
func pricePtr(s string) *num.Price { p := price(s); return &p }

// listing builds a listing for inst settling in settle with the given
// contract multiplier.
func listing(t *testing.T, inst instrument.Instrument, symbol, multiplier string, settle num.Currency) instrument.Listing {
	t.Helper()
	return listingAt(t, inst, symbol, "", multiplier, settle)
}

// listingAt is listing on a specific venue.
func listingAt(t *testing.T, inst instrument.Instrument, symbol, venue, multiplier string, settle num.Currency) instrument.Listing {
	t.Helper()
	spec, err := instrument.NewSpec(price("0.01"), qty("1"), rate(multiplier), settle)
	require.NoError(t, err)
	l, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     symbol,
		Venue:      venue,
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return l
}

func equity(t *testing.T, ticker string) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewEquity("ARCX", ticker)
	require.NoError(t, err)
	return listing(t, inst, ticker, "1", usd)
}

func future(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewFuture("ES", time.Date(2026, time.December, 18, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return listing(t, inst, "ESZ6", "50", usd)
}

func jpySettled(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(usd, num.MustParseCurrency("JPY"))
	require.NoError(t, err)
	return listing(t, inst, "USD_JPY", "1", num.MustParseCurrency("JPY"))
}

func accountID(t *testing.T) id.AccountID {
	t.Helper()
	gen := id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))
	aid, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	return aid
}

func position(t *testing.T, l instrument.Listing, side order.PositionSide, quantity, avg string) runtimeorder.Position {
	t.Helper()
	p := runtimeorder.Position{AccountID: accountID(t), Listing: l, Side: side, Quantity: qty(quantity)}
	if side != order.Flat {
		p.AvgPrice = pricePtr(avg)
	}
	p, err := runtimeorder.NewPosition(p)
	require.NoError(t, err)
	return p
}

func ratio(t *testing.T, s string) Ratio {
	t.Helper()
	r, err := NewRatio(rate(s))
	require.NoError(t, err)
	return r
}

func assertMoney(t *testing.T, want string, got num.Money) {
	t.Helper()
	assert.True(t, money(want).Equal(got), "want %s USD, got %s", want, got)
}

func TestNewRatio(t *testing.T) {
	r, err := NewRatio(rate("0.5"))
	require.NoError(t, err)
	assert.True(t, rate("0.5").Equal(r.Value()))

	for _, bad := range []string{"0", "-1"} {
		_, err := NewRatio(rate(bad))
		assert.ErrorIs(t, err, ErrInvalidPolicy, bad)
	}
}

func TestRatio_ZeroValueIsInvalid(t *testing.T) {
	_, err := Ratio{}.RequiredMargin(Valued{Listing: equity(t, "SPY"), Quantity: qty("1"), Price: price("1")}, usd)
	assert.ErrorIs(t, err, ErrInvalidPolicy)
}

func TestNotional(t *testing.T) {
	t.Run("multiplier one", func(t *testing.T) {
		n, err := Notional(Valued{Listing: equity(t, "SPY"), Quantity: qty("100"), Price: price("463.08")}, usd)
		require.NoError(t, err)
		assertMoney(t, "46308", n)
	})
	t.Run("multiplier not one", func(t *testing.T) {
		// 2 ES contracts × 5000.25 × 50 = 500025.
		n, err := Notional(Valued{Listing: future(t), Quantity: qty("2"), Price: price("5000.25")}, usd)
		require.NoError(t, err)
		assertMoney(t, "500025", n)
	})
	t.Run("currency mismatch", func(t *testing.T) {
		_, err := Notional(Valued{Listing: jpySettled(t), Quantity: qty("1"), Price: price("150")}, usd)
		assert.ErrorIs(t, err, ErrCurrencyMismatch)
	})
	t.Run("unconstructed listing", func(t *testing.T) {
		_, err := Notional(Valued{Quantity: qty("1"), Price: price("1")}, usd)
		assert.ErrorIs(t, err, ErrInvalidInput)
	})
}

func TestRatio_RequiredMargin(t *testing.T) {
	v := Valued{Listing: equity(t, "SPY"), Quantity: qty("100"), Price: price("100")}
	for _, tc := range []struct{ ratio, want string }{
		{"1", "10000"},
		{"0.5", "5000"},
		{"0.25", "2500"},
	} {
		got, err := ratio(t, tc.ratio).RequiredMargin(v, usd)
		require.NoError(t, err)
		assertMoney(t, tc.want, got)
	}

	_, err := ratio(t, "1").RequiredMargin(Valued{Listing: jpySettled(t), Quantity: qty("1"), Price: price("1")}, usd)
	assert.ErrorIs(t, err, ErrCurrencyMismatch)
}

func TestAccount(t *testing.T) {
	spy, qqq := equity(t, "SPY"), equity(t, "QQQ")
	marks := Marks{account.KeyOf(spy): price("100"), account.KeyOf(qqq): price("200")}

	t.Run("flat account", func(t *testing.T) {
		req, err := Account(nil, nil, ratio(t, "1"), usd)
		require.NoError(t, err)
		assertMoney(t, "0", req.Gross)
		assertMoney(t, "0", req.Required)
	})
	t.Run("flat position needs no mark", func(t *testing.T) {
		req, err := Account([]runtimeorder.Position{position(t, spy, order.Flat, "0", "")}, nil, ratio(t, "1"), usd)
		require.NoError(t, err)
		assertMoney(t, "0", req.Gross)
	})
	t.Run("single long", func(t *testing.T) {
		req, err := Account([]runtimeorder.Position{position(t, spy, order.Long, "100", "90")}, marks, ratio(t, "1"), usd)
		require.NoError(t, err)
		// Valued at the mark (100), never AvgPrice (90).
		assertMoney(t, "10000", req.Gross)
		assertMoney(t, "10000", req.Required)
	})
	t.Run("single short", func(t *testing.T) {
		req, err := Account([]runtimeorder.Position{position(t, spy, order.Short, "100", "110")}, marks, ratio(t, "1"), usd)
		require.NoError(t, err)
		assertMoney(t, "10000", req.Gross)
	})
	t.Run("long and short are gross, not net", func(t *testing.T) {
		positions := []runtimeorder.Position{
			position(t, spy, order.Long, "100", "100"), // +10000
			position(t, qqq, order.Short, "50", "200"), // -10000 net, +10000 gross
		}
		req, err := Account(positions, marks, ratio(t, "1"), usd)
		require.NoError(t, err)
		assertMoney(t, "20000", req.Gross)
		assertMoney(t, "20000", req.Required)
	})
	t.Run("multi-instrument at ratio 0.5", func(t *testing.T) {
		positions := []runtimeorder.Position{
			position(t, spy, order.Long, "100", "100"),
			position(t, qqq, order.Long, "25", "200"),
		}
		req, err := Account(positions, marks, ratio(t, "0.5"), usd)
		require.NoError(t, err)
		assertMoney(t, "15000", req.Gross)
		assertMoney(t, "7500", req.Required)
	})
	t.Run("multiplier not one", func(t *testing.T) {
		es := future(t)
		req, err := Account([]runtimeorder.Position{position(t, es, order.Long, "1", "5000")}, Marks{account.KeyOf(es): price("5000")}, ratio(t, "0.1"), usd)
		require.NoError(t, err)
		assertMoney(t, "250000", req.Gross)
		assertMoney(t, "25000", req.Required)
	})
	t.Run("missing mark", func(t *testing.T) {
		_, err := Account([]runtimeorder.Position{position(t, spy, order.Long, "1", "1")}, Marks{}, ratio(t, "1"), usd)
		assert.ErrorIs(t, err, ErrMissingMark)
	})
	t.Run("currency mismatch", func(t *testing.T) {
		jpy := jpySettled(t)
		_, err := Account([]runtimeorder.Position{position(t, jpy, order.Long, "1", "150")}, Marks{account.KeyOf(jpy): price("150")}, ratio(t, "1"), usd)
		assert.ErrorIs(t, err, ErrCurrencyMismatch)
	})
	t.Run("nil policy", func(t *testing.T) {
		_, err := Account(nil, nil, nil, usd)
		assert.ErrorIs(t, err, ErrInvalidPolicy)
	})
	t.Run("unusable currency", func(t *testing.T) {
		_, err := Account(nil, nil, ratio(t, "1"), num.Currency{})
		assert.ErrorIs(t, err, ErrInvalidInput)
	})
}

func TestRequirement_Within(t *testing.T) {
	spy := equity(t, "SPY")
	marks := Marks{account.KeyOf(spy): price("100")}
	eq := money("10000")

	t.Run("exact limit is admitted", func(t *testing.T) {
		req, err := Account([]runtimeorder.Position{position(t, spy, order.Long, "100", "100")}, marks, ratio(t, "1"), usd)
		require.NoError(t, err)
		ok, err := req.Within(eq)
		require.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("one unit over the limit", func(t *testing.T) {
		req, err := Account([]runtimeorder.Position{position(t, spy, order.Long, "101", "100")}, marks, ratio(t, "1"), usd)
		require.NoError(t, err)
		ok, err := req.Within(eq)
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("equity in another currency", func(t *testing.T) {
		_, err := Requirement{Gross: money("0"), Required: money("0")}.Within(num.MustParseMoney("1", num.MustParseCurrency("EUR")))
		assert.Error(t, err)
	})
}

func TestAssess(t *testing.T) {
	spy, qqq := equity(t, "SPY"), equity(t, "QQQ")
	marks := Marks{account.KeyOf(spy): price("100"), account.KeyOf(qqq): price("200")}
	one := ratio(t, "1")

	t.Run("motivating SPY order from flat", func(t *testing.T) {
		// #409: $10,000 equity, 100 SPY at 463.08 is 4.63× equity.
		a, err := Assess(nil, nil, Change{Listing: spy, Resulting: qty("100"), Price: price("463.08")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "0", a.Current.Gross)
		assertMoney(t, "46308", a.Prospective.Gross)
		up, err := a.Increases()
		require.NoError(t, err)
		assert.True(t, up)
		ok, err := a.Prospective.Within(money("10000"))
		require.NoError(t, err)
		assert.False(t, ok)

		// 21 shares fit: 9724.68 ≤ 10000.
		a, err = Assess(nil, nil, Change{Listing: spy, Resulting: qty("21"), Price: price("463.08")}, one, usd)
		require.NoError(t, err)
		ok, err = a.Prospective.Within(money("10000"))
		require.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("changed instrument valued at change price in both states", func(t *testing.T) {
		// SPY's mark is 100, but the change prices it at 110 in both
		// states, so the comparison is a pure quantity comparison.
		positions := []runtimeorder.Position{position(t, spy, order.Long, "100", "95")}
		a, err := Assess(positions, marks, Change{Listing: spy, Resulting: qty("150"), Price: price("110")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "11000", a.Current.Gross)
		assertMoney(t, "16500", a.Prospective.Gross)
	})
	t.Run("changed instrument needs no mark", func(t *testing.T) {
		positions := []runtimeorder.Position{position(t, spy, order.Long, "100", "95")}
		a, err := Assess(positions, Marks{}, Change{Listing: spy, Resulting: qty("100"), Price: price("110")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "11000", a.Current.Gross)
	})
	t.Run("other positions valued at their marks in both states", func(t *testing.T) {
		positions := []runtimeorder.Position{
			position(t, spy, order.Long, "10", "100"),
			position(t, qqq, order.Short, "10", "150"), // mark 200 → 2000 gross
		}
		a, err := Assess(positions, marks, Change{Listing: spy, Resulting: qty("20"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "3000", a.Current.Gross)
		assertMoney(t, "4000", a.Prospective.Gross)
	})
	t.Run("partial reduction does not increase", func(t *testing.T) {
		positions := []runtimeorder.Position{position(t, spy, order.Long, "100", "100")}
		a, err := Assess(positions, marks, Change{Listing: spy, Resulting: qty("40"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "4000", a.Prospective.Gross)
		up, err := a.Increases()
		require.NoError(t, err)
		assert.False(t, up)
	})
	t.Run("close contributes nothing", func(t *testing.T) {
		positions := []runtimeorder.Position{
			position(t, spy, order.Long, "100", "100"),
			position(t, qqq, order.Long, "10", "200"),
		}
		a, err := Assess(positions, marks, Change{Listing: spy, Resulting: qty("0"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "12000", a.Current.Gross)
		assertMoney(t, "2000", a.Prospective.Gross)
	})
	t.Run("reversal uses the resulting position", func(t *testing.T) {
		// 100 long, sell 200 → 100 short: 100 units, not 200 or 300.
		positions := []runtimeorder.Position{position(t, spy, order.Long, "100", "100")}
		a, err := Assess(positions, marks, Change{Listing: spy, Resulting: qty("100"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "10000", a.Prospective.Gross)
		up, err := a.Increases()
		require.NoError(t, err)
		assert.False(t, up)

		// 100 long, sell 250 → 150 short grows exposure.
		a, err = Assess(positions, marks, Change{Listing: spy, Resulting: qty("150"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		up, err = a.Increases()
		require.NoError(t, err)
		assert.True(t, up)
	})
	t.Run("already over the limit, reduction still does not increase", func(t *testing.T) {
		positions := []runtimeorder.Position{position(t, spy, order.Long, "300", "100")}
		a, err := Assess(positions, marks, Change{Listing: spy, Resulting: qty("200"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		ok, err := a.Prospective.Within(money("10000"))
		require.NoError(t, err)
		assert.False(t, ok, "still over the limit")
		up, err := a.Increases()
		require.NoError(t, err)
		assert.False(t, up, "but de-risking")
	})
	t.Run("missing mark for another position", func(t *testing.T) {
		positions := []runtimeorder.Position{position(t, qqq, order.Long, "1", "200")}
		_, err := Assess(positions, Marks{}, Change{Listing: spy, Resulting: qty("1"), Price: price("100")}, one, usd)
		assert.ErrorIs(t, err, ErrMissingMark)
	})
	t.Run("more than one open position in the changed instrument", func(t *testing.T) {
		positions := []runtimeorder.Position{
			position(t, spy, order.Long, "1", "100"),
			position(t, spy, order.Long, "1", "100"),
		}
		_, err := Assess(positions, marks, Change{Listing: spy, Resulting: qty("1"), Price: price("100")}, one, usd)
		assert.ErrorIs(t, err, ErrInvalidInput)
	})
	t.Run("unconstructed change listing", func(t *testing.T) {
		_, err := Assess(nil, nil, Change{Resulting: qty("1"), Price: price("100")}, one, usd)
		assert.ErrorIs(t, err, ErrInvalidInput)
	})
	t.Run("currency mismatch on a closing change", func(t *testing.T) {
		_, err := Assess(nil, nil, Change{Listing: jpySettled(t), Resulting: qty("0"), Price: price("150")}, one, usd)
		assert.ErrorIs(t, err, ErrCurrencyMismatch)
	})
	t.Run("currency mismatch on an increasing change", func(t *testing.T) {
		_, err := Assess(nil, nil, Change{Listing: jpySettled(t), Resulting: qty("1"), Price: price("150")}, one, usd)
		assert.ErrorIs(t, err, ErrCurrencyMismatch)
	})
	t.Run("currency mismatch on the current position", func(t *testing.T) {
		jpy := jpySettled(t)
		positions := []runtimeorder.Position{position(t, jpy, order.Long, "1", "150")}
		_, err := Assess(positions, nil, Change{Listing: jpy, Resulting: qty("0"), Price: price("150")}, one, usd)
		assert.ErrorIs(t, err, ErrCurrencyMismatch)
	})
	t.Run("zero-value policy", func(t *testing.T) {
		_, err := Assess(nil, nil, Change{Listing: spy, Resulting: qty("1"), Price: price("100")}, Ratio{}, usd)
		assert.ErrorIs(t, err, ErrInvalidPolicy)
	})
	t.Run("nil policy", func(t *testing.T) {
		_, err := Assess(nil, nil, Change{Listing: spy, Resulting: qty("1"), Price: price("100")}, nil, usd)
		assert.ErrorIs(t, err, ErrInvalidPolicy)
	})
}

func TestAssessment_IncreasesCurrencyMismatch(t *testing.T) {
	eur := num.MustParseMoney("1", num.MustParseCurrency("EUR"))
	_, err := Assessment{Current: Requirement{Required: money("1")}, Prospective: Requirement{Required: eur}}.Increases()
	assert.Error(t, err)
}

func TestPolicyValidatedUpFront(t *testing.T) {
	spy := equity(t, "SPY")
	t.Run("flat account with zero-value Ratio", func(t *testing.T) {
		_, err := Account(nil, nil, Ratio{}, usd)
		assert.ErrorIs(t, err, ErrInvalidPolicy)
	})
	t.Run("closing change with zero-value Ratio", func(t *testing.T) {
		_, err := Assess(nil, nil, Change{Listing: spy, Resulting: qty("0"), Price: price("100")}, Ratio{}, usd)
		assert.ErrorIs(t, err, ErrInvalidPolicy)
	})
	t.Run("constructed Ratio validates", func(t *testing.T) {
		assert.NoError(t, ratio(t, "0.5").Validate())
	})
}

func TestIncreases_ComparesGrossNotRoundedMargin(t *testing.T) {
	// At ratio 0.1, growing gross from 1.00000000 to 1.00000001 leaves
	// required margin at 0.10000000 after rounding. The smallest
	// representable increase in gross must still count as an increase.
	spy := equity(t, "SPY")
	positions := []runtimeorder.Position{position(t, spy, order.Long, "1", "1")}
	a, err := Assess(positions, nil, Change{Listing: spy, Resulting: qty("1.00000001"), Price: price("1")}, ratio(t, "0.1"), usd)
	require.NoError(t, err)
	require.True(t, a.Prospective.Required.Equal(a.Current.Required), "required margin rounds to the same value")
	up, err := a.Increases()
	require.NoError(t, err)
	assert.True(t, up)
}

func TestListingLevelIdentity(t *testing.T) {
	inst, err := instrument.NewEquity("ARCX", "SPY")
	require.NoError(t, err)
	arca := listingAt(t, inst, "SPY", "ARCA", "1", usd)
	bats := listingAt(t, inst, "SPY", "BATS", "1", usd)
	require.True(t, arca.InstrumentID().Equal(bats.InstrumentID()), "same instrument")
	one := ratio(t, "1")

	t.Run("each listing uses its own mark", func(t *testing.T) {
		positions := []runtimeorder.Position{
			position(t, arca, order.Long, "10", "100"),
			position(t, bats, order.Long, "10", "100"),
		}
		marks := Marks{account.KeyOf(arca): price("100"), account.KeyOf(bats): price("101")}
		req, err := Account(positions, marks, one, usd)
		require.NoError(t, err)
		assertMoney(t, "2010", req.Gross)
	})
	t.Run("a mark for one listing does not value another", func(t *testing.T) {
		positions := []runtimeorder.Position{position(t, bats, order.Long, "10", "100")}
		_, err := Account(positions, Marks{account.KeyOf(arca): price("100")}, one, usd)
		assert.ErrorIs(t, err, ErrMissingMark)
	})
	t.Run("two listings of one instrument are not duplicates", func(t *testing.T) {
		positions := []runtimeorder.Position{
			position(t, arca, order.Long, "10", "100"),
			position(t, bats, order.Long, "10", "100"),
		}
		marks := Marks{account.KeyOf(bats): price("101")}
		a, err := Assess(positions, marks, Change{Listing: arca, Resulting: qty("20"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "2010", a.Current.Gross)     // 10×100 + 10×101
		assertMoney(t, "3010", a.Prospective.Gross) // 20×100 + 10×101
	})
	t.Run("a change in another listing does not match the existing position", func(t *testing.T) {
		// Open in ARCA; the change is a fresh position in BATS, so the
		// ARCA position stays at its mark and needs one.
		positions := []runtimeorder.Position{position(t, arca, order.Long, "10", "100")}
		_, err := Assess(positions, Marks{}, Change{Listing: bats, Resulting: qty("5"), Price: price("100")}, one, usd)
		assert.ErrorIs(t, err, ErrMissingMark)

		a, err := Assess(positions, Marks{account.KeyOf(arca): price("100")}, Change{Listing: bats, Resulting: qty("5"), Price: price("100")}, one, usd)
		require.NoError(t, err)
		assertMoney(t, "1000", a.Current.Gross)
		assertMoney(t, "1500", a.Prospective.Gross)
	})
}
