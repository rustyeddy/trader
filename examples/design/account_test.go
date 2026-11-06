package main

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"testing"

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
		{"zero", 0, 0},
		{"negative", -usd(50), -usd(50)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newAccount("a", tt.balance).marginAvailable()

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// registerInstrument adds a test instrument to the registry for one test.
func registerInstrument(t *testing.T, inst *instrument) {
	t.Helper()
	instruments[inst.symbol] = inst
	t.Cleanup(func() { delete(instruments, inst.symbol) })
}

// testInstrument is EURUSD-like with overridable limits.
func testInstrument(symbol string) *instrument {
	return &instrument{
		symbol: symbol, base: "EUR", quote: "USD",
		pipSize: 10, marginRate: rateTwoPercent, minUnits: 1, unitStep: 1,
	}
}

// eurusd1_10 is the price 1.10000: the default 1% stop is 1100 points away.
const eurusd1_10 price = 110000

// openOrderAt is an open order with the stop a strategy would give it: 1% of
// the price away on the losing side, at least one point.
func openOrderAt(sym string, d direction, p price) *order {
	dist := price(int64(p) * int64(rateOnePercent) / rateScale)
	if dist < 1 {
		dist = 1
	}
	stop := p - dist
	if d == short {
		stop = p + dist
	}
	return &order{Time: testStart, instrument: sym, direction: d, action: open, orderType: marketOrder, price: p, stopLoss: stop}
}

func TestAccount_Size_Open(t *testing.T) {
	captureLogs(t)

	// units = floor(balance * riskPct * priceScale / (rateScale * stopDistance))
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
		{"the budget is exact to the cent", 1100, rateOnePercent, 10},                      // $11.00
	}
	for _, tt := range tests {
		for _, d := range []direction{long, short} {
			t.Run(fmt.Sprintf("%s/%s", tt.name, d), func(t *testing.T) {
				act := newAccount("a", tt.balance)
				act.riskPct = tt.riskPct
				o := openOrderAt("EURUSD", d, eurusd1_10)

				require.NoError(t, act.size(o, 0))

				assert.Equal(t, tt.wantUnits, o.units)
				assert.Equal(t, stateSized, o.state)
				require.Len(t, o.history, 1)
				assert.Equal(t, stateSized, o.history[0].state)
			})
		}
	}
}

func TestAccount_Size_Open_NeedsAStopFromTheStrategy(t *testing.T) {
	captureLogs(t)
	for _, d := range []direction{long, short} {
		o := openOrderAt("EURUSD", d, eurusd1_10)
		o.stopLoss = 0 // the strategy did not set one

		err := newAccount("a", usd(10000)).size(o, 0)

		require.ErrorIs(t, err, errInvalidOrder, d.String())
		assert.Contains(t, err.Error(), "needs a stop loss")
		assert.False(t, isRejection(err), "a missing stop is a strategy bug, not a trading outcome")
		assert.Zero(t, o.units)
		assert.Zero(t, o.stopLoss, "the account never invents a stop")
		assert.Equal(t, stateRequested, o.state)
		assert.Empty(t, o.history)
	}
}

func TestAccount_Size_Open_NeverChangesTheStrategysStop(t *testing.T) {
	captureLogs(t)
	l := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(l, 0))
	assert.Equal(t, price(108900), l.stopLoss)

	s := openOrderAt("EURUSD", short, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(s, 0))
	assert.Equal(t, price(111100), s.stopLoss)
}

func TestAccount_Size_Open_UsesAStrategyStop(t *testing.T) {
	captureLogs(t)

	// A 2% stop (2200 points) halves the units compared with the default 1%.
	l := openOrderAt("EURUSD", long, eurusd1_10)
	l.stopLoss = 107800
	require.NoError(t, newAccount("a", usd(10000)).size(l, 0))
	assert.Equal(t, int64(4545), l.units) // 4545.45
	assert.Equal(t, price(107800), l.stopLoss, "the strategy's stop is kept")

	s := openOrderAt("EURUSD", short, eurusd1_10)
	s.stopLoss = 112200
	require.NoError(t, newAccount("a", usd(10000)).size(s, 0))
	assert.Equal(t, int64(4545), s.units)
	assert.Equal(t, price(112200), s.stopLoss)
}

func TestAccount_Size_Open_StopOnTheWrongSideIsAnInvalidOrder(t *testing.T) {
	captureLogs(t)
	for _, tt := range []struct {
		d    direction
		stop price
	}{
		{long, 111000},  // above a long entry
		{long, 110000},  // at the entry
		{short, 109000}, // below a short entry
		{short, 110000},
	} {
		o := openOrderAt("EURUSD", tt.d, eurusd1_10)
		o.stopLoss = tt.stop
		err := newAccount("a", usd(10000)).size(o, 0)

		require.ErrorIs(t, err, errInvalidOrder, "%s stop %d", tt.d, tt.stop)
		assert.False(t, isRejection(err), "a bad stop is a strategy bug, not a trading outcome")
		assert.Zero(t, o.units)
		assert.Equal(t, stateRequested, o.state)
		assert.Empty(t, o.history)
	}
}

