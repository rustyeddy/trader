package trader

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccount_MarginAvailable_NoPositionsIsTheBalance(t *testing.T) {
	tests := []struct {
		name    string
		balance money
		want    money
	}{
		{"positive", usd(10000), usd(10000)},
		{"zero", cents(0), cents(0)},
		{"negative", mneg(usd(50)), mneg(usd(50))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, must1(newAccount("a", tt.balance).marginAvailable()))
		})
	}
}

// registerInstrument adds a test instrument to the registry for one test.
func registerInstrument(t *testing.T, inst *instrument) {
	t.Helper()
	was, ok := instruments[inst.symbol]
	instruments[inst.symbol] = inst
	t.Cleanup(func() {
		if ok {
			instruments[inst.symbol] = was
		} else {
			delete(instruments, inst.symbol)
		}
	})
}

// testInstrument is EURUSD-like with overridable limits.
func testInstrument(symbol string) *instrument {
	return &instrument{
		symbol: symbol, quote: usdCurrency, marginRate: rateTwoPercent, minUnits: oneUnit, unitStep: oneUnit,
	}
}

// eurusd1_10 is the price 1.10000: the default 1% stop is 1100 points away.
var eurusd1_10 = px(110000)

// openOrderAt is an open order with the stop a strategy would give it: 1% of
// the price away on the losing side, at least one point.
// fillOn applies a fill at the price and time already on the order.
func fillOn(a *account, o *order) error { return a.applyFill(o, o.fillPrice, o.Time) }

func openOrderAt(sym string, d direction, p num.Price) *order {
	pts := points(p)
	dist := max(pts/100, 1)  // 1% of the price, in points
	stop := max(pts-dist, 0) // a Price is never negative
	if d == short {
		stop = pts + dist
	}
	return &order{Time: testStart, instrument: sym, direction: d, action: open, orderType: marketOrder, price: p, stopLoss: px(stop)}
}

func TestAccount_Size_Open(t *testing.T) {
	captureLogs(t)

	// units = floor(balance * riskPct * 100_000 / (1_000_000 * stopDistance))
	//       = floor(balance / 1.1)   for a 1% risk and a 1% stop at 1.10000.
	tests := []struct {
		name      string
		balance   money
		riskPct   rate
		wantUnits int64
	}{
		{"10000 at 1% risk rounds down, not to nearest", usd(10000), rateOnePercent, 9090}, // 9090.9
		{"1000", usd(1000), rateOnePercent, 909},                                           // 909.09
		{"exact division is not lost to rounding", usd(11), rateOnePercent, 10},            // 11/1.1
		{"2% risk doubles the budget", usd(10000), rateTwoPercent, 18181},                  // 18181.8
		{"the budget is exact to the cent", cents(1100), rateOnePercent, 10},               // $11.00
	}
	for _, tt := range tests {
		for _, d := range []direction{long, short} {
			t.Run(fmt.Sprintf("%s/%s", tt.name, d), func(t *testing.T) {
				act := newAccount("a", tt.balance)
				act.riskPct = tt.riskPct
				o := openOrderAt("EURUSD", d, eurusd1_10)

				require.NoError(t, act.size(o, cents(0)))

				assert.Equal(t, qty(tt.wantUnits), o.units)
				assert.Equal(t, stateSized, o.state)
				require.Len(t, o.history, 1)
				assert.Equal(t, stateSized, o.history[0].state)
			})
		}
	}
}

func TestAccount_Size_Open_NeverChangesTheStrategysStop(t *testing.T) {
	captureLogs(t)
	l := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(l, cents(0)))
	assert.Equal(t, px(108900), l.stopLoss)

	s := openOrderAt("EURUSD", short, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(s, cents(0)))
	assert.Equal(t, px(111100), s.stopLoss)
}

func TestAccount_Size_Open_UsesAStrategyStop(t *testing.T) {
	captureLogs(t)

	// A 2% stop (2200 points) halves the units compared with the default 1%.
	l := openOrderAt("EURUSD", long, eurusd1_10)
	l.stopLoss = px(107800)
	require.NoError(t, newAccount("a", usd(10000)).size(l, cents(0)))
	assert.Equal(t, qty(4545), l.units) // 4545.45
	assert.Equal(t, px(107800), l.stopLoss, "the strategy's stop is kept")

	s := openOrderAt("EURUSD", short, eurusd1_10)
	s.stopLoss = px(112200)
	require.NoError(t, newAccount("a", usd(10000)).size(s, cents(0)))
	assert.Equal(t, qty(4545), s.units)
	assert.Equal(t, px(112200), s.stopLoss)
}

func TestAccount_Size_Open_WithoutMarginIsRejected(t *testing.T) {
	captureLogs(t)
	for _, bal := range []money{cents(0), mneg(usd(10))} {
		o := openOrderAt("EURUSD", long, eurusd1_10)
		err := newAccount("a", bal).size(o, cents(0))

		require.ErrorIs(t, err, errInsufficientMargin)
		assert.True(t, isRejection(err))
		assert.Zero(t, o.units)
		assert.Equal(t, stateRejected, o.state)
		require.Len(t, o.history, 1)
		assert.Contains(t, o.history[0].reason, "insufficient margin")
	}
}

