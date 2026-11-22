package trader

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	emaxworker "github.com/rustyeddy/trader/examples/emax/emax"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	remotestrat "github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTraderWithBalance(balance money) *trader {
	br := newBroker("t", testMarkets())
	act := br.addAccount("a", balance)
	return &trader{broker: br, account: act}
}

func newTestTrader() *trader { return newTestTraderWithBalance(usd(1000)) }

func TestTrader_HandleOrder(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	openO := func() *order {
		return &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)}
	}

	t.Run("open is sized then submitted", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Equal(t, qty(909), o.units, "balance 1000: floor(1000 / 1.1)")
		assert.Equal(t, px(108900), o.stopLoss)
		assert.Equal(t, stateSubmitted, o.state)
		assert.NotEmpty(t, o.id)
		assert.Equal(t, "a", o.accountID, "the trader names the account")
		require.Len(t, o.history, 3)
		assert.Equal(t, stateRequested, o.history[0].state)
		assert.Equal(t, stateSized, o.history[1].state)
		assert.Equal(t, stateSubmitted, o.history[2].state)
	})
	t.Run("limit open is sized at its limit price", func(t *testing.T) {
		o := openO()
		o.orderType, o.price, o.stopLoss = limitOrder, px(100000), px(99000)
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Equal(t, qty(1000), o.units, "balance 1000: floor(1000 / 1.0)")
		assert.Equal(t, stateSubmitted, o.state)
	})
	t.Run("close is submitted without units", func(t *testing.T) {
		o := &order{Time: testStart, instrument: "EURUSD", action: close, orderType: marketOrder}
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Zero(t, o.units, "units 0 means the whole position")
		assert.Equal(t, stateSubmitted, o.state)
		assert.Equal(t, "a", o.accountID)
	})
	t.Run("open is rejected with zero margin and never submitted", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTraderWithBalance(usd(0)).handleOrder(ctx, o), "a rejection is recorded, not returned")
		assert.Equal(t, stateRejected, o.state)
		require.Len(t, o.history, 2)
		assert.Equal(t, stateRequested, o.history[0].state)
		assert.Equal(t, stateRejected, o.history[1].state)
		assert.Empty(t, o.id, "a rejected order never reaches the broker")
		assert.Equal(t, "a", o.accountID, "but it is still recorded against its account")
	})
	t.Run("open is rejected with negative margin", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTraderWithBalance(mneg(usd(1))).handleOrder(ctx, o))
		assert.Equal(t, stateRejected, o.state)
		assert.Contains(t, o.history[len(o.history)-1].reason, "insufficient margin")
	})
	t.Run("add and reduce are not supported yet", func(t *testing.T) {
		tr := newTestTrader()
		require.ErrorIs(t, tr.handleOrder(ctx, &order{Time: testStart, instrument: "EURUSD", action: add, orderType: marketOrder}), errUnsupportedAction)
		require.ErrorIs(t, tr.handleOrder(ctx, &order{Time: testStart, instrument: "EURUSD", action: reduce, orderType: marketOrder}), errUnsupportedAction)
	})
	t.Run("unknown account at the broker is an error", func(t *testing.T) {
		tr := &trader{broker: newBroker("t", testMarkets()), account: newAccount("ghost", usd(1000), testMarkets())}
		require.ErrorIs(t, tr.handleOrder(ctx, openO()), errUnknownAccount)
	})
}

func TestTrader_HandleOrder_InvalidOrderType(t *testing.T) {
	captureLogs(t)
	for _, tt := range []struct {
		name string
		typ  orderType
	}{
		{"missing", noOrder},
		{"unknown", orderType(99)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tr := newTestTrader()
			o := openOrder()
			o.orderType = tt.typ

			err := tr.handleOrder(context.Background(), o)

			require.ErrorIs(t, err, errInvalidOrder)
			assert.False(t, isRejection(err), "an invalid request stops the run")
			assert.Empty(t, o.history, "never recorded or sized")
			assert.Empty(t, o.id)
			assert.Zero(t, o.units)
			assert.Empty(t, tr.broker.orders)
			reserved := reservedOf(t, tr.broker, tr.account.id)
			assert.True(t, reserved.IsZero())
			assert.Empty(t, tr.account.positions)
			assert.Equal(t, usd(1000), tr.account.balance)
		})
	}
}

