package strategysdk_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/strategysdk"
)

// TestServe_MissingSocketPathEnvProducesClearError exercises Serve
// itself (not just ServeContext), covering its own OS-signal-context
// setup/teardown wrapper.
func TestServe_MissingSocketPathEnvProducesClearError(t *testing.T) {
	require.NoError(t, os.Unsetenv(strategysdk.SocketPathEnv))
	err := strategysdk.Serve(newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest")))
	require.Error(t, err)
	require.Contains(t, err.Error(), strategysdk.SocketPathEnv)
}

// TestServeContext_MissingSocketPathEnvProducesClearError proves
// ServeContext fails immediately and explicitly when
// TRADER_STRATEGY_SOCKET is not set, rather than hanging or panicking.
func TestServeContext_MissingSocketPathEnvProducesClearError(t *testing.T) {
	t.Setenv(strategysdk.SocketPathEnv, "")
	require.NoError(t, os.Unsetenv(strategysdk.SocketPathEnv))

	err := strategysdk.ServeContext(context.Background(), newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest")))
	require.Error(t, err)
	require.Contains(t, err.Error(), strategysdk.SocketPathEnv)
}

// TestServeContext_RealUnixSocket proves Serve/ServeContext actually
// work end to end over a real Unix-domain socket file (not just
// bufconn) — issue #381's own "one minimal example binary compiles
// and serves over Unix socket" acceptance criterion, exercised
// directly rather than only implied by the example.
func TestServeContext_RealUnixSocket(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	lis, err := net.Listen("unix", sockPath)
	require.NoError(t, err)

	host := external.NewHost(logging.Discard())
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- host.Serve(ctx, lis) }()
	t.Cleanup(func() {
		cancel()
		<-served
	})

	t.Setenv(strategysdk.SocketPathEnv, sockPath)

	guestCtx, cancelGuest := context.WithCancel(context.Background())
	defer cancelGuest()

	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- strategysdk.ServeContext(guestCtx, guest) }()

	strat, err := host.Strategy(context.Background())
	require.NoError(t, err)
	require.Equal(t, "sdk_guest", strat.Describe().Name)

	cancelGuest()
	select {
	case <-serveErrCh:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeContext did not return after its ctx was canceled")
	}
}

// fakeViewWithHistory is a strategy.View that also satisfies
// strategy.History, letting the host answer a real GetHistoryBars RPC.
type fakeViewWithHistory struct {
	fakeView
	bars map[historyKey][]marketdata.Bar
}

type historyKey struct {
	inst     instrument.ID
	interval marketdata.Interval
}

func (v fakeViewWithHistory) HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) ([]marketdata.Bar, bool) {
	bars, ok := v.bars[historyKey{inst: instID, interval: interval}]
	if !ok {
		return nil, false
	}
	if n < len(bars) {
		bars = bars[len(bars)-n:]
	}
	return bars, true
}

// TestServeConn_HistoryBarsRoundTrip proves a guest's own
// View.HistoryBars call reaches the host's real GetHistoryBars RPC
// and returns real bars, oldest-first.
func TestServeConn_HistoryBarsRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancelGuest := context.WithCancel(context.Background())
	defer cancelGuest()

	inst := eurUSD(t)
	iv := mustInterval(t)
	older := testBar(t)
	older.Time = older.Time.Add(-time.Hour)

	historyResult := make(chan []marketdata.Bar, 1)
	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))
	guest.onBar = func(event strategysdk.BarEvent, view strategysdk.View) ([]strategysdk.DescribedIntent, []strategysdk.DescribedSignal, error) {
		bars, ok := view.HistoryBars(inst, iv, 5)
		require.True(t, ok)
		historyResult <- bars
		return nil, nil, nil
	}

	conn := h.dial()
	go func() { _ = strategysdk.ServeConn(ctx, conn, guest) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))
	<-guest.startedCh

	view := fakeViewWithHistory{
		fakeView: fakeView{acct: testFlatSnapshot(t)},
		bars:     map[historyKey][]marketdata.Bar{{inst: inst, interval: iv}: {older}},
	}
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}

	_, err = strat.OnBar(context.Background(), event, view)
	require.NoError(t, err)

	select {
	case bars := <-historyResult:
		require.Len(t, bars, 1)
		require.True(t, bars[0].Time.Equal(older.Time))
	case <-time.After(2 * time.Second):
		t.Fatal("guest never received history bars")
	}
}

// TestServeConn_OnBarCallbackErrorReportedToHost proves a guest's own
// OnBar error is reported to the host as a structured callback
// failure, not silently swallowed or crashing the session.
func TestServeConn_OnBarCallbackErrorReportedToHost(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancelGuest := context.WithCancel(context.Background())
	defer cancelGuest()

	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))
	guest.onBar = func(event strategysdk.BarEvent, view strategysdk.View) ([]strategysdk.DescribedIntent, []strategysdk.DescribedSignal, error) {
		return nil, nil, fmt.Errorf("boom")
	}

	conn := h.dial()
	go func() { _ = strategysdk.ServeConn(ctx, conn, guest) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))
	<-guest.startedCh

	event := strategy.BarEvent{Instrument: eurUSD(t), Interval: mustInterval(t), Bar: testBar(t)}
	_, err = strat.OnBar(context.Background(), event, fakeView{acct: testFlatSnapshot(t)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}

// TestServeConn_SessionEndFromHostReturnsCleanly proves a guest
// observes the host's own graceful SessionEnd (sent via
// ExternalStrategyAdapter's Close) and ServeConn returns nil.
func TestServeConn_SessionEndFromHostReturnsCleanly(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))

	conn := h.dial()
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- strategysdk.ServeConn(ctx, conn, guest) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))
	<-guest.startedCh

	closer, ok := strat.(interface{ Close(context.Context) error })
	require.True(t, ok)
	require.NoError(t, closer.Close(context.Background()))

	select {
	case err := <-serveErrCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ServeConn did not return after the host's own SessionEnd")
	}
}
