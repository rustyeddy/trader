package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testStart is the fixed timestamp of the first test bar (a Monday).
var testStart = time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)

// closes builds consecutive daily test bars for "TEST" with the given closes.
func closes(vals ...float64) []bar {
	bars := make([]bar, len(vals))
	for i, v := range vals {
		bars[i] = bar{
			instrument: "TEST",
			Time:       testStart.AddDate(0, 0, i),
			ohlc:       ohlc{c: price(v * priceScale)},
		}
	}
	return bars
}

// actionOf is the action of o, or noAction for a nil (do nothing) order.
func actionOf(o *order) action {
	if o == nil {
		return noAction
	}
	return o.action
}

func newTestEmax(t *testing.T, short, long int) *emax {
	t.Helper()
	e := &emax{short: short, long: long, stopPct: rateOnePercent}
	require.NoError(t, e.init(nil))
	return e
}

func TestEmax_NoOrderUntilBothWarm(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	// The long EMA needs 4 bars, so nothing is ordered on bars 0-2.
	for i, b := range closes(10, 9, 8) {
		assert.Nil(t, e.onBar(ctx, b), "bar %d", i)
	}
}

func TestEmax_LongWarmsOnSameBars(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	for _, b := range closes(1, 2, 3, 4) {
		e.onBar(ctx, b)
	}
	// Regression: the long EMA must be seeded from bars 1-4 (value 2.5). If it
	// were only fed after the short EMA warmed, the next value would differ.
	v, ok := e.emaLong.onBar(ctx, bar{ohlc: ohlc{c: price(4 * priceScale)}})
	require.True(t, ok)
	assert.InDelta(t, 2.5+0.4*(4-2.5), v, 1e-9)
}

// flipSeries starts bearish, flips bullish, then bearish again.
var flipSeries = []float64{10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2}

func TestEmax_Crossovers(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var acts []action
	var dirs []direction
	for _, b := range closes(flipSeries...) {
		o := e.onBar(ctx, b)
		acts = append(acts, actionOf(o))
		if o != nil {
			dirs = append(dirs, o.direction)
		}
	}

	// Warm on bar 3, already bearish: open short. The short EMA crosses above
	// the long one on the jump to 12 (bar 5): close, then open long on bar 6.
	// It crosses back below on the drop to 8 (bar 8): close, open short on 9.
	assert.Equal(t, []action{
		noAction, noAction, noAction, open, noAction, close,
		open, noAction, close, open, noAction,
	}, acts)
	assert.Equal(t, []direction{short, noDirection, long, noDirection, short}, dirs)
}

func TestEmax_SustainedTrendOpensOnce(t *testing.T) {
	tests := []struct {
		name   string
		series []float64
		want   direction
	}{
		{"rising goes long", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, long},
		{"falling goes short", []float64{12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}, short},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEmax(t, 2, 4)
			var orders []*order
			for _, b := range closes(tt.series...) {
				if o := e.onBar(context.Background(), b); o != nil {
					orders = append(orders, o)
				}
			}
			require.Len(t, orders, 1, "a sustained trend must not re-emit open")
			assert.Equal(t, open, orders[0].action)
			assert.Equal(t, tt.want, orders[0].direction)
		})
	}
}

func TestEmax_FirstValidBarAlreadyTrending(t *testing.T) {
	// Documents current behavior: if the EMAs are already apart on the first
	// valid bar there is no crossover, but emax opens in the trend's direction.
	tests := []struct {
		name   string
		series []float64
		want   direction
	}{
		{"bullish", []float64{1, 2, 3, 4}, long},
		{"bearish", []float64{4, 3, 2, 1}, short},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEmax(t, 2, 4)
			var got []*order
			for _, b := range closes(tt.series...) {
				got = append(got, e.onBar(context.Background(), b))
			}
			require.Len(t, got, 4)
			assert.Nil(t, got[0])
			assert.Nil(t, got[1])
			assert.Nil(t, got[2])
			require.NotNil(t, got[3])
			assert.Equal(t, open, got[3].action)
			assert.Equal(t, tt.want, got[3].direction)
		})
	}
}

func TestEmax_EqualEMAsChangeNothing(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()
	// A flat series: both EMAs are identical, so there is no trend to act on.
	for i, b := range closes(5, 5, 5, 5, 5, 5, 5) {
		assert.Nil(t, e.onBar(ctx, b), "bar %d", i)
	}
}

func TestEmax_Init(t *testing.T) {
	tests := []struct {
		name        string
		short, long int
		stopPct     rate
		wantErr     bool
	}{
		{"valid", 2, 5, rateOnePercent, false},
		{"zero short", 0, 5, rateOnePercent, true},
		{"zero long", 2, 0, rateOnePercent, true},
		{"negative short", -1, 5, rateOnePercent, true},
		{"equal windows", 5, 5, rateOnePercent, true},
		{"short above long", 6, 5, rateOnePercent, true},
		{"stop not set", 2, 5, 0, true},
		{"negative stop", 2, 5, -rateOnePercent, true},
		{"stop of 100%", 2, 5, rate(rateScale), true},
		{"stop above 100%", 2, 5, rate(2 * rateScale), true},
		{"just under 100%", 2, 5, rate(rateScale - 1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &emax{short: tt.short, long: tt.long, stopPct: tt.stopPct}
			err := e.init(nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, e.emaShort)
				assert.Nil(t, e.emaLong)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, e.emaShort)
			assert.NotNil(t, e.emaLong)
		})
	}
}

