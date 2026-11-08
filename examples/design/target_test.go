package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// submitWithTarget submits a market open for account "a" on day 0 with a take
// profit, and returns it. Long: entry 1.10000, stop 1.08900, target tp.
func submitWithTarget(t *testing.T, br *broker, d direction, tp price) *order {
	t.Helper()
	o := openOrderAt("EURUSD", d, eurusd1_10)
	o.accountID, o.takeProfit = "a", tp
	require.NoError(t, br.submit(context.Background(), o))
	return o
}

func TestBroker_FilledOpenQueuesAStopAndATarget(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	submitWithTarget(t, br, long, 112000)

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 110000)))

	require.Len(t, br.orders["EURUSD"], 2)
	stop, target := br.orders["EURUSD"][0], br.orders["EURUSD"][1]
	assert.Equal(t, reasonStopLoss, stop.reason)
	assert.Equal(t, reasonTakeProfit, target.reason)
	assert.Equal(t, limitOrder, target.orderType)
	assert.Equal(t, close, target.action)
	assert.Equal(t, price(112000), target.price)
	assert.Equal(t, long, target.direction)
	assert.Equal(t, stateSubmitted, target.state)
}

func TestBroker_OpenWithoutATargetQueuesOnlyAStop(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	submittedOrder(t, br, open, long)

	require.NoError(t, br.onBar(context.Background(), fillBar(1, 110000)))

	require.Len(t, br.orders["EURUSD"], 1)
	assert.Equal(t, reasonStopLoss, br.orders["EURUSD"][0].reason)
}

func TestBroker_OnBar_TakeProfitClosesAtTheTargetAndCancelsTheStop(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submitWithTarget(t, br, long, 112000)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	stop := br.orders["EURUSD"][0]

	rally := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 111000, h: 112300, l: 110500, c: 112100}}
	require.NoError(t, br.onBar(ctx, rally))

	assert.Empty(t, act.positions)
	n := len(br.completed)
	tp := br.completed[n-2]
	assert.Equal(t, reasonTakeProfit, tp.reason)
	assert.Equal(t, stateFilled, tp.state)
	assert.Equal(t, price(112000), tp.fillPrice, "at the target, not the high")
	assert.Equal(t, op.units, tp.units)
	assert.Same(t, stop, br.completed[n-1], "the stop went with the position")
	assert.Equal(t, stateCancelled, stop.state)
	assert.Empty(t, br.orders["EURUSD"])
	// 110000 -> 112000 is 2000 points a unit: 2 cents a unit.
	assert.Equal(t, usd(10000)+money(op.units*2), act.balance)
}

func TestBroker_OnBar_TakeProfitGapFillsAtTheBetterOpen(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	op := submitWithTarget(t, br, long, 112000)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))

	gap := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 112500, h: 113000, l: 112200, c: 112800}}
	require.NoError(t, br.onBar(ctx, gap))

	tp := br.completed[len(br.completed)-2]
	assert.Equal(t, price(112500), tp.fillPrice, "the open is better than the target")
	assert.Equal(t, usd(10000)+money(op.units*5/2), act.balance)
	assert.Empty(t, act.positions)
}

func TestBroker_OnBar_StopCancelsTheTarget(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	submitWithTarget(t, br, long, 112000)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	target := br.orders["EURUSD"][1]

	crash := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 109500, h: 109600, l: 108000, c: 108200}}
	require.NoError(t, br.onBar(ctx, crash))

	assert.Empty(t, act.positions)
	assert.Equal(t, stateCancelled, target.state)
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_BarReachingBothStopAndTargetTakesTheStop(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	submitWithTarget(t, br, long, 112000)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	target := br.orders["EURUSD"][1]

	wide := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 110500, h: 112500, l: 108000, c: 110000}}
	require.NoError(t, br.onBar(ctx, wide))

	assert.Empty(t, act.positions)
	assert.Equal(t, stateCancelled, target.state, "the pessimistic path: the stop wins")
	stop := br.completed[len(br.completed)-2]
	assert.Equal(t, reasonStopLoss, stop.reason)
	assert.Equal(t, price(108900), stop.fillPrice)
}

func TestBroker_OnBar_StrategyCloseCancelsBothExits(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	ctx := context.Background()
	submitWithTarget(t, br, long, 112000)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	exits := append([]*order(nil), br.orders["EURUSD"]...)
	cl := &order{accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder, Time: testStart.AddDate(0, 0, 1)}
	require.NoError(t, br.submit(ctx, cl))

	require.NoError(t, br.onBar(ctx, fillBar(2, 110500)))

	require.Len(t, exits, 2)
	for _, e := range exits {
		assert.Equal(t, stateCancelled, e.state)
	}
	assert.Empty(t, br.orders["EURUSD"])
}

