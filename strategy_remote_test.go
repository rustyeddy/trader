package trader

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	remotestrat "github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteStrategy_OnBars(t *testing.T) {
	ctx := context.Background()
	bars := []marketdata.Bar{{Instrument: "EURUSD", Volume: 1}}

	t.Run("an empty answer is no orders and no error", func(t *testing.T) {
		w := startWorker(t, workerOpts{})
		r := w.remote("remote")

		assert.Equal(t, "remote", r.name())
		require.NoError(t, r.init())
		for range 2 {
			orders, err := r.onBars(ctx, bars)
			require.NoError(t, err)
			assert.Empty(t, orders)
		}
		assert.Len(t, w.finish(), 2, "the worker saw both groups")
	})
	t.Run("a dead worker is an error", func(t *testing.T) {
		w := startWorker(t, workerOpts{dropOn: 2})
		r := w.remote("remote")

		_, err := r.onBars(ctx, bars)
		require.NoError(t, err, "the first group was answered")
		orders, err := r.onBars(ctx, bars)
		assert.Error(t, err)
		assert.Empty(t, orders)
	})
}

func TestToOrder(t *testing.T) {
	at := testStart
	req := remotestrat.Order{Instrument: "EURUSD", Action: "open", Direction: "long", Price: px(110000), StopLoss: px(109000)}

	t.Run("an open maps every field it may carry and nothing else", func(t *testing.T) {
		q := req
		q.Type, q.TakeProfit = "limit", px(112000)
		o, err := toOrder(q, at)
		require.NoError(t, err)
		assert.Equal(t, &order{
			instrument: "EURUSD", action: open, direction: long, orderType: limitOrder,
			price: px(110000), stopLoss: px(109000), takeProfit: px(112000), Time: at,
		}, o)
		assert.Empty(t, o.id)
		assert.Empty(t, o.accountID)
		assert.True(t, o.decidedAt.IsZero(), "the trader stamps these, the worker never does")
		assert.Zero(t, o.units)
		assert.Equal(t, stateRequested, o.state)
	})
	t.Run("an empty type is a market order", func(t *testing.T) {
		o, err := toOrder(req, at)
		require.NoError(t, err)
		assert.Equal(t, marketOrder, o.orderType)
	})
	t.Run("a close needs no direction, price or stop", func(t *testing.T) {
		o, err := toOrder(remotestrat.Order{Instrument: "EURUSD", Action: "close"}, at)
		require.NoError(t, err)
		assert.Equal(t, close, o.action)
		assert.Equal(t, noDirection, o.direction)
	})
	t.Run("what the trader's own validation refuses is refused", func(t *testing.T) {
		tests := []struct {
			name string
			edit func(*remotestrat.Order)
			want string
		}{
			{"missing stop", func(q *remotestrat.Order) { q.StopLoss = num.Price{} }, "stop loss"},
			{"missing price", func(q *remotestrat.Order) { q.Price = num.Price{} }, "reference price"},
			{"limit stop on the wrong side", func(q *remotestrat.Order) { q.Type, q.StopLoss = "limit", px(111000) }, "not beyond"},
			{"open without a direction", func(q *remotestrat.Order) { q.Direction = "" }, "direction"},
			{"open without an instrument", func(q *remotestrat.Order) { q.Instrument = "" }, "instrument"},
			{"close with a direction", func(q *remotestrat.Order) { q.Action, q.StopLoss = "close", num.Price{} }, "direction"},
			{"unsupported action", func(q *remotestrat.Order) { q.Action = "amend" }, `unsupported action "amend"`},
			{"no action", func(q *remotestrat.Order) { q.Action = "" }, "unsupported action"},
			{"unknown direction", func(q *remotestrat.Order) { q.Direction = "sideways" }, "unknown direction"},
			{"unknown type", func(q *remotestrat.Order) { q.Type = "iceberg" }, "unknown order type"},
			{"limit order without a price", func(q *remotestrat.Order) { q.Type, q.Price = "limit", num.Price{} }, "price"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				q := req
				tt.edit(&q)
				_, err := toOrder(q, at)
				assert.ErrorContains(t, err, tt.want)
			})
		}
	})
	t.Run("an order without the bar's time is refused", func(t *testing.T) {
		_, err := toOrder(req, time.Time{})
		assert.ErrorContains(t, err, "time")
	})
}

