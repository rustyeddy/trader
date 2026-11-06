package main

import (
	"context"
	"iter"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emaxRun is what a finished backtest leaves behind, for tests to assert on.
type emaxRun struct {
	trader  *trader
	broker  *broker
	account *account
	emax    *emax
}

// newEMAXRun builds fresh instances: real EMAX(2/4) on a real trader, broker and
// account.
func newEMAXRun(t *testing.T, balance money) emaxRun {
	t.Helper()
	captureLogs(t)
	e := &emax{id: "emax", short: 2, long: 4, stopPct: rateOnePercent}
	require.NoError(t, e.init(nil))
	br := newBroker("sim")
	tr := &trader{broker: br, account: br.addAccount("sim123", balance), strategy: e}
	return emaxRun{trader: tr, broker: br, account: tr.account, emax: e}
}

// scenarioBars is a scenario's bars, read from its reviewed CSV.
func scenarioBars(t *testing.T, scenario string) iter.Seq2[bar, error] {
	t.Helper()
	prices, err := loadBars(filepath.Join("testdata", "emax", scenario,
		"data", "synthetic", "canonical", "EURUSD", "2010", "01", "EURUSD-2010-01-D1.csv"))
	require.NoError(t, err)
	return prices.bars(context.Background())
}

// run takes the bars through the real backtest.
func (r emaxRun) run(t *testing.T, bars iter.Seq2[bar, error]) {
	t.Helper()
	require.NoError(t, (&backtest{}).run(context.Background(), bars, r.trader))
}

func runEMAX(t *testing.T, scenario string) emaxRun {
	t.Helper()
	return runEMAXWithBalance(t, scenario, usd(10000))
}

func runEMAXWithBalance(t *testing.T, scenario string, balance money) emaxRun {
	t.Helper()
	r := newEMAXRun(t, balance)
	r.run(t, scenarioBars(t, scenario))
	return r
}

// orderResult is the part of an order the expected tables pin. at is when the
// order last moved: when it ended if it completed, when it was made if it waits.
type orderResult struct {
	id        string
	action    action
	direction direction
	orderType orderType
	reason    orderReason
	state     orderState
	price     price
	stopLoss  price
	units     int64
	fillPrice price
	at        time.Time
	why       string // the reason on the last history event
}

func orderResults(orders []*order) []orderResult {
	out := make([]orderResult, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderResult{
			id: o.id, action: o.action, direction: o.direction, orderType: o.orderType,
			reason: o.reason, state: o.state, price: o.price, stopLoss: o.stopLoss,
			units: o.units, fillPrice: o.fillPrice, at: o.Time,
			why: o.history[len(o.history)-1].reason,
		})
	}
	return out
}

// emaxFinal is the state at the end of the data.
type emaxFinal struct {
	balance, unrealized, equity, used, reserved money
	signal, pending                             direction
	position                                    *position
}

func finalState(t *testing.T, r emaxRun) emaxFinal {
	t.Helper()
	unrealized, err := r.account.unrealizedPnL()
	require.NoError(t, err)
	equity, err := r.account.equity()
	require.NoError(t, err)
	used, err := r.account.usedMargin()
	require.NoError(t, err)
	reserved, err := r.broker.reservedMargin(r.account.id)
	require.NoError(t, err)
	return emaxFinal{
		balance: r.account.balance, unrealized: unrealized, equity: equity, used: used, reserved: reserved,
		signal: r.emax.pos, pending: r.emax.pending, position: r.account.positions["EURUSD"],
	}
}

// jan is a bar time: the fixtures are January 2010 weekdays, bar 0 on the 4th.
func jan(d int) time.Time { return time.Date(2010, time.January, d, 0, 0, 0, 0, time.UTC) }

// The approved results from emax-validation.org. They are written out here
// rather than computed, in points and cents, so a change to the engine or the
// bars cannot change what the test expects without review.
var (
	longEntry = orderResult{id: "sim-1", action: open, direction: long, orderType: marketOrder, state: stateFilled,
		price: 110300, stopLoss: 109197, units: 9066, fillPrice: 110323, at: jan(8)}
	longStop = orderResult{id: "sim-2", action: close, direction: long, orderType: stopOrder, reason: reasonStopLoss,
		price: 109197}
	reversalClose = orderResult{id: "sim-3", action: close, orderType: marketOrder, state: stateFilled,
		price: 110700, units: 9066, fillPrice: 110687, at: jan(15)}
	stopCancelled = withEnd(longStop, stateCancelled, 0, 0, jan(15), "position closed")
	profitTrade   = trade{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8),
		exitPrice: 110687, exitTime: jan(15), reason: reasonStrategy, pnl: 3300}
)

