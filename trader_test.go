package trader

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

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
	decide func(n int, bars []marketdata.Bar) ([]remotestrat.Order, error)
}

// testWorker is the example worker in process: it listens on loopback, serves one
// connection with strategy.Serve and keeps every group of bars it was sent.
type testWorker struct {
	addr   string
	groups [][]marketdata.Bar
	done   sync.WaitGroup
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
		_ = remotestrat.Serve(conn, func(bars []marketdata.Bar) ([]remotestrat.Order, error) {
			w.groups = append(w.groups, bars)
			n := len(w.groups)
			if o.dropOn > 0 && n == o.dropOn {
				return nil, errors.New("hang up")
			}
			if o.decide != nil {
				return o.decide(n, bars)
			}
			return nil, nil
		})
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
	tr := newTestTrader()
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

	ref := newRemoteTrader(t, &openThenClose{})
	require.NoError(t, (&backtest{}).runSeries(ctx, ref, prices))
	assert.Equal(t, outcomeOf(ref), outcomeOf(tr), "the real example trades like the in-process rule")
	assert.Equal(t, 1, len(tr.account.trades), "it opened and closed one position")
	assert.Contains(t, stderr.String(), "done: ")
}

// The example worker's rule, once as the worker answers it and once as an in-process
// strategy: open long on the third group with a stop 0.015 below the close, close on
// the sixth.
const (
	ruleOpenAt  = 3
	ruleCloseAt = 6
)

var ruleStopDistance = num.MustParsePrice("0.015")

func openThenCloseRequests(n int, bars []marketdata.Bar) ([]remotestrat.Order, error) {
	b := bars[0]
	c := b.OHLC().Close
	switch n {
	case ruleOpenAt:
		stop, err := c.Sub(ruleStopDistance)
		if err != nil {
			return nil, err
		}
		return []remotestrat.Order{{Instrument: b.Instrument, Action: "open", Direction: "long", Price: c, StopLoss: stop}}, nil
	case ruleCloseAt:
		return []remotestrat.Order{{Instrument: b.Instrument, Action: "close", Price: c}}, nil
	}
	return nil, nil
}

// openThenClose is the same rule as an in-process strategy, the reference the
// remote run is compared with.
type openThenClose struct{ n int }

func (*openThenClose) name() string { return "open-then-close" }
func (*openThenClose) init() error  { return nil }
func (s *openThenClose) onBars(_ context.Context, bars []marketdata.Bar) ([]*order, error) {
	s.n++
	requests, err := openThenCloseRequests(s.n, bars)
	if err != nil {
		return nil, err
	}
	var orders []*order
	for _, q := range requests {
		o := &order{instrument: q.Instrument, price: q.Price, stopLoss: q.StopLoss, orderType: marketOrder, Time: bars[0].Time}
		if q.Action == "open" {
			o.action, o.direction = open, long
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
	orders  []orderResult
	balance num.Money
	trades  int
}

func outcomeOf(tr *trader) outcome {
	return outcome{
		orders:  orderResults(slices.Concat(tr.broker.completed, tr.broker.orders["EURUSD"])),
		balance: tr.account.balance,
		trades:  len(tr.account.trades),
	}
}

func TestRemoteStrategy_OrdersMatchInProcess(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	prices := scenarioPrices(t, remoteScenario)

	inProc := newRemoteTrader(t, &openThenClose{})
	require.NoError(t, (&backtest{}).runSeries(ctx, inProc, prices))
	want := outcomeOf(inProc)
	require.Len(t, want.orders, 3, "the open, the close, and the broker's stop cancelled by it")
	assert.Equal(t, stateFilled, want.orders[0].state, "open")
	assert.Equal(t, stateFilled, want.orders[1].state, "close")
	assert.Equal(t, stateCancelled, want.orders[2].state, "the stop")
	assert.Equal(t, 1, want.trades)
	assert.NotZero(t, want.orders[0].units, "the trader sized it, the strategy did not")

	w := startWorker(t, workerOpts{decide: openThenCloseRequests})
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
			w := startWorker(t, workerOpts{decide: func(n int, _ []marketdata.Bar) ([]remotestrat.Order, error) {
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