func TestTrader_HandleOrder_ConflictingAccountIsAnError(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, accountID: "someone-else", instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)}

	err := newTestTrader().handleOrder(context.Background(), o)

	require.ErrorIs(t, err, errAccountConflict)
	assert.Equal(t, "someone-else", o.accountID, "an existing account is never overwritten")
	assert.Empty(t, o.history, "the order was neither requested, sized nor submitted")
}

func TestTrader_HandleOrder_MatchingAccountIsKept(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, accountID: "a", instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)}

	require.NoError(t, newTestTrader().handleOrder(context.Background(), o))

	assert.Equal(t, "a", o.accountID)
	assert.Equal(t, stateSubmitted, o.state)
}

func TestTrader_HandleOrder_KeepsOrdersRefusedBeforeTheBroker(t *testing.T) {
	captureLogs(t)
	tr := newTestTraderWithBalance(usd(0))
	o := openOrder()

	require.NoError(t, tr.handleOrder(context.Background(), o), "a refusal does not stop the run")

	assert.Equal(t, []*order{o}, tr.rejected)
	assert.Equal(t, stateRejected, o.state)
	assert.Empty(t, tr.broker.completed, "kept once, by the trader")
	assert.Empty(t, tr.broker.orders["EURUSD"])
}

func TestTrader_HandleOrder_KeepsASecondOpenRefusedWhileOneIsPending(t *testing.T) {
	captureLogs(t)
	tr := newTestTraderWithBalance(usd(10000))
	first, second := openOrder(), openOrder()

	require.NoError(t, tr.handleOrder(context.Background(), first))
	require.NoError(t, tr.handleOrder(context.Background(), second))

	assert.Equal(t, []*order{second}, tr.rejected, "the accepted open is not kept here")
	assert.Equal(t, []*order{first}, tr.broker.orders["EURUSD"])
}

func TestTrader_HandleOrder_FailureIsReturnedNotKeptAsRejected(t *testing.T) {
	captureLogs(t)
	tr := newTestTraderWithBalance(usd(10000))
	o := openOrder()
	o.instrument = "NOSUCH"

	require.ErrorIs(t, tr.handleOrder(context.Background(), o), errUnknownInstrument)
	assert.Empty(t, tr.rejected)
}

func TestTrader_HandleOrder_Amend(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	held := func(t *testing.T) (*trader, *order) {
		tr := newTestTraderWithBalance(usd(10000))
		stop := fixedEntry(t, tr.broker, tr.account, long)
		return tr, stop
	}

	t.Run("a tightening amend is applied and is not a rejection", func(t *testing.T) {
		tr, stop := held(t)
		o := amendOrder(109500, 0, 0)

		require.NoError(t, tr.handleOrder(ctx, o))

		assert.Equal(t, px(109500), stop.price)
		assert.Equal(t, stateFilled, o.state)
		assert.Equal(t, "t-3", o.id, "the broker assigns the id")
		assert.Empty(t, tr.rejected)
	})

	t.Run("a loosening amend is kept as rejected and the run goes on", func(t *testing.T) {
		tr, stop := held(t)
		o := amendOrder(108000, 0, 0)

		require.NoError(t, tr.handleOrder(ctx, o), "an expected rejection does not stop the run")

		assert.Equal(t, []*order{o}, tr.rejected)
		assert.Equal(t, px(108900), stop.price)
	})

	t.Run("an amend with nothing to change is a strategy bug and stops the run", func(t *testing.T) {
		tr, _ := held(t)

		err := tr.handleOrder(ctx, amendOrder(0, 0, 0))

		require.ErrorIs(t, err, errInvalidOrder)
		assert.Empty(t, tr.rejected)
	})

	t.Run("the trader binds the account", func(t *testing.T) {
		tr, _ := held(t)
		o := amendOrder(109500, 0, 0)
		o.accountID = ""

		require.NoError(t, tr.handleOrder(ctx, o))

		assert.Equal(t, "a", o.accountID)
	})
}

// workerOpts shapes testWorker. The zero value answers every group with no orders
// until the trader hangs up.
type workerOpts struct {
	dropOn int // hang up on receiving this group without answering it; 0 never
	// decide, if set, says which orders to request on the n-th group (from 1).
	decide func(n int, bars []marketdata.Bar, events []remotestrat.Event) ([]remotestrat.Order, error)
}

