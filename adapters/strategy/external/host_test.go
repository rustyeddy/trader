package external_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
)

// fakeView is the minimal strategy.View this test suite drives OnBar/
// OnFill with. It additionally satisfies strategy.History when
// history is non-nil, so tests can exercise both the "history
// available" and "view does not support history" GetHistoryBars
// paths against two distinct concrete types (Go method sets are
// static — see core.go's own doc comment on exactly this point).
type fakeView struct {
	acct    account.Snapshot
	history map[historyKey][]marketdata.Bar
}

type historyKey struct {
	inst     instrument.ID
	interval marketdata.Interval
}

func (v fakeView) Account() account.Snapshot { return v.acct }

func (v fakeView) HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) ([]marketdata.Bar, bool) {
	bars, ok := v.history[historyKey{inst: instID, interval: interval}]
	if !ok {
		return nil, false
	}
	if n < len(bars) {
		bars = bars[len(bars)-n:]
	}
	return bars, true
}

// fakeViewNoHistory satisfies only strategy.View, never
// strategy.History — used to prove GetHistoryBars fails explicitly
// against a view that does not support lookback, rather than
// panicking on a failed type assertion.
type fakeViewNoHistory struct {
	acct account.Snapshot
}

func (v fakeViewNoHistory) Account() account.Snapshot { return v.acct }

// testHarness bundles a live Host with a raw gRPC client dialed
// against it over an in-process bufconn listener — issue #379's own
// "unit tests use a fake/in-process protocol server" acceptance
// criterion.
type testHarness struct {
	t      *testing.T
	host   *external.Host
	client v1.StrategyHostServiceClient
	cancel context.CancelFunc
	served chan error
}

func newTestHarness(t *testing.T, opts ...external.HostOption) *testHarness {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	host := external.NewHost(logging.Discard(), opts...)

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- host.Serve(ctx, lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	h := &testHarness{
		t:      t,
		host:   host,
		client: v1.NewStrategyHostServiceClient(conn),
		cancel: cancel,
		served: served,
	}
	t.Cleanup(h.stop)
	return h
}

func (h *testHarness) stop() {
	h.cancel()
	<-h.served
}

// handshake performs a Handshake and returns the accepted session_id.
func (h *testHarness) handshake(ctx context.Context, descriptor *v1.StrategyDescriptor, capabilities []v1.Capability) *v1.HandshakeResponse {
	h.t.Helper()
	resp, err := h.client.Handshake(ctx, &v1.HandshakeRequest{
		ProtocolVersion:    v1.ProtocolVersion,
		StrategyDescriptor: descriptor,
		Capabilities:       capabilities,
	})
	require.NoError(h.t, err)
	return resp
}

// openRun opens the Run stream and sends its own mandatory first
// RunOpen message.
func (h *testHarness) openRun(ctx context.Context, sessionID string) v1.StrategyHostService_RunClient {
	h.t.Helper()
	stream, err := h.client.Run(ctx)
	require.NoError(h.t, err)
	require.NoError(h.t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_RunOpen{RunOpen: &v1.RunOpen{SessionId: sessionID}}}))
	return stream
}

func simpleDescriptor(t *testing.T, name string) *v1.StrategyDescriptor {
	t.Helper()
	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	wireIv, err := external.ToWireInterval(iv)
	require.NoError(t, err)
	return &v1.StrategyDescriptor{
		Name:    name,
		Version: "1.0",
		Requirements: []*v1.DataRequirement{
			{InstrumentId: inst.String(), Interval: wireIv, WarmupBars: 0},
		},
	}
}

func testEnvironment(t *testing.T) strategy.Environment {
	t.Helper()
	return strategy.Environment{
		Clock:   clock.NewSimulated(time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)),
		Intents: newIntentFactory(t, "guest"),
		Logger:  logging.Discard(),
		RunID:   mustRunID(t),
	}
}