// withEnd is an order's result once it has ended.
func withEnd(o orderResult, s orderState, units int64, fill price, at time.Time, why string) orderResult {
	o.state, o.units, o.fillPrice, o.at, o.why = s, units, fill, at, why
	return o
}

// waiting is an order's result while it is still submitted.
func waiting(o orderResult, at time.Time) orderResult {
	return withEnd(o, stateSubmitted, o.units, 0, at, "")
}

var emaxScenarios = []struct {
	name      string
	trades    []trade
	completed []orderResult
	waiting   []orderResult
	final     emaxFinal
}{
	{
		name:      "a-reversal-exit",
		trades:    []trade{profitTrade},
		completed: []orderResult{longEntry, reversalClose, stopCancelled},
		waiting: []orderResult{waiting(orderResult{id: "sim-4", action: open, direction: short, orderType: marketOrder,
			price: 110450, stopLoss: 111554, units: 9087}, jan(15))},
		final: emaxFinal{balance: 1003300, equity: 1003300, reserved: 20074, signal: short},
	},
	{
		name: "b1-stop-wick",
		trades: []trade{{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8),
			exitPrice: 109197, exitTime: jan(11), reason: reasonStopLoss, pnl: -10209}},
		completed: []orderResult{longEntry, withEnd(longStop, stateFilled, 9066, 109197, jan(11), "")},
		final:     emaxFinal{balance: 989791, equity: 989791, signal: long},
	},
	{
		name: "b2-stop-cross",
		trades: []trade{{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8),
			exitPrice: 109197, exitTime: jan(11), reason: reasonStopLoss, pnl: -10209}},
		completed: []orderResult{
			longEntry,
			withEnd(longStop, stateFilled, 9066, 109197, jan(11), ""),
			{id: "sim-3", action: close, orderType: marketOrder, state: stateRejected, price: 108700, at: jan(12),
				why: "no position to close: EURUSD"},
			{id: "sim-4", action: open, direction: short, orderType: marketOrder, state: stateFilled,
				price: 108500, stopLoss: 109585, units: 9122, fillPrice: 108490, at: jan(13)},
		},
		waiting: []orderResult{waiting(orderResult{id: "sim-5", action: close, direction: short, orderType: stopOrder,
			reason: reasonStopLoss, price: 109585}, jan(13))},
		final: emaxFinal{balance: 989791, unrealized: 3557, equity: 993348, used: 19722, signal: short,
			position: &position{instrument: "EURUSD", direction: short, units: 9122, entryPrice: 108490, entryTime: jan(13), markPrice: 108100}},
	},
	{
		name: "b3-stop-gap",
		trades: []trade{{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8),
			exitPrice: 108900, exitTime: jan(11), reason: reasonStopLoss, pnl: -12901}},
		completed: []orderResult{longEntry, withEnd(longStop, stateFilled, 9066, 108900, jan(11), "")},
		final:     emaxFinal{balance: 987099, equity: 987099, signal: long},
	},
	{
		name:   "c-reversal",
		trades: []trade{profitTrade},
		completed: []orderResult{
			longEntry, reversalClose, stopCancelled,
			{id: "sim-4", action: open, direction: short, orderType: marketOrder, state: stateFilled,
				price: 110450, stopLoss: 111554, units: 9087, fillPrice: 110437, at: jan(18)},
		},
		waiting: []orderResult{waiting(orderResult{id: "sim-5", action: close, direction: short, orderType: stopOrder,
			reason: reasonStopLoss, price: 111554}, jan(18))},
		final: emaxFinal{balance: 1003300, unrealized: 4243, equity: 1007543, used: 19986, signal: short,
			position: &position{instrument: "EURUSD", direction: short, units: 9087, entryPrice: 110437, entryTime: jan(18), markPrice: 109970}},
	},
	{
		name: "d-short-stop",
		trades: []trade{{instrument: "EURUSD", direction: short, units: 9115, entryPrice: 109680, entryTime: jan(8),
			exitPrice: 110797, exitTime: jan(11), reason: reasonStopLoss, pnl: -10182}},
		completed: []orderResult{
			{id: "sim-1", action: open, direction: short, orderType: marketOrder, state: stateFilled,
				price: 109700, stopLoss: 110797, units: 9115, fillPrice: 109680, at: jan(8)},
			{id: "sim-2", action: close, direction: short, orderType: stopOrder, reason: reasonStopLoss, state: stateFilled,
				price: 110797, units: 9115, fillPrice: 110797, at: jan(11)},
		},
		final: emaxFinal{balance: 989818, equity: 989818, signal: short},
	},
}