func TestToOrders(t *testing.T) {
	t1, t2 := testStart, testStart.Add(time.Hour)
	avail := t2.Add(time.Hour) // bars of different opening times, available together
	bars := []marketdata.Bar{
		{Instrument: "EURUSD", Time: t1, AvailableAt: avail},
		{Instrument: "GBPUSD", Time: t2, AvailableAt: avail},
	}
	good := remotestrat.Order{Instrument: "EURUSD", Action: "open", Direction: "long", Price: px(110000), StopLoss: px(109000)}

	t.Run("none is none", func(t *testing.T) {
		orders, err := toOrders(nil, bars)
		require.NoError(t, err)
		assert.Empty(t, orders)
	})
	t.Run("keeps the worker's order and takes each time from its instrument's bar", func(t *testing.T) {
		gbp := good
		gbp.Instrument = "GBPUSD"
		orders, err := toOrders([]remotestrat.Order{gbp, good, {Instrument: "EURUSD", Action: "close"}}, bars)
		require.NoError(t, err)
		require.Len(t, orders, 3)
		assert.Equal(t, []string{"GBPUSD", "EURUSD", "EURUSD"}, []string{orders[0].instrument, orders[1].instrument, orders[2].instrument})
		assert.Equal(t, t2, orders[0].Time)
		assert.Equal(t, t1, orders[1].Time)
	})
	t.Run("an instrument outside the group is not refused; it takes the time the group became available", func(t *testing.T) {
		other := good
		other.Instrument = "USDJPY"
		orders, err := toOrders([]remotestrat.Order{other}, bars)
		require.NoError(t, err)
		require.Len(t, orders, 1)
		assert.Equal(t, avail, orders[0].Time, "not the opening time of some other instrument's bar")
	})
	t.Run("one bad order fails them all and names it", func(t *testing.T) {
		bad := good
		bad.StopLoss = num.Price{}
		orders, err := toOrders([]remotestrat.Order{good, bad}, bars)
		assert.ErrorContains(t, err, "requested order 2 (open EURUSD)")
		assert.Nil(t, orders, "the valid first order is not returned either")
	})
}

func TestRemoteStrategy_Feedback(t *testing.T) {
	ctx := context.Background()
	bars := []marketdata.Bar{{Instrument: "EURUSD", Time: testStart, AvailableAt: testStart}}
	at := testStart
	broker := &order{id: "t-2", instrument: "EURUSD", action: close, direction: long, reason: reasonStopLoss, fillPrice: px(109000)}

	reqs := []remotestrat.Order{{Instrument: "EURUSD", Action: "open", Direction: "long", Price: px(110000), StopLoss: px(109000), Ref: "entry"}}
	w := startWorker(t, workerOpts{decide: func(n int, _ []marketdata.Bar, _ []remotestrat.Event) ([]remotestrat.Order, error) {
		if n == 1 {
			return reqs, nil
		}
		return nil, nil
	}})
	r := w.remote("remote")

	orders, err := r.onBars(ctx, bars) // the worker asks for an order labelled "entry"
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "entry", r.refs[orders[0]], "the adapter remembers the label of the order it made")

	// The trader hands over what happened; it goes with the next group.
	r.feedback([]feedbackEvent{
		{orders[0], orderEvent{state: stateSubmitted, time: at}},
		{orders[0], orderEvent{state: stateFilled, time: at}},
		{broker, orderEvent{state: stateFilled, time: at}},
	})
	assert.Len(t, r.pending, 3)
	assert.Empty(t, r.refs, "a filled order's label is forgotten")

	_, err = r.onBars(ctx, bars)
	require.NoError(t, err)
	assert.Empty(t, r.pending, "sent once")
	_, err = r.onBars(ctx, bars)
	require.NoError(t, err)
	w.finish()

	require.Len(t, w.events, 3)
	assert.Empty(t, w.events[0], "nothing before the first order")
	require.Len(t, w.events[1], 3)
	assert.Empty(t, w.events[2], "not repeated")
	assert.Equal(t, []string{"accepted", "filled", "filled"}, []string{w.events[1][0].Kind, w.events[1][1].Kind, w.events[1][2].Kind})
	assert.Equal(t, "entry", w.events[1][0].Ref)
	assert.Equal(t, "entry", w.events[1][1].Ref)
	assert.Empty(t, w.events[1][2].Ref, "the broker's own stop was not the worker's order")
	assert.Equal(t, "stop loss", w.events[1][2].Cause)
}