// testWorker is the example worker in process: it listens on loopback, serves one
// connection with strategy.Serve and keeps every group of bars it was sent.
type testWorker struct {
	addr   string
	groups [][]marketdata.Bar
	events [][]remotestrat.Event // what came with each group
	// warmups is what came marked as warm-up, in order; log is every message, warm-up
	// or group, as "warm" or "group", to show which came first.
	warmups [][]marketdata.Bar
	log     []string
	done    sync.WaitGroup
}

func startWorker(t *testing.T, o workerOpts) *testWorker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	w := &testWorker{addr: ln.Addr().String()}
	w.done.Add(1)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		defer w.done.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = remotestrat.Serve(conn, func(bars []marketdata.Bar, events []remotestrat.Event) ([]remotestrat.Order, error) {
			w.log = append(w.log, "group")
			w.groups = append(w.groups, bars)
			w.events = append(w.events, events)
			n := len(w.groups)
			if o.dropOn > 0 && n == o.dropOn {
				return nil, errors.New("hang up")
			}
			if o.decide != nil {
				return o.decide(n, bars, events)
			}
			return nil, nil
		}, remotestrat.WithWarmup(func(bars []marketdata.Bar) {
			w.log = append(w.log, "warm")
			w.warmups = append(w.warmups, bars)
		}))
	}()
	return w
}

// finish waits for the worker to stop (the trader closed or it hung up) and returns
// what it was sent.
func (w *testWorker) finish() [][]marketdata.Bar {
	w.done.Wait()
	return w.groups
}

// jsonOf is a value as the wire carries it, to compare bars without caring how
// time.Time or num values are held in memory.
func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// recorder is an in-process strategy that records the groups it is shown and
// trades nothing: the reference the remote path is compared with.
type recorder struct{ groups [][]marketdata.Bar }

func (r *recorder) name() string { return "recorder" }
func (r *recorder) init() error  { return nil }
func (r *recorder) onBars(_ context.Context, bars []marketdata.Bar) ([]*order, error) {
	r.groups = append(r.groups, slices.Clone(bars))
	return nil, nil
}

func newRemoteTrader(t *testing.T, s strategy) *trader {
	t.Helper()
	return newRemoteTraderWith(t, s, usd(1000))
}

func newRemoteTraderWith(t *testing.T, s strategy, balance money) *trader {
	t.Helper()
	tr := newTestTraderWithBalance(balance)
	tr.strategy = s
	return tr
}

// failingStrategy returns an order together with an error: the order must not be
// used.
type failingStrategy struct{}

func (failingStrategy) name() string { return "failing" }
func (failingStrategy) init() error  { return nil }
func (failingStrategy) onBars(context.Context, []marketdata.Bar) ([]*order, error) {
	return []*order{{instrument: "EURUSD", action: open, direction: long, orderType: marketOrder}}, errors.New("worker is gone")
}

func TestTrader_OnFrame_StopsWhenTheStrategyHasFailed(t *testing.T) {
	captureLogs(t)
	tr := newRemoteTrader(t, failingStrategy{})
	f := frame{availableAt: testStart, bars: []marketdata.Bar{{Instrument: "EURUSD", Time: testStart, AvailableAt: testStart}}}

	assert.ErrorContains(t, tr.onFrame(context.Background(), f), "worker is gone")
	assert.Empty(t, tr.broker.orders, "the order it returned was not submitted")
	assert.Empty(t, tr.rejected)
}

const remoteScenario = "a-reversal-exit"

func TestRemoteStrategy_BacktestMatchesInProcess(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	prices := scenarioPrices(t, remoteScenario)

	ref := &recorder{}
	require.NoError(t, (&backtest{}).runSeries(ctx, newRemoteTrader(t, ref), prices))
	require.Greater(t, len(ref.groups), 3, "the fixture gives several deliveries")

	w := startWorker(t, workerOpts{})
	remote, err := dialRemote("remote", w.addr)
	require.NoError(t, err)
	tr := newRemoteTrader(t, remote)
	require.NoError(t, (&backtest{}).runSeries(ctx, tr, prices))
	require.NoError(t, remote.close())

	assert.Equal(t, jsonOf(t, ref.groups), jsonOf(t, w.finish()), "the worker saw the same groups, in the same order")
	assert.Empty(t, tr.broker.completed, "no orders, so nothing happened")
	assert.Empty(t, tr.account.trades, "no trades")
	assert.True(t, tr.account.balance.Equal(usd(1000)), "balance untouched")
}