func requiredMargin(units int64, p price, mr rate) *big.Rat {
	r := new(big.Rat).SetInt64(units)
	r.Mul(r, big.NewRat(pts(p), 100_000))
	return r.Mul(r, ratOfText(mr.String()))
}

// ratOfText is the exact value of a canonical decimal text.
func ratOfText(text string) *big.Rat {
	r, ok := new(big.Rat).SetString(text)
	if !ok {
		panic("not a decimal: " + text)
	}
	return r
}

// ratOf is the exact value of m.
func ratOf(m money) *big.Rat {
	text, _, _ := strings.Cut(m.String(), " ")
	return ratOfText(text)
}
func TestAccount_Size_Open_MarginShortfallReducesUnitsInsteadOfRejecting(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTMR")
	inst.marginRate = rt(2_000_000) // 200%: every unit needs twice its value
	registerInstrument(t, inst)

	// The risk budget alone would allow 9090 units, but 9090 units at 1.10
	// need 19998 of margin against 10000 available. The margin covers 4545.
	o := openOrderAt("TESTMR", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, cents(0)))

	assert.Equal(t, qty(4545), o.units, "floor(10000 / (1.10 * 2))")
	assert.Equal(t, stateSized, o.state)
	assert.LessOrEqual(t, requiredMargin(whole(o.units), eurusd1_10, inst.marginRate).Cmp(ratOf(usd(10000))), 0)
	assert.Positive(t, requiredMargin(whole(o.units)+1, eurusd1_10, inst.marginRate).Cmp(ratOf(usd(10000))),
		"one more unit would not be covered: the size is the largest the margin allows")
}

func TestAccount_Size_Open_MarginLimitsAGenerousRiskBudget(t *testing.T) {
	captureLogs(t)
	// 100% risk would size 909090 units; at 50:1 the margin covers 454545.
	a := newAccount("a", usd(10000))
	a.riskPct = rt(1_000_000)
	o := openOrderAt("EURUSD", long, eurusd1_10)

	require.NoError(t, a.size(o, cents(0)))

	assert.Equal(t, qty(454545), o.units)
	mr := instruments["EURUSD"].marginRate
	assert.LessOrEqual(t, requiredMargin(454545, eurusd1_10, mr).Cmp(ratOf(usd(10000))), 0)
	assert.Positive(t, requiredMargin(454546, eurusd1_10, mr).Cmp(ratOf(usd(10000))))
}

func TestAccount_Size_Open_RiskLimitWinsWhenItIsSmaller(t *testing.T) {
	captureLogs(t)
	// The default 1% risk (9090 units) is far below what 50:1 margin allows
	// (454545), so margin plays no part.
	o := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, cents(0)))
	assert.Equal(t, qty(9090), o.units)
}

func TestAccount_Size_Open_MarginUnitsRoundDown(t *testing.T) {
	captureLogs(t)
	// Cap the size at 1000 units and risk 100%, so only the cap and the margin
	// can limit the order.
	inst := testInstrument("TESTCEIL")
	inst.maxUnits = qty(1000)
	registerInstrument(t, inst)
	newAcct := func() *account {
		a := newAccount("a", usd(22))
		a.riskPct = rt(1_000_000)
		return a
	}

	// 1000 units at 1.10000 need exactly $22 of margin: all 1000 fit.
	o := openOrderAt("TESTCEIL", long, px(110000))
	require.NoError(t, newAcct().size(o, cents(0)))
	assert.Equal(t, qty(1000), o.units)

	// At 1.10001 the margin covers 999.99 units, and 1000 would need $22.0002,
	// more than the $22 available. The size is 999 (rounded down), not 1000 and not
	// a rejection.
	o = openOrderAt("TESTCEIL", long, px(110001))
	require.NoError(t, newAcct().size(o, cents(0)))
	assert.Equal(t, qty(999), o.units)
	assert.LessOrEqual(t, requiredMargin(999, px(110001), inst.marginRate).Cmp(ratOf(usd(22))), 0)
	assert.Positive(t, requiredMargin(1000, px(110001), inst.marginRate).Cmp(ratOf(usd(22))))
	assert.Equal(t, "22.0002", requiredMargin(1000, px(110001), inst.marginRate).FloatString(4), "exact, not rounded up to the cent")
}

func TestAccount_Size_Open_MarginBelowTheMinimumIsAnInsufficientMarginRejection(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTMINM")
	inst.minUnits = qty(500_000)
	registerInstrument(t, inst)

	// Margin covers 454545 units, less than the 500000 minimum, and the risk
	// budget (100%) is not what limits the order.
	a := newAccount("a", usd(10000))
	a.riskPct = rt(1_000_000)
	o := openOrderAt("TESTMINM", long, eurusd1_10)
	err := a.size(o, cents(0))

	require.ErrorIs(t, err, errInsufficientMargin)
	assert.NotErrorIs(t, err, errOrderTooSmall)
	assert.True(t, isRejection(err))
	assert.Contains(t, err.Error(), "margin covers 454545 units, minimum 500000")
	assert.Zero(t, o.units)
	assert.Equal(t, stateRejected, o.state)
}