func TestBroker_OnBar_ShortTakeProfit(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	submitWithTarget(t, br, short, 108000) // entry 1.10000, stop 1.11100
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))

	drop := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 109000, h: 109500, l: 107900, c: 108100}}
	require.NoError(t, br.onBar(ctx, drop))

	assert.Empty(t, act.positions)
	tp := br.completed[len(br.completed)-2]
	assert.Equal(t, reasonTakeProfit, tp.reason)
	assert.Equal(t, price(108000), tp.fillPrice)
}

func TestOrder_Validate_TakeProfitMustBeOnTheWinningSide(t *testing.T) {
	tests := []struct {
		name string
		d    direction
		tp   price
		ok   bool
	}{
		{"long above entry", long, 112000, true},
		{"long at entry", long, eurusd1_10, false},
		{"long below entry", long, 108000, false},
		{"short below entry", short, 108000, true},
		{"short at entry", short, eurusd1_10, false},
		{"short above entry", short, 112000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := openOrderAt("EURUSD", tt.d, eurusd1_10)
			o.takeProfit = tt.tp
			err := o.validate()
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errInvalidOrder)
			assert.False(t, isRejection(err), "a bad target is a strategy bug")
		})
	}
}

func TestOrder_Validate_TakeProfit(t *testing.T) {
	base := func() order {
		return order{Time: testStart, instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder, price: 110000, stopLoss: 108900}
	}
	ok := base()
	ok.takeProfit = 112000
	require.NoError(t, ok.validate())

	neg := base()
	neg.takeProfit = -1
	require.ErrorIs(t, neg.validate(), errInvalidOrder)

	onClose := order{Time: testStart, instrument: "EUR/USD", action: close, orderType: marketOrder, takeProfit: 112000}
	require.ErrorIs(t, onClose.validate(), errInvalidOrder, "only an open carries a target")
}

func TestOrderReason_TakeProfitString(t *testing.T) {
	assert.Equal(t, "take profit", reasonTakeProfit.String())
}

// A bar opens beyond the target and later crosses the stop. The open is known
// to come first, so the target fills there; the stop-first policy only applies
// to what the open does not settle.
func TestBroker_OnBar_TargetAlreadyReachedAtTheOpenBeatsALaterStop(t *testing.T) {
	tests := []struct {
		name  string
		d     direction
		tp    price
		bar   ohlc
		fill  price
		gain  func(units int64) money // cents
		stopP price
	}{
		{"long", long, 112000, ohlc{o: 112500, h: 113000, l: 108500, c: 111000}, 112500,
			func(u int64) money { return money(u * 5 / 2) }, 108900},
		{"short", short, 108000, ohlc{o: 107500, h: 111500, l: 107000, c: 109000}, 107500,
			func(u int64) money { return money(u * 5 / 2) }, 111100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captureLogs(t)
			br, act := newFillBroker()
			ctx := context.Background()
			op := submitWithTarget(t, br, tt.d, tt.tp)
			require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
			require.Len(t, br.orders["EURUSD"], 2)
			stop := br.orders["EURUSD"][0]
			require.Equal(t, tt.stopP, stop.price)

			require.NoError(t, br.onBar(ctx, bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: tt.bar}))

			assert.Empty(t, act.positions)
			tp := br.completed[len(br.completed)-2]
			assert.Equal(t, reasonTakeProfit, tp.reason, "the target won")
			assert.Equal(t, tt.fill, tp.fillPrice, "at the open, which is better than the target")
			assert.Equal(t, stateCancelled, stop.state, "the stop was cancelled, never executed")
			assert.Equal(t, usd(10000)+tt.gain(op.units), act.balance)
		})
	}
}

// The mirror case: the open is already through the stop, and the same bar later
// reaches the target. The stop fills at the open and cancels the target.
func TestBroker_OnBar_StopAlreadyReachedAtTheOpenBeatsALaterTarget(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	ctx := context.Background()
	submitWithTarget(t, br, long, 112000)
	require.NoError(t, br.onBar(ctx, fillBar(1, 110000)))
	target := br.orders["EURUSD"][1]

	gap := bar{instrument: "EURUSD", Time: testStart.AddDate(0, 0, 2), ohlc: ohlc{o: 108000, h: 112500, l: 107500, c: 111000}}
	require.NoError(t, br.onBar(ctx, gap))

	assert.Empty(t, act.positions)
	stop := br.completed[len(br.completed)-2]
	assert.Equal(t, reasonStopLoss, stop.reason)
	assert.Equal(t, price(108000), stop.fillPrice)
	assert.Equal(t, stateCancelled, target.state)
}
