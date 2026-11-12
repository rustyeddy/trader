package trader

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceDiff_IsAlwaysNonNegative(t *testing.T) {
	d, above, err := priceDiff(px(110000), px(109000))
	require.NoError(t, err)
	assert.Equal(t, px(1000), d)
	assert.True(t, above)

	d, above, err = priceDiff(px(109000), px(110000))
	require.NoError(t, err)
	assert.Equal(t, px(1000), d)
	assert.False(t, above)

	d, above, err = priceDiff(px(5), px(5))
	require.NoError(t, err)
	assert.True(t, d.IsZero())
	assert.True(t, above, "equal counts as at or above")
}

func TestMoneySign(t *testing.T) {
	for want, m := range map[int]money{-1: mneg(usd(1)), 0: usd(0), 1: cents(1)} {
		got, err := moneySign(m)
		require.NoError(t, err)
		assert.Equal(t, want, got, m.String())
	}
	_, err := moneySign(num.Money{})
	assert.Error(t, err, "money with no currency has no sign")
}

func TestZeroMoney_IsAValidZeroInItsCurrency(t *testing.T) {
	z := zeroMoney(usdCurrency)
	assert.True(t, z.IsValid())
	assert.True(t, z.IsZero())
	assert.Equal(t, usdCurrency, z.Currency())
}

func TestFloorCents_FloorsTowardNegativeInfinity(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"33.00024", 3300},
		{"-102.08316", -10209},
		{"9897.91684", 989791},
		{"0.00999999", 0},
		{"-0.00000001", -1},
		{"-5", -500},
		{"-5.00", -500},
		{"-0.01", -1},
		{"0.5", 50},
		{"12345.6", 1234560},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := floorCents(num.MustParseMoney(tt.in, usdCurrency))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	_, err := floorCents(num.Money{})
	assert.Error(t, err)
}

func TestFormatCents(t *testing.T) {
	assert.Equal(t, "$0.00", formatCents(0))
	assert.Equal(t, "$0.05", formatCents(5))
	assert.Equal(t, "-$0.05", formatCents(-5))
	assert.Equal(t, "$10033.00", formatCents(1003300))
	assert.Equal(t, "-$102.09", formatCents(-10209))
}

func TestFormatPrice_FiveDecimals(t *testing.T) {
	assert.Equal(t, "1.10437", formatPrice(px(110437)))
	assert.Equal(t, "1.10000", formatPrice(px(110000)), "trailing zeros are kept")
	assert.Equal(t, "0.00005", formatPrice(px(5)))
	assert.Equal(t, "0.00000", formatPrice(px(0)))
	assert.Equal(t, "1.10437", formatPrice(num.MustParsePrice("1.104379")), "digits beyond the tick are cut, never rounded")
}

func TestParsePrice_RefusesWhatTheBarsFileDoesNot(t *testing.T) {
	for name, in := range map[string]string{
		"four decimals":  "1.1000",
		"six decimals":   "1.100000",
		"no decimals":    "1",
		"no whole part":  ".10000",
		"signed":         "-1.10000",
		"plus sign":      "+1.10000",
		"letters":        "1.1a000",
		"zero":           "0.00000",
		"above the max":  "10000.00001",
		"not a decimal":  "x.xxxxx",
		"empty":          "",
		"exponent":       "1e1.0000",
		"embedded space": "1 .10000",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parsePrice(in)
			assert.Error(t, err, in)
		})
	}
	got, err := parsePrice("10000.00000")
	require.NoError(t, err, "the maximum itself is allowed")
	assert.Equal(t, maxPrice, got)
	got, err = parsePrice("0.00001")
	require.NoError(t, err)
	assert.Equal(t, px(1), got)
}

func TestAbove_IsTheDistanceAboveOrZero(t *testing.T) {
	assert.Equal(t, px(4), above(px(9), px(5)))
	assert.True(t, above(px(5), px(9)).IsZero(), "below is no distance above")
	assert.True(t, above(px(5), px(5)).IsZero())
	assert.Equal(t, px(4), mustDiff(px(9), px(5)))
}

func TestNewATR_RefusesAWindowThatCouldLeaveTheRange(t *testing.T) {
	assert.Panics(t, func() { newATR(0) })
	assert.Panics(t, func() { newATR(maxATRWindow + 1) })
	assert.NotPanics(t, func() { newATR(maxATRWindow) })
}

func TestATR_TrueRangeIncludesTheGapFromThePreviousClose(t *testing.T) {
	a := newATR(1)
	_, ok := a.onBar(bar{ohlc: ohlc{o: px(100000), h: px(100500), l: px(99500), c: px(100000)}})
	require.False(t, ok, "the first bar only supplies a close")

	// Gaps up: the range 100100..100300 is 200 points, but the high is 300 above the previous close.
	got, ok := a.onBar(bar{ohlc: ohlc{o: px(100200), h: px(100300), l: px(100100), c: px(100250)}})
	require.True(t, ok)
	assert.InDelta(t, 0.003, got, 1e-12)

	// Gaps down: the low is 400 below the previous close of 100250.
	got, ok = a.onBar(bar{ohlc: ohlc{o: px(99900), h: px(99950), l: px(99850), c: px(99900)}})
	require.True(t, ok)
	assert.InDelta(t, 0.004, got, 1e-12)
}