func TestRemoteStrategy_BacktestFailsWhenTheWorkerDisconnects(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	prices := scenarioPrices(t, remoteScenario)

	w := startWorker(t, workerOpts{dropOn: 3})
	remote, err := dialRemote("remote", w.addr)
	require.NoError(t, err)
	defer func() { _ = remote.close() }()

	err = (&backtest{}).runSeries(ctx, newRemoteTrader(t, remote), prices)
	require.Error(t, err, "a dead worker ends the run instead of hanging it")
	assert.ErrorContains(t, err, "strategy remote")
	assert.Len(t, w.finish(), 3, "the worker hung up on its third group, which the run never got an answer to")
}

// TestRemoteStrategy_ExampleWorker runs the real example as a separate process.
func TestRemoteStrategy_ExampleWorker(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	prices := scenarioPrices(t, remoteScenario)

	bin := filepath.Join(t.TempDir(), "remote-strategy")
	out, err := exec.Command("go", "build", "-o", bin, "./examples/remote-strategy").CombinedOutput()
	require.NoError(t, err, string(out))

	var stderr bytes.Buffer
	cmd := exec.Command(bin, "-addr", "127.0.0.1:0")
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	addr := strings.TrimSpace(strings.TrimPrefix(line, "listening on "))

	remote, err := dialRemote("example", addr)
	require.NoError(t, err)
	tr := newRemoteTrader(t, remote)
	require.NoError(t, (&backtest{}).runSeries(ctx, tr, prices))
	require.NoError(t, remote.close())
	require.NoError(t, cmd.Wait(), "the worker exits when the trader closes")

	ref := newRemoteTrader(t, newInProcessRule(ruleStopDistance))
	require.NoError(t, (&backtest{}).runSeries(ctx, ref, prices))
	assert.Equal(t, outcomeOf(ref), outcomeOf(tr), "the real example trades like the in-process rule")
	assert.Equal(t, 1, len(tr.account.trades), "it opened and closed one position")
	assert.Contains(t, stderr.String(), "done: ")
	assert.Contains(t, stderr.String(), "event: 2010-01-07 filled open EURUSD strategy ref=\"entry\"", "the example prints what Trader reports")
}

// The example worker's rule, once as the worker answers it and once as an
// in-process strategy: open long on the third group with a stop below the close,
// close on the sixth if the entry filled and the stop has not taken it out.
const (
	ruleOpenAt  = 3
	ruleCloseAt = 6
)

var ruleStopDistance = num.MustParsePrice("0.015")

// feedbackRule is that rule, driven by the events the trader reports.
type feedbackRule struct {
	stopDistance num.Price
	groups       int
	inPosition   bool
}

func (r *feedbackRule) step(bars []marketdata.Bar, events []remotestrat.Event) ([]remotestrat.Order, error) {
	for _, e := range events {
		switch {
		case e.Ref == "entry" && e.Kind == remotestrat.Filled:
			r.inPosition = true
		case e.Kind == remotestrat.Filled && e.Cause == "stop loss":
			r.inPosition = false
		}
	}
	r.groups++
	b := bars[0]
	c := b.OHLC().Close
	switch {
	case r.groups == ruleOpenAt:
		stop, err := c.Sub(r.stopDistance)
		if err != nil {
			return nil, err
		}
		return []remotestrat.Order{{Instrument: b.Instrument, Action: "open", Direction: "long", Price: c, StopLoss: stop, Ref: "entry"}}, nil
	case r.groups == ruleCloseAt && r.inPosition:
		return []remotestrat.Order{{Instrument: b.Instrument, Action: "close", Price: c}}, nil
	}
	return nil, nil
}

// ruleWorker is the rule as the worker's callback.
func ruleWorker(distance num.Price) func(int, []marketdata.Bar, []remotestrat.Event) ([]remotestrat.Order, error) {
	r := &feedbackRule{stopDistance: distance}
	return func(_ int, bars []marketdata.Bar, events []remotestrat.Event) ([]remotestrat.Order, error) {
		return r.step(bars, events)
	}
}

