package main

import (
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccount_MarginAvailable(t *testing.T) {
	tests := []struct {
		name    string
		balance money
		want    money
	}{
		{"positive", 10000, 10000},
		{"zero", 0, 0},
		{"negative", -50, -50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, newAccount("a", tt.balance).marginAvailable())
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

func openOrderAt(sym string, d direction, p price) *order {
	return &order{instrument: sym, direction: d, action: open, orderType: marketOrder, price: p}
}

func TestAccount_Size_Open(t *testing.T) {
	captureLogs(t)
	stubClock(t, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))

	// units = floor(balance * riskPct * priceScale / (rateScale * stopDistance))
	//       = floor(balance / 1.1)   for a 1% risk and a 1% stop at 1.10000.
	tests := []struct {
		name      string
		balance   money
		riskPct   rate
		wantUnits int64
	}{
		{"10000 at 1% risk rounds down, not to nearest", 10000, rateOnePercent, 9090}, // 9090.9
		{"1000", 1000, rateOnePercent, 909},                                           // 909.09
		{"exact division is not lost to rounding", 11, rateOnePercent, 10},            // 11/1.1
		{"2% risk doubles the budget", 10000, rateTwoPercent, 18181},                  // 18181.8
	}
	for _, tt := range tests {
		for _, d := range []direction{long, short} {
			t.Run(fmt.Sprintf("%s/%s", tt.name, d), func(t *testing.T) {
				act := newAccount("a", tt.balance)
				act.riskPct = tt.riskPct
				o := openOrderAt("EURUSD", d, eurusd1_10)

				require.NoError(t, act.size(o))

				assert.Equal(t, tt.wantUnits, o.units)
				assert.Equal(t, stateSized, o.state)
				require.Len(t, o.history, 1)
				assert.Equal(t, stateSized, o.history[0].state)
			})
		}
	}
}

func TestAccount_Size_Open_PlacesTheDefaultStopOnTheLosingSide(t *testing.T) {
	captureLogs(t)

	l := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, newAccount("a", 10000).size(l))
	assert.Equal(t, price(108900), l.stopLoss, "long: 1% below the entry")

	s := openOrderAt("EURUSD", short, eurusd1_10)
	require.NoError(t, newAccount("a", 10000).size(s))
	assert.Equal(t, price(111100), s.stopLoss, "short: 1% above the entry")
}

func TestAccount_Size_Open_UsesAStrategyStop(t *testing.T) {
	captureLogs(t)

	// A 2% stop (2200 points) halves the units compared with the default 1%.
	l := openOrderAt("EURUSD", long, eurusd1_10)
	l.stopLoss = 107800
	require.NoError(t, newAccount("a", 10000).size(l))
	assert.Equal(t, int64(4545), l.units) // 4545.45
	assert.Equal(t, price(107800), l.stopLoss, "the strategy's stop is kept")

	s := openOrderAt("EURUSD", short, eurusd1_10)
	s.stopLoss = 112200
	require.NoError(t, newAccount("a", 10000).size(s))
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
		err := newAccount("a", 10000).size(o)

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

	err := newAccount("a", 10000).size(o)

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Equal(t, stateRequested, o.state)
}

func TestAccount_Size_Open_WithoutMarginIsRejected(t *testing.T) {
	captureLogs(t)
	for _, bal := range []money{0, -10} {
		o := openOrderAt("EURUSD", long, eurusd1_10)
		err := newAccount("a", bal).size(o)

		require.ErrorIs(t, err, errInsufficientMargin)
		assert.True(t, isRejection(err))
		assert.Zero(t, o.units)
		assert.Equal(t, stateRejected, o.state)
		require.Len(t, o.history, 1)
		assert.Contains(t, o.history[0].reason, "insufficient margin")
	}
}

func TestAccount_Size_Open_RequiredMarginAboveAvailableIsRejected(t *testing.T) {
	captureLogs(t)
	inst := testInstrument("TESTMR")
	inst.marginRate = rate(2 * rateScale) // 200%: every unit needs twice its value
	registerInstrument(t, inst)

	// 9090 units * 1.10 = 9999 of notional; 200% of that is 19998 > 10000.
	o := openOrderAt("TESTMR", long, eurusd1_10)
	err := newAccount("a", 10000).size(o)

	require.ErrorIs(t, err, errInsufficientMargin)
	assert.Contains(t, err.Error(), "need 19998, have 10000")
	assert.Zero(t, o.units)
	assert.Zero(t, o.stopLoss)
	assert.Equal(t, stateRejected, o.state)
}

func TestAccount_Size_Open_RequiredMarginRoundsUp(t *testing.T) {
	captureLogs(t)
	// Cap the size at 1000 units and risk 100% so the cap, not the budget,
	// decides the size and only the margin requirement is in question.
	inst := testInstrument("TESTCEIL")
	inst.maxUnits = 1000
	registerInstrument(t, inst)
	newAcct := func() *account {
		a := newAccount("a", 22)
		a.riskPct = rate(rateScale)
		return a
	}

	// 1000 units at 1.10000 is 1100 of notional; 2% of that is exactly 22,
	// which 22 covers.
	o := openOrderAt("TESTCEIL", long, 110000)
	require.NoError(t, newAcct().size(o))
	assert.Equal(t, int64(1000), o.units)

	// At 1.10001 the requirement is 22.0002, which rounds up to 23 and 22
	// cannot cover. Rounding down would wrongly accept this order.
	o = openOrderAt("TESTCEIL", long, 110001)
	err := newAcct().size(o)
	require.ErrorIs(t, err, errInsufficientMargin)
	assert.Contains(t, err.Error(), "need 23, have 22")
}

func TestAccount_Size_Open_RespectsInstrumentLimits(t *testing.T) {
	captureLogs(t)

	step := testInstrument("TESTSTEP")
	step.unitStep = 100
	registerInstrument(t, step)
	o := openOrderAt("TESTSTEP", long, eurusd1_10)
	require.NoError(t, newAccount("a", 10000).size(o))
	assert.Equal(t, int64(9000), o.units, "9090 rounded down to the 100 step")

	capped := testInstrument("TESTMAX")
	capped.maxUnits = 5000
	registerInstrument(t, capped)
	o = openOrderAt("TESTMAX", long, eurusd1_10)
	require.NoError(t, newAccount("a", 10000).size(o))
	assert.Equal(t, int64(5000), o.units, "capped at the maximum")

	both := testInstrument("TESTBOTH")
	both.maxUnits = 5000
	both.unitStep = 300
	registerInstrument(t, both)
	o = openOrderAt("TESTBOTH", long, eurusd1_10)
	require.NoError(t, newAccount("a", 10000).size(o))
	assert.Equal(t, int64(4800), o.units, "the cap is also rounded down to the step")
}

func TestAccount_Size_Open_BelowTheMinimumIsRejected(t *testing.T) {
	captureLogs(t)

	t.Run("budget buys less than one unit", func(t *testing.T) {
		o := openOrderAt("EURUSD", long, eurusd1_10)
		err := newAccount("a", 1).size(o) // 1/1.1 = 0.9 units

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
		err := newAccount("a", 10000).size(o) // 9090 < 10000

		require.ErrorIs(t, err, errOrderTooSmall)
		assert.Contains(t, err.Error(), "9090 units, minimum 10000")
	})
	t.Run("zero risk budget", func(t *testing.T) {
		a := newAccount("a", 10000)
		a.riskPct = 0
		require.ErrorIs(t, a.size(openOrderAt("EURUSD", long, eurusd1_10)), errOrderTooSmall)
	})
}

// A stop-out must never lose more than riskPct of the balance, and the size
// must be the largest whole number of units that satisfies that.
func TestAccount_Size_Open_RiskIsBoundedAndTight(t *testing.T) {
	captureLogs(t)
	balances := []money{1, 11, 100, 999, 10000, 123456, 10_000_000_000}
	prices := []price{1, 50000, 110000, 250000, 12_345_678}

	for _, bal := range balances {
		for _, p := range prices {
			for _, d := range []direction{long, short} {
				o := openOrderAt("EURUSD", d, p)
				act := newAccount("a", bal)
				err := act.size(o)

				// loss at the stop, in point-units, vs the budget, both scaled
				// by rateScale*priceScale so the comparison is exact.
				var dist int64
				if d == long {
					dist = int64(p - o.stopLoss)
				} else {
					dist = int64(o.stopLoss - p)
				}
				if err != nil {
					// Only "too small" is acceptable here; then even one unit
					// would exceed the budget, or the margin could not cover it.
					require.True(t, errors.Is(err, errOrderTooSmall) || errors.Is(err, errInsufficientMargin),
						"bal %d price %d %s: %v", bal, p, d, err)
					continue
				}
				budget := new(big.Int).Mul(big.NewInt(int64(bal)), big.NewInt(int64(act.riskPct)))
				budget.Mul(budget, big.NewInt(priceScale))
				loss := func(units int64) *big.Int {
					v := big.NewInt(units)
					v.Mul(v, big.NewInt(dist))
					return v.Mul(v, big.NewInt(rateScale))
				}
				msg := fmt.Sprintf("bal %d price %d %s units %d dist %d", bal, p, d, o.units, dist)
				assert.LessOrEqual(t, loss(o.units).Cmp(budget), 0, "risk exceeded: "+msg)
				assert.Greater(t, loss(o.units+1).Cmp(budget), 0, "not the largest size: "+msg)
			}
		}
	}
}

func TestAccount_Size_CloseIsTheWholePositionAndNeedsNoMargin(t *testing.T) {
	captureLogs(t)
	o := &order{instrument: "EURUSD", action: close, orderType: marketOrder}

	require.NoError(t, newAccount("a", 0).size(o))

	assert.Zero(t, o.units)
	assert.Zero(t, o.stopLoss)
	assert.Equal(t, stateSized, o.state)
	assert.Len(t, o.history, 1)
}

func TestAccount_Size_AddReduceAndNoActionAreUnsupported(t *testing.T) {
	captureLogs(t)
	for _, a := range []action{add, reduce, noAction, action(99)} {
		o := &order{instrument: "EURUSD", action: a}
		require.ErrorIs(t, newAccount("a", 1000).size(o), errUnsupportedAction, a.String())
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
	o := &order{instrument: "NOPE", direction: long, action: open}

	err := newAccount("a", 1000).size(o)

	require.ErrorIs(t, err, errUnknownInstrument)
	assert.NotErrorIs(t, err, errInsufficientMargin)
	assert.Zero(t, o.units)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.history, "a bad instrument is a setup error, not a recorded rejection")
}

func TestNewAccount_PositionsMapIsReady(t *testing.T) {
	a := newAccount("a", 100)
	require.NotNil(t, a.positions)
	assert.NotPanics(t, func() { a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1} })
}

func TestNewAccount_DefaultRiskIsOnePercent(t *testing.T) {
	assert.Equal(t, rateOnePercent, newAccount("a", 100).riskPct)
}
