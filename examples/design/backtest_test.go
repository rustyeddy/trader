package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptStrategy returns a fixed order per bar index (nil means no order) and
// records the bars it was shown.
type scriptStrategy struct {
	orders []*order
	seen   []bar
}

func (s *scriptStrategy) name() string { return "script" }

func (s *scriptStrategy) init(map[string]any) error { return nil }

func (s *scriptStrategy) onBar(_ context.Context, b bar) *order {
	i := len(s.seen)
	s.seen = append(s.seen, b)
	if i < len(s.orders) {
		return s.orders[i]
	}
	return nil
}

// openOrder is a valid open-long order for "TEST".
func openOrder() *order {
	return &order{instrument: "TEST", direction: long, action: open, orderType: marketOrder}
}

// opens returns n distinct valid open orders, one per bar.
func opens(n int) []*order {
	orders := make([]*order, n)
	for i := range orders {
		orders[i] = openOrder()
	}
	return orders
}

// recordHandler records orders and fails on a chosen call.
type recordHandler struct {
	got    []*order
	failAt int // 1-based call number to fail on; 0 means never
	err    error
}

func (h *recordHandler) handleOrder(_ context.Context, o *order) error {
	h.got = append(h.got, o)
	if h.failAt != 0 && len(h.got) == h.failAt {
		return h.err
	}
	return nil
}

func seq(bars []bar, errAt int, err error) iter.Seq2[bar, error] {
	return func(yield func(bar, error) bool) {
		for i, b := range bars {
			if i == errAt {
				yield(bar{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
	}
}

func TestBacktestRun_AcceptedRequests(t *testing.T) {
	bars := closes(1, 2, 3)
	o1 := openOrder()
	o3 := &order{instrument: "TEST", action: close, orderType: marketOrder}
	strat := &scriptStrategy{orders: []*order{o1, nil, o3}}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(bars, -1, nil), strat, h)

	require.NoError(t, err)
	assert.Equal(t, bars, strat.seen, "every bar reaches the strategy")
	require.Len(t, h.got, 2, "the bar with no order never reaches the handler")
	assert.Same(t, o1, h.got[0])
	assert.Same(t, o3, h.got[1])
}

func TestBacktestRun_NoOrderSkipsHandler(t *testing.T) {
	strat := &scriptStrategy{} // returns nil on every bar
	h := &recordHandler{}

	require.NoError(t, (&backtest{}).run(context.Background(), seq(closes(1, 2, 3), -1, nil), strat, h))
	assert.Len(t, strat.seen, 3)
	assert.Empty(t, h.got)
}

func TestBacktestRun_NoBarsNoCalls(t *testing.T) {
	strat := &scriptStrategy{}
	h := &recordHandler{}

	require.NoError(t, (&backtest{}).run(context.Background(), seq(nil, -1, nil), strat, h))
	assert.Empty(t, strat.seen)
	assert.Empty(t, h.got)
}

func TestBacktestRun_RecordsRequestedInHistory(t *testing.T) {
	captureLogs(t)
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	stubClock(t, t0)
	o := openOrder()
	strat := &scriptStrategy{orders: []*order{nil, o}}

	require.NoError(t, (&backtest{}).run(context.Background(), seq(closes(1, 2), -1, nil), strat, &recordHandler{}))

	assert.Equal(t, stateRequested, o.state)
	assert.Equal(t, []orderEvent{{state: stateRequested, time: t0}}, o.history)
}

func TestBacktestRun_FatalHandlerErrorStopsRun(t *testing.T) {
	boom := errors.New("rejected")
	strat := &scriptStrategy{orders: opens(4)}
	h := &recordHandler{failAt: 2, err: boom}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3, 4), -1, nil), strat, h)

	require.ErrorIs(t, err, boom)
	assert.Len(t, strat.seen, 2, "no bars processed after the failure")
	assert.Len(t, h.got, 2)
}

func TestBacktestRun_DataErrorStopsRun(t *testing.T) {
	boom := errors.New("bad data")
	strat := &scriptStrategy{}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3), 2, boom), strat, h)

	require.ErrorIs(t, err, boom)
	assert.Len(t, strat.seen, 2, "bars before the error are processed, none after")
}