func TestAccount_Size_Open_NeedsAReferencePrice(t *testing.T) {
	captureLogs(t)
	o := openOrderAt("EURUSD", long, 0)

	err := newAccount("a", usd(10000)).size(o, 0)

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Equal(t, stateRequested, o.state)
}

func TestAccount_Size_Open_WithoutMarginIsRejected(t *testing.T) {
	captureLogs(t)
	for _, bal := range []money{0, -usd(10)} {
		o := openOrderAt("EURUSD", long, eurusd1_10)
		err := newAccount("a", bal).size(o, 0)

		require.ErrorIs(t, err, errInsufficientMargin)
		assert.True(t, isRejection(err))
		assert.Zero(t, o.units)
		assert.Equal(t, stateRejected, o.state)
		require.Len(t, o.history, 1)
		assert.Contains(t, o.history[0].reason, "insufficient margin")
	}
}

// requiredMargin is the margin an order of units at p needs, rounded up,
// computed independently of production code with arbitrary precision.
func requiredMargin(units int64, p price, mr rate) int64 {
	n := big.NewInt(units)
	n.Mul(n, big.NewInt(int64(p)))
	n.Mul(n, big.NewInt(int64(mr)))
	n.Mul(n, big.NewInt(moneyScale))
	d := big.NewInt(priceScale * rateScale)
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	if r.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Int64()
}

func TestAccount_Size_Open_MarginShortfallReducesUnitsInsteadOfRejecting(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTMR")
	inst.marginRate = rate(2 * rateScale) // 200%: every unit needs twice its value
	registerInstrument(t, inst)

	// The risk budget alone would allow 9090 units, but 9090 units at 1.10
	// need 19998 of margin against 10000 available. The margin covers 4545.
	o := openOrderAt("TESTMR", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, 0))

	assert.Equal(t, int64(4545), o.units, "floor(10000 / (1.10 * 2))")
	assert.Equal(t, stateSized, o.state)
	assert.LessOrEqual(t, requiredMargin(o.units, eurusd1_10, inst.marginRate), int64(usd(10000)))
	assert.Greater(t, requiredMargin(o.units+1, eurusd1_10, inst.marginRate), int64(usd(10000)),
		"one more unit would not be covered: the size is the largest the margin allows")
}

func TestAccount_Size_Open_MarginLimitsAGenerousRiskBudget(t *testing.T) {
	captureLogs(t)
	// 100% risk would size 909090 units; at 50:1 the margin covers 454545.
	a := newAccount("a", usd(10000))
	a.riskPct = rate(rateScale)
	o := openOrderAt("EURUSD", long, eurusd1_10)

	require.NoError(t, a.size(o, 0))

	assert.Equal(t, int64(454545), o.units)
	mr := instruments["EURUSD"].marginRate
	assert.LessOrEqual(t, requiredMargin(454545, eurusd1_10, mr), int64(usd(10000)))
	assert.Greater(t, requiredMargin(454546, eurusd1_10, mr), int64(usd(10000)))
}

func TestAccount_Size_Open_RiskLimitWinsWhenItIsSmaller(t *testing.T) {
	captureLogs(t)
	// The default 1% risk (9090 units) is far below what 50:1 margin allows
	// (454545), so margin plays no part.
	o := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, 0))
	assert.Equal(t, int64(9090), o.units)
}

func TestAccount_Size_Open_MarginUnitsRoundDown(t *testing.T) {
	captureLogs(t)
	// Cap the size at 1000 units and risk 100%, so only the cap and the margin
	// can limit the order.
	inst := testInstrument("TESTCEIL")
	inst.maxUnits = 1000
	registerInstrument(t, inst)
	newAcct := func() *account {
		a := newAccount("a", usd(22))
		a.riskPct = rate(rateScale)
		return a
	}

	// 1000 units at 1.10000 need exactly $22 of margin: all 1000 fit.
	o := openOrderAt("TESTCEIL", long, 110000)
	require.NoError(t, newAcct().size(o, 0))
	assert.Equal(t, int64(1000), o.units)

	// At 1.10001 the margin covers 999.99 units, and 1000 would need $22.0002,
	// which rounds up to the next cent, $22.01. The size is 999 (rounded down), not 1000 and not
	// a rejection.
	o = openOrderAt("TESTCEIL", long, 110001)
	require.NoError(t, newAcct().size(o, 0))
	assert.Equal(t, int64(999), o.units)
	assert.LessOrEqual(t, requiredMargin(999, 110001, inst.marginRate), int64(usd(22)))
	assert.Equal(t, int64(usd(22))+1, requiredMargin(1000, 110001, inst.marginRate))
}

