package sdk_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/logging"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/sdk"
)

// This file proves sdk against the real, production
// adapters/strategy/external.Host implementation (issue #379/#380),
// not a hand-rolled fake protocol server: a bufconn listener connects
// a real Host on one side to sdk.ServeConn on the other,
// exercising both halves of the v1 boundary together end to end —
// exactly the guarantee a unit test against either side alone cannot
// give.

// testIDs is a deterministic id.Generator for building fixture
// values on the "host application" side of these tests.
var testIDs = id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func mustRunID(t *testing.T) id.RunID {
	t.Helper()
	v, err := id.GenerateRunID(testIDs)
	require.NoError(t, err)
	return v
}

func mustAccountID(t *testing.T) id.AccountID {
	t.Helper()
	v, err := id.GenerateAccountID(testIDs)
	require.NoError(t, err)
	return v
}

func eurUSD(t *testing.T) instrument.ID {
	t.Helper()
	base := num.MustParseCurrency("EUR")
	quote := num.MustParseCurrency("USD")
	return instrument.CurrencyPairID(base, quote)
}

func eurUSDListing(t *testing.T) instrument.Listing {
	t.Helper()
	base := num.MustParseCurrency("EUR")
	quote := num.MustParseCurrency("USD")
	inst, err := instrument.NewCurrencyPair(base, quote)
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		quote,
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func testFlatSnapshot(t *testing.T) account.Snapshot {
	t.Helper()
	usd := num.MustParseCurrency("USD")
	zero := num.MustParseMoney("0", usd)
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       mustAccountID(t),
		Broker:          "sim",
		Currency:        usd,
		AsOf:            time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
		Equity:          zero,
		BuyingPower:     zero,
		MarginUsed:      zero,
		MarginAvailable: zero,
		RealizedPnL:     zero,
		UnrealizedPnL:   zero,
		Fees:            zero,
		Financing:       zero,
	})
	require.NoError(t, err)
	return snap
}

func testBar(t *testing.T) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:      time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
		Open:      num.MustParsePrice("1.1000"),
		High:      num.MustParsePrice("1.1050"),
		Low:       num.MustParsePrice("1.0990"),
		Close:     num.MustParsePrice("1.1020"),
		AvgSpread: num.MustParsePrice("0.0001"),
		MaxSpread: num.MustParsePrice("0.0002"),
		Ticks:     123,
	}
}

// fakeView is the minimal strategy.View the "host application" side
// of these tests drives Host-returned strategy.Strategy calls with.
type fakeView struct {
	acct account.Snapshot
}

func (v fakeView) Account() account.Snapshot { return v.acct }

// testGuestStrategy is a minimal sdk.Strategy whose behavior
// each test configures via its own fields/callbacks.
type testGuestStrategy struct {
	descriptor sdk.Descriptor

	startedCh chan sdk.Environment // Start sends its env here; buffered 1

	onBar func(event sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error)
}

func newTestGuestStrategy(descriptor sdk.Descriptor) *testGuestStrategy {
	return &testGuestStrategy{descriptor: descriptor, startedCh: make(chan sdk.Environment, 1)}
}

func (s *testGuestStrategy) Describe() sdk.Descriptor { return s.descriptor }

func (s *testGuestStrategy) Start(_ context.Context, env sdk.Environment) error {
	s.startedCh <- env
	return nil
}

func (s *testGuestStrategy) OnBar(_ context.Context, event sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
	if s.onBar == nil {
		return nil, nil, nil
	}
	return s.onBar(event, view)
}

// testGuestStrategyWithFill additionally implements
// sdk.FillHandler.
type testGuestStrategyWithFill struct {
	*testGuestStrategy
	onFill func(event sdk.FillEvent, view sdk.View) error
}

func (s *testGuestStrategyWithFill) OnFill(_ context.Context, event sdk.FillEvent, view sdk.View) error {
	if s.onFill == nil {
		return nil
	}
	return s.onFill(event, view)
}

func simpleSDKDescriptor(t *testing.T, name string) sdk.Descriptor {
	t.Helper()
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	return sdk.Descriptor{
		Name:    name,
		Version: "1.0",
		Requirements: []sdk.DataRequirement{
			{Instrument: eurUSD(t), Interval: iv, WarmupBars: 0},
		},
	}
}

// testHarness starts a real external.Host over a bufconn listener and
// connects a *grpc.ClientConn to it, giving a test both a bufconn
// listener (for a sdk.ServeConn caller to dial the SAME
// in-memory network) and a ready client connection.
type testHarness struct {
	t      *testing.T
	host   *external.Host
	lis    *bufconn.Listener
	cancel context.CancelFunc
	served chan error
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	host := external.NewHost(logging.Discard())

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- host.Serve(ctx, lis) }()

	h := &testHarness{t: t, host: host, lis: lis, cancel: cancel, served: served}
	t.Cleanup(h.stop)
	return h
}

func (h *testHarness) stop() {
	h.cancel()
	<-h.served
}

// dial returns a fresh *grpc.ClientConn to this harness's own bufconn
// listener — the same connection a real sdk guest would dial
// over the Unix-domain socket in production.
func (h *testHarness) dial() *grpc.ClientConn {
	h.t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return h.lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(h.t, err)
	h.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func testEnvironment(t *testing.T) strategy.Environment {
	t.Helper()
	return strategy.Environment{
		Clock: clock.NewSimulated(time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)),
		Intents: strategy.NewIntentFactory(
			clock.NewSimulated(time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)),
			testIDs, "guest"),
		Logger: logging.Discard(),
		RunID:  mustRunID(t),
	}
}

