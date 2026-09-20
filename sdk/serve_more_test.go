package sdk_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/sdk"
)

// TestServe_MissingSocketPathEnvProducesClearError exercises Serve
// itself (not just ServeContext), covering its own OS-signal-context
// setup/teardown wrapper.
func TestServe_MissingSocketPathEnvProducesClearError(t *testing.T) {
	require.NoError(t, os.Unsetenv(sdk.SocketPathEnv))
	err := sdk.Serve(newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest")))
	require.Error(t, err)
	require.Contains(t, err.Error(), sdk.SocketPathEnv)
}

// TestServeContext_MissingSocketPathEnvProducesClearError proves
// ServeContext fails immediately and explicitly when
// TRADER_STRATEGY_SOCKET is not set, rather than hanging or panicking.
func TestServeContext_MissingSocketPathEnvProducesClearError(t *testing.T) {
	t.Setenv(sdk.SocketPathEnv, "")
	require.NoError(t, os.Unsetenv(sdk.SocketPathEnv))

	err := sdk.ServeContext(context.Background(), newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest")))
	require.Error(t, err)
	require.Contains(t, err.Error(), sdk.SocketPathEnv)
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

	t.Setenv(sdk.SocketPathEnv, sockPath)

	guestCtx, cancelGuest := context.WithCancel(context.Background())
	defer cancelGuest()

	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- sdk.ServeContext(guestCtx, guest) }()

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
	guest.onBar = func(event sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
		bars, ok, err := view.HistoryBars(inst, iv, 5)
		require.NoError(t, err)
		require.True(t, ok)
		historyResult <- bars
		return nil, nil, nil
	}

	conn := h.dial()
	go func() { _ = sdk.ServeConn(ctx, conn, guest) }()

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
	guest.onBar = func(event sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
		return nil, nil, fmt.Errorf("boom")
	}

	conn := h.dial()
	go func() { _ = sdk.ServeConn(ctx, conn, guest) }()

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
	go func() { serveErrCh <- sdk.ServeConn(ctx, conn, guest) }()

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

// acceptingNoCapabilityHost accepts every Handshake but never
// negotiates any capability the guest requested — used to prove
// ServeConn's own capability-verification rejects this rather than
// silently proceeding.
type acceptingNoCapabilityHost struct {
	v1.UnimplementedStrategyHostServiceServer
}

func (acceptingNoCapabilityHost) Handshake(_ context.Context, req *v1.HandshakeRequest) (*v1.HandshakeResponse, error) {
	return &v1.HandshakeResponse{
		Accepted:        true,
		ProtocolVersion: v1.ProtocolVersion,
		SessionId:       "sess-test",
		Capabilities:    nil, // deliberately drops whatever req.GetCapabilities() requested
	}, nil
}

func dialFakeServer(t *testing.T, srv v1.StrategyHostServiceServer) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	v1.RegisterStrategyHostServiceServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestServeConn_CapabilityNotNegotiatedRejected proves a guest that
// implements FillHandler, but whose Handshake is accepted without
// CAPABILITY_FILL_HANDLER actually negotiated, fails explicitly
// rather than silently running without fills.
func TestServeConn_CapabilityNotNegotiatedRejected(t *testing.T) {
	conn := dialFakeServer(t, acceptingNoCapabilityHost{})

	guest := &testGuestStrategyWithFill{testGuestStrategy: newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))}
	err := sdk.ServeConn(context.Background(), conn, guest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "capability")
}

// hangingHandshakeHost never responds to Handshake, used to prove
// WithHandshakeTimeout actually bounds the wait.
type hangingHandshakeHost struct {
	v1.UnimplementedStrategyHostServiceServer
}

func (hangingHandshakeHost) Handshake(ctx context.Context, _ *v1.HandshakeRequest) (*v1.HandshakeResponse, error) {
	<-ctx.Done() // block until the caller's own per-RPC deadline fires
	return nil, ctx.Err()
}

func TestServeConn_HandshakeTimeout(t *testing.T) {
	conn := dialFakeServer(t, hangingHandshakeHost{})
	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))

	start := time.Now()
	err := sdk.ServeConn(context.Background(), conn, guest, sdk.WithHandshakeTimeout(200*time.Millisecond))
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, 2*time.Second, "ServeConn should fail once WithHandshakeTimeout elapses, not hang forever")
}