func TestAccount_Size_Open_MarginBelowTheMinimumIsAnInsufficientMarginRejection(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTMINM")
	inst.minUnits = 500_000
	registerInstrument(t, inst)

	// Margin covers 454545 units, less than the 500000 minimum, and the risk
	// budget (100%) is not what limits the order.
	a := newAccount("a", usd(10000))
	a.riskPct = rate(rateScale)
	o := openOrderAt("TESTMINM", long, eurusd1_10)
	err := a.size(o, 0)

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
	inst.marginRate = rate(2 * rateScale)
	inst.unitStep = 100
	registerInstrument(t, inst)

	o := openOrderAt("TESTSTEPM", long, eurusd1_10) // margin covers 4545
	require.NoError(t, newAccount("a", usd(10000)).size(o, 0))
	assert.Equal(t, int64(4500), o.units, "rounded down to the step after the margin limit")
}

func TestAccount_Size_Open_InstrumentWithoutAMarginRateIsAConfigurationError(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTNOMR")
	inst.marginRate = 0
	registerInstrument(t, inst)

	o := openOrderAt("TESTNOMR", long, eurusd1_10)
	err := newAccount("a", usd(10000)).size(o, 0)

	require.ErrorIs(t, err, errInstrumentConfig)
	assert.False(t, isRejection(err), "a missing margin rate is a setup bug, not a trading outcome")
	assert.Zero(t, o.units)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.history)
}

func TestAccount_Size_Open_RespectsInstrumentLimits(t *testing.T) {
	captureLogs(t)

	step := testInstrument("TESTSTEP")
	step.unitStep = 100
	registerInstrument(t, step)
	o := openOrderAt("TESTSTEP", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, 0))
	assert.Equal(t, int64(9000), o.units, "9090 rounded down to the 100 step")

	capped := testInstrument("TESTMAX")
	capped.maxUnits = 5000
	registerInstrument(t, capped)
	o = openOrderAt("TESTMAX", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, 0))
	assert.Equal(t, int64(5000), o.units, "capped at the maximum")

	both := testInstrument("TESTBOTH")
	both.maxUnits = 5000
	both.unitStep = 300
	registerInstrument(t, both)
	o = openOrderAt("TESTBOTH", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000)).size(o, 0))
	assert.Equal(t, int64(4800), o.units, "the cap is also rounded down to the step")
}

func TestAccount_Size_Open_BelowTheMinimumIsRejected(t *testing.T) {
	captureLogs(t)

	t.Run("budget buys less than one unit", func(t *testing.T) {
		o := openOrderAt("EURUSD", long, eurusd1_10)
		err := newAccount("a", usd(1)).size(o, 0) // 1/1.1 = 0.9 units

		require.ErrorIs(t, err, errOrderTooSmall)
		assert.True(t, isRejection(err))
		assert.Zero(t, o.units)
		assert.Equal(t, stateRejected, o.state)
		assert.Contains(t, o.history[0].reason, "0 units, minimum 1")
	})
	t.Run("below the instrument minimum", func(t *testing.T) {
		inst := testInstrument("TESTMIN")
		inst.minUnits = 10000
		registerInstrument(t, inst)

		o := openOrderAt("TESTMIN", long, eurusd1_10)
		err := newAccount("a", usd(10000)).size(o, 0) // 9090 < 10000

		require.ErrorIs(t, err, errOrderTooSmall)
		assert.Contains(t, err.Error(), "9090 units, minimum 10000")
	})
	t.Run("zero risk budget", func(t *testing.T) {
		a := newAccount("a", usd(10000))
		a.riskPct = 0
		require.ErrorIs(t, a.size(openOrderAt("EURUSD", long, eurusd1_10), 0), errOrderTooSmall)
	})
}