// TestHost_OnBarRoundTrip is the core acceptance-criteria path:
// Describe/Start/OnBar all translate through the v1 protocol, and a
// guest-described intent becomes a real canonical order.Intent built
// through the retained IntentFactory.
func TestHost_OnBarRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	descriptor := simpleDescriptor(t, "guest_strategy")
	hsResp := h.handshake(ctx, descriptor, nil)
	require.True(t, hsResp.GetAccepted())
	require.NotEmpty(t, hsResp.GetSessionId())

	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)
	require.Equal(t, "guest_strategy", strat.Describe().Name)

	require.NoError(t, strat.Start(ctx, testEnvironment(t)))

	sm, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, sm.GetSessionStart())

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	type result struct {
		intents []order.Intent
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		intents, err := strat.OnBar(ctx, event, view)
		resultCh <- result{intents, err}
	}()

	sm, err = stream.Recv()
	require.NoError(t, err)
	be := sm.GetBarEvent()
	require.NotNil(t, be)
	require.Equal(t, inst.String(), be.GetInstrumentId())
	require.NotNil(t, be.GetAccount())

	require.NoError(t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{OnBarResponse: &v1.OnBarResponse{
		Sequence: be.GetSequence(),
		Intents: []*v1.DescribedIntent{
			{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY},
		},
		Signals: []*v1.DescribedSignal{
			{Strategy: "guest_strategy", Values: map[string]string{"reason": "cross"}},
		},
	}}}))

	res := <-resultCh
	require.NoError(t, res.err)
	require.Len(t, res.intents, 1)
	require.Equal(t, order.IntentEnter, res.intents[0].Kind)
	require.Equal(t, order.Buy, res.intents[0].Side)
	require.False(t, res.intents[0].IntentID.IsZero())
}

// recordingJournal is a minimal journal.Recorder test double that
// retains every journal.Record it receives.
type recordingJournal struct {
	mu      sync.Mutex
	records []journal.Record
}

func (r *recordingJournal) Record(_ context.Context, rec journal.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *recordingJournal) Close() error { return nil }

// TestHost_OnBar_RecordsSignals proves a described signal is
// journaled with the exact real CorrelationID minted for its
// matching intent group — issue #384's own deterministic-equivalence
// requirement, exercised end to end through the actual wire protocol.
func TestHost_OnBar_RecordsSignals(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)

	rec := &recordingJournal{}
	runID := mustRunID(t)
	env := testEnvironment(t)
	env.Journal = rec
	env.RunID = runID
	require.NoError(t, strat.Start(ctx, env))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	type result struct {
		intents []order.Intent
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		intents, err := strat.OnBar(ctx, event, view)
		resultCh <- result{intents, err}
	}()

	sm, err := stream.Recv()
	require.NoError(t, err)
	be := sm.GetBarEvent()

	require.NoError(t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{OnBarResponse: &v1.OnBarResponse{
		Sequence: be.GetSequence(),
		Intents: []*v1.DescribedIntent{
			{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, CorrelationToken: "grp"},
		},
		Signals: []*v1.DescribedSignal{
			{Strategy: "guest_strategy", Values: map[string]string{"reason": "cross"}, CorrelationToken: "grp"},
		},
	}}}))

	res := <-resultCh
	require.NoError(t, res.err)
	require.Len(t, res.intents, 1)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.records, 1)
	got := rec.records[0]
	require.Equal(t, journal.KindSignal, got.Kind)
	require.Equal(t, runID, got.RunID)
	require.Equal(t, "guest_strategy", got.Signal.Strategy)
	require.Equal(t, "cross", got.Signal.Values["reason"])
	require.True(t, got.Metadata.CorrelationID.Equal(res.intents[0].Metadata.CorrelationID))
	require.Equal(t, event.Bar.Time, got.Metadata.Timestamp)
}

// TestHost_OnBar_InvalidSignalFailsExplicitly proves a malformed
// described signal (empty strategy) fails OnBar explicitly rather
// than being journaled or silently dropped.
func TestHost_OnBar_InvalidSignalFailsExplicitly(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)

	rec := &recordingJournal{}
	env := testEnvironment(t)
	env.Journal = rec
	require.NoError(t, strat.Start(ctx, env))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	errCh := make(chan error, 1)
	go func() {
		_, err := strat.OnBar(ctx, event, view)
		errCh <- err
	}()

	sm, err := stream.Recv()
	require.NoError(t, err)
	be := sm.GetBarEvent()

	require.NoError(t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{OnBarResponse: &v1.OnBarResponse{
		Sequence: be.GetSequence(),
		Signals:  []*v1.DescribedSignal{{Values: map[string]string{"reason": "cross"}}}, // empty Strategy
	}}}))

	err = <-errCh
	require.ErrorIs(t, err, external.ErrInvalidWireValue)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Empty(t, rec.records)
}