func TestAccount_Size_Open_MarginLimitThenUnitStep(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTSTEPM")
	inst.marginRate = rt(2_000_000)
	inst.unitStep = qty(100)
	registerInstrument(t, inst)

	o := openOrderAt("TESTSTEPM", long, eurusd1_10) // margin covers 4545
	require.NoError(t, newAccount("a", usd(10000)).size(o, cents(0)))
	assert.Equal(t, qty(4500), o.units, "rounded down to the step after the margin limit")
}

func TestAccount_Size_Open_InstrumentWithoutAMarginRateIsAConfigurationError(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTNOMR")
	inst.marginRate = rt(0)
	registerInstrument(t, inst)

	o := openOrderAt("TESTNOMR", long, eurusd1_10)
	err := newAccount("a", usd(10000)).size(o, cents(0))

	require.ErrorIs(t, err, errInstrumentConfig)
	assert.False(t, isRejection(err), "a missing margin rate is a setup bug, not a trading outcome")
	assert.Zero(t, o.units)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.history)
}

func TestAccount_Size_Open_RespectsInstrumentLimits(t *testing.T) {
	captureLogs(t)

	step := testInstrument("TESTSTEP")
	step.unitStep = qty(100)
	registerInstrument(t, step)
	o := openOrderAt("TESTSTEP", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, cents(0)))
	assert.Equal(t, qty(9000), o.units, "9090 rounded down to the 100 step")

	capped := testInstrument("TESTMAX")
	capped.maxUnits = qty(5000)
	registerInstrument(t, capped)
	o = openOrderAt("TESTMAX", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, cents(0)))
	assert.Equal(t, qty(5000), o.units, "capped at the maximum")

	both := testInstrument("TESTBOTH")
	both.maxUnits = qty(5000)
	both.unitStep = qty(300)
	registerInstrument(t, both)
	o = openOrderAt("TESTBOTH", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, cents(0)))
	assert.Equal(t, qty(4800), o.units, "the cap is also rounded down to the step")
}

func TestAccount_Size_Open_BelowTheMinimumIsRejected(t *testing.T) {
	captureLogs(t)

	t.Run("budget buys less than one unit", func(t *testing.T) {
		o := openOrderAt("EURUSD", long, eurusd1_10)
		err := newAccount("a", usd(1)).size(o, cents(0)) // 1/1.1 = 0.9 units

		require.ErrorIs(t, err, errOrderTooSmall)
		assert.True(t, isRejection(err))
		assert.Zero(t, o.units)
		assert.Equal(t, stateRejected, o.state)
		assert.Contains(t, o.history[0].reason, "0 units, minimum 1")
	})
	t.Run("below the instrument minimum", func(t *testing.T) {
		inst := testInstrument("TESTMIN")
		inst.minUnits = qty(10000)
		registerInstrument(t, inst)

		o := openOrderAt("TESTMIN", long, eurusd1_10)
		err := newAccount("a", usd(10000)).size(o, cents(0)) // 9090 < 10000

		require.ErrorIs(t, err, errOrderTooSmall)
		assert.Contains(t, err.Error(), "9090 units, minimum 10000")
	})
	t.Run("zero risk budget", func(t *testing.T) {
		a := newAccount("a", usd(10000))
		a.riskPct = rt(0)
		require.ErrorIs(t, a.size(openOrderAt("EURUSD", long, eurusd1_10), cents(0)), errOrderTooSmall)
	})
}