// inProcessRule is the same rule as an in-process strategy that asks for feedback:
// the reference the remote run is compared with.
type inProcessRule struct {
	rule    feedbackRule
	entry   *order
	pending []remotestrat.Event
	seen    [][]remotestrat.Event // the events given with each group
}

func newInProcessRule(distance num.Price) *inProcessRule {
	return &inProcessRule{rule: feedbackRule{stopDistance: distance}}
}

func (*inProcessRule) name() string { return "in-process-rule" }
func (*inProcessRule) init() error  { return nil }

func (s *inProcessRule) feedback(events []feedbackEvent) {
	for _, e := range events {
		ref := ""
		if e.order == s.entry {
			ref = "entry"
		}
		s.pending = append(s.pending, toWireEvent(e, ref))
	}
}

func (s *inProcessRule) onBars(_ context.Context, bars []marketdata.Bar) ([]*order, error) {
	events := s.pending
	if events == nil {
		events = []remotestrat.Event{} // as on the wire: an empty list, not null
	}
	s.pending = nil
	s.seen = append(s.seen, events)
	requests, err := s.rule.step(bars, events)
	if err != nil {
		return nil, err
	}
	var orders []*order
	for _, q := range requests {
		o := &order{instrument: q.Instrument, price: q.Price, stopLoss: q.StopLoss, orderType: marketOrder, Time: bars[0].Time}
		if q.Action == "open" {
			o.action, o.direction = open, long
			s.entry = o
		} else {
			o.action = close
		}
		orders = append(orders, o)
	}
	return orders, nil
}

// outcome is what a run came to: every order that ended or waits, with its sizing
// and fill, and the account.
type outcome struct {
	orders   []orderResult
	rejected []orderResult // refused before they reached the broker
	balance  num.Money
	trades   int
}

func outcomeOf(tr *trader) outcome {
	return outcome{
		orders:   orderResults(slices.Concat(tr.broker.completed, tr.broker.orders["EURUSD"])),
		rejected: orderResults(tr.rejected),
		balance:  tr.account.balance,
		trades:   len(tr.account.trades),
	}
}

func TestRemoteStrategy_OrdersMatchInProcess(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	prices := scenarioPrices(t, remoteScenario)

	inProc := newRemoteTrader(t, newInProcessRule(ruleStopDistance))
	require.NoError(t, (&backtest{}).runSeries(ctx, inProc, prices))
	want := outcomeOf(inProc)
	require.Len(t, want.orders, 3, "the open, the close, and the broker's stop cancelled by it")
	assert.Equal(t, stateFilled, want.orders[0].state, "open")
	assert.Equal(t, stateFilled, want.orders[1].state, "close")
	assert.Equal(t, stateCancelled, want.orders[2].state, "the stop")
	assert.Equal(t, 1, want.trades)
	assert.NotZero(t, want.orders[0].units, "the trader sized it, the strategy did not")

	w := startWorker(t, workerOpts{decide: ruleWorker(ruleStopDistance)})
	remote, err := dialRemote("remote", w.addr)
	require.NoError(t, err)
	tr := newRemoteTrader(t, remote)
	require.NoError(t, (&backtest{}).runSeries(ctx, tr, prices))
	require.NoError(t, remote.close())
	w.finish()

	got := outcomeOf(tr)
	assert.Equal(t, want.orders, got.orders, "same orders, sizes, fills and times")
	assert.True(t, want.balance.Equal(got.balance), "same final balance: %s vs %s", want.balance, got.balance)
	assert.Equal(t, want.trades, got.trades)
	assert.Equal(t, orderHistoryOf(inProc), orderHistoryOf(tr), "same lifecycle")
}