// TestHost_OnBarCallbackErrorPropagates proves a guest-reported
// callback failure (OnBarResponse.error) surfaces as a Go error from
// OnBar, not a silently empty intent list.
func TestHost_OnBarCallbackErrorPropagates(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)
	require.NoError(t, strat.Start(ctx, testEnvironment(t)))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	errCh := make(chan error, 1)
	go func() {
		_, err := strat.OnBar(ctx, event, view)
		errCh <- err
	}()

	sm, err := stream.Recv()
	require.NoError(t, err)
	be := sm.GetBarEvent()

	require.NoError(t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{OnBarResponse: &v1.OnBarResponse{
		Sequence: be.GetSequence(),
		Error:    external.ToWireError(v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED, nil),
	}}}))

	err = <-errCh
	require.Error(t, err)

	var wireErr *external.WireError
	require.ErrorAs(t, err, &wireErr)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED, wireErr.Code)
}

// TestHost_OnBarContextCancellation is the acceptance criterion that
// context cancellation propagates predictably: a guest that never
// responds must not hang OnBar forever.
func TestHost_OnBarContextCancellation(t *testing.T) {
	h := newTestHarness(t)
	handshakeCtx := context.Background()

	hsResp := h.handshake(handshakeCtx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(handshakeCtx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(handshakeCtx)
	require.NoError(t, err)
	require.NoError(t, strat.Start(handshakeCtx, testEnvironment(t)))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	onBarCtx, cancelOnBar := context.WithTimeout(handshakeCtx, 200*time.Millisecond)
	defer cancelOnBar()

	_, err = strat.OnBar(onBarCtx, event, view) // guest never responds
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestHost_FillHandlerCapabilityNegotiated proves a negotiated
// CAPABILITY_FILL_HANDLER both selects the FillHandler-satisfying
// wrapper and round-trips a real OnFill call.
func TestHost_FillHandlerCapabilityNegotiated(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), []v1.Capability{v1.Capability_CAPABILITY_FILL_HANDLER})
	require.Equal(t, []v1.Capability{v1.Capability_CAPABILITY_FILL_HANDLER}, hsResp.GetCapabilities())
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)

	fillHandler, ok := strat.(strategy.FillHandler)
	require.True(t, ok, "negotiated capability must select the FillHandler-satisfying wrapper")

	require.NoError(t, strat.Start(ctx, testEnvironment(t)))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	fill, err := order.NewFill(order.Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   eurUSDListing(t),
		Side:      order.Buy,
		Price:     num.MustParsePrice("1.1005"),
		Quantity:  num.MustParseQuantity("1000"),
		Timestamp: time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	view := fakeView{acct: testFlatSnapshot(t)}

	errCh := make(chan error, 1)
	go func() {
		errCh <- fillHandler.OnFill(ctx, strategy.FillEvent{Fill: fill}, view)
	}()

	sm, err := stream.Recv()
	require.NoError(t, err)
	fe := sm.GetFillEvent()
	require.NotNil(t, fe)
	require.Equal(t, fill.OrderID.String(), fe.GetOrderId())

	require.NoError(t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnFillResponse{OnFillResponse: &v1.OnFillResponse{
		Sequence: fe.GetSequence(),
	}}}))

	require.NoError(t, <-errCh)
}

// TestHost_NoCapabilityNegotiatedDoesNotSatisfyFillHandler proves the
// converse: with no capabilities negotiated, the returned
// strategy.Strategy must not satisfy strategy.FillHandler at all — Go
// method sets are static (core.go's own doc comment).
func TestHost_NoCapabilityNegotiatedDoesNotSatisfyFillHandler(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	require.Empty(t, hsResp.GetCapabilities())
	h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)

	_, ok := strat.(strategy.FillHandler)
	require.False(t, ok)
}