// A stop-out must never lose more than riskPct of the balance, and the size
// must be the largest whole number of units that satisfies that.
func TestAccount_Size_Open_RiskIsBoundedAndTight(t *testing.T) {
	captureLogs(t)
	balances := []money{1, usd(1), usd(11), usd(100), usd(999), usd(10000), usd(123456), usd(10_000_000_000)}
	prices := []price{10, 50000, 110000, 250000, 12_345_678}
	// 1% and 2% keep risk the limit; 50% and 100% make margin the limit.
	risks := []rate{rateOnePercent, rateTwoPercent, rate(rateScale / 2), rate(rateScale)}

	for _, bal := range balances {
		for _, p := range prices {
			for _, d := range []direction{long, short} {
				for _, rk := range risks {
					o := openOrderAt("EURUSD", d, p)
					act := newAccount("a", bal)
					act.riskPct = rk
					err := act.size(o, 0)

					// loss at the stop, in point-units, vs the budget, both scaled
					// by rateScale*priceScale so the comparison is exact.
					var dist int64
					if d == long {
						dist = int64(p - o.stopLoss)
					} else {
						dist = int64(o.stopLoss - p)
					}
					if err != nil {
						// Only a rejection is acceptable here: the order is below the
						// minimum, or the margin cannot cover even the minimum.
						require.True(t, errors.Is(err, errOrderTooSmall) || errors.Is(err, errInsufficientMargin),
							"bal %d price %d %s: %v", bal, p, d, err)
						continue
					}
					budget := new(big.Int).Mul(big.NewInt(int64(bal)), big.NewInt(int64(act.riskPct)))
					budget.Mul(budget, big.NewInt(priceScale))
					loss := func(units int64) *big.Int {
						v := big.NewInt(units)
						v.Mul(v, big.NewInt(dist))
						v.Mul(v, big.NewInt(rateScale))
						return v.Mul(v, big.NewInt(moneyScale))
					}
					mr := instruments["EURUSD"].marginRate
					msg := fmt.Sprintf("bal %d price %d %s units %d dist %d", bal, p, d, o.units, dist)

					assert.LessOrEqual(t, loss(o.units).Cmp(budget), 0, "risk exceeded: "+msg)
					assert.LessOrEqual(t, requiredMargin(o.units, p, mr), int64(bal), "margin exceeded: "+msg)

					// The size is the largest allowed: one more unit would break
					// either the risk budget or the margin.
					riskBroken := loss(o.units+1).Cmp(budget) > 0
					marginBroken := requiredMargin(o.units+1, p, mr) > int64(bal)
					assert.True(t, riskBroken || marginBroken, "not the largest size: "+msg)
				}
			}
		}
	}
}

func TestAccount_Size_CloseIsTheWholePositionAndNeedsNoMargin(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, instrument: "EURUSD", action: close, orderType: marketOrder}

	require.NoError(t, newAccount("a", 0).size(o, 0))

	assert.Zero(t, o.units)
	assert.Zero(t, o.stopLoss)
	assert.Equal(t, stateSized, o.state)
	assert.Len(t, o.history, 1)
}

func TestAccount_Size_AddReduceAndNoActionAreUnsupported(t *testing.T) {
	captureLogs(t)
	for _, a := range []action{add, reduce, noAction, action(99)} {
		o := &order{Time: testStart, instrument: "EURUSD", action: a}
		require.ErrorIs(t, newAccount("a", usd(1000)).size(o, 0), errUnsupportedAction, a.String())
		assert.Equal(t, stateRequested, o.state)
		assert.Empty(t, o.history, "an unsupported action changes nothing")
	}
}

func TestMuldiv(t *testing.T) {
	tests := []struct {
		name    string
		nums    []int64
		dens    []int64
		up      bool
		want    int64
		wantErr bool
	}{
		{"exact", []int64{6, 7}, []int64{3, 2}, false, 7, false},
		{"floor", []int64{10}, []int64{3}, false, 3, false},
		{"ceil", []int64{10}, []int64{3}, true, 4, false},
		{"ceil of an exact quotient does not add one", []int64{9}, []int64{3}, true, 3, false},
		{"floor of a negative quotient goes down", []int64{-10}, []int64{3}, false, -4, false},
		{"ceil of a negative quotient goes toward zero", []int64{-10}, []int64{3}, true, -3, false},
		{"negative divisor floors too", []int64{10}, []int64{-3}, false, -4, false},
		{"no overflow in the intermediate product", []int64{1 << 62, 1 << 62}, []int64{1 << 62}, false, 1 << 62, false},
		{"result overflow", []int64{1 << 62, 4}, []int64{1}, false, 0, true},
		{"division by zero", []int64{1}, []int64{0}, false, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := muldiv(tt.nums, tt.dens, tt.up)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAccount_Size_UnknownInstrumentIsAnErrorNotARejection(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, instrument: "NOPE", direction: long, action: open}

	err := newAccount("a", usd(1000)).size(o, 0)

	require.ErrorIs(t, err, errUnknownInstrument)
	assert.NotErrorIs(t, err, errInsufficientMargin)
	assert.Zero(t, o.units)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.history, "a bad instrument is a setup error, not a recorded rejection")
}

func TestNewAccount_PositionsMapIsReady(t *testing.T) {
	a := newAccount("a", usd(100))
	require.NotNil(t, a.positions)
	assert.NotPanics(t, func() { a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1} })
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
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10000, entryPrice: 110000, markPrice: 111000}

	eq, err := a.equity()
	require.NoError(t, err)
	used, err := a.usedMargin()
	require.NoError(t, err)
	avail, err := a.marginAvailable()
	require.NoError(t, err)

	assert.Equal(t, usd(12345+100), eq)
	assert.Equal(t, usd(222), used)
	assert.Equal(t, usd(12345+100-222), avail)
}