func TestRemoteStrategy_FeedbackMatchesInProcess(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	tests := []struct {
		name     string
		distance num.Price
		balance  money
		want     []string // "group: kind action cause" of every event the worker is told
	}{
		{"fill then a close it asked for", ruleStopDistance, usd(1000), []string{
			"4: accepted open strategy", "4: filled open strategy",
			"7: accepted close strategy", "7: filled close strategy"}},
		{"a protective stop takes it out and the worker skips its close", num.MustParsePrice("0.002"), usd(1000), []string{
			"4: accepted open strategy", "4: filled open strategy",
			"6: filled close stop loss"}},
		{"a refusal at sizing reaches the worker with its reason", ruleStopDistance, usd(0), []string{
			"4: rejected open strategy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prices := scenarioPrices(t, remoteScenario)

			ref := newInProcessRule(tt.distance)
			inProc := newRemoteTraderWith(t, ref, tt.balance)
			require.NoError(t, (&backtest{}).runSeries(ctx, inProc, prices))

			w := startWorker(t, workerOpts{decide: ruleWorker(tt.distance)})
			remote, err := dialRemote("remote", w.addr)
			require.NoError(t, err)
			tr := newRemoteTraderWith(t, remote, tt.balance)
			require.NoError(t, (&backtest{}).runSeries(ctx, tr, prices))
			require.NoError(t, remote.close())
			w.finish()

			var told []string
			for i, events := range w.events {
				for _, e := range events {
					told = append(told, fmt.Sprintf("%d: %s %s %s", i+1, e.Kind, e.Action, e.Cause))
				}
			}
			assert.Equal(t, tt.want, told)
			assert.Equal(t, jsonOf(t, ref.seen), jsonOf(t, w.events), "the worker is told what the in-process strategy is told")
			assert.Equal(t, outcomeOf(inProc), outcomeOf(tr), "and the runs end the same")
			assert.Len(t, w.events, len(w.groups), "every group carries an events list, possibly empty")
		})
	}
}

// orderHistoryOf is every order's lifecycle, by id.
func orderHistoryOf(tr *trader) map[string][]orderEvent {
	histories := make(map[string][]orderEvent)
	for _, o := range slices.Concat(tr.broker.completed, tr.broker.orders["EURUSD"]) {
		histories[o.id] = o.history
	}
	return histories
}

func TestRemoteStrategy_BadOrdersFailTheRunAndProcessNone(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	prices := scenarioPrices(t, remoteScenario)
	valid := remotestrat.Order{Instrument: "EURUSD", Action: "open", Direction: "long", Price: px(110000), StopLoss: px(109000)}

	tests := []struct {
		name   string
		orders []remotestrat.Order
		want   string
	}{
		{"open without a stop", []remotestrat.Order{{Instrument: "EURUSD", Action: "open", Direction: "long", Price: px(110000)}}, "stop loss"},
		{"unsupported action", []remotestrat.Order{{Instrument: "EURUSD", Action: "amend", StopLoss: px(109000)}}, `unsupported action "amend"`},
		{"a bad order after a good one", []remotestrat.Order{valid, {Instrument: "EURUSD", Action: "frobnicate"}}, "requested order 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := startWorker(t, workerOpts{decide: func(n int, _ []marketdata.Bar, _ []remotestrat.Event) ([]remotestrat.Order, error) {
				if n == ruleOpenAt {
					return tt.orders, nil
				}
				return nil, nil
			}})
			remote, err := dialRemote("remote", w.addr)
			require.NoError(t, err)
			defer func() { _ = remote.close() }()
			tr := newRemoteTrader(t, remote)

			err = (&backtest{}).runSeries(ctx, tr, prices)
			require.ErrorContains(t, err, tt.want)
			assert.Empty(t, tr.broker.completed, "no order of the failed group was processed")
			assert.Empty(t, tr.broker.orders["EURUSD"])
			assert.Empty(t, tr.rejected)
			assert.Empty(t, tr.account.trades)
			require.NoError(t, remote.close()) // the worker stops when the trader hangs up
			assert.Len(t, w.finish(), ruleOpenAt, "the run stopped at the group that failed")
		})
	}
}

// emaxConfig is the built-in EMAX's settings (historical.go: windows 2 and 4, a 1%
// stop) and the tick of the instruments the scenarios trade, for the worker.
func emaxConfig() emaxworker.Config {
	return emaxworker.Config{Short: runShort, Long: runLong, StopPct: rateOnePercent, Tick: px(1)}
}

// serveEMAX serves examples/emax in this process over loopback TCP, through the
// public SDK only, and returns its address. The worker stops when the trader hangs
// up, which the run does when it ends; the test waits for that.
func serveEMAX(t *testing.T, cfg emaxworker.Config) string {
	t.Helper()
	s, err := emaxworker.New(cfg)
	require.NoError(t, err)

	addrs := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- remotestrat.ListenAndServe("127.0.0.1:0",
			func(a net.Addr) { addrs <- a.String() },
			func(bars []marketdata.Bar, events []remotestrat.Event) ([]remotestrat.Order, error) {
				return s.OnBars(bars, events)
			}, remotestrat.WithWarmup(s.Warm))
	}()
	addr := <-addrs
	t.Cleanup(func() { require.NoError(t, <-done, "the worker ends cleanly when the run closes the connection") })
	return addr
}