func TestAccount_Size_Open_RiskIsBoundedAndTight(t *testing.T) {
	captureLogs(t)
	balances := []money{cents(1), usd(1), usd(11), usd(100), usd(999), usd(10000), usd(123456), usd(10_000_000_000)}
	prices := []price{px(10), px(50000), px(110000), px(250000), px(12_345_678)}
	// 1% and 2% keep risk the limit; 50% and 100% make margin the limit.
	risks := []rate{rateOnePercent, rateTwoPercent, rateHalf, rateOne}
	// The budgets are each one half-even rounding at 1e-8 of the account
	// currency (ADR-087), so a size is safe within that tolerance.
	tolerance := big.NewRat(5, 1_000_000_000)

	for _, bal := range balances {
		for _, p := range prices {
			for _, d := range []direction{long, short} {
				for _, rk := range risks {
					o := openOrderAt("EURUSD", d, p)
					act := newAccount("a", bal)
					act.riskPct = rk
					err := act.size(o, cents(0))
					msg := fmt.Sprintf("bal %s price %s %s risk %s", bal, p, d, rk)

					if err != nil {
						// Only a rejection is acceptable here: the order is below the
						// minimum, or the margin cannot cover even the minimum, or every
						// limit is too many units for a Quantity (a price of a few points).
						require.True(t, errors.Is(err, errOrderTooSmall) || errors.Is(err, errInsufficientMargin) || errors.Is(err, num.ErrOverflow), "%s: %v", msg, err)
						continue
					}

					// Everything below is exact rational arithmetic, independent of num.
					dist := big.NewRat(abs64(pts(p)-pts(o.stopLoss)), 100_000)
					budget := new(big.Rat).Mul(ratOf(bal), ratOfText(rk.String()))
					perUnitMargin := requiredMargin(1, p, instruments["EURUSD"].marginRate)
					perUnitNotional := big.NewRat(pts(p), 100_000)
					units := whole(o.units)
					loss := func(n int64) *big.Rat { return new(big.Rat).Mul(big.NewRat(n, 1), dist) }
					margin := func(n int64) *big.Rat { return new(big.Rat).Mul(big.NewRat(n, 1), perUnitMargin) }
					notional := func(n int64) *big.Rat { return new(big.Rat).Mul(big.NewRat(n, 1), perUnitNotional) }
					within := func(v, limit *big.Rat) bool { return v.Cmp(new(big.Rat).Add(limit, tolerance)) <= 0 }
					notionalLimit := ratOfText(defaultMaxOrderNotional)

					assert.True(t, within(loss(units), budget), "risk exceeded: "+msg)
					assert.True(t, within(margin(units), ratOf(bal)), "margin exceeded: "+msg)
					assert.True(t, within(notional(units), notionalLimit), "notional exceeded: "+msg)

					// The size is the largest allowed, up to that tolerance: one more
					// unit would break the risk budget, the margin or the notional
					// limit by more than the tolerance.
					riskBroken := !within(loss(units+1), budget)
					marginBroken := !within(margin(units+1), ratOf(bal))
					notionalBroken := !within(notional(units+1), notionalLimit)
					assert.True(t, riskBroken || marginBroken || notionalBroken, "not the largest size: "+msg)
				}
			}
		}
	}
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
func TestAccount_Size_CloseIsTheWholePositionAndNeedsNoMargin(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, instrument: "EURUSD", action: close, orderType: marketOrder}

	require.NoError(t, newAccount("a", cents(0)).size(o, cents(0)))

	assert.True(t, o.units.IsZero())
	assert.True(t, o.stopLoss.IsZero())
	assert.Equal(t, stateSized, o.state)
	assert.Len(t, o.history, 1)
}

func TestAccount_Size_AddReduceAndNoActionAreUnsupported(t *testing.T) {
	captureLogs(t)
	for _, a := range []action{add, reduce, noAction, action(99)} {
		o := &order{Time: testStart, instrument: "EURUSD", action: a}
		require.ErrorIs(t, newAccount("a", usd(1000)).size(o, cents(0)), errUnsupportedAction, a.String())
		assert.Equal(t, stateRequested, o.state)
		assert.Empty(t, o.history, "an unsupported action changes nothing")
	}
}

func TestMoneyMath_StaysExactAndChecked(t *testing.T) {
	// A move from zero to the largest price on 5,000,000 units is $50 billion:
	// inside num's range, and exact.
	p := &position{direction: long, units: qty(5_000_000), entryPrice: px(0)}
	got, err := p.pnlAt(maxPrice, usdCurrency)
	require.NoError(t, err)
	assert.Equal(t, usd(50_000_000_000), got)

	p.entryPrice = maxPrice
	got, err = p.pnlAt(px(0), usdCurrency)
	require.NoError(t, err)
	assert.Equal(t, mneg(usd(50_000_000_000)), got)

	// A thousand times the units is $50 trillion: past the range, an error and
	// never a wrapped or saturated amount.
	p.units = qty(5_000_000_000)
	_, err = p.pnlAt(px(0), usdCurrency)
	require.ErrorIs(t, err, num.ErrOverflow)

	_, err = marginFor("EURUSD", qty(5_000_000_000), maxPrice, usdCurrency)
	require.ErrorIs(t, err, num.ErrOverflow)
}
func TestAccount_Size_Open_CapsUnitsAtTheOrderNotional(t *testing.T) {
	// A price of 1.00000 and a 1-point stop: the $1B account's risk budget alone
	// is 1e12 units, more than a Quantity holds, so that limit does not bind and
	// the $50 million default notional limit decides: 50,000,000 units.
	o := openOrderAt("EURUSD", long, px(100000))
	o.stopLoss = px(99999)
	a := newAccount("a", usd(1_000_000_000))

	require.NoError(t, a.size(o, cents(0)))

	assert.Equal(t, qty(50_000_000), o.units)

	// The limit is the account's policy: configurable, and in its currency.
	a.maxOrderNotional = usd(1000)
	o = openOrderAt("EURUSD", long, px(100000))
	o.stopLoss = px(99999)
	require.NoError(t, a.size(o, cents(0)))
	assert.Equal(t, qty(1000), o.units)
}