// hangingHistoryHost accepts Handshake/Run normally but never answers
// GetHistoryBars, used to prove WithHistoryBarsTimeout bounds that
// RPC too.
type hangingHistoryHost struct {
	v1.UnimplementedStrategyHostServiceServer
}

func (hangingHistoryHost) Handshake(_ context.Context, _ *v1.HandshakeRequest) (*v1.HandshakeResponse, error) {
	return &v1.HandshakeResponse{Accepted: true, ProtocolVersion: v1.ProtocolVersion, SessionId: "sess-test"}, nil
}

func (hangingHistoryHost) Run(stream v1.StrategyHostService_RunServer) error {
	if _, err := stream.Recv(); err != nil { // RunOpen
		return err
	}
	if err := stream.Send(&v1.RunServerMessage{Payload: &v1.RunServerMessage_SessionStart{
		SessionStart: &v1.SessionStart{RunId: "run-test", StartTimeUnixNanos: time.Now().UnixNano()},
	}}); err != nil {
		return err
	}
	inst := "fx:EUR/USD"
	if err := stream.Send(&v1.RunServerMessage{Payload: &v1.RunServerMessage_BarEvent{BarEvent: &v1.BarEvent{
		Sequence:     1,
		InstrumentId: inst,
		Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
		Bar:          testWireBarForHost(),
		Account:      &v1.AccountSnapshot{AccountId: "acc", Currency: "USD"},
	}}}); err != nil {
		return err
	}
	_, err := stream.Recv() // OnBarResponse
	return err
}

func (hangingHistoryHost) GetHistoryBars(ctx context.Context, _ *v1.GetHistoryBarsRequest) (*v1.GetHistoryBarsResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func testWireBarForHost() *v1.Bar {
	return &v1.Bar{
		TimeUnixNanos: time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC).UnixNano(),
		Open:          "1.1", High: "1.105", Low: "1.099", Close: "1.102",
		AvgSpread: "0.0001", MaxSpread: "0.0002", Ticks: 1,
	}
}

func TestServeConn_HistoryBarsTimeout(t *testing.T) {
	conn := dialFakeServer(t, hangingHistoryHost{})
	inst := eurUSD(t)
	iv := mustInterval(t)

	guest := newTestGuestStrategy(sdk.Descriptor{
		Name: "sdk_guest", Version: "1.0",
		Requirements: []sdk.DataRequirement{{Instrument: inst, Interval: iv}},
	})
	errCh := make(chan error, 1)
	guest.onBar = func(event sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
		_, _, err := view.HistoryBars(inst, iv, 5)
		errCh <- err
		return nil, nil, nil
	}

	go func() {
		_ = sdk.ServeConn(context.Background(), conn, guest, sdk.WithHistoryBarsTimeout(200*time.Millisecond))
	}()

	select {
	case err := <-errCh:
		require.Error(t, err, "a hanging GetHistoryBars RPC must surface as a real error, not ok=false")
	case <-time.After(3 * time.Second):
		t.Fatal("HistoryBars did not return after WithHistoryBarsTimeout elapsed")
	}
}

// endlessRunHost accepts Handshake/RunOpen but never sends SessionEnd
// before ending the stream — used to prove a bare EOF without
// SessionEnd is treated as abnormal termination, not success.
type endlessRunHost struct {
	v1.UnimplementedStrategyHostServiceServer
}

func (endlessRunHost) Handshake(_ context.Context, _ *v1.HandshakeRequest) (*v1.HandshakeResponse, error) {
	return &v1.HandshakeResponse{Accepted: true, ProtocolVersion: v1.ProtocolVersion, SessionId: "sess-test"}, nil
}

func (endlessRunHost) Run(stream v1.StrategyHostService_RunServer) error {
	if _, err := stream.Recv(); err != nil { // RunOpen
		return err
	}
	if err := stream.Send(&v1.RunServerMessage{Payload: &v1.RunServerMessage_SessionStart{
		SessionStart: &v1.SessionStart{RunId: "run-test", StartTimeUnixNanos: time.Now().UnixNano()},
	}}); err != nil {
		return err
	}
	return nil // ends the stream (EOF client-side) without ever sending session_end
}

