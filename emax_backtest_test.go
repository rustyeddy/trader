package trader

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"
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
	require.NoError(t, e.init())
	br := newBroker("sim")
	tr := &trader{broker: br, account: br.addAccount("sim123", balance), strategy: e}
	return emaxRun{trader: tr, broker: br, account: tr.account, emax: e}
}

// scenarioPrices reads a scenario's reviewed CSV.
func scenarioPrices(t *testing.T, scenario string) *marketData {
	t.Helper()
	prices, err := loadBars(scenarioPath(scenario))
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

func TestEMAX_SkipsUnrepresentableStopsWithoutRetrying(t *testing.T) {
	for _, tt := range []struct {
		name       string
		closes     []int // whole-dollar prices, all within the parser's bounds
		skippedBar int
		filled     int
	}{
		{"initial short", []int{10000, 9999, 9998, 9997, 9900}, 3, 0},
		{"reversal short", []int{9990, 9991, 9992, 9993, 9994, 9990, 9990, 9800}, 6, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var csv strings.Builder
			csv.WriteString("# schema=design-bars-v1 instrument=EURUSD tf=d1\ntime,open,high,low,close,volume\n")
			for i, p := range tt.closes {
				fmt.Fprintf(&csv, "%s,%d.00000,%d.00000,%d.00000,%d.00000,1000\n",
					testStart.AddDate(0, 0, i).Format(barTimeLayout), p, p, p, p)
			}
			prices, err := readBars(strings.NewReader(csv.String()))
			require.NoError(t, err)
			r := newEMAXRun(t, usd(10000))
			states := watchEMAX(t, r, prices.bars(context.Background()))
			require.Len(t, states, len(tt.closes), "the backtest processes every valid bar")
			for _, i := range []int{tt.skippedBar, tt.skippedBar + 1} {
				assert.Empty(t, states[i].waiting, "bar %d: no invalid entry or retry", i)
				assert.Nil(t, states[i].state.position)
				assert.Equal(t, short, states[i].state.signal)
				assert.Equal(t, noDirection, states[i].state.pending)
			}
			result := newResult("bounds", len(tt.closes), r.trader)
			assert.Equal(t, tt.filled, result.filled)
			assert.Zero(t, result.rejected, "an unsupported stop never reaches the trader")
			assert.Zero(t, result.pending)
		})
	}
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
	f := emaxState{
		balance: r.account.balance, unrealized: r.account.unrealizedPnL(), equity: r.account.equity(),
		used: r.account.usedMargin(), reserved: r.broker.reservedMargin(r.account.id),
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
			assert.Equal(t, orderResults(first.broker.orders["EURUSD"]), orderResults(second.broker.orders["EURUSD"]))
			assert.Equal(t, orderResults(first.trader.rejected), orderResults(second.trader.rejected))
			assert.Equal(t, orderHistories(first), orderHistories(second))
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

// reversalBarStates is the bars scenarios A and C share, through the close's fill on
// the 15th, from emax-validation.org. The money here is worked by hand from the
// rules, independently of the page's generated end state: unrealized is (close -
// 1.10323) * 9066 floored to the cent, used margin is 9066 * close * 2% rounded up,
// and reserved margin is units * signal close * 2% rounded up.
func reversalBarStates() []barState {
	holding := func(mark price, unrealized, used money) emaxState {
		return emaxState{balance: 1000000, unrealized: unrealized, equity: 1000000 + unrealized, used: used, signal: long,
			position: &position{instrument: "EURUSD", direction: long, units: 9066, entryPrice: 110323, entryTime: jan(8), markPrice: mark}}
	}
	afterCrossover := holding(110700, 3417, 20073)
	afterCrossover.signal, afterCrossover.pending = noDirection, short

	flat := emaxState{balance: 1000000, equity: 1000000}
	return []barState{
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
	}
}

func TestEMAXBacktest_ScenarioAProfitableLongBarByBar(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	assert.Equal(t, reversalBarStates(), watchEMAX(t, r, scenarioBars(t, "a-reversal-exit")))
}

// orderHistories is every order's history, by id, wherever the order now is.
func orderHistories(r emaxRun) map[string][]orderEvent {
	histories := make(map[string][]orderEvent)
	for _, o := range slices.Concat(r.broker.completed, r.broker.orders["EURUSD"]) {
		histories[o.id] = o.history
	}
	return histories
}

// reversalHistories is every step of the orders in scenarios A and C, through the
// 15th, so the signal, submission and fill bars are told apart: an order's Time moves
// when it fills, its history does not.
func reversalHistories() map[string][]orderEvent {
	return map[string][]orderEvent{
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
	}
}

func TestEMAXBacktest_ScenarioAOrderHistories(t *testing.T) {
	assert.Equal(t, reversalHistories(), orderHistories(runEMAX(t, "a-reversal-exit")))
}

// assertEMAs compares the EMAs after each bar with the page's exact fractions in
// points, from the first warm bar on. The engine's are floats, so they are compared
// within far less than the fixture rule's 5 points between them.
func assertEMAs(t *testing.T, scenario string, exact []struct{ short, long float64 }) {
	t.Helper()
	require.Len(t, scenarioPrices(t, scenario).data, len(exact)+3)
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
		{"d-short-stop", map[string][]orderEvent{"sim-1": entry, "sim-2": stop}},
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

			assert.Equal(t, tc.want, orderHistories(r))
		})
	}
}