func TestAccount_Size_Open_EverySizingLimitOverflowingIsAnErrorNotAWrap(t *testing.T) {
	// At a price of one point every limit is above the largest quantity.
	a := newAccount("a", usd(1_000_000_000))
	a.maxOrderNotional = usd(1_000_000_000)
	o := openOrderAt("EURUSD", long, px(2))
	o.stopLoss = px(1)

	err := a.size(o, cents(0))

	require.ErrorIs(t, err, num.ErrOverflow)
	assert.False(t, isRejection(err))
}

func TestAccount_Size_RejectsAnInstrumentQuotedInAnotherCurrency(t *testing.T) {
	inst := testInstrument("TESTEUR")
	inst.quote = num.MustParseCurrency("EUR")
	registerInstrument(t, inst)
	o := openOrderAt("TESTEUR", long, eurusd1_10)

	err := newAccount("a", usd(10000)).size(o, cents(0))

	require.ErrorIs(t, err, num.ErrCurrencyMismatch)
	assert.False(t, isRejection(err), "a configuration error, not a trading outcome")
	assert.True(t, o.units.IsZero())
}

// Rounding tolerance, pinned (ADR-087): the budgets are rounded half-even at
// 1e-8 of the account currency, so a size can exceed an exact budget by at most
// that. 9.99999 x 0.000001 = 0.00000999999 rounds up to 0.00001000, and a one
// unit order at a 0.00001 stop then plans a loss of 0.00001, just over the
// exact budget.
func TestAccount_Size_Open_RiskToleranceIsPinned(t *testing.T) {
	captureLogs(t)
	a := newAccount("a", num.MustParseMoney("9.99999", usdCurrency))
	a.riskPct = num.MustParseRate("0.000001")
	o := openOrderAt("EURUSD", long, px(100))
	o.stopLoss = px(99)

	require.NoError(t, a.size(o, cents(0)))

	assert.Equal(t, qty(1), o.units)
	exact := new(big.Rat).Mul(ratOfText("9.99999"), ratOfText("0.000001"))
	plan := big.NewRat(1, 100_000) // one unit, one point of stop
	over := new(big.Rat).Sub(plan, exact)
	assert.Positive(t, over.Sign(), "the plan exceeds the exact budget")
	assert.LessOrEqual(t, over.Cmp(big.NewRat(5, 1_000_000_000)), 0, "by at most 0.5e-8")
}

// Rates have six decimals, so a per-unit cost is not exact at 1e-8:
// 0.04999 x 0.0004 = 0.000019996 would round to 0.00002 and size 50,000 units.
// Scaling the money first and dividing by the price last keeps the one
// rounding on a value of account size, and the exact 50,010 units results.
func TestAccount_Size_Open_PerUnitCostIsNotRoundedFirst(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTRATE")
	inst.marginRate = rt(400) // 0.04%
	registerInstrument(t, inst)
	a := newAccount("a", usd(1))
	a.riskPct = rateOne // only the margin can limit the order
	o := openOrderAt("TESTRATE", long, px(4999))
	o.stopLoss = px(4998)

	require.NoError(t, a.size(o, cents(0)))

	assert.Equal(t, qty(50010), o.units)
	assert.LessOrEqual(t, requiredMargin(50010, px(4999), inst.marginRate).Cmp(ratOf(usd(1))), 0)
	assert.Positive(t, requiredMargin(50011, px(4999), inst.marginRate).Cmp(ratOf(usd(1))))
}
func TestAccount_Size_UnknownInstrumentIsAnErrorNotARejection(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, instrument: "NOPE", direction: long, action: open}

	err := newAccount("a", usd(1000)).size(o, cents(0))

	require.ErrorIs(t, err, errUnknownInstrument)
	assert.NotErrorIs(t, err, errInsufficientMargin)
	assert.Zero(t, o.units)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.history, "a bad instrument is a setup error, not a recorded rejection")
}

func TestNewAccount_PositionsMapIsReady(t *testing.T) {
	a := newAccount("a", usd(100))
	require.NotNil(t, a.positions)
	assert.NotPanics(t, func() { a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(1)} })
}

func TestNewAccount_DefaultRiskIsOnePercent(t *testing.T) {
	assert.Equal(t, rateOnePercent, newAccount("a", usd(100)).riskPct)
}

// Risk is measured against equity and the margin limit against the margin
// available. They are separate questions: equity moves with unrealized P/L,
// the margin available is equity less the margin in use.
func TestAccount_MarginAvailableIsEquityLessUsedMargin(t *testing.T) {
	a := newAccount("a", usd(12345))
	// 10000 units marked at 1.11000 need 10000 * 1.11 * 2% = $222 of margin and
	// are $100 in profit.
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(10000), entryPrice: px(110000), markPrice: px(111000)}

	eq := must1(a.equity())
	used := must1(a.usedMargin())
	avail := must1(a.marginAvailable())

	assert.Equal(t, usd(12345+100), eq)
	assert.Equal(t, usd(222), used)
	assert.Equal(t, usd(12345+100-222), avail)
}