func TestEMAXBacktest_MatchesApprovedResults(t *testing.T) {
	for _, want := range emaxScenarios {
		t.Run(want.name, func(t *testing.T) {
			r := runEMAX(t, want.name)

			assert.Equal(t, want.trades, r.account.trades)
			assert.Equal(t, want.completed, orderResults(r.broker.completed))
			assert.Equal(t, orderResults(nil), orderResults(r.trader.rejected), "no scenario is refused before the broker's queue")
			assert.Equal(t, want.waiting, nilIfEmpty(orderResults(r.broker.orders["EURUSD"])))
			assert.Equal(t, want.final, finalState(t, r))

			var realized money
			for _, tr := range want.trades {
				realized += tr.pnl
			}
			assert.Equal(t, usd(10000)+realized, r.account.balance, "the balance moves only by the trades' P/L")
		})
	}
}

// nilIfEmpty lets a scenario with nothing waiting leave the field out.
func nilIfEmpty(results []orderResult) []orderResult {
	if len(results) == 0 {
		return nil
	}
	return results
}

func TestEMAXBacktest_RepeatsExactlyFromFreshInstances(t *testing.T) {
	for _, s := range emaxScenarios {
		t.Run(s.name, func(t *testing.T) {
			first, second := runEMAX(t, s.name), runEMAX(t, s.name)

			assert.Equal(t, first.account.trades, second.account.trades)
			assert.True(t, slices.Equal(orderResults(first.broker.completed), orderResults(second.broker.completed)))
			assert.Equal(t, finalState(t, first), finalState(t, second))
		})
	}
}

// With one cent the risk budget sizes every open to zero units, so EMAX's opens
// never reach the broker's queue. Its close does, and is rejected at the fill,
// so each refusal is kept in exactly one place. Worked by hand: the budget is
// $0.0001 against a stop 1103 or 1104 points away.
func TestEMAXBacktest_OpensRefusedAtSizingStayWithTheTrader(t *testing.T) {
	r := runEMAXWithBalance(t, "a-reversal-exit", 1)

	refused := func(d direction, p, stop price, at time.Time) orderResult {
		return orderResult{action: open, direction: d, orderType: marketOrder, state: stateRejected,
			price: p, stopLoss: stop, at: at, why: "order size below the minimum: 0 units, minimum 1"}
	}
	assert.Equal(t, []orderResult{
		refused(long, 110300, 109197, jan(7)),
		refused(short, 110450, 111554, jan(15)),
	}, orderResults(r.trader.rejected))
	assert.Equal(t, []orderResult{
		{id: "sim-1", action: close, orderType: marketOrder, state: stateRejected, price: 110700, at: jan(15),
			why: "no position to close: EURUSD"},
	}, orderResults(r.broker.completed))
	assert.Empty(t, r.broker.orders["EURUSD"])
	assert.Empty(t, r.account.trades)
	assert.Equal(t, emaxFinal{balance: 1, equity: 1, signal: short}, finalState(t, r), "the signal state moves on regardless")
}

// A wrapper around the bars sees the run between bars, because yield returns
// only once the bar has been processed. Here it follows scenario C's reversal:
// never long and short at once, and flat on the bar the close fills.
func TestEMAXBacktest_BarsCanBeWrappedToWatchStateBetweenBars(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	bars := scenarioBars(t, "c-reversal")

	var sides []direction
	r.run(t, func(yield func(bar, error) bool) {
		for b, err := range bars {
			if !yield(b, err) {
				return
			}
			side := noDirection
			if p := r.account.positions["EURUSD"]; p != nil {
				side = p.direction
			}
			sides = append(sides, side)
		}
	})

	none := noDirection
	assert.Equal(t, []direction{none, none, none, none, long, long, long, long, long, none, short, short}, sides)
}