func TestBacktestRun_ContextCancelled(t *testing.T) {
	md := &marketData{data: closes(1, 2, 3, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	strat := &scriptStrategy{orders: opens(4)}
	h := &cancelAfterFirst{cancel: cancel}

	err := (&backtest{}).run(ctx, md.bars(ctx), strat, h)

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, strat.seen, 1)
}

type cancelAfterFirst struct{ cancel context.CancelFunc }

func (c *cancelAfterFirst) handleOrder(context.Context, *order) error {
	c.cancel()
	return nil
}

// captureLogs routes the default slog logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestBacktestRun_ExpectedRejectionIsRecordedLoggedAndRunContinues(t *testing.T) {
	logs := captureLogs(t)
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	stubClock(t, t0)
	o := &order{instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder}
	strat := &scriptStrategy{orders: []*order{o}}
	h := &recordHandler{failAt: 1, err: fmt.Errorf("open: %w", errInsufficientMargin)}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3), -1, nil), strat, h)

	require.NoError(t, err, "a margin rejection must not stop the run")
	assert.Len(t, strat.seen, 3, "bars after the rejection are still processed")
	require.Len(t, h.got, 1)

	assert.Equal(t, stateRejected, o.state)
	require.Len(t, o.history, 2)
	assert.Equal(t, stateRequested, o.history[0].state)
	assert.Equal(t, stateRejected, o.history[1].state)
	assert.Contains(t, o.history[1].reason, "insufficient margin")
	assert.Equal(t, t0.Add(time.Second), o.history[1].time)

	out := logs.String()
	assert.Contains(t, out, "to=rejected", "the state change is logged too")
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "order rejected")
	assert.Contains(t, out, "insufficient margin available")
	assert.Contains(t, out, "time=2010-01-04")
	assert.Contains(t, out, "instrument=EUR/USD")
	assert.Contains(t, out, "action=open")
	assert.Contains(t, out, "direction=long")
}

// rejectingHandler records the rejection itself, as account.size does.
type rejectingHandler struct{}

func (rejectingHandler) handleOrder(_ context.Context, o *order) error {
	o.record(stateRejected, errInsufficientMargin.Error())
	return errInsufficientMargin
}

func TestBacktestRun_DoesNotRecordRejectionTwice(t *testing.T) {
	captureLogs(t)
	o := openOrder()

	require.NoError(t, (&backtest{}).run(context.Background(), seq(closes(1), -1, nil),
		&scriptStrategy{orders: []*order{o}}, rejectingHandler{}))

	require.Len(t, o.history, 2)
	assert.Equal(t, []orderState{stateRequested, stateRejected},
		[]orderState{o.history[0].state, o.history[1].state})
}

func TestBacktestRun_FatalHandlerErrorIsNotLoggedAsRejection(t *testing.T) {
	logs := captureLogs(t)
	o := openOrder()
	h := &recordHandler{failAt: 1, err: errors.New("broker down")}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2), -1, nil), &scriptStrategy{orders: []*order{o}}, h)

	require.Error(t, err)
	assert.NotContains(t, logs.String(), "order rejected")
	assert.Equal(t, stateRequested, o.state, "a fatal error is not recorded as a rejection")
}

func TestBacktestRun_AcceptedRequestCallsHandlerOnceWithOrder(t *testing.T) {
	want := &order{instrument: "EUR/USD", direction: short, action: open, orderType: marketOrder}
	strat := &scriptStrategy{orders: []*order{want}}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(closes(1), -1, nil), strat, h)

	require.NoError(t, err)
	require.Len(t, h.got, 1)
	assert.Same(t, want, h.got[0])
	assert.Equal(t, "EUR/USD", h.got[0].instrument)
	assert.Equal(t, short, h.got[0].direction)
	assert.Equal(t, open, h.got[0].action)
}

// rejectFirstOpen rejects only the first open it sees for margin, then
// accepts everything, modelling margin that becomes available later.
type rejectFirstOpen struct {
	rejected bool
	got      []*order
}

func (h *rejectFirstOpen) handleOrder(_ context.Context, o *order) error {
	h.got = append(h.got, o)
	if o.action == open && !h.rejected {
		h.rejected = true
		return errInsufficientMargin
	}
	return nil
}