// TestServeConn_DescribeStartOnBarRoundTrip is the core acceptance
// path: sdk.ServeConn, driving a real testGuestStrategy,
// completes Handshake against a real external.Host and a described
// intent becomes a real canonical order.Intent host-side.
func TestServeConn_DescribeStartOnBarRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancelGuest := context.WithCancel(context.Background())
	defer cancelGuest()

	inst := eurUSD(t)
	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))
	guest.onBar = func(event sdk.BarEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
		require.True(t, event.Instrument.Equal(inst))
		require.Equal(t, "USD", view.Account().Currency)
		return []sdk.DescribedIntent{sdk.Enter(inst, order.Buy)}, nil, nil
	}

	conn := h.dial()
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- sdk.ServeConn(ctx, conn, guest) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.Equal(t, "sdk_guest", strat.Describe().Name)

	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))

	select {
	case env := <-guest.startedCh:
		require.NotEmpty(t, env.RunID)
		require.False(t, env.Clock.Now().IsZero())
	case <-time.After(2 * time.Second):
		t.Fatal("guest Start was never called")
	}

	event := strategy.BarEvent{Instrument: inst, Interval: mustInterval(t), Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	intents, err := strat.OnBar(context.Background(), event, view)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentEnter, intents[0].Kind)
	require.Equal(t, order.Buy, intents[0].Side)
	require.False(t, intents[0].IntentID.IsZero())

	cancelGuest()
	select {
	case <-serveErrCh:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeConn did not return after its ctx was canceled")
	}
}

func mustInterval(t *testing.T) marketdata.Interval {
	t.Helper()
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	return iv
}

// TestServeConn_FillHandlerRoundTrip proves a guest implementing
// sdk.FillHandler negotiates CAPABILITY_FILL_HANDLER and
// receives a real FillEvent.
func TestServeConn_FillHandlerRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancelGuest := context.WithCancel(context.Background())
	defer cancelGuest()

	inst := eurUSD(t)
	onFillCalled := make(chan sdk.FillEvent, 1)
	guest := &testGuestStrategyWithFill{
		testGuestStrategy: newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest")),
		onFill: func(event sdk.FillEvent, view sdk.View) error {
			onFillCalled <- event
			return nil
		},
	}

	conn := h.dial()
	go func() { _ = sdk.ServeConn(ctx, conn, guest) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)

	fillHandler, ok := strat.(strategy.FillHandler)
	require.True(t, ok, "negotiated capability must select the FillHandler-satisfying wrapper host-side")

	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))
	<-guest.startedCh

	fill, err := runtimeorder.NewFill(runtimeorder.Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   eurUSDListing(t),
		Side:      order.Buy,
		Price:     num.MustParsePrice("1.1005"),
		Quantity:  num.MustParseQuantity("1000"),
		Timestamp: time.Now().UTC(),
	})
	require.NoError(t, err)

	err = fillHandler.OnFill(context.Background(), strategy.FillEvent{Fill: fill}, fakeView{acct: testFlatSnapshot(t)})
	require.NoError(t, err)

	select {
	case got := <-onFillCalled:
		require.True(t, got.Instrument.Equal(inst))
		require.Equal(t, order.Buy, got.Side)
	case <-time.After(2 * time.Second):
		t.Fatal("guest OnFill was never called")
	}
}

func mustFillID(t *testing.T) id.FillID {
	t.Helper()
	v, err := id.GenerateFillID(testIDs)
	require.NoError(t, err)
	return v
}

func mustOrderID(t *testing.T) id.OrderID {
	t.Helper()
	v, err := id.GenerateOrderID(testIDs)
	require.NoError(t, err)
	return v
}

// rejectingHost is a minimal fake StrategyHostServiceServer whose
// Handshake always rejects with PROTOCOL_VERSION_MISMATCH — used to
// exercise ServeConn's own rejection-handling path without needing a
// real host that actually disagrees with this SDK's own
// v1.ProtocolVersion constant (there is none in this repo to disagree
// with it).
type rejectingHost struct {
	v1.UnimplementedStrategyHostServiceServer
}

func (rejectingHost) Handshake(context.Context, *v1.HandshakeRequest) (*v1.HandshakeResponse, error) {
	return &v1.HandshakeResponse{
		Accepted:        false,
		ProtocolVersion: "v99",
		RejectReason: &v1.Error{
			Code:    v1.ErrorCode_ERROR_CODE_PROTOCOL_VERSION_MISMATCH,
			Message: "host speaks v99, this SDK speaks v1",
		},
	}, nil
}

// TestServeConn_ProtocolVersionMismatchProducesClearError proves a
// rejected Handshake surfaces a clear, explicit error from ServeConn
// — issue #381's own "protocol mismatch produces a clear error"
// acceptance criterion — rather than an opaque or silently-ignored
// failure.
func TestServeConn_ProtocolVersionMismatchProducesClearError(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	v1.RegisterStrategyHostServiceServer(server, rejectingHost{})
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "sdk_guest"))
	err = sdk.ServeConn(context.Background(), conn, guest)
	require.Error(t, err)

	var wireErr *sdk.WireError
	require.ErrorAs(t, err, &wireErr)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_PROTOCOL_VERSION_MISMATCH, wireErr.Code)
}