func TestAccount_UsedMargin(t *testing.T) {
	registerInstrument(t, testInstrument("GBPUSD"))
	t.Run("none", func(t *testing.T) {
		used, err := newAccount("a", usd(1)).usedMargin()
		require.NoError(t, err)
		assert.Zero(t, used)
	})
	t.Run("sums every position at its mark, long or short", func(t *testing.T) {
		a := newAccount("a", usd(1))
		a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10000, markPrice: 111000} // $222
		a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: short, units: 5000, markPrice: 150000} // $150
		used, err := a.usedMargin()
		require.NoError(t, err)
		assert.Equal(t, usd(372), used)
	})
	t.Run("rounds up to the cent", func(t *testing.T) {
		a := newAccount("a", usd(1))
		a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1, markPrice: 110000} // 2.2 cents
		used, err := a.usedMargin()
		require.NoError(t, err)
		assert.Equal(t, money(3), used)
	})
	t.Run("an unknown instrument is an error, not a zero", func(t *testing.T) {
		a := newAccount("a", usd(1))
		a.positions["XXXYYY"] = &position{instrument: "XXXYYY", units: 1, markPrice: 1}
		_, err := a.usedMargin()
		require.ErrorIs(t, err, errUnknownInstrument)
	})
	t.Run("overflow is an error, not a partial sum", func(t *testing.T) {
		a := newAccount("a", usd(1))
		a.positions["EURUSD"] = &position{instrument: "EURUSD", units: math.MaxInt64, markPrice: math.MaxInt64}
		_, err := a.usedMargin()
		require.ErrorIs(t, err, errSizeOverflow)
		_, err = a.marginAvailable()
		require.ErrorIs(t, err, errSizeOverflow)
	})
}

func TestAccount_Size_Open_UsedMarginReducesTheMarginLimit(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("TESTUM"))
	newAcct := func() *account {
		a := newAccount("a", usd(10000))
		a.riskPct = rate(rateScale) // only the margin can limit the order
		return a
	}

	free := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAcct().size(free, 0))
	assert.Equal(t, int64(454545), free.units, "floor($10000 / (1.10 * 2%))")

	// 100000 units at 1.00 hold $2000 of margin, leaving $8000.
	a := newAcct()
	a.positions["TESTUM"] = &position{instrument: "TESTUM", direction: long, units: 100000, entryPrice: 100000, markPrice: 100000}
	held := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, a.size(held, 0))
	assert.Equal(t, int64(363636), held.units, "floor($8000 / (1.10 * 2%))")

	// Margin the broker reserves for submitted opens that have not filled
	// counts the same way.
	reserved := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAcct().size(reserved, usd(2000)))
	assert.Equal(t, int64(363636), reserved.units)

	// Reserving all of it leaves nothing.
	none := openOrderAt("EURUSD", long, eurusd1_10)
	require.ErrorIs(t, newAcct().size(none, usd(10000)), errInsufficientMargin)
}

func TestAccount_Size_Open_AllMarginInUseRejects(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("TESTUM"))
	// 500000 units at 1.00 need $10000: all of the equity.
	a := newAccount("a", usd(10000))
	a.positions["TESTUM"] = &position{instrument: "TESTUM", direction: long, units: 500000, entryPrice: 100000, markPrice: 100000}
	o := openOrderAt("EURUSD", long, eurusd1_10)

	err := a.size(o, 0)

	require.ErrorIs(t, err, errInsufficientMargin)
	assert.Equal(t, stateRejected, o.state)
	assert.Zero(t, o.units)
}

func TestAccount_Size_Open_UnknownInstrumentInUseIsNotARejection(t *testing.T) {
	captureLogs(t)
	a := newAccount("a", usd(10000))
	a.positions["XXXYYY"] = &position{instrument: "XXXYYY", units: 1, markPrice: 1}

	err := a.size(openOrderAt("EURUSD", long, eurusd1_10), 0)

	require.ErrorIs(t, err, errUnknownInstrument)
	assert.False(t, isRejection(err))
}

func TestAccount_EquityWithoutPositionsIsTheBalance(t *testing.T) {
	eq, err := newAccount("a", usd(12345)).equity()

	require.NoError(t, err)
	assert.Equal(t, usd(12345), eq)
}