// tapStrategy wraps a strategy and records the bar index of every order it emits.
type tapStrategy struct {
	strategy
	bars int
	at   []int
}

func (s *tapStrategy) onBar(ctx context.Context, b bar) *order {
	o := s.strategy.onBar(ctx, b)
	if o != nil {
		s.at = append(s.at, s.bars)
	}
	s.bars++
	return o
}

func actions(orders []*order) (as []action) {
	for _, o := range orders {
		as = append(as, o.action)
	}
	return as
}

// §2 critical regression: a rejected entry must not become a late fill.
func TestMissedEntry_RejectedOpenIsNotRetried(t *testing.T) {
	captureLogs(t)
	e := &tapStrategy{strategy: newTestEmax(t, 2, 4)}
	h := &rejectFirstOpen{}

	// Bearish, one bullish cross at index 5, then bullish for many bars while
	// margin "becomes available" (the handler accepts any later open).
	bars := closes(10, 9, 8, 7, 6, 12, 14, 16, 18, 20, 22, 24, 26)
	err := (&backtest{}).run(context.Background(), seq(bars, -1, nil), e, h)

	require.NoError(t, err)
	require.Equal(t, []action{open}, actions(h.got), "the strategy asked once and never again")
	assert.True(t, h.rejected)
	assert.Equal(t, stateRejected, h.got[0].state)
	assert.Equal(t, []int{5}, e.at, "the one entry opportunity, bar 5")
}

// §2: a rejected entry must not suppress re-entry forever.
func TestMissedEntry_FreshSignalAfterRejectionIsAllowed(t *testing.T) {
	captureLogs(t)
	e := &tapStrategy{strategy: newTestEmax(t, 2, 4)}
	h := &rejectFirstOpen{}

	// bullish cross (rejected), bearish cross, then a fresh bullish cross.
	bars := closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2, 1, 14, 20, 30)
	err := (&backtest{}).run(context.Background(), seq(bars, -1, nil), e, h)

	require.NoError(t, err)
	require.Equal(t, []action{open, close, open}, actions(h.got))
	assert.Equal(t, stateRejected, h.got[0].state, "first opportunity was rejected")
	assert.Equal(t, stateRequested, h.got[2].state, "the fresh opportunity is not rejected")
	require.Len(t, e.at, 3)
	assert.Equal(t, 5, e.at[0])
	assert.Greater(t, e.at[2], e.at[1], "the fresh open comes after the close")
}

func TestBacktestRun_FatalHandlerErrorHasInstrumentAndTimeContext(t *testing.T) {
	boom := errors.New("broker down")
	h := &recordHandler{failAt: 2, err: boom}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3), -1, nil), &scriptStrategy{orders: opens(3)}, h)

	require.ErrorIs(t, err, boom, "cause preserved")
	assert.Contains(t, err.Error(), "TEST")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the second bar")
}

func TestBacktestRun_DataErrorHasContext(t *testing.T) {
	boom := errors.New("bad data")

	// error on the first bar: nothing processed yet
	err := (&backtest{}).run(context.Background(), seq(closes(1, 2), 0, boom),
		&scriptStrategy{}, &recordHandler{})
	require.ErrorIs(t, err, boom)

	// error after bars were processed: mentions the last good bar
	err = (&backtest{}).run(context.Background(), seq(closes(1, 2, 3), 2, boom),
		&scriptStrategy{}, &recordHandler{})
	require.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "TEST")
	assert.Contains(t, err.Error(), "2010-01-05", "last successfully processed bar")
}

func TestBacktestRun_InvalidOrderStopsRunBeforeHandler(t *testing.T) {
	bad := &order{instrument: "TEST", action: open} // open without a direction
	strat := &scriptStrategy{orders: []*order{nil, bad, openOrder()}}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3, 4), -1, nil), strat, h)

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "TEST")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the offending bar")
	assert.Len(t, strat.seen, 2, "no bars processed after the invalid order")
	assert.Empty(t, h.got, "the invalid order never reaches the handler")
	assert.Empty(t, bad.history, "an invalid order is never recorded as requested")
}