func TestEmax_OrdersCarryInstrumentDirectionAndPrice(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var got []*order
	for _, b := range closes(flipSeries...) {
		if o := e.onBar(ctx, b); o != nil {
			got = append(got, o)
		}
	}
	// Each order is priced at the close of the bar that triggered it. An open
	// carries a stop 1% away on the losing side; a close takes no direction
	// and no stop.
	assert.Equal(t, []*order{
		{instrument: "TEST", direction: short, action: open, orderType: marketOrder,
			price: price(7 * priceScale), stopLoss: price(707000)},
		{instrument: "TEST", action: close, orderType: marketOrder, price: price(12 * priceScale)},
		{instrument: "TEST", direction: long, action: open, orderType: marketOrder,
			price: price(14 * priceScale), stopLoss: price(1386000)},
		{instrument: "TEST", action: close, orderType: marketOrder, price: price(8 * priceScale)},
		{instrument: "TEST", direction: short, action: open, orderType: marketOrder,
			price: price(4 * priceScale), stopLoss: price(404000)},
	}, got)
}

func TestEmax_OrderPriceIsTheTriggeringBarsClose(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	for i, b := range closes(flipSeries...) {
		if o := e.onBar(ctx, b); o != nil {
			assert.Equal(t, b.c, o.price, "bar %d (%s)", i, o.action)
			assert.NotZero(t, o.price)
		}
	}
}

// There is no atomic reverse: a flip is a close on the crossover bar, then the
// opposite open on the very next bar, never both on one bar.
func TestEmax_FlipIsCloseThenOpenOnTheNextBar(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var seq []action
	var openBar, closeBar []int
	for i, b := range closes(flipSeries...) {
		switch a := actionOf(e.onBar(ctx, b)); a {
		case open:
			openBar = append(openBar, i)
			seq = append(seq, a)
		case close:
			closeBar = append(closeBar, i)
			seq = append(seq, a)
		}
	}

	assert.Equal(t, []action{open, close, open, close, open}, seq, "opens and closes strictly alternate")
	require.Len(t, closeBar, 2)
	for i, c := range closeBar {
		assert.Equal(t, c+1, openBar[i+1], "the opposite open follows its close by exactly one bar")
	}
}

func TestEmax_OrdersValidate(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var n int
	for i, b := range closes(flipSeries...) {
		o := e.onBar(ctx, b)
		if o == nil {
			continue
		}
		n++
		assert.Equal(t, "TEST", o.instrument, "bar %d (%s)", i, o.action)
		assert.NoError(t, o.validate(), "bar %d (%s)", i, o.action)
	}
	assert.Equal(t, 5, n)
}

func TestEmax_Name(t *testing.T) {
	var s strategy = &emax{id: "emax-20-50"}
	assert.Equal(t, "emax-20-50", s.name())
}

func TestEmax_OpenOrdersCarryAStopOnTheLosingSide(t *testing.T) {
	tests := []struct {
		name    string
		series  []float64
		stopPct rate
		want    direction
		stop    price
	}{
		{"long 1%", []float64{1, 2, 3, 4}, rateOnePercent, long, 396000},   // 4.00 - 0.04
		{"short 1%", []float64{4, 3, 2, 1}, rateOnePercent, short, 101000}, // 1.00 + 0.01
		{"long 2%", []float64{1, 2, 3, 4}, rateTwoPercent, long, 392000},   // 4.00 - 0.08
		{"short 2%", []float64{4, 3, 2, 1}, rateTwoPercent, short, 102000}, // 1.00 + 0.02
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &emax{short: 2, long: 4, stopPct: tt.stopPct}
			require.NoError(t, e.init(nil))

			var got *order
			for _, b := range closes(tt.series...) {
				if o := e.onBar(context.Background(), b); o != nil {
					got = o
				}
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.direction)
			assert.Equal(t, tt.stop, got.stopLoss)
			if tt.want == long {
				assert.Less(t, got.stopLoss, got.price, "a long's stop is below the entry")
			} else {
				assert.Greater(t, got.stopLoss, got.price, "a short's stop is above the entry")
			}
		})
	}
}

func TestEmax_StopDistanceRoundsDownAndIsAtLeastOnePoint(t *testing.T) {
	e := &emax{short: 2, long: 4, stopPct: rateOnePercent}
	require.NoError(t, e.init(nil))

	// 1% of 12_345_678 is 123456.78, which rounds down to 123456.
	assert.Equal(t, price(12_345_678-123_456), e.stopFor(12_345_678, long))
	assert.Equal(t, price(12_345_678+123_456), e.stopFor(12_345_678, short))
	// 1% of 10 points is 0.1: never a zero-distance stop.
	assert.Equal(t, price(9), e.stopFor(10, long))
	assert.Equal(t, price(11), e.stopFor(10, short))
}

func TestEmax_ClosesCarryNoStop(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	for _, b := range closes(flipSeries...) {
		if o := e.onBar(context.Background(), b); o != nil && o.action == close {
			assert.Zero(t, o.stopLoss)
		}
	}
}

func TestEmax_StopsPassAccountSizing(t *testing.T) {
	// The strategy's stops are what the account sizes from: every open order
	// emax produces must be sizeable.
	captureLogs(t)
	e := newTestEmax(t, 2, 4)
	acct := newAccount("a", 10000)
	for i, b := range closes(flipSeries...) {
		o := e.onBar(context.Background(), b)
		if o == nil || o.action != open {
			continue
		}
		o.instrument = "EURUSD" // the test bars are "TEST"; size needs a known instrument
		require.NoError(t, acct.size(o), "bar %d", i)
		assert.Positive(t, o.units)
	}
}