func TestPosition_UnrealizedPnL(t *testing.T) {
	tests := []struct {
		name string
		dir  direction
		mark price
		want money
	}{
		{"long up", long, 111000, usd(100)},
		{"long down", long, 109000, -usd(100)},
		{"short up", short, 111000, -usd(100)},
		{"short down", short, 109000, usd(100)},
		{"marked at entry", long, 110000, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &position{direction: tt.dir, units: 10000, entryPrice: 110000, markPrice: tt.mark}

			got, err := p.unrealizedPnL()

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAccount_UnrealizedPnL_SumsEveryPosition(t *testing.T) {
	a := newAccount("a", usd(1000))
	require.Zero(t, mustPnL(t, a), "no positions")

	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10000, entryPrice: 110000, markPrice: 111000} // +100
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: short, units: 5000, entryPrice: 150000, markPrice: 151000} // -50
	a.positions["AUDUSD"] = &position{instrument: "AUDUSD", direction: long, units: 1000, entryPrice: 70000, markPrice: 69000}    // -10

	assert.Equal(t, usd(40), mustPnL(t, a))
}

func mustPnL(t *testing.T, a *account) money {
	t.Helper()
	pnl, err := a.unrealizedPnL()
	require.NoError(t, err)
	return pnl
}

func TestAccount_UnrealizedPnL_OverflowIsAnErrorNotAPartialSum(t *testing.T) {
	a := newAccount("a", usd(1000))
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: math.MaxInt64, entryPrice: 1, markPrice: math.MaxInt64}

	_, err := a.unrealizedPnL()
	require.ErrorIs(t, err, errSizeOverflow)

	_, err = a.equity()
	require.ErrorIs(t, err, errSizeOverflow)
}

func TestAccount_Size_Open_RiskIsMeasuredAgainstEquity(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("GBPUSD"))
	// $1000 balance buys floor(1000/1.1)=909 units; another position's
	// unrealized +$100 lifts equity to $1100 and the size to floor(1100/1.1)=1000.
	a := newAccount("a", usd(1000))
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: 10000, entryPrice: 150000, markPrice: 151000}
	o := openOrderAt("EURUSD", long, eurusd1_10)

	require.NoError(t, a.size(o, 0))

	assert.Equal(t, int64(1000), o.units)
}

func TestAccount_Size_Open_UnrealizedLossShrinksTheRiskBudget(t *testing.T) {
	captureLogs(t)
	registerInstrument(t, testInstrument("GBPUSD"))
	a := newAccount("a", usd(1000))
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: 10000, entryPrice: 150000, markPrice: 149000}
	o := openOrderAt("EURUSD", long, eurusd1_10)

	require.NoError(t, a.size(o, 0))

	assert.Equal(t, int64(818), o.units, "floor(900 / 1.1)")
}

func TestAccount_Mark_ValuesThePositionAtTheBarClose(t *testing.T) {
	act := newAccount("a", usd(1000))
	pos := &position{instrument: "EURUSD", direction: long, units: 1, entryPrice: 110000, markPrice: 110000}
	act.positions["EURUSD"] = pos

	act.mark(fillBar(1, 111500)) // closes at 111600
	assert.Equal(t, price(111600), pos.markPrice)

	other := fillBar(2, 90000)
	other.instrument = "GBPUSD"
	act.mark(other)
	assert.Equal(t, price(111600), pos.markPrice, "another instrument's bar does not move the mark")

	act.mark(bar{instrument: "AUDUSD"}) // no position: nothing to value
}

func TestAccount_ApplyFill_OpenIsMarkedAtTheFillPrice(t *testing.T) {
	act := newAccount("a", usd(1000))

	require.NoError(t, act.applyFill(&order{instrument: "EURUSD", direction: short, action: open, units: 5, fillPrice: 110000}))

	assert.Equal(t, price(110000), act.positions["EURUSD"].markPrice)
	assert.Zero(t, mustPnL(t, act), "no profit or loss the moment it opens")
}

