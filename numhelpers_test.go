package trader

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test-only names for the num types, so fixtures read as they did when the
// engine had its own types. Production code uses num.Price, num.Money and
// num.Rate directly.
type (
	price = num.Price
	money = num.Money
	rate  = num.Rate
)

// Test-only constructors that keep fixtures in the units they were written in
// (price points, cents, millionths) while the engine itself holds only num
// values. They go through exact decimal text, never a float.

// px is a price given in points, the five-decimal resolution: px(110323) is
// 1.10323.
func px(points int64) num.Price {
	return num.MustParsePrice(fmt.Sprintf("%d.%05d", points/100000, points%100000))
}

// usd is whole dollars as money.
func usd(dollars int64) num.Money {
	return num.MustParseMoney(strconv.FormatInt(dollars, 10), usdCurrency)
}

// cents is an amount of USD given in cents; it may be negative.
func cents(n int64) num.Money {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	return num.MustParseMoney(fmt.Sprintf("%s%d.%02d", sign, n/100, n%100), usdCurrency)
}

// qty is a whole number of units.
func qty(n int64) num.Quantity { return num.MustParseQuantity(strconv.FormatInt(n, 10)) }

// rt is a rate given in millionths: rt(10_000) is 1%.
func rt(micro int64) num.Rate {
	sign := ""
	if micro < 0 {
		sign, micro = "-", -micro
	}
	return num.MustParseRate(fmt.Sprintf("%s%d.%06d", sign, micro/1_000_000, micro%1_000_000))
}

// reservedOf is the margin the broker holds for the account's pending opens.
func reservedOf(t *testing.T, br *broker, accountID string) num.Money {
	t.Helper()
	act := br.accounts[accountID]
	if act == nil {
		act = &account{id: accountID, currency: usdCurrency}
	}
	m, err := br.reservedMargin(act)
	require.NoError(t, err)
	return m
}

// whole is q as a whole number of units; q must be whole.
func whole(q num.Quantity) int64 {
	n, err := strconv.ParseInt(q.String(), 10, 64)
	if err != nil {
		panic(fmt.Sprintf("quantity %s is not whole: %v", q, err))
	}
	return n
}

// pts is points, for arithmetic on prices in the units fixtures were written in.
func pts(p num.Price) int64 { return points(p) }

// points is p in the five-decimal points px takes; p must be at that resolution.
func points(p num.Price) int64 {
	whole, frac, _ := strings.Cut(p.String(), ".")
	n, err := strconv.ParseInt(whole+(frac + "00000")[:5], 10, 64)
	if err != nil {
		panic(fmt.Sprintf("price %s: %v", p, err))
	}
	return n
}

func madd(a, b num.Money) num.Money {
	m, err := a.Add(b)
	if err != nil {
		panic(err)
	}
	return m
}

func msub(a, b num.Money) num.Money {
	m, err := a.Sub(b)
	if err != nil {
		panic(err)
	}
	return m
}

func mneg(a num.Money) num.Money {
	m, err := a.Neg()
	if err != nil {
		panic(err)
	}
	return m
}

// must1 returns v, panicking on err: for fixtures that cannot fail.
func must1[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// presentFloor is m floored to whole cents as money: how the engine prints it
// (ADR-086).
func presentFloor(m num.Money) num.Money {
	c, err := floorCents(m)
	if err != nil {
		panic(err)
	}
	return cents(c)
}

// presentCeil is m rounded up to whole cents. The approved EMAX margins were
// rounded up to the cent by the prototype; the engine now keeps margin exact, so
// the tests compare it at the old resolution.
func presentCeil(m num.Money) num.Money {
	c, err := floorCents(mneg(m))
	if err != nil {
		panic(err)
	}
	return cents(-c)
}

// presentTrades is trades with each P/L floored to cents.
func presentTrades(trades []trade) []trade {
	out := make([]trade, len(trades))
	for i, tr := range trades {
		tr.pnl = presentFloor(tr.pnl)
		out[i] = tr
	}
	return out
}

// sumPnL is the exact sum of the trades' P/L.
func sumPnL(trades []trade) num.Money {
	total := zeroMoney(usdCurrency)
	for _, tr := range trades {
		total = madd(total, tr.pnl)
	}
	return total
}

// priceLess and priceGreater assert an ordering of Prices, which testify's
// Less and Greater cannot compare.
func priceLess(t *testing.T, a, b num.Price, msgAndArgs ...any) {
	t.Helper()
	assert.Negative(t, a.Cmp(b), msgAndArgs...)
}

func priceGreater(t *testing.T, a, b num.Price, msgAndArgs ...any) {
	t.Helper()
	assert.Positive(t, a.Cmp(b), msgAndArgs...)
}

// unsetIfZero is the zero Money for an amount of zero: expected states leave a
// zero field unset.
func unsetIfZero(m num.Money) num.Money {
	if m.IsZero() {
		return num.Money{}
	}
	return m
}
