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

// scenarioPrices reads a scenario's reviewed CSV.
func scenarioPrices(t *testing.T, scenario string) *marketData {
	t.Helper()
	prices, err := loadBars(filepath.Join("testdata", "emax", scenario,
		"data", "synthetic", "canonical", "EURUSD", "2010", "01", "EURUSD-2010-01-D1.csv"))
	require.NoError(t, err)
	return prices
}

func scenarioBars(t *testing.T, scenario string) iter.Seq2[bar, error] {
	t.Helper()
	return scenarioPrices(t, scenario).bars(context.Background())
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
	f := emaxFinal{
		balance: r.account.balance, unrealized: unrealized, equity: equity, used: used, reserved: reserved,
		signal: r.emax.pos, pending: r.emax.pending,
	}
	if p := r.account.positions["EURUSD"]; p != nil {
		held := *p // a copy: the account keeps marking its own
		f.position = &held
	}
	return f
}

// barState is what the run looks like once a bar is fully processed, the
// strategy's reaction included.
type barState struct {
	at        time.Time
	state     emaxFinal
	waiting   []string // order ids, in queue order
	completed []string // order ids, in the order they ended
}

func orderIDs(orders []*order) []string {
	var ids []string
	for _, o := range orders {
		ids = append(ids, o.id)
	}
	return ids
}

// watchEMAX takes the bars through the run and returns its state after each one.
// yield returns only once the backtest has processed the bar, so no production
// callback is needed to see between bars.
func watchEMAX(t *testing.T, r emaxRun, bars iter.Seq2[bar, error]) []barState {
	t.Helper()
	var states []barState
	r.run(t, func(yield func(bar, error) bool) {
		for b, err := range bars {
			if !yield(b, err) {
				return
			}
			states = append(states, barState{
				at:        b.Time,
				state:     finalState(t, r),
				waiting:   orderIDs(r.broker.orders["EURUSD"]),
				completed: orderIDs(r.broker.completed),
			})
		}
	})
	return states
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

// Scenario C's reversal, seen between bars: never long and short at once, and flat
// on the bar the close fills.
func TestEMAXBacktest_BarsCanBeWrappedToWatchStateBetweenBars(t *testing.T) {
	var sides []direction
	for _, st := range watchEMAX(t, newEMAXRun(t, usd(10000)), scenarioBars(t, "c-reversal")) {
		side := noDirection
		if st.state.position != nil {
			side = st.state.position.direction
		}
		sides = append(sides, side)
	}

	none := noDirection
	assert.Equal(t, []direction{none, none, none, none, long, long, long, long, long, none, short, short}, sides)
}

// Scenario A, bar by bar, from emax-validation.org. The money here is worked by
// hand from the rules, independently of the page's generated end state: unrealized
// is (close - 1.10323) * 9066 floored to the cent, used margin is 9066 * close * 2%
// rounded up, and reserved margin is units * signal close * 2% rounded up.
func TestEMAXBacktest_ScenarioAProfitableLongBarByBar(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	states := watchEMAX(t, r, scenarioBars(t, "a-reversal-exit"))

	holding := func(mark price, unrealized, used money) emaxFinal {
		return emaxFinal{balance: 1000000, unrealized: unrealized, equity: 1000000 + unrealized, used: used, signal: long,
			position: &position{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8), markPrice: mark}}
	}
	afterCrossover := holding(110700, 3417, 20073)
	afterCrossover.signal, afterCrossover.pending = noDirection, short

	flat := emaxFinal{balance: 1000000, equity: 1000000}
	assert.Equal(t, []barState{
		{at: jan(4), state: flat},
		{at: jan(5), state: flat},
		{at: jan(6), state: flat},
		// First warm bar, EMA(2) above EMA(4): the long is sized from this close.
		{at: jan(7), waiting: []string{"sim-1"},
			state: emaxFinal{balance: 1000000, equity: 1000000, reserved: 20000, signal: long}},
		// Fills at this bar's open and places its stop.
		{at: jan(8), waiting: []string{"sim-2"}, completed: []string{"sim-1"}, state: holding(110600, 2511, 20054)},
		// The low is one point above the stop: still open.
		{at: jan(11), waiting: []string{"sim-2"}, completed: []string{"sim-1"}, state: holding(110950, 5684, 20118)},
		{at: jan(12), waiting: []string{"sim-2"}, completed: []string{"sim-1"}, state: holding(111250, 8404, 20172)},
		{at: jan(13), waiting: []string{"sim-2"}, completed: []string{"sim-1"}, state: holding(111700, 12483, 20254)},
		// Bearish crossover: the close is only submitted, and the long stays open.
		{at: jan(14), waiting: []string{"sim-2", "sim-3"}, completed: []string{"sim-1"}, state: afterCrossover},
		// The close fills at the open and cancels the stop, so sim-3 ends before
		// sim-2. The short is sized from the new balance and left waiting.
		{at: jan(15), waiting: []string{"sim-4"}, completed: []string{"sim-1", "sim-3", "sim-2"},
			state: emaxFinal{balance: 1003300, equity: 1003300, reserved: 20074, signal: short}},
	}, states)
}