// Each loss is calculated from its fill, then compared with the trade, its orders and
// the balance: -1126 points * 9066 units = -$102.08316, floored to -$102.09, -1423
// points * 9066 units = -$129.00918, floored to -$129.01, and D's short, 1.09680 -
// 1.10797 = -1117 points * 9115 units = -$101.81455, floored to -$101.82. 1% is planned
// risk, not a cap: the budget is $100.00 and every loss is larger, because the entry
// was past the signal close and, in B3, the stop filled at the gap's open.
func TestEMAXBacktest_StopLossIsExactAndExceedsThePlannedRisk(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit price
		pnl  money
	}{
		{"b1-stop-wick", 109197, -10209},
		{"b2-stop-cross", 109197, -10209},
		{"b3-stop-gap", 108900, -12901},
		{"d-short-stop", 110797, -10182},
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

// Scenario C is scenario A with two more bars, so the reversal plays out in full: flat
// on the 15th, short from the 18th, and never both. The short is sized from the balance
// after the close: $100.33 / 0.01104 is 9087 units, where the $100.00 from before it
// would give 9057. It fills at the open, which is not the signal close. Worked by hand:
// on the 18th unrealized is 217 points * 9087 = $19.71879 and margin 9087 * 1.1022 * 2%
// = $200.313828; on the 19th they are 467 points = $42.43629 and $199.859478. The data
// ends with the short open and its stop resting: nothing closes it.
func TestEMAXBacktest_ScenarioCFullReversalBarByBar(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	states := watchEMAX(t, r, scenarioBars(t, "c-reversal"))

	holdingShort := func(mark price, unrealized, used money) emaxState {
		return emaxState{balance: 1003300, unrealized: unrealized, equity: 1003300 + unrealized, used: used, signal: short,
			position: &position{instrument: "EURUSD", direction: short, units: 9087, entryPrice: 110437, entryTime: jan(18), markPrice: mark}}
	}
	completed := []string{"sim-1", "sim-3", "sim-2", "sim-4"}
	assert.Equal(t, slices.Concat(reversalBarStates(), []barState{
		{at: jan(18), waiting: []string{"sim-5"}, completed: completed, state: holdingShort(110220, 1971, 20032)},
		{at: jan(19), waiting: []string{"sim-5"}, completed: completed, state: holdingShort(109970, 4243, 19986)},
	}), states)
	assert.Len(t, r.account.trades, 1, "the open short is not a trade yet")
}

func TestEMAXBacktest_ScenarioCOrderHistories(t *testing.T) {
	want := reversalHistories()
	want["sim-4"] = append(want["sim-4"], orderEvent{state: stateFilled, time: jan(18)})
	want["sim-5"] = []orderEvent{
		{state: stateRequested, time: jan(18)},
		{state: stateSubmitted, time: jan(18)},
	}
	assert.Equal(t, want, orderHistories(runEMAX(t, "c-reversal")))
}

// Rule 10 pinned, not endorsed: EMAX opens its pending side without re-checking the
// trend. The bars follow scenario C through the crossover on the 14th, then jump back
// up, so the EMAs have crossed back by the time the short is emitted on the 15th. The
// short still goes in, and the bar it fills on reverses it again: a round trip the
// trend never asked for, which costs -$4.48 here. They are Go bars, not a scenario on
// the page, because only this behavior needs them.
//
// Worked by hand. On the 15th the EMAs are about 1.11655 and 1.11396. The short: $100.33
// / 0.0112 is 8958 units, reserve 8958 * 1.12 * 2% = $200.6592. On the 18th unrealized is
// -40 points * 8958 = -$3.5832, floored, and margin 8958 * 1.1205 * 2% = $200.74878. On
// the 19th it closes at -50 points * 8958 = -$4.479, floored. The long: $100.2852 /
// 0.01121 is 8946 units, reserve 8946 * 1.121 * 2% = $200.56932. On the 20th unrealized
// is 60 points * 8946 = $5.3676 and margin 8946 * 1.1218 * 2% = $200.712456.
func TestEMAXBacktest_PendingOpenIgnoresATrendThatCrossedBack(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	prices := scenarioPrices(t, "c-reversal")
	next := func(day int, o, h, l, c price) bar {
		return daily(bar{instrument: "EURUSD", Time: jan(day), ohlc: ohlc{o: o, h: h, l: l, c: c, v: 1000}})
	}
	prices.data = append(slices.Clone(prices.data[:9]),
		next(15, 110687, 112050, 110650, 112000),
		next(18, 112010, 112100, 111950, 112050),
		next(19, 112060, 112150, 112000, 112100),
		next(20, 112120, 112200, 112100, 112180),
	)

	bars := func(yield func(bar, error) bool) {
		for b, err := range prices.bars(context.Background()) {
			if !yield(b, err) {
				return
			}
			if b.Equal(jan(15)) {
				assert.Greater(t, r.emax.emaShort.value, r.emax.emaLong.value, "the trend is long again when the short goes in")
			}
		}
	}
	states := watchEMAX(t, r, bars)

	assert.Equal(t, []barState{
		{at: jan(15), waiting: []string{"sim-4"}, completed: []string{"sim-1", "sim-3", "sim-2"},
			state: emaxState{balance: 1003300, equity: 1003300, reserved: 20066, signal: short}},
		{at: jan(18), waiting: []string{"sim-5", "sim-6"}, completed: []string{"sim-1", "sim-3", "sim-2", "sim-4"},
			state: emaxState{balance: 1003300, unrealized: -359, equity: 1002941, used: 20075, pending: long,
				position: &position{instrument: "EURUSD", direction: short, units: 8958, entryPrice: 112010, entryTime: jan(18), markPrice: 112050}}},
		{at: jan(19), waiting: []string{"sim-7"}, completed: []string{"sim-1", "sim-3", "sim-2", "sim-4", "sim-6", "sim-5"},
			state: emaxState{balance: 1002852, equity: 1002852, reserved: 20057, signal: long}},
		{at: jan(20), waiting: []string{"sim-8"}, completed: []string{"sim-1", "sim-3", "sim-2", "sim-4", "sim-6", "sim-5", "sim-7"},
			state: emaxState{balance: 1002852, unrealized: 536, equity: 1003388, used: 20072, signal: long,
				position: &position{instrument: "EURUSD", direction: long, units: 8946, entryPrice: 112120, entryTime: jan(20), markPrice: 112180}}},
	}, states[9:])

	assert.Equal(t, []trade{
		profitTrade,
		{instrument: "EURUSD", direction: short, units: 8958, entryPrice: 112010, entryTime: jan(18),
			exitPrice: 112060, exitTime: jan(19), reason: reasonStrategy, pnl: -448},
	}, r.account.trades)
}

// The short's stop is above it, so the high is what fires it, and the fill is the stop
// price: not the high, and not the close far below. Afterwards EMAX's signal state still
// says short (rule 11), so it stays flat. Worked by hand: $100.00 / 0.01097 is 9115
// units, reserve 9115 * 1.097 * 2% = $199.9831; on the 8th unrealized is 280 points *
// 9115 = $25.522 and margin 9115 * 1.094 * 2% = $199.4362.
func TestEMAXBacktest_ScenarioDShortStoppedByTheHighBarByBar(t *testing.T) {
	r := newEMAXRun(t, usd(10000))
	states := watchEMAX(t, r, scenarioBars(t, "d-short-stop"))

	flat := emaxState{balance: 1000000, equity: 1000000}
	stopped := emaxState{balance: 989818, equity: 989818, signal: short}
	completed := []string{"sim-1", "sim-2"}
	assert.Equal(t, []barState{
		{at: jan(4), state: flat},
		{at: jan(5), state: flat},
		{at: jan(6), state: flat},
		{at: jan(7), waiting: []string{"sim-1"},
			state: emaxState{balance: 1000000, equity: 1000000, reserved: 19999, signal: short}},
		{at: jan(8), waiting: []string{"sim-2"}, completed: []string{"sim-1"},
			state: emaxState{balance: 1000000, unrealized: 2552, equity: 1002552, used: 19944, signal: short,
				position: &position{instrument: "EURUSD", direction: short, units: 9115, entryPrice: 109680, entryTime: jan(8), markPrice: 109400}}},
		{at: jan(11), completed: completed, state: stopped},
		{at: jan(12), completed: completed, state: stopped},
	}, states)
	assert.Empty(t, r.trader.rejected)
}

func TestEMAXBacktest_ReversalAndShortStopEMAs(t *testing.T) {
	assertEMAs(t, "c-reversal", []struct{ short, long float64 }{
		{110250, 110150},
		{331450.0 / 3, 110330},
		{997150.0 / 9, 110578},
		{2999650.0 / 27, 554234.0 / 5},
		{9031450.0 / 81, 2779702.0 / 25},
		{26964850.0 / 243, 13874106.0 / 125},
		{80643550.0 / 729, 69234818.0 / 625},
		{241344310.0 / 2187, 345479454.0 / 3125},
		{722353090.0 / 6561, 1723750862.0 / 15625},
	})
	assertEMAs(t, "d-short-stop", []struct{ short, long float64 }{
		{109750, 109850},
		{328550.0 / 3, 109670},
		{983150.0 / 9, 109442},
		{2941550.0 / 27, 545926.0 / 5},
	})
}

// C extends A, and its short's stop is never reached, so the data ends with it live.
// D's high goes past its stop and comes back, so the bar's close cannot be what fired it.
func TestEMAXBacktest_ReversalAndShortStopFixturesHoldTheirBoundaries(t *testing.T) {
	c := scenarioPrices(t, "c-reversal").data
	assert.Equal(t, scenarioPrices(t, "a-reversal-exit").data, c[:10], "C is A with more bars")
	assert.NotEqual(t, c[9].c, c[10].o, "the short fills away from its signal close")
	for i, b := range c[10:] {
		assert.Less(t, b.h, price(111554), "bar %d stays below the short's stop", i+10)
	}

	const stop = price(110797)
	d := scenarioPrices(t, "d-short-stop").data
	assert.Less(t, d[4].h, stop, "the entry bar stays below the stop")
	assert.Less(t, d[5].o, stop)
	assert.Equal(t, stop+3, d[5].h, "the high goes past the stop")
	assert.Less(t, d[5].c, stop, "and the bar closes back below it")
}

// floorDiv divides, rounding toward negative infinity.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// pnlFromFills is rule 2 worked from the prices alone: the points the position
// moved in its favour, times its units, floored to the cent.
func pnlFromFills(d direction, units int64, entry, exit price) money {
	points := int64(exit - entry)
	if d == short {
		points = -points
	}
	return money(floorDiv(points*units*moneyScale, priceScale))
}

// marginAt is rule 5: units * price * 2%, rounded up to the cent.
func marginAt(units int64, p price) money {
	return money(-floorDiv(-units*int64(p)*2*moneyScale, 100*priceScale))
}

// Each realized P/L is worked from the fills alone: a filled open, then the filled
// close that ends it, in the order they completed. Only then is the sum compared with
// the balance, so the balance check cannot pass by being derived from itself.
func TestEMAXBacktest_RealizedPnLReconcilesFromTheFills(t *testing.T) {
	for _, want := range emaxScenarios {
		t.Run(want.name, func(t *testing.T) {
			r := runEMAX(t, want.name)

			var entry *order
			var realized []money
			for _, o := range r.broker.completed {
				if o.state != stateFilled {
					continue
				}
				if o.action == open {
					require.Nil(t, entry, "%s opens while %s is still open", o.id, entry)
					entry = o
					continue
				}
				require.NotNil(t, entry, "%s closes nothing", o.id)
				assert.Equal(t, entry.units, o.units, "%s closes the whole position", o.id)
				realized = append(realized, pnlFromFills(entry.direction, entry.units, entry.fillPrice, o.fillPrice))
				entry = nil
			}

			var approved []money
			for _, tr := range want.trades {
				approved = append(approved, tr.pnl)
			}
			require.Equal(t, approved, realized, "each trade's P/L from its fills")
			var recorded []money
			for _, tr := range r.account.trades {
				recorded = append(recorded, tr.pnl)
			}
			assert.Equal(t, realized, recorded, "the account's trades")
			var sum money
			for _, pnl := range realized {
				sum += pnl
			}
			assert.Equal(t, usd(10000)+sum, r.account.balance)
			assert.Equal(t, entry != nil, r.account.positions["EURUSD"] != nil, "an open fill with no close is the live position")
		})
	}
}

// What is still open is valued from the position and the last bar alone, and equity
// is the balance plus it. B2 and C end short with a profit that is not whole cents, 390
// points * 9122 = 3557.58 cents and 467 points * 9087 = 4243.629, so the floor goes
// against the account. Reserved margin is the waiting opens' margin at their
// reference prices.
func TestEMAXBacktest_OpenPositionReconcilesAtTheLastMark(t *testing.T) {
	for _, want := range emaxScenarios {
		t.Run(want.name, func(t *testing.T) {
			r := runEMAX(t, want.name)
			data := scenarioPrices(t, want.name).data
			last := data[len(data)-1]
			st := snapshotState(t, r)

			var unrealized, used, reserved money
			if p := st.position; p != nil {
				assert.Equal(t, last.c, p.markPrice, "marked at the last close")
				assert.Equal(t, short, p.direction)
				points := int64(p.entryPrice - last.c)
				assert.NotZero(t, points*p.units*moneyScale%priceScale, "a fraction of a cent to floor")
				unrealized = pnlFromFills(p.direction, p.units, p.entryPrice, last.c)
				used = marginAt(p.units, last.c)
			}
			for _, o := range r.broker.orders["EURUSD"] {
				if o.action == open {
					reserved += marginAt(o.units, o.price)
				}
			}
			assert.Equal(t, unrealized, st.unrealized)
			assert.Equal(t, st.balance+unrealized, st.equity)
			assert.Equal(t, used, st.used)
			assert.Equal(t, reserved, st.reserved)
		})
	}
}

// assertLifecycles checks every order the run made: the trader's refusals, the
// broker's completed orders and those still waiting. Each order reaches at most one
// end, and only as its last event; the broker's ids are each issued once and all
// accounted for; and the exits still waiting are the live position's stop, if any.
func assertLifecycles(t *testing.T, r emaxRun) {
	t.Helper()
	ends := func(o *order) int {
		n := 0
		for _, ev := range o.history {
			if ev.state == stateFilled || ev.state == stateRejected || ev.state == stateCancelled {
				n++
			}
		}
		return n
	}
	ordered := func(o *order) {
		require.NotEmpty(t, o.history, o.id)
		assert.Equal(t, stateRequested, o.history[0].state, "%s starts requested", o.id)
		for i := 1; i < len(o.history); i++ {
			assert.False(t, o.history[i].time.Before(o.history[i-1].time), "%s's history is in time order", o.id)
		}
	}

	endsLast := func(o *order) {
		last := o.history[len(o.history)-1]
		assert.Equal(t, o.state, last.state, "%s ends on its last event", o.id)
		assert.Equal(t, o.Time, last.time, "%s's time is when it ended", o.id)
	}

	for _, o := range r.trader.rejected {
		ordered(o)
		assert.Empty(t, o.id, "refused before the broker issued an id")
		assert.Equal(t, stateRejected, o.state)
		assert.Equal(t, 1, ends(o))
		endsLast(o)
	}
	var previous time.Time
	for _, o := range r.broker.completed {
		ordered(o)
		assert.Equal(t, 1, ends(o), "%s ends exactly once", o.id)
		endsLast(o)
		assert.False(t, o.Time.Before(previous), "%s completes in time order", o.id)
		previous = o.Time
	}
	pos := r.account.positions["EURUSD"]
	stops := 0
	for _, o := range r.broker.orders["EURUSD"] {
		ordered(o)
		assert.Equal(t, stateSubmitted, o.state, "%s waits submitted", o.id)
		assert.Zero(t, ends(o), "%s has not ended", o.id)
		if o.reason != reasonStrategy {
			require.NotNil(t, pos, "%s is an exit with no position", o.id)
			assert.Equal(t, pos.direction, o.direction, "%s protects the live position", o.id)
			stops++
		}
	}
	if pos != nil {
		assert.Equal(t, 1, stops, "the live position's stop is still waiting")
	}
	assert.LessOrEqual(t, len(r.account.positions), 1)

	var ids, issued []string
	for _, o := range slices.Concat(r.broker.completed, r.broker.orders["EURUSD"]) {
		ids = append(ids, o.id)
	}
	for i := 1; i <= r.broker.seq; i++ {
		issued = append(issued, fmt.Sprintf("sim-%d", i))
	}
	assert.ElementsMatch(t, issued, ids, "every id issued once, and none lost")
}

func TestEMAXBacktest_EveryOrderEndsAtMostOnce(t *testing.T) {
	for _, s := range emaxScenarios {
		t.Run(s.name, func(t *testing.T) {
			assertLifecycles(t, runEMAX(t, s.name))
		})
	}
	t.Run("refused at sizing", func(t *testing.T) {
		r := runEMAXWithBalance(t, "a-reversal-exit", 1)
		require.NotEmpty(t, r.trader.rejected)
		assertLifecycles(t, r)
	})
}