// parityRun is one run, journaled.
type parityRun struct {
	journal string
	summary Summary
	text    string // Result.Write
}

func runParity(t *testing.T, opts RunOptions, series ...marketdata.Series) parityRun {
	t.Helper()
	return runParityFrom(t, opts, time.Time{}, series...)
}

// runParityFrom is runParity with the bars before tradeFrom as warm-up.
func runParityFrom(t *testing.T, opts RunOptions, tradeFrom time.Time, series ...marketdata.Series) parityRun {
	t.Helper()
	var buf bytes.Buffer
	opts.Journal = &buf
	res, err := RunSeriesWith(context.Background(), opts, "parity", tradeFrom, series...)
	require.NoError(t, err)
	var text bytes.Buffer
	require.NoError(t, res.Write(&text))
	return parityRun{journal: buf.String(), summary: res.Summary(), text: text.String()}
}

// assertParity says the remote run is the built-in run: the same orders in the same
// order, the same sizes and fills and exits, the same final positions and account.
// Only what names the strategy may differ.
func assertParity(t *testing.T, builtin, remote parityRun) {
	t.Helper()
	require.NotEmpty(t, builtin.journal)
	assert.Equal(t, builtin.journal, remote.journal,
		"every order transition, fill and protective exit, in the same order and to the same price")
	b, r := builtin.summary, remote.summary
	assert.Equal(t, StrategyEMAX, b.Strategy)
	assert.Equal(t, StrategyRemote, r.Strategy)
	b.Strategy, r.Strategy = "", ""
	assert.Equal(t, b, r, "orders, trades, balance, equity and open positions")
	assert.Equal(t, builtin.text, remote.text, "the summary a person reads")
}

func TestEMAXParity_RemoteMatchesBuiltInOnTheScenarios(t *testing.T) {
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			captureLogs(t)
			prices := scenarioPrices(t, scenario)

			builtin := runParity(t, RunOptions{}, prices)
			remote := runParity(t, RunOptions{Worker: serveEMAX(t, emaxConfig())}, prices)

			assertParity(t, builtin, remote)
		})
	}
}

// randomWalk is hourly mid bars for an instrument: a seeded walk in ticks, so a
// test gets many crossovers, stops and flips from data that is the same every time.
func randomWalk(instrument string, start int64, bars int, seed uint64) marketdata.Series {
	rng := rand.New(rand.NewPCG(seed, seed*7+1))
	t0 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	s := marketdata.Series{Source: "oanda", Instrument: instrument, Interval: "h1"}
	prev := start
	for i := range bars {
		closeP := prev + rng.Int64N(401) - 200 // up to 20 pips a bar
		hi := max(prev, closeP) + rng.Int64N(60)
		lo := min(prev, closeP) - rng.Int64N(60)
		at := t0.Add(time.Duration(i) * time.Hour)
		s.Bars = append(s.Bars, marketdata.Bar{
			Instrument: instrument, Time: at, AvailableAt: at.Add(time.Hour), Volume: 100,
			Mid: marketdata.OHLC{Open: px(prev), High: px(hi), Low: px(lo), Close: px(closeP)},
		})
		prev = closeP
	}
	return s
}

func TestEMAXParity_RemoteMatchesBuiltInOnALongTwoInstrumentWalk(t *testing.T) {
	captureLogs(t)
	eur, gbp := randomWalk("EURUSD", 110000, 1500, 1), randomWalk("GBPUSD", 125000, 1500, 2)

	builtin := runParity(t, RunOptions{}, eur, gbp)
	remote := runParity(t, RunOptions{Worker: serveEMAX(t, emaxConfig())}, eur, gbp)

	assertParity(t, builtin, remote)
	// The walk is not a vacuous comparison: it opens, flips and stops out.
	assert.Greater(t, builtin.summary.Orders.Filled, 100)
	assert.Greater(t, builtin.summary.Trades, 20)
	assert.Contains(t, builtin.journal, `"cause":"stop loss"`)
	assert.Contains(t, builtin.journal, `"direction":"short"`)
	assert.Positive(t, builtin.summary.Orders.Cancelled, "a close cancels the position's stop")
}