// Every step of every order, so the signal, submission and fill bars are told
// apart: an order's Time moves when it fills, its history does not.
func TestEMAXBacktest_ScenarioAOrderHistories(t *testing.T) {
	r := runEMAX(t, "a-reversal-exit")

	all := slices.Concat(r.broker.completed, r.broker.orders["EURUSD"])
	histories := make(map[string][]orderEvent, len(all))
	for _, o := range all {
		histories[o.id] = o.history
	}
	assert.Equal(t, map[string][]orderEvent{
		"sim-1": {
			{state: stateRequested, time: jan(7)},
			{state: stateSized, time: jan(7)},
			{state: stateSubmitted, time: jan(7)},
			{state: stateFilled, time: jan(8)},
		},
		"sim-2": {
			{state: stateRequested, time: jan(8)},
			{state: stateSubmitted, time: jan(8)},
			{state: stateCancelled, time: jan(15), reason: "position closed"},
		},
		"sim-3": {
			{state: stateRequested, time: jan(14)},
			{state: stateSized, time: jan(14)},
			{state: stateSubmitted, time: jan(14)},
			{state: stateFilled, time: jan(15)},
		},
		"sim-4": {
			{state: stateRequested, time: jan(15)},
			{state: stateSized, time: jan(15)},
			{state: stateSubmitted, time: jan(15)},
		},
	}, histories)
}

// The EMAs the signals come from, against the page's exact fractions in points.
// The engine's are floats, so they are compared within far less than the 26 points
// by which the crossover bar separates them.
func TestEMAXBacktest_ScenarioAEMAs(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	bars := scenarioBars(t, "a-reversal-exit")
	exact := []struct{ short, long float64 }{
		{110250, 110150},
		{331450.0 / 3, 110330},
		{997150.0 / 9, 110578},
		{2999650.0 / 27, 554234.0 / 5},
		{9031450.0 / 81, 2779702.0 / 25},
		{26964850.0 / 243, 13874106.0 / 125},
		{80643550.0 / 729, 69234818.0 / 625},
	}

	i := 0
	r.run(t, func(yield func(bar, error) bool) {
		for b, err := range bars {
			if !yield(b, err) {
				return
			}
			i++
			if i <= 3 {
				assert.Less(t, r.emax.emaLong.count, r.emax.long, "EMA(4) is not warm before the fourth bar")
				continue
			}
			want := exact[i-4]
			assert.InDelta(t, want.short/priceScale, r.emax.emaShort.value, 1e-9, "EMA(2) on bar %d", i-1)
			assert.InDelta(t, want.long/priceScale, r.emax.emaLong.value, 1e-9, "EMA(4) on bar %d", i-1)
		}
	})
	assert.Equal(t, len(exact)+3, i)
}

// The closed-trade record, the orders behind it and the account agree, each read
// from its own place. The expected P/L is 364 points * 9066 units = $33.00024,
// floored to $33.00; it is not derived from the balance.
func TestEMAXBacktest_ScenarioATradeAgreesWithOrdersAndAccount(t *testing.T) {
	r := runEMAX(t, "a-reversal-exit")

	require.Len(t, r.account.trades, 1)
	tr := r.account.trades[0]
	assert.Equal(t, money(3300), tr.pnl)
	assert.Equal(t, usd(10000)+tr.pnl, r.account.balance)

	require.Len(t, r.broker.completed, 3)
	entry, exit := r.broker.completed[0], r.broker.completed[1]
	assert.Equal(t, entry.fillPrice, tr.entryPrice)
	assert.Equal(t, entry.units, tr.units)
	assert.Equal(t, entry.Time, tr.entryTime)
	assert.Equal(t, exit.fillPrice, tr.exitPrice)
	assert.Equal(t, exit.units, tr.units)
	assert.Equal(t, exit.Time, tr.exitTime)
	assert.Equal(t, exit.reason, tr.reason)
	assert.Equal(t, long, tr.direction)
	assert.NotContains(t, r.account.positions, "EURUSD")
}

// The fixture itself keeps the boundaries the scenario is built on: an entry away
// from the signal close, and a low exactly one point above the stop.
func TestEMAXBacktest_ScenarioAFixtureHoldsItsBoundaries(t *testing.T) {
	prices := scenarioPrices(t, "a-reversal-exit")
	require.Len(t, prices.data, 10)

	assert.Equal(t, price(110300), prices.data[3].c, "signal close")
	assert.Equal(t, price(110323), prices.data[4].o, "entry open")
	assert.Equal(t, price(109197+1), prices.data[5].l, "one point above the stop")
	for i, b := range prices.data[4:] {
		assert.Greater(t, b.l, price(109197), "bar %d stays above the stop", i+4)
	}
}