// TestHost_GetHistoryBars proves the GetHistoryBars RPC answers
// against the exact frozen View the in-flight callback was built
// from.
func TestHost_GetHistoryBars(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)
	require.NoError(t, strat.Start(ctx, testEnvironment(t)))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)

	older := testBar(t)
	older.Time = older.Time.Add(-time.Hour)
	view := fakeView{
		acct:    testFlatSnapshot(t),
		history: map[historyKey][]marketdata.Bar{{inst: inst, interval: iv}: {older}},
	}
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}

	onBarDone := make(chan struct{})
	go func() {
		_, _ = strat.OnBar(ctx, event, view)
		close(onBarDone)
	}()

	sm, err := stream.Recv()
	require.NoError(t, err)
	be := sm.GetBarEvent()
	require.NotNil(t, be)

	histResp, err := h.client.GetHistoryBars(ctx, &v1.GetHistoryBarsRequest{
		SessionId:        hsResp.GetSessionId(),
		CallbackSequence: be.GetSequence(),
		InstrumentId:     inst.String(),
		Interval:         be.GetInterval(),
		Count:            5,
	})
	require.NoError(t, err)
	require.Len(t, histResp.GetBars(), 1)
	require.Equal(t, older.Time.UTC().UnixNano(), histResp.GetBars()[0].GetTimeUnixNanos())

	// Unblock OnBar so the test can clean up.
	require.NoError(t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{OnBarResponse: &v1.OnBarResponse{
		Sequence: be.GetSequence(),
	}}}))
	<-onBarDone
}

// TestHost_GetHistoryBars_ViewWithoutHistoryCapability proves this
// fails explicitly (not a panic) when the run's own View does not
// implement strategy.History.
func TestHost_GetHistoryBars_ViewWithoutHistoryCapability(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)
	require.NoError(t, strat.Start(ctx, testEnvironment(t)))
	_, err = stream.Recv()
	require.NoError(t, err)

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeViewNoHistory{acct: testFlatSnapshot(t)}

	onBarDone := make(chan struct{})
	go func() {
		_, _ = strat.OnBar(ctx, event, view)
		close(onBarDone)
	}()

	sm, err := stream.Recv()
	require.NoError(t, err)
	be := sm.GetBarEvent()

	_, err = h.client.GetHistoryBars(ctx, &v1.GetHistoryBarsRequest{
		SessionId:        hsResp.GetSessionId(),
		CallbackSequence: be.GetSequence(),
		InstrumentId:     inst.String(),
		Interval:         be.GetInterval(),
		Count:            5,
	})
	require.Error(t, err)

	require.NoError(t, stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{OnBarResponse: &v1.OnBarResponse{
		Sequence: be.GetSequence(),
	}}}))
	<-onBarDone
}

// TestHost_Handshake_ProtocolVersionMismatchRejected proves a guest
// declaring a different protocol version is rejected explicitly,
// through HandshakeResponse itself, not a transport-level error.
func TestHost_Handshake_ProtocolVersionMismatchRejected(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	resp, err := h.client.Handshake(ctx, &v1.HandshakeRequest{
		ProtocolVersion:    "v99",
		StrategyDescriptor: simpleDescriptor(t, "guest_strategy"),
	})
	require.NoError(t, err)
	require.False(t, resp.GetAccepted())
	require.Empty(t, resp.GetSessionId())
	require.Equal(t, v1.ErrorCode_ERROR_CODE_PROTOCOL_VERSION_MISMATCH, resp.GetRejectReason().GetCode())
}

// TestHost_Handshake_InvalidDescriptorRejected proves a malformed
// descriptor fails the RPC explicitly rather than producing a session
// for a strategy the host could not actually describe.
func TestHost_Handshake_InvalidDescriptorRejected(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	_, err := h.client.Handshake(ctx, &v1.HandshakeRequest{
		ProtocolVersion: v1.ProtocolVersion,
		StrategyDescriptor: &v1.StrategyDescriptor{
			Name: "bad",
			Requirements: []*v1.DataRequirement{
				{InstrumentId: "not-a-real-id", Interval: &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1}},
			},
		},
	})
	require.Error(t, err)
}

// TestHost_Close_SendsSessionEnd exercises the Close escape hatch
// core.go documents (not part of strategy.Strategy itself).
func TestHost_Close_SendsSessionEnd(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)
	require.NoError(t, strat.Start(ctx, testEnvironment(t)))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	closer, ok := strat.(interface{ Close(context.Context) error })
	require.True(t, ok)
	require.NoError(t, closer.Close(ctx))

	sm, err := stream.Recv()
	require.NoError(t, err)
	end := sm.GetSessionEnd()
	require.NotNil(t, end)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_UNSPECIFIED, end.GetCode())
}