func TestEmax_StopFor_RoundsHalfEvenAtOneEightThenFloorsToTheTick(t *testing.T) {
	e := &emax{tick: px(1), short: 2, long: 4, stopPct: rt(400)} // 0.04%
	require.NoError(t, e.init())

	// 0.04999 x 0.0004 = 0.000019996 exactly: its floor is one tick (0.00001),
	// but the product is rounded half-even at 1e-8 to 0.00002 first, so the stop
	// is two ticks away: at most one tick wider than the exact floor (ADR-087).
	stop, err := e.stopFor(px(4999), long)
	require.NoError(t, err)
	assert.Equal(t, px(4997), stop)

	stop, err = e.stopFor(px(4999), short)
	require.NoError(t, err)
	assert.Equal(t, px(5001), stop)
}

func TestEmax_StopFor_AStopBelowZeroIsAnError(t *testing.T) {
	e := &emax{tick: px(1), short: 2, long: 4, stopPct: rateOnePercent}
	require.NoError(t, e.init())

	_, err := e.stopFor(px(0), long)

	require.ErrorIs(t, err, num.ErrNegative)
}

func TestAccount_Aggregates_ReportOverflowInsteadOfWrapping(t *testing.T) {
	// Two positions each $50 billion in profit sum to $100 billion, past num's
	// range, and a balance of $92 billion plus $50 billion is too.
	big := func(sym string) *position {
		return &position{instrument: sym, direction: long, units: qty(5_000_000), entryPrice: px(0), markPrice: maxPrice}
	}
	a := newAccount("a", num.MustParseMoney("92000000000", usdCurrency), testMarkets())
	a.positions["EURUSD"] = big("EURUSD")

	_, err := a.equity()
	require.ErrorIs(t, err, num.ErrOverflow, "balance plus unrealized")
	_, err = a.marginAvailable()
	require.ErrorIs(t, err, num.ErrOverflow)

	a.positions["GBPUSD"] = big("GBPUSD")
	_, err = a.unrealizedPnL()
	require.ErrorIs(t, err, num.ErrOverflow, "the sum of the positions")

	b := newAccount("b", usd(1), testMarkets())
	b.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: qty(5_000_000_000), entryPrice: maxPrice, markPrice: maxPrice}
	_, err = b.usedMargin()
	require.ErrorIs(t, err, num.ErrOverflow, "one position's margin")
	_, err = defaultMarginPolicy(b)
	require.ErrorIs(t, err, num.ErrOverflow, "the margin policy surfaces it")

	r := &trader{broker: newBroker("t", testMarkets()), account: a}
	_, err = newResult("s", 0, r)
	require.ErrorIs(t, err, num.ErrOverflow, "a report of it is an error, not a wrong number")
}

// The size is checked after quantizing (ADR-087): a quotient that rounds up
// across an integer plans a loss above the budget, and the size drops a step.
// 9.99999999 / 10 = 0.999999999 rounds to 1.00000000, but one unit at a stop
// distance of 10 loses 10, more than 9.99999999.
func TestAccount_Size_Open_VerificationDropsAStepThatOvershoots(t *testing.T) {
	captureLogs(t)
	a := newAccount("a", num.MustParseMoney("9.99999999", usdCurrency), testMarkets())
	a.riskPct = rateOne
	o := openOrderAt("EURUSD", long, px(2_000_000)) // price 20, stop 19.8: replaced below
	o.stopLoss = px(1_000_000)                      // a stop of 10 away

	err := a.size(o, cents(0))

	require.ErrorIs(t, err, errOrderTooSmall, "the one unit that overshoots is dropped, leaving none")
	assert.True(t, o.units.IsZero())
}

func TestTrailStops_AStopNeverMovesBelowZero(t *testing.T) {
	br := newBroker("t", testMarkets())
	stop := &order{instrument: "EURUSD", reason: reasonStopLoss, direction: long, action: close, orderType: stopOrder,
		price: px(100000), trail: px(500000), state: stateSubmitted}
	br.orders["EURUSD"] = []*order{stop}

	// The high 1.10 less a trail of 5.00 is below zero: the stop stays.
	br.trailStops(bar{instrument: "EURUSD", ohlc: ohlc{o: px(110000), h: px(110000), l: px(109000), c: px(110000)}})

	assert.Equal(t, px(100000), stop.price)
	assert.Empty(t, stop.history)
}

func TestBroker_Submit_ReportsAReservedMarginOverflow(t *testing.T) {
	br := newBroker("t", testMarkets())
	act := br.addAccount("a", usd(1000))
	br.orders["GBPUSD"] = []*order{{accountID: "a", instrument: "GBPUSD", action: open, units: qty(5_000_000_000), price: maxPrice, state: stateSubmitted}}
	o := openOrderAt("EURUSD", long, eurusd1_10)
	o.accountID = act.id

	err := br.submit(context.Background(), o)

	require.ErrorIs(t, err, num.ErrOverflow)
	assert.False(t, isRejection(err))
}