func TestAccount_UsedMargin(t *testing.T) {
	registerInstrument(t, testInstrument("GBPUSD"))
	t.Run("none", func(t *testing.T) {
		assert.True(t, must1(newAccount("a", usd(1)).usedMargin()).IsZero())
	})
	t.Run("sums every position at its mark, long or short", func(t *testing.T) {
		a := newAccount("a", usd(1))
		a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(10000), markPrice: px(111000)} // $222
		a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: short, units: qty(5000), markPrice: px(150000)} // $150
		used := must1(a.usedMargin())
		assert.Equal(t, usd(372), used)
	})
	t.Run("is exact, not rounded to the cent", func(t *testing.T) {
		a := newAccount("a", usd(1))
		a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(1), markPrice: px(110000)} // 2.2 cents
		used := must1(a.usedMargin())
		assert.Equal(t, num.MustParseMoney("0.022", usdCurrency), used)
		assert.Equal(t, cents(2), presentFloor(used))
	})
	t.Run("an unknown instrument is a bug, not a zero", func(t *testing.T) {
		a := newAccount("a", usd(1))
		a.positions["XXXYYY"] = &position{instrument: "XXXYYY", units: qty(1), markPrice: px(1)}
		assert.Panics(t, func() { must1(a.usedMargin()) })
	})
}

func TestAccount_Size_Open_UsedMarginReducesTheMarginLimit(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("TESTUM"))
	newAcct := func() *account {
		a := newAccount("a", usd(10000))
		a.riskPct = rt(1_000_000) // only the margin can limit the order
		return a
	}

	free := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAcct().size(free, cents(0)))
	assert.Equal(t, qty(454545), free.units, "floor($10000 / (1.10 * 2%))")

	// 100000 units at 1.00 hold $2000 of margin, leaving $8000.
	a := newAcct()
	a.positions["TESTUM"] = &position{instrument: "TESTUM", direction: long, units: qty(100000), entryPrice: px(100000), markPrice: px(100000)}
	held := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, a.size(held, cents(0)))
	assert.Equal(t, qty(363636), held.units, "floor($8000 / (1.10 * 2%))")

	// Margin the broker reserves for submitted opens that have not filled
	// counts the same way.
	reserved := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAcct().size(reserved, usd(2000)))
	assert.Equal(t, qty(363636), reserved.units)

	// Reserving all of it leaves nothing.
	none := openOrderAt("EURUSD", long, eurusd1_10)
	require.ErrorIs(t, newAcct().size(none, usd(10000)), errInsufficientMargin)
}

func TestAccount_Size_Open_AllMarginInUseRejects(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("TESTUM"))
	// 500000 units at 1.00 need $10000: all of the equity.
	a := newAccount("a", usd(10000))
	a.positions["TESTUM"] = &position{instrument: "TESTUM", direction: long, units: qty(500000), entryPrice: px(100000), markPrice: px(100000)}
	o := openOrderAt("EURUSD", long, eurusd1_10)

	err := a.size(o, cents(0))

	require.ErrorIs(t, err, errInsufficientMargin)
	assert.Equal(t, stateRejected, o.state)
	assert.Zero(t, o.units)
}

func TestAccount_EquityWithoutPositionsIsTheBalance(t *testing.T) {
	assert.Equal(t, usd(12345), must1(newAccount("a", usd(12345)).equity()))
}

func TestPosition_UnrealizedPnL(t *testing.T) {
	tests := []struct {
		name string
		dir  direction
		mark price
		want money
	}{
		{"long up", long, px(111000), usd(100)},
		{"long down", long, px(109000), mneg(usd(100))},
		{"short up", short, px(111000), mneg(usd(100))},
		{"short down", short, px(109000), usd(100)},
		{"marked at entry", long, px(110000), cents(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &position{direction: tt.dir, units: qty(10000), entryPrice: px(110000), markPrice: tt.mark}

			got := must1(p.unrealizedPnL(usdCurrency))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAccount_UnrealizedPnL_SumsEveryPosition(t *testing.T) {
	a := newAccount("a", usd(1000))
	require.True(t, must1(a.unrealizedPnL()).IsZero(), "no positions")

	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(10000), entryPrice: px(110000), markPrice: px(111000)} // +100
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: short, units: qty(5000), entryPrice: px(150000), markPrice: px(151000)} // -50
	a.positions["AUDUSD"] = &position{instrument: "AUDUSD", direction: long, units: qty(1000), entryPrice: px(70000), markPrice: px(69000)}    // -10

	assert.Equal(t, usd(40), must1(a.unrealizedPnL()))
}

func TestAccount_Size_Open_RiskIsMeasuredAgainstEquity(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("GBPUSD"))
	// $1000 balance buys floor(1000/1.1)=909 units; another position's
	// unrealized +$100 lifts equity to $1100 and the size to floor(1100/1.1)=1000.
	a := newAccount("a", usd(1000))
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: qty(10000), entryPrice: px(150000), markPrice: px(151000)}
	o := openOrderAt("EURUSD", long, eurusd1_10)

	require.NoError(t, a.size(o, cents(0)))

	assert.Equal(t, qty(1000), o.units)
}

