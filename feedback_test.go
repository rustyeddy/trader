package trader

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	remotestrat "github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// feedbackScript is a strategy that asks for feedback, requests the orders it is
// given by group number, and keeps what it was told with each group.
type feedbackScript struct {
	orders  map[int]func(b marketdata.Bar) *order
	groups  int
	pending []feedbackEvent
	told    [][]feedbackEvent
}

func (*feedbackScript) name() string { return "script" }
func (*feedbackScript) init() error  { return nil }

func (s *feedbackScript) feedback(events []feedbackEvent) { s.pending = append(s.pending, events...) }

func (s *feedbackScript) onBars(_ context.Context, bars []marketdata.Bar) ([]*order, error) {
	s.groups++
	s.told = append(s.told, s.pending)
	s.pending = nil
	if mk := s.orders[s.groups]; mk != nil {
		return []*order{mk(bars[0])}, nil
	}
	return nil, nil
}

// told is each group's events as "kind action cause".
func (s *feedbackScript) kinds() [][]string {
	out := make([][]string, len(s.told))
	for i, g := range s.told {
		for _, e := range g {
			w := toWireEvent(e, "")
			out[i] = append(out[i], w.Kind+" "+w.Action+" "+w.Cause)
		}
	}
	return out
}

func entryOrder(stop, target int64) func(marketdata.Bar) *order {
	return func(b marketdata.Bar) *order {
		o := &order{instrument: b.Instrument, action: open, direction: long, orderType: marketOrder,
			price: b.OHLC().Close, stopLoss: px(stop), Time: b.Time}
		if target != 0 {
			o.takeProfit = px(target)
		}
		return o
	}
}

func runScript(t *testing.T, s *feedbackScript, balance money) *trader {
	t.Helper()
	captureLogs(t)
	tr := newRemoteTraderWith(t, s, balance)
	require.NoError(t, (&backtest{}).runSeries(context.Background(), tr, scenarioPrices(t, remoteScenario)))
	return tr
}

func TestCollectEvents_Timing(t *testing.T) {
	// Entry requested on group 3: its acceptance and fill are told with group 4,
	// never with group 3, and never again.
	s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{3: entryOrder(108700, 0)}}
	runScript(t, s, usd(1000))

	kinds := s.kinds()
	require.GreaterOrEqual(t, len(kinds), 6)
	assert.Empty(t, kinds[0], "nothing has happened before the first order")
	assert.Empty(t, kinds[1])
	assert.Empty(t, kinds[2], "the order this very call asks for is not in the call that asks")
	assert.Equal(t, []string{"accepted open strategy", "filled open strategy"}, kinds[3])
	for i := 4; i < len(kinds); i++ {
		assert.Empty(t, kinds[i], "group %d: delivered once", i+1)
	}
	assert.Equal(t, time.Date(2010, 1, 6, 0, 0, 0, 0, time.UTC), s.told[3][0].time, "the accepted event keeps the time it happened")
	assert.Equal(t, time.Date(2010, 1, 7, 0, 0, 0, 0, time.UTC), s.told[3][1].time)
}

func TestCollectEvents_BrokerExits(t *testing.T) {
	t.Run("a protective stop is reported when it fills, and nothing of its housekeeping", func(t *testing.T) {
		s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{3: entryOrder(109800, 0)}}
		tr := runScript(t, s, usd(1000))

		var all []string
		for _, g := range s.kinds() {
			all = append(all, g...)
		}
		assert.Equal(t, []string{"accepted open strategy", "filled open strategy", "filled close stop loss"}, all,
			"the broker's own stop is not 'accepted' to the worker, and its sibling's cancellation is not reported")
		stop := s.told[5][0]
		assert.Equal(t, stateFilled, stop.state)
		assert.Equal(t, "stop loss", stop.order.reason.String())
		assert.Equal(t, 1, len(tr.account.trades))
	})
	t.Run("a take profit is reported with its cause", func(t *testing.T) {
		s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{3: entryOrder(108700, 110800)}}
		runScript(t, s, usd(1000))

		var all []string
		for _, g := range s.kinds() {
			all = append(all, g...)
		}
		assert.Equal(t, []string{"accepted open strategy", "filled open strategy", "filled close take profit"}, all)
	})
}