func TestPosition_RealizedPnL(t *testing.T) {
	// pnl (cents) = floor(units * (exit - entry) * moneyScale / priceScale),
	// sign flipped for shorts. A price point is 0.00001, so 1 unit moving 1
	// point is a thousandth of a cent.
	tests := []struct {
		name  string
		dir   direction
		units int64
		exit  price
		want  money
	}{
		{"long win", long, 10000, 111000, usd(100)},
		{"long loss", long, 10000, 109000, -usd(100)},
		{"short win", short, 10000, 109000, usd(100)},
		{"short loss", short, 10000, 111000, -usd(100)},
		{"flat", long, 10000, 110000, 0},
		{"pennies are kept", long, 15, 111000, 15}, // $0.15
		{"penny loss", long, 15, 109000, -15},
		{"a win under a cent rounds down to zero", long, 1, 110001, 0},     // +0.001 cent
		{"a loss under a cent rounds away from zero", long, 1, 109999, -1}, // -0.001 cent
		{"a short win under a cent rounds down to zero", short, 1, 109999, 0},
		{"a short loss under a cent rounds away from zero", short, 1, 110001, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &position{instrument: "EURUSD", direction: tt.dir, units: tt.units, entryPrice: 110000}

			got, err := p.pnlAt(tt.exit)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPosition_RealizedPnL_Overflow(t *testing.T) {
	p := &position{direction: long, units: math.MaxInt64, entryPrice: 1}

	_, err := p.pnlAt(math.MaxInt64)

	require.ErrorIs(t, err, errSizeOverflow)
}

func TestAccount_ApplyFill_CloseRealizesPnLIntoTheBalanceAndRemovesThePosition(t *testing.T) {
	tests := []struct {
		name string
		dir  direction
		exit price
		want money
	}{
		{"long win", long, 111000, usd(10100)},
		{"long loss", long, 109000, usd(9900)},
		{"short win", short, 109000, usd(10100)},
		{"short loss", short, 111000, usd(9900)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act := newAccount("a", usd(10000))
			act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: tt.dir, units: 10000, entryPrice: 110000}
			cl := &order{instrument: "EURUSD", action: close, fillPrice: tt.exit}

			require.NoError(t, act.applyFill(cl))

			assert.Equal(t, tt.want, act.balance)
			assert.Equal(t, int64(10000), cl.units, "the order records what was closed")
			assert.Empty(t, act.positions)
		})
	}
}

func TestAccount_ApplyFill_OpenAndRejectedCloseLeaveTheBalanceAlone(t *testing.T) {
	act := newAccount("a", usd(10000))

	require.NoError(t, act.applyFill(&order{instrument: "EURUSD", direction: long, action: open, units: 5, fillPrice: 110000}))
	require.ErrorIs(t, act.applyFill(&order{instrument: "GBPUSD", action: close, fillPrice: 110000}), errNoPosition)

	assert.Equal(t, usd(10000), act.balance)
}

func TestCheckedAdd_OverflowAndUnderflow(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b money
		want money
		bad  bool
	}{
		{"ordinary", 120, -20, 100, false},
		{"max boundary", money(math.MaxInt64) - 1, 1, money(math.MaxInt64), false},
		{"min boundary", money(math.MinInt64) + 1, -1, money(math.MinInt64), false},
		{"positive overflow", money(math.MaxInt64), 1, 0, true},
		{"negative overflow", money(math.MinInt64), -1, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkedAdd(tc.a, tc.b)
			if tc.bad {
				require.ErrorIs(t, err, errSizeOverflow)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestAccount_UnrealizedPnL_TotalOverflow(t *testing.T) {
	a := newAccount("a", 0)
	// A 1000-point move equals one cent per unit.
	units := int64(math.MaxInt64/2 + 1)
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: units, entryPrice: 110000, markPrice: 111000}
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: units, entryPrice: 110000, markPrice: 111000}
	_, err := a.unrealizedPnL()
	require.ErrorIs(t, err, errSizeOverflow)
}

func TestAccount_Equity_AdditionOverflow(t *testing.T) {
	a := newAccount("a", money(math.MaxInt64))
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1, entryPrice: 110000, markPrice: 111000}
	_, err := a.equity()
	require.ErrorIs(t, err, errSizeOverflow)
}

func TestAccount_ApplyFill_BalanceOverflowLeavesPositionIntact(t *testing.T) {
	for _, tc := range []struct {
		name    string
		balance money
		exit    price
	}{
		{"profit overflow", money(math.MaxInt64), 112000},
		{"loss underflow", money(math.MinInt64), 108000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAccount("a", tc.balance)
			pos := &position{instrument: "EURUSD", direction: long, units: 1, entryPrice: 110000}
			a.positions["EURUSD"] = pos
			o := &order{instrument: "EURUSD", action: close, fillPrice: tc.exit}
			require.ErrorIs(t, a.applyFill(o), errSizeOverflow)
			assert.Equal(t, tc.balance, a.balance)
			assert.Same(t, pos, a.positions["EURUSD"])
			assert.Zero(t, o.units, "failed close must not change order units")
		})
	}
}

func TestPosition_PnLAt_ExtremePriceDifferenceDoesNotWrap(t *testing.T) {
	p := &position{direction: long, units: 1, entryPrice: price(math.MinInt64)}
	got, err := p.pnlAt(price(math.MaxInt64))
	require.NoError(t, err)
	// floor((MaxInt64 - MinInt64) * 100 / 100000) cents.
	n := new(big.Int).Sub(big.NewInt(math.MaxInt64), big.NewInt(math.MinInt64))
	n.Mul(n, big.NewInt(moneyScale))
	n.Div(n, big.NewInt(priceScale))
	assert.Equal(t, money(n.Int64()), got)
}