func TestAccount_Size_Open_UnrealizedLossShrinksTheRiskBudget(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("GBPUSD"))
	a := newAccount("a", usd(1000))
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: qty(10000), entryPrice: px(150000), markPrice: px(149000)}
	o := openOrderAt("EURUSD", long, eurusd1_10)

	require.NoError(t, a.size(o, cents(0)))

	assert.Equal(t, qty(818), o.units, "floor(900 / 1.1)")
}

func TestAccount_Mark_ValuesThePositionAtTheBarClose(t *testing.T) {
	act := newAccount("a", usd(1000))
	pos := &position{instrument: "EURUSD", direction: long, units: qty(1), entryPrice: px(110000), markPrice: px(110000)}
	act.positions["EURUSD"] = pos

	act.mark(fillBar(1, 111500)) // closes at 111600
	assert.Equal(t, px(111600), pos.markPrice)

	other := fillBar(2, 90000)
	other.instrument = "GBPUSD"
	act.mark(other)
	assert.Equal(t, px(111600), pos.markPrice, "another instrument's bar does not move the mark")

	act.mark(bar{instrument: "AUDUSD"}) // no position: nothing to value
}

func TestAccount_ApplyFill_OpenIsMarkedAtTheFillPrice(t *testing.T) {
	act := newAccount("a", usd(1000))

	require.NoError(t, fillOn(act, &order{instrument: "EURUSD", direction: short, action: open, units: qty(5), fillPrice: px(110000)}))

	assert.Equal(t, px(110000), act.positions["EURUSD"].markPrice)
	assert.True(t, must1(act.unrealizedPnL()).IsZero(), "no profit or loss the moment it opens")
}

func TestPosition_RealizedPnL(t *testing.T) {
	// pnl = units * (exit - entry), sign flipped for shorts: exact, with no
	// rounding at all. A price point is 0.00001, so 1 unit moving 1 point is
	// $0.00001. Cents appear only when it is printed (ADR-086).
	tests := []struct {
		name  string
		dir   direction
		units int64
		exit  price
		want  string
		cents int64 // as printed: floored toward negative infinity
	}{
		{"long win", long, 10000, px(111000), "100", 10000},
		{"long loss", long, 10000, px(109000), "-100", -10000},
		{"short win", short, 10000, px(109000), "100", 10000},
		{"short loss", short, 10000, px(111000), "-100", -10000},
		{"flat", long, 10000, px(110000), "0", 0},
		{"pennies are kept", long, 15, px(111000), "0.15", 15},
		{"penny loss", long, 15, px(109000), "-0.15", -15},
		{"a win under a cent is kept exactly", long, 1, px(110001), "0.00001", 0},
		{"a loss under a cent is kept exactly and prints away from zero", long, 1, px(109999), "-0.00001", -1},
		{"a short win under a cent", short, 1, px(109999), "0.00001", 0},
		{"a short loss under a cent", short, 1, px(110001), "-0.00001", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &position{instrument: "EURUSD", direction: tt.dir, units: qty(tt.units), entryPrice: px(110000)}

			got, err := p.pnlAt(tt.exit, usdCurrency)

			require.NoError(t, err)
			assert.Equal(t, num.MustParseMoney(tt.want, usdCurrency), got)
			assert.Equal(t, cents(tt.cents), presentFloor(got))
		})
	}
}
func TestAccount_ApplyFill_CloseRealizesPnLIntoTheBalanceAndRemovesThePosition(t *testing.T) {
	tests := []struct {
		name string
		dir  direction
		exit price
		want money
	}{
		{"long win", long, px(111000), usd(10100)},
		{"long loss", long, px(109000), usd(9900)},
		{"short win", short, px(109000), usd(10100)},
		{"short loss", short, px(111000), usd(9900)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act := newAccount("a", usd(10000))
			act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: tt.dir, units: qty(10000), entryPrice: px(110000)}
			cl := &order{instrument: "EURUSD", action: close, fillPrice: tt.exit}

			require.NoError(t, fillOn(act, cl))

			assert.Equal(t, tt.want, act.balance)
			assert.Equal(t, qty(10000), cl.units, "the order records what was closed")
			assert.Empty(t, act.positions)
		})
	}
}

func TestAccount_ApplyFill_OpenAndRejectedCloseLeaveTheBalanceAlone(t *testing.T) {
	act := newAccount("a", usd(10000))

	require.NoError(t, fillOn(act, &order{instrument: "EURUSD", direction: long, action: open, units: qty(5), fillPrice: px(110000)}))
	require.ErrorIs(t, fillOn(act, &order{instrument: "GBPUSD", action: close, fillPrice: px(110000)}), errNoPosition)

	assert.Equal(t, usd(10000), act.balance)
}