// The comparison can fail: a worker that differs in one setting is not the built-in
// strategy, and the journals say so.
func TestEMAXParity_ADifferentWorkerIsCaught(t *testing.T) {
	captureLogs(t)
	walk := randomWalk("EURUSD", 110000, 400, 6)
	wider := emaxConfig()
	wider.StopPct = num.MustParseRate("0.02")

	builtin := runParity(t, RunOptions{}, walk)
	other := runParity(t, RunOptions{Worker: serveEMAX(t, wider)}, walk)

	assert.NotEqual(t, builtin.journal, other.journal)
}

// The bars before tradeFrom reach a worker as warm-up, first and in order, and are
// not groups to decide: the worker is asked about nothing before tradeFrom.
func TestRunSeriesWith_AWorkerIsWarmedWithTheBarsBeforeTradeFrom(t *testing.T) {
	captureLogs(t)
	walk := randomWalk("EURUSD", 110000, 300, 3)
	from := walk.Bars[100].Time
	w := startWorker(t, workerOpts{})

	res, err := RunSeriesWith(context.Background(), RunOptions{Worker: w.addr}, "x", from, walk)

	require.NoError(t, err)
	groups := w.finish()
	require.Len(t, groups, 200, "one group for each bar from tradeFrom on")
	assert.Equal(t, from, groups[0][0].Time, "the first decision is at tradeFrom")
	require.Len(t, w.warmups, 100, "one warm-up message for each bar before it")
	assert.Equal(t, walk.Bars[0].Time, w.warmups[0][0].Time)
	assert.Equal(t, walk.Bars[99].Time, w.warmups[99][0].Time)
	assert.Equal(t, append(slices.Repeat([]string{"warm"}, 100), slices.Repeat([]string{"group"}, 200)...), w.log,
		"all the warm-up first, then the groups")
	assert.Equal(t, 200, res.Summary().Bars, "warm-up bars are not counted")
	assert.Zero(t, res.Summary().Orders.Submitted)
}

// A worker that is warmed trades like the built-in strategy that is warmed. The
// comparison is not vacuous: warming changes what the strategy does, so a cold run
// of the same bars differs.
func TestEMAXParity_RemoteMatchesBuiltInWhenBothAreWarmed(t *testing.T) {
	captureLogs(t)
	walk := randomWalk("EURUSD", 110000, 600, 8)
	from := walk.Bars[200].Time

	builtin := runParityFrom(t, RunOptions{}, from, walk)
	remote := runParityFrom(t, RunOptions{Worker: serveEMAX(t, emaxConfig())}, from, walk)

	assertParity(t, builtin, remote)
	cold := walk
	cold.Bars = walk.Bars[200:]
	assert.NotEqual(t, builtin.journal, runParity(t, RunOptions{}, cold).journal, "warm-up changes the trading")
	assert.Greater(t, builtin.summary.Orders.Filled, 30)
}

// Two instruments, warmed through the merged feed.
func TestEMAXParity_RemoteMatchesBuiltInWhenBothAreWarmedOnTwoInstruments(t *testing.T) {
	captureLogs(t)
	eur := randomWalk("EURUSD", 110000, 600, 9)
	gbp := randomWalk("GBPUSD", 125000, 600, 10)
	from := eur.Bars[250].Time

	builtin := runParityFrom(t, RunOptions{}, from, eur, gbp)
	remote := runParityFrom(t, RunOptions{Worker: serveEMAX(t, emaxConfig())}, from, eur, gbp)

	assertParity(t, builtin, remote)
	assert.Greater(t, builtin.summary.Orders.Filled, 30)
}

// A worker that cannot take the warm-up ends the run before anything is traded.
func TestRunSeriesWith_AWorkerThatDropsDuringWarmupFailsTheRun(t *testing.T) {
	captureLogs(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		if conn, err := ln.Accept(); err == nil {
			_ = conn.Close()
		}
	}()
	walk := randomWalk("EURUSD", 110000, 100, 3)
	var journal bytes.Buffer

	_, err = RunSeriesWith(context.Background(), RunOptions{Worker: ln.Addr().String(), Journal: &journal}, "x", walk.Bars[50].Time, walk)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "warm-up")
	assert.Empty(t, journal.String())
}
