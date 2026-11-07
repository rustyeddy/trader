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

// emaxState is the account, margin and signal state at one moment in a run.
type emaxState struct {
	balance, unrealized, equity, used, reserved money
	signal, pending                             direction
	position                                    *position
}

func snapshotState(t *testing.T, r emaxRun) emaxState {
	t.Helper()
	unrealized, err := r.account.unrealizedPnL()
	require.NoError(t, err)
	equity, err := r.account.equity()
	require.NoError(t, err)
	used, err := r.account.usedMargin()
	require.NoError(t, err)
	reserved, err := r.broker.reservedMargin(r.account.id)
	require.NoError(t, err)
	f := emaxState{
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
	state     emaxState
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
				state:     snapshotState(t, r),
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
	final     emaxState
}{
	{
		name:      "a-reversal-exit",
		trades:    []trade{profitTrade},
		completed: []orderResult{longEntry, reversalClose, stopCancelled},
		waiting: []orderResult{waiting(orderResult{id: "sim-4", action: open, direction: short, orderType: marketOrder,
			price: 110450, stopLoss: 111554, units: 9087}, jan(15))},
		final: emaxState{balance: 1003300, equity: 1003300, reserved: 20074, signal: short},
	},
	{
		name: "b1-stop-wick",
		trades: []trade{{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8),
			exitPrice: 109197, exitTime: jan(11), reason: reasonStopLoss, pnl: -10209}},
		completed: []orderResult{longEntry, withEnd(longStop, stateFilled, 9066, 109197, jan(11), "")},
		final:     emaxState{balance: 989791, equity: 989791, signal: long},
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
		final: emaxState{balance: 989791, unrealized: 3557, equity: 993348, used: 19722, signal: short,
			position: &position{instrument: "EURUSD", direction: short, units: 9122, entryPrice: 108490, entryTime: jan(13), markPrice: 108100}},
	},
	{
		name: "b3-stop-gap",
		trades: []trade{{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8),
			exitPrice: 108900, exitTime: jan(11), reason: reasonStopLoss, pnl: -12901}},
		completed: []orderResult{longEntry, withEnd(longStop, stateFilled, 9066, 108900, jan(11), "")},
		final:     emaxState{balance: 987099, equity: 987099, signal: long},
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
		final: emaxState{balance: 1003300, unrealized: 4243, equity: 1007543, used: 19986, signal: short,
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
		final: emaxState{balance: 989818, equity: 989818, signal: short},
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
			assert.Equal(t, want.final, snapshotState(t, r))

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
			assert.Equal(t, snapshotState(t, first), snapshotState(t, second))
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
	assert.Equal(t, emaxState{balance: 1, equity: 1, signal: short}, snapshotState(t, r), "the signal state moves on regardless")
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

	holding := func(mark price, unrealized, used money) emaxState {
		return emaxState{balance: 1000000, unrealized: unrealized, equity: 1000000 + unrealized, used: used, signal: long,
			position: &position{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8), markPrice: mark}}
	}
	afterCrossover := holding(110700, 3417, 20073)
	afterCrossover.signal, afterCrossover.pending = noDirection, short

	flat := emaxState{balance: 1000000, equity: 1000000}
	assert.Equal(t, []barState{
		{at: jan(4), state: flat},
		{at: jan(5), state: flat},
		{at: jan(6), state: flat},
		// First warm bar, EMA(2) above EMA(4): the long is sized from this close.
		{at: jan(7), waiting: []string{"sim-1"},
			state: emaxState{balance: 1000000, equity: 1000000, reserved: 20000, signal: long}},
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
			state: emaxState{balance: 1003300, equity: 1003300, reserved: 20074, signal: short}},
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

// assertEMAs compares the EMAs after each bar with the page's exact fractions in
// points, from the first warm bar on. The engine's are floats, so they are compared
// within far less than the fixture rule's 5 points between them.
func assertEMAs(t *testing.T, scenario string, exact []struct{ short, long float64 }) {
	t.Helper()
	r := newEMAXRun(t, usd(10000))
	bars := scenarioBars(t, scenario)

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

func TestEMAXBacktest_ScenarioAEMAs(t *testing.T) {
	assertEMAs(t, "a-reversal-exit", []struct{ short, long float64 }{
		{110250, 110150},
		{331450.0 / 3, 110330},
		{997150.0 / 9, 110578},
		{2999650.0 / 27, 554234.0 / 5},
		{9031450.0 / 81, 2779702.0 / 25},
		{26964850.0 / 243, 13874106.0 / 125},
		{80643550.0 / 729, 69234818.0 / 625},
	})
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

// entryBarStates is the bars the stop-out scenarios share, through the long's fill
// on the 8th. Unrealized is 277 points * 9066 units = $25.11282, floored; used
// margin is 9066 * 1.106 * 2% = $200.53992, rounded up; the reserve on the 7th is
// 9066 * 1.103 * 2% = $199.99596, rounded up.
func entryBarStates() []barState {
	flat := emaxState{balance: 1000000, equity: 1000000}
	return []barState{
		{at: jan(4), state: flat},
		{at: jan(5), state: flat},
		{at: jan(6), state: flat},
		{at: jan(7), waiting: []string{"sim-1"},
			state: emaxState{balance: 1000000, equity: 1000000, reserved: 20000, signal: long}},
		{at: jan(8), waiting: []string{"sim-2"}, completed: []string{"sim-1"},
			state: emaxState{balance: 1000000, unrealized: 2511, equity: 1002511, used: 20054, signal: long,
				position: &position{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8), markPrice: 110600}}},
	}
}

// The stopped position closes during its bar, so that bar's close never marks it.
// Afterwards EMAX's signal state still says long, which is the known contract
// (rule 11): with no crossover it stays flat and emits nothing, so there is no
// retry and no second position.
func TestEMAXBacktest_StopOutWithoutACrossoverStaysFlatBarByBar(t *testing.T) {
	stopped := func(balance money) emaxState {
		return emaxState{balance: balance, equity: balance, signal: long}
	}
	completed := []string{"sim-1", "sim-2"}
	for _, tc := range []struct {
		name  string
		after []barState
	}{
		{"b1-stop-wick", []barState{
			{at: jan(11), completed: completed, state: stopped(989791)},
			{at: jan(12), completed: completed, state: stopped(989791)},
			{at: jan(13), completed: completed, state: stopped(989791)},
		}},
		{"b3-stop-gap", []barState{
			{at: jan(11), completed: completed, state: stopped(987099)},
			{at: jan(12), completed: completed, state: stopped(987099)},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newEMAXRun(t, usd(10000))
			states := watchEMAX(t, r, scenarioBars(t, tc.name))

			assert.Equal(t, slices.Concat(entryBarStates(), tc.after), states)
			assert.Empty(t, r.trader.rejected)
		})
	}
}

// The stop fills in the bar's range, then EMAX sees the same bar's crossover and
// asks to close a position that is already gone. The rejection is expected and the
// run continues: the short follows, sized from the balance after the loss. Worked by
// hand: $98.9791 / 0.01085 is 9122 units; the reserve is 9122 * 1.085 * 2% = $197.9474;
// on the 13th unrealized is 190 points * 9122 = $17.3318 and margin 9122 * 1.083 * 2%
// = $197.58252; on the 14th they are 390 points = $35.5758 and $197.21764.
func TestEMAXBacktest_ScenarioB2StopThenRejectedCloseThenShortBarByBar(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	states := watchEMAX(t, r, scenarioBars(t, "b2-stop-cross"))

	holdingShort := func(mark price, unrealized, used money) emaxState {
		return emaxState{balance: 989791, unrealized: unrealized, equity: 989791 + unrealized, used: used, signal: short,
			position: &position{instrument: "EURUSD", direction: short, units: 9122, entryPrice: 108490, entryTime: jan(13), markPrice: mark}}
	}
	assert.Equal(t, slices.Concat(entryBarStates(), []barState{
		{at: jan(11), waiting: []string{"sim-3"}, completed: []string{"sim-1", "sim-2"},
			state: emaxState{balance: 989791, equity: 989791, pending: short}},
		{at: jan(12), waiting: []string{"sim-4"}, completed: []string{"sim-1", "sim-2", "sim-3"},
			state: emaxState{balance: 989791, equity: 989791, reserved: 19795, signal: short}},
		{at: jan(13), waiting: []string{"sim-5"}, completed: []string{"sim-1", "sim-2", "sim-3", "sim-4"}, state: holdingShort(108300, 1733, 19759)},
		{at: jan(14), waiting: []string{"sim-5"}, completed: []string{"sim-1", "sim-2", "sim-3", "sim-4"}, state: holdingShort(108100, 3557, 19722)},
	}), states)

	assert.Empty(t, r.trader.rejected, "the close was refused at the fill, so the broker keeps it")
	require.Len(t, r.broker.completed, 4)
	rejectedClose := r.broker.completed[2]
	assert.Equal(t, stateRejected, rejectedClose.state)
	assert.Zero(t, rejectedClose.fillPrice)
	assert.Zero(t, rejectedClose.units)
}

// positionWatcher wraps the real strategy and records the position on the account at
// the moment each bar reaches it.
type positionWatcher struct {
	strategy
	account *account
	sides   []direction
}

func (w *positionWatcher) onBar(ctx context.Context, b bar) *order {
	side := noDirection
	if p := w.account.positions[b.instrument]; p != nil {
		side = p.direction
	}
	w.sides = append(w.sides, side)
	return w.strategy.onBar(ctx, b)
}

// Snapshots taken after a bar cannot show the order within it. Here the broker's stop
// has already closed the long when EMAX sees the crossover on the 11th, which is why
// its close finds nothing to close.
func TestEMAXBacktest_ScenarioB2BrokerStopsOutBeforeEMAXSeesTheBar(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	watcher := &positionWatcher{strategy: r.emax, account: r.account}
	r.trader.strategy = watcher

	r.run(t, scenarioBars(t, "b2-stop-cross"))

	none := noDirection
	assert.Equal(t, []direction{none, none, none, none, long, none, none, short, short}, watcher.sides)
}

// Every step of the stop-out orders. The stop has no sized step: the broker makes it
// from the filled open.
func TestEMAXBacktest_StopOutOrderHistories(t *testing.T) {
	entry := []orderEvent{
		{state: stateRequested, time: jan(7)},
		{state: stateSized, time: jan(7)},
		{state: stateSubmitted, time: jan(7)},
		{state: stateFilled, time: jan(8)},
	}
	stop := []orderEvent{
		{state: stateRequested, time: jan(8)},
		{state: stateSubmitted, time: jan(8)},
		{state: stateFilled, time: jan(11)},
	}
	for _, tc := range []struct {
		name string
		want map[string][]orderEvent
	}{
		{"b1-stop-wick", map[string][]orderEvent{"sim-1": entry, "sim-2": stop}},
		{"b3-stop-gap", map[string][]orderEvent{"sim-1": entry, "sim-2": stop}},
		{"b2-stop-cross", map[string][]orderEvent{
			"sim-1": entry,
			"sim-2": stop,
			"sim-3": {
				{state: stateRequested, time: jan(11)},
				{state: stateSized, time: jan(11)},
				{state: stateSubmitted, time: jan(11)},
				{state: stateRejected, time: jan(12), reason: "no position to close: EURUSD"},
			},
			"sim-4": {
				{state: stateRequested, time: jan(12)},
				{state: stateSized, time: jan(12)},
				{state: stateSubmitted, time: jan(12)},
				{state: stateFilled, time: jan(13)},
			},
			"sim-5": {
				{state: stateRequested, time: jan(13)},
				{state: stateSubmitted, time: jan(13)},
			},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runEMAX(t, tc.name)

			got := make(map[string][]orderEvent)
			for _, o := range slices.Concat(r.broker.completed, r.broker.orders["EURUSD"]) {
				got[o.id] = o.history
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// Each loss is calculated from its fill, then compared with the trade, its orders and
// the balance: -1126 points * 9066 units = -$102.08316, floored to -$102.09, and -1423
// points * 9066 units = -$129.00918, floored to -$129.01. 1% is planned risk, not a
// cap: the budget is $100.00 and both losses are larger, because the entry was past
// the signal close and, in B3, the stop filled at the gap's open.
func TestEMAXBacktest_StopLossIsExactAndExceedsThePlannedRisk(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit price
		pnl  money
	}{
		{"b1-stop-wick", 109197, -10209},
		{"b2-stop-cross", 109197, -10209},
		{"b3-stop-gap", 108900, -12901},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runEMAX(t, tc.name)

			require.Len(t, r.account.trades, 1, "no retry or second closed position")
			tr := r.account.trades[0]
			assert.Equal(t, tc.pnl, tr.pnl)
			assert.Equal(t, usd(10000)+tc.pnl, r.account.balance)
			assert.Less(t, tr.pnl, -usd(100), "worse than the 1% budget")

			entry, stop := r.broker.completed[0], r.broker.completed[1]
			assert.Equal(t, reasonStopLoss, tr.reason)
			assert.Equal(t, reasonStopLoss, stop.reason)
			assert.Equal(t, stateFilled, stop.state)
			assert.Equal(t, tc.exit, stop.fillPrice)
			assert.Equal(t, stop.fillPrice, tr.exitPrice)
			assert.Equal(t, stop.Time, tr.exitTime)
			assert.Equal(t, entry.fillPrice, tr.entryPrice)
			assert.Equal(t, entry.units, tr.units)
			assert.Equal(t, entry.units, stop.units)
		})
	}
}

func TestEMAXBacktest_StopOutEMAs(t *testing.T) {
	warm := []struct{ short, long float64 }{{110250, 110150}, {331450.0 / 3, 110330}}
	for name, rest := range map[string][]struct{ short, long float64 }{
		"b1-stop-wick": {
			{997150.0 / 9, 110578},
			{2999650.0 / 27, 554234.0 / 5},
			{9023350.0 / 81, 2778202.0 / 25},
		},
		"b2-stop-cross": {
			{983650.0 / 9, 109678},
			{2936650.0 / 27, 546034.0 / 5},
			{8784850.0 / 81, 2721102.0 / 25},
			{26297050.0 / 243, 13568306.0 / 125},
		},
		"b3-stop-gap": {
			{996250.0 / 9, 110518},
			{2996050.0 / 27, 553754.0 / 5},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assertEMAs(t, name, slices.Concat(warm, rest))
		})
	}
}

// The fixtures keep the boundaries each stop-out is built on: an exact touch that
// closes above the stop, a bar that passes the stop and closes through the crossover,
// and an open already past the stop on a bar that recovers.
func TestEMAXBacktest_StopOutFixturesHoldTheirBoundaries(t *testing.T) {
	const stop = price(109197)

	b1 := scenarioPrices(t, "b1-stop-wick").data[5]
	assert.Equal(t, stop, b1.l, "B1's low touches the stop exactly")
	assert.Greater(t, b1.o, stop)
	assert.Greater(t, b1.c, stop)

	b2 := scenarioPrices(t, "b2-stop-cross").data[5]
	assert.Greater(t, b2.o, stop)
	assert.Less(t, b2.l, stop, "B2's low goes past the stop")
	assert.Less(t, b2.c, stop)

	b3 := scenarioPrices(t, "b3-stop-gap").data[5]
	assert.Less(t, b3.o, stop, "B3's open is already past the stop")
	assert.Greater(t, b3.c, stop, "and the bar recovers")
}