// A fill is all or nothing: whatever the reason it fails, the balance, the
// positions and the order's units are exactly as they were.
func TestAccount_ApplyFill_FailureChangesNothing(t *testing.T) {
	existing := func() *position {
		return &position{instrument: "EURUSD", direction: long, units: qty(10), entryPrice: px(110000), markPrice: px(110000)}
	}
	tests := []struct {
		name    string
		balance money
		pos     *position // existing EURUSD position, nil for none
		o       *order
		wantErr error
	}{
		{"open over a position", usd(10), existing(),
			&order{instrument: "EURUSD", direction: short, action: open, units: qty(5), fillPrice: px(111000)}, errPositionExists},
		{"close with no position", usd(10), nil,
			&order{instrument: "EURUSD", action: close, fillPrice: px(111000)}, errNoPosition},
		{"unsupported action", usd(10), existing(),
			&order{instrument: "EURUSD", action: reduce, fillPrice: px(111000)}, errUnsupportedAction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAccount("a", tt.balance)
			var before position
			if tt.pos != nil {
				a.positions["EURUSD"] = tt.pos
				before = *tt.pos
			}
			unitsBefore := tt.o.units

			require.ErrorIs(t, fillOn(a, tt.o), tt.wantErr)

			assert.Equal(t, tt.balance, a.balance)
			assert.Equal(t, unitsBefore, tt.o.units)
			if tt.pos == nil {
				assert.Empty(t, a.positions)
				return
			}
			require.Contains(t, a.positions, "EURUSD")
			assert.Same(t, tt.pos, a.positions["EURUSD"])
			assert.Equal(t, before, *a.positions["EURUSD"])
		})
	}
}

func TestAccount_ApplyFill_OpenChecksMarginAtTheFillPrice(t *testing.T) {
	// 10000 units at 1.10000: margin 10000 * 1.1 * 2% = $220.
	fill := func() *order {
		return &order{instrument: "EURUSD", direction: long, action: open, units: qty(10000), fillPrice: px(110000)}
	}
	t.Run("exactly enough margin fills", func(t *testing.T) {
		a := newAccount("a", usd(220))
		require.NoError(t, fillOn(a, fill()))
		assert.Contains(t, a.positions, "EURUSD")
	})
	t.Run("one cent short is rejected and changes nothing", func(t *testing.T) {
		a := newAccount("a", msub(usd(220), cents(1)))
		err := fillOn(a, fill())
		require.ErrorIs(t, err, errInsufficientMargin)
		assert.True(t, isRejection(err))
		assert.Empty(t, a.positions)
		assert.Equal(t, msub(usd(220), cents(1)), a.balance)
	})
	t.Run("a higher fill price needs more margin", func(t *testing.T) {
		a := newAccount("a", usd(220))
		o := fill()
		o.fillPrice = px(111000) // $222.00
		require.ErrorIs(t, fillOn(a, o), errInsufficientMargin)
	})
}

func TestApplyFill_CloseRecordsTheTrade(t *testing.T) {
	entered, exited := testStart, testStart.AddDate(0, 0, 7)
	for name, tc := range map[string]struct {
		direction direction
		exit      price
		reason    orderReason
		pnl       string // exact
		printed   int64  // floored to cents
	}{
		"long profit":    {long, px(110364), reasonStrategy, "32.76364", 3276},
		"long loss":      {long, px(109999), reasonStopLoss, "-0.09001", -10},
		"short profit":   {short, px(109999), reasonStrategy, "0.09001", 9},
		"short loss":     {short, px(110001), reasonStopLoss, "-0.09001", -10},
		"margin closing": {long, px(110000), reasonMarginCloseout, "0", 0}, // break-even is still a trade
	} {
		t.Run(name, func(t *testing.T) {
			units := qty(9001)
			a := newAccount("a", usd(1000))
			a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: tc.direction, units: units,
				entryPrice: px(110000), entryTime: entered, markPrice: px(110000)}
			o := &order{instrument: "EURUSD", action: close, reason: tc.reason, fillPrice: tc.exit, Time: exited}

			require.NoError(t, fillOn(a, o))

			pnl := num.MustParseMoney(tc.pnl, usdCurrency)
			assert.Equal(t, []trade{{instrument: "EURUSD", direction: tc.direction, units: units,
				entryPrice: px(110000), entryTime: entered, exitPrice: tc.exit, exitTime: exited,
				reason: tc.reason, pnl: pnl}}, a.trades)
			assert.Equal(t, cents(tc.printed), presentFloor(pnl))
			assert.Equal(t, madd(usd(1000), pnl), a.balance)
		})
	}
}
func TestApplyFill_OpenRecordsItsEntryTimeAndNoTrade(t *testing.T) {
	a := newAccount("a", usd(10000))
	o := &order{instrument: "EURUSD", action: open, direction: long, units: qty(100), fillPrice: px(110000), Time: testStart}

	require.NoError(t, fillOn(a, o))

	assert.Equal(t, testStart, a.positions["EURUSD"].entryTime)
	assert.Empty(t, a.trades, "an open position is not a trade yet")
}

func TestApplyFill_FailedCloseRecordsNoTrade(t *testing.T) {
	a := newAccount("a", usd(10000))
	require.ErrorIs(t, fillOn(a, &order{instrument: "EURUSD", action: close, fillPrice: px(110000)}), errNoPosition)
	assert.Empty(t, a.trades)
}