// A fill is all or nothing: whatever the reason it fails, the balance, the
// positions and the order's units are exactly as they were.
func TestAccount_ApplyFill_FailureChangesNothing(t *testing.T) {
	existing := func() *position {
		return &position{instrument: "EURUSD", direction: long, units: 10, entryPrice: 110000, markPrice: 110000}
	}
	tests := []struct {
		name    string
		balance money
		pos     *position // existing EURUSD position, nil for none
		o       *order
		wantErr error
	}{
		{"open over a position", usd(10), existing(),
			&order{instrument: "EURUSD", direction: short, action: open, units: 5, fillPrice: 111000}, errPositionExists},
		{"close with no position", usd(10), nil,
			&order{instrument: "EURUSD", action: close, fillPrice: 111000}, errNoPosition},
		{"P/L arithmetic overflows", usd(10), &position{instrument: "EURUSD", direction: long, units: math.MaxInt64, entryPrice: 1},
			&order{instrument: "EURUSD", action: close, fillPrice: math.MaxInt64}, errSizeOverflow},
		{"balance overflows", money(math.MaxInt64), existing(),
			&order{instrument: "EURUSD", action: close, fillPrice: 112000}, errSizeOverflow},
		{"unsupported action", usd(10), existing(),
			&order{instrument: "EURUSD", action: reduce, fillPrice: 111000}, errUnsupportedAction},
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

			require.ErrorIs(t, a.applyFill(tt.o), tt.wantErr)

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
		return &order{instrument: "EURUSD", direction: long, action: open, units: 10000, fillPrice: 110000}
	}
	t.Run("exactly enough margin fills", func(t *testing.T) {
		a := newAccount("a", usd(220))
		require.NoError(t, a.applyFill(fill()))
		assert.Contains(t, a.positions, "EURUSD")
	})
	t.Run("one cent short is rejected and changes nothing", func(t *testing.T) {
		a := newAccount("a", usd(220)-1)
		err := a.applyFill(fill())
		require.ErrorIs(t, err, errInsufficientMargin)
		assert.True(t, isRejection(err))
		assert.Empty(t, a.positions)
		assert.Equal(t, usd(220)-1, a.balance)
	})
	t.Run("a higher fill price needs more margin", func(t *testing.T) {
		a := newAccount("a", usd(220))
		o := fill()
		o.fillPrice = 111000 // $222.00
		require.ErrorIs(t, a.applyFill(o), errInsufficientMargin)
	})
}

func TestApplyFill_CloseRecordsTheTrade(t *testing.T) {
	entered, exited := testStart, testStart.AddDate(0, 0, 7)
	for name, tc := range map[string]struct {
		direction direction
		exit      price
		reason    orderReason
		pnl       money
	}{
		"long profit":    {long, 110364, reasonStrategy, 3276},    // +$32.76364 floors to +$32.76
		"long loss":      {long, 109999, reasonStopLoss, -10},     // -$0.09001 floors to -$0.10
		"short profit":   {short, 109999, reasonStrategy, 9},      // +$0.09001 floors to +$0.09
		"short loss":     {short, 110001, reasonStopLoss, -10},    // -$0.09001 floors to -$0.10
		"margin closing": {long, 110000, reasonMarginCloseout, 0}, // break-even is still a trade
	} {
		t.Run(name, func(t *testing.T) {
			units := int64(9001)
			a := newAccount("a", usd(1000))
			a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: tc.direction, units: units,
				entryPrice: 110000, entryTime: entered, markPrice: 110000}
			o := &order{instrument: "EURUSD", action: close, reason: tc.reason, fillPrice: tc.exit, Time: exited}

			require.NoError(t, a.applyFill(o))

			assert.Equal(t, []trade{{instrument: "EURUSD", direction: tc.direction, units: units,
				entryPrice: 110000, entryTime: entered, exitPrice: tc.exit, exitTime: exited,
				reason: tc.reason, pnl: tc.pnl}}, a.trades)
			assert.Equal(t, usd(1000)+tc.pnl, a.balance)
		})
	}
}

func TestApplyFill_OpenRecordsItsEntryTimeAndNoTrade(t *testing.T) {
	a := newAccount("a", usd(10000))
	o := &order{instrument: "EURUSD", action: open, direction: long, units: 100, fillPrice: 110000, Time: testStart}

	require.NoError(t, a.applyFill(o))

	assert.Equal(t, testStart, a.positions["EURUSD"].entryTime)
	assert.Empty(t, a.trades, "an open position is not a trade yet")
}

func TestApplyFill_FailedCloseRecordsNoTrade(t *testing.T) {
	t.Run("no position", func(t *testing.T) {
		a := newAccount("a", usd(10000))
		require.ErrorIs(t, a.applyFill(&order{instrument: "EURUSD", action: close, fillPrice: 110000}), errNoPosition)
		assert.Empty(t, a.trades)
	})
	t.Run("balance overflow", func(t *testing.T) {
		a := newAccount("a", money(math.MaxInt64))
		a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 100000, entryPrice: 110000, markPrice: 110000}
		require.ErrorIs(t, a.applyFill(&order{instrument: "EURUSD", action: close, fillPrice: 120000}), errSizeOverflow)
		assert.Empty(t, a.trades)
		assert.Contains(t, a.positions, "EURUSD", "a failed fill leaves the account as it was")
	})
}