// TestHost_Close_EndsRunStream extends TestHost_Close_SendsSessionEnd:
// after SessionEnd is sent, the Run RPC itself must actually end (a
// prior bug sent SessionEnd but left the Run handler still receiving,
// so a guest that kept its stream open stayed "active" forever).
func TestHost_Close_EndsRunStream(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)
	require.NoError(t, strat.Start(ctx, testEnvironment(t)))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	closer, ok := strat.(interface{ Close(context.Context) error })
	require.True(t, ok)
	require.NoError(t, closer.Close(ctx))

	_, err = stream.Recv() // session_end
	require.NoError(t, err)

	_, err = stream.Recv() // the RPC itself must now be over
	require.Error(t, err)
}

// TestHost_CallbackTimeout_TearsDownSession is ADR-062's own
// supervision contract end to end: a guest that never responds to a
// BarEvent must not block OnBar (or the session) forever — the fired
// per-callback timer both fails the in-flight OnBar call and ends the
// Run stream, so no later callback can be sent to the unresponsive
// guest either.
func TestHost_CallbackTimeout_TearsDownSession(t *testing.T) {
	h := newTestHarness(t, external.WithCallbackTimeout(100*time.Millisecond))
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	stream := h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)
	require.NoError(t, strat.Start(ctx, testEnvironment(t)))
	_, err = stream.Recv() // session_start
	require.NoError(t, err)

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	_, err = strat.OnBar(context.Background(), event, view) // guest never responds; no ctx timeout of our own
	require.Error(t, err)

	_, err = stream.Recv() // the bar_event itself, sent but never answered
	require.NoError(t, err)

	// The Run stream itself must now be over — a later callback could
	// never reach this guest either.
	_, err = stream.Recv()
	require.Error(t, err)
}

// TestHost_OneSessionAtATime_EnforcedAfterClaim proves the review's
// blocking finding is fixed: a second Handshake is rejected while the
// first session remains active even after Host.Strategy has already
// claimed its strategy (the old channel-buffer check alone stopped
// enforcing once drained).
func TestHost_OneSessionAtATime_EnforcedAfterClaim(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "first"), nil)
	require.True(t, hsResp.GetAccepted())
	h.openRun(ctx, hsResp.GetSessionId())

	_, err := h.host.Strategy(ctx) // claims the first strategy, draining any buffer
	require.NoError(t, err)

	second, err := h.client.Handshake(ctx, &v1.HandshakeRequest{
		ProtocolVersion:    v1.ProtocolVersion,
		StrategyDescriptor: simpleDescriptor(t, "second"),
	})
	require.Error(t, err, "a second handshake must be rejected while the first session is still active")
	require.Nil(t, second)
}

// TestHost_AbandonedHandshake_ReclaimedAfterAdmissionTimeout proves a
// Handshake that never opens its own Run stream does not hold this
// Host's one session slot forever.
func TestHost_AbandonedHandshake_ReclaimedAfterAdmissionTimeout(t *testing.T) {
	h := newTestHarness(t, external.WithAdmissionTimeout(100*time.Millisecond))
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "abandoned"), nil)
	require.True(t, hsResp.GetAccepted())
	// Deliberately never open Run for this session.

	require.Eventually(t, func() bool {
		resp, err := h.client.Handshake(ctx, &v1.HandshakeRequest{
			ProtocolVersion:    v1.ProtocolVersion,
			StrategyDescriptor: simpleDescriptor(t, "second"),
		})
		return err == nil && resp.GetAccepted()
	}, 2*time.Second, 20*time.Millisecond, "the abandoned session's slot should free up after its admission timeout")
}

// TestHost_Start_RejectsJournalWithZeroRunID mirrors strategy/smatrend's
// own Start-time check: env.Journal set with a zero env.RunID can
// never build a valid journal.Record, so this must fail at Start,
// not later at the first signal.
func TestHost_Start_RejectsJournalWithZeroRunID(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	hsResp := h.handshake(ctx, simpleDescriptor(t, "guest_strategy"), nil)
	h.openRun(ctx, hsResp.GetSessionId())

	strat, err := h.host.Strategy(ctx)
	require.NoError(t, err)

	env := testEnvironment(t)
	env.Journal = &recordingJournal{}
	env.RunID = id.RunID{} // zero value

	err = strat.Start(ctx, env)
	require.Error(t, err)
}