func TestCollectEvents_Refusals(t *testing.T) {
	t.Run("a refusal at sizing has a reason and no id", func(t *testing.T) {
		s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{3: entryOrder(108700, 0)}}
		tr := runScript(t, s, usd(0))

		require.Len(t, s.told[3], 1)
		e := toWireEvent(s.told[3][0], "")
		assert.Equal(t, remotestrat.Rejected, e.Kind)
		assert.Empty(t, e.ID, "it never reached the broker, which issues ids")
		assert.NotEmpty(t, e.Reason)
		assert.Len(t, tr.rejected, 1)
		for i, g := range s.told {
			if i != 3 {
				assert.Empty(t, g)
			}
		}
	})
	t.Run("a waiting order that expires is reported cancelled", func(t *testing.T) {
		s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{3: func(b marketdata.Bar) *order {
			// a buy limit far below the market, good for two days
			return &order{instrument: b.Instrument, action: open, direction: long, orderType: limitOrder,
				price: px(100000), stopLoss: px(99000), expires: b.Time.Add(48 * time.Hour), Time: b.Time}
		}}}
		runScript(t, s, usd(1000))

		var all []string
		for _, g := range s.kinds() {
			all = append(all, g...)
		}
		assert.Equal(t, []string{"accepted open strategy", "cancelled open strategy"}, all)
	})
}

func TestCollectEvents_OnceAndSorted(t *testing.T) {
	tr := newTestTrader()
	tr.strategy = &feedbackScript{}
	early := &order{id: "a", instrument: "EURUSD", action: open}
	late := &order{id: "b", instrument: "EURUSD", action: open}
	t1 := time.Date(2010, 1, 5, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	// watched in the order b, a, but a's step happened first
	late.record(stateRequested, t2, "")
	late.record(stateSubmitted, t2, "")
	early.record(stateRequested, t1, "")
	early.record(stateSubmitted, t1, "")
	tr.watch(late)
	tr.watch(early)

	events := tr.collectEvents()
	require.Len(t, events, 2, "requested is the trader's own step, not reported")
	assert.Equal(t, []string{"a", "b"}, []string{events[0].order.id, events[1].order.id}, "by time, not by who was watched first")
	assert.Empty(t, tr.collectEvents(), "once")

	early.record(stateFilled, t2, "")
	again := tr.collectEvents()
	require.Len(t, again, 1, "only what is new")
	assert.Equal(t, stateFilled, again[0].state)
	_, kept := tr.feedbackLog.sent[early]
	assert.False(t, kept, "a finished order is forgotten")
}

func TestTrader_Watch_OnlyForAStrategyThatAsks(t *testing.T) {
	tr := newTestTrader()
	tr.strategy = &recorder{} // does not implement feedbacker
	tr.watch(&order{})
	assert.Empty(t, tr.feedbackLog.watched)
}

func TestToWireEvent(t *testing.T) {
	at := time.Date(2010, 1, 5, 0, 0, 0, 0, time.UTC)
	o := &order{id: "t-9", instrument: "EURUSD", action: close, direction: long, units: qty(500), fillPrice: px(110000), reason: reasonMarginCloseout}
	tests := []struct {
		name  string
		state orderState
		why   string
		want  remotestrat.Event
	}{
		{"accepted", stateSubmitted, "", remotestrat.Event{Kind: remotestrat.Accepted}},
		{"filled carries the price", stateFilled, "", remotestrat.Event{Kind: remotestrat.Filled, Price: px(110000)}},
		{"rejected carries the reason", stateRejected, "no margin", remotestrat.Event{Kind: remotestrat.Rejected, Reason: "no margin"}},
		{"cancelled carries the reason", stateCancelled, "position closed", remotestrat.Event{Kind: remotestrat.Cancelled, Reason: "position closed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toWireEvent(feedbackEvent{o, orderEvent{state: tt.state, time: at, reason: tt.why}}, "label")
			want := tt.want
			want.Time, want.ID, want.Ref = at, "t-9", "label"
			want.Instrument, want.Action, want.Direction = "EURUSD", "close", "long"
			want.Units, want.Cause = qty(500), "margin closeout"
			assert.Equal(t, want, got)
		})
	}
	t.Run("an order with no direction leaves it out", func(t *testing.T) {
		got := toWireEvent(feedbackEvent{&order{id: "t-1", action: close}, orderEvent{state: stateSubmitted}}, "")
		assert.Empty(t, got.Direction)
		assert.Equal(t, "strategy", got.Cause)
	})
}