func TestServeConn_EOFWithoutSessionEndIsAbnormal(t *testing.T) {
	conn := dialFakeServer(t, endlessRunHost{})
	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))

	err := sdk.ServeConn(context.Background(), conn, guest)
	require.Error(t, err, "a stream that ends without session_end must not look like a successful run")
}

// TestServeConn_WithLoggerIsUsedForEnvironment proves WithLogger's
// own logger is exactly the one a guest's Start sees on
// Environment.Logger — not a silently discarding default.
func TestServeConn_WithLoggerIsUsedForEnvironment(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancelGuest := context.WithCancel(context.Background())
	defer cancelGuest()

	logger, rec := logging.Capture()
	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))

	conn := h.dial()
	go func() { _ = sdk.ServeConn(ctx, conn, guest, sdk.WithLogger(logger)) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))

	select {
	case env := <-guest.startedCh:
		env.Logger.Info("hello from guest")
	case <-time.After(2 * time.Second):
		t.Fatal("guest Start was never called")
	}

	require.Eventually(t, func() bool {
		for _, r := range rec.Records() {
			if r.Message == "hello from guest" {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "the logger passed via WithLogger should have received the guest's own log record")
}

// describeOnceStrategy returns a different Descriptor.Requirements set
// on its second and later Describe() calls, simulating a stateful,
// time-sensitive, or simply buggy Describe implementation — used to
// prove ServeConn calls Describe() exactly once and reuses that same
// value for both the wire Handshake and its own local
// history-authorization set (review finding).
type describeOnceStrategy struct {
	*testGuestStrategy
	callCount int
	second    sdk.Descriptor
}

func (s *describeOnceStrategy) Describe() sdk.Descriptor {
	s.callCount++
	if s.callCount == 1 {
		return s.descriptor
	}
	return s.second
}

// TestServeConn_DescribeCalledOnceAndReused proves guestRun's own
// history-authorization set is built from the exact Descriptor sent at
// Handshake, not from a second, possibly divergent Describe() call.
func TestServeConn_DescribeCalledOnceAndReused(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := simpleSDKDescriptor(t, "sdk_guest") // requires EUR/USD H1
	otherInst := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	iv := mustInterval(t)
	second := sdk.Descriptor{
		Name:    "sdk_guest",
		Version: "1.0",
		Requirements: []sdk.DataRequirement{
			{Instrument: otherInst, Interval: iv}, // a requirement never sent at Handshake
		},
	}

	base := newTestGuestStrategy(first)
	guest := &describeOnceStrategy{testGuestStrategy: base, second: second}

	historyCh := make(chan struct {
		ok  bool
		err error
	}, 1)
	guest.onBar = func(_ sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
		_, ok, err := view.HistoryBars(otherInst, iv, 1)
		historyCh <- struct {
			ok  bool
			err error
		}{ok, err}
		return nil, nil, nil
	}

	conn := h.dial()
	go func() { _ = sdk.ServeConn(ctx, conn, guest) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))
	<-guest.startedCh

	event := strategy.BarEvent{
		Instrument: first.Requirements[0].Instrument,
		Interval:   first.Requirements[0].Interval,
		Bar:        testBar(t),
	}
	_, err = strat.OnBar(context.Background(), event, fakeView{acct: testFlatSnapshot(t)})
	require.NoError(t, err)

	select {
	case res := <-historyCh:
		require.False(t, res.ok, "GBP/USD H1 was never declared at Handshake — a second, divergent Describe() call must not locally authorize it")
		require.NoError(t, res.err)
	case <-time.After(2 * time.Second):
		t.Fatal("OnBar was never delivered to the guest")
	}
	require.GreaterOrEqual(t, guest.callCount, 1)
}

// TestWithLogger_NilDoesNotOverrideDefault proves passing a nil
// *slog.Logger to WithLogger leaves the non-nil default in place,
// honoring Environment.Logger's own "never nil" contract (review
// finding).
func TestWithLogger_NilDoesNotOverrideDefault(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))

	conn := h.dial()
	go func() { _ = sdk.ServeConn(ctx, conn, guest, sdk.WithLogger(nil)) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))

	select {
	case env := <-guest.startedCh:
		require.NotNil(t, env.Logger, "Environment.Logger must never be nil, even when WithLogger(nil) is passed")
	case <-time.After(2 * time.Second):
		t.Fatal("guest Start was never called")
	}
}
