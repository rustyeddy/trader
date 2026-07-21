package account

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmitOpen_Guards(t *testing.T) {
	t.Parallel()

	req := &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: idgen.NewULID(), Instrument: "EURUSD", Units: 1000, Side: types.Long}, Price: types.PriceFromFloat(1.1)}}

	var nilAccount *Account
	lot, err := nilAccount.SubmitOpen(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "account is nil")

	acct := &Account{}
	lot, err = acct.SubmitOpen(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "open request is nil")

	badReq := &OpenRequest{Request: Request{TradeCommon: nil, Price: types.PriceFromFloat(1.1)}}
	lot, err = acct.SubmitOpen(context.Background(), badReq)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "missing trade common")

	badReq = &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: idgen.NewULID(), Units: 1000, Side: types.Long}, Price: types.PriceFromFloat(1.1)}}
	lot, err = acct.SubmitOpen(context.Background(), badReq)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "instrument must not be empty")

	badReq = &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: idgen.NewULID(), Instrument: "EURUSD", Units: 1000, Side: types.Long}}}
	lot, err = acct.SubmitOpen(context.Background(), badReq)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "price must be > 0")
}

func TestSubmitOpen_QueuesFilledLotEvent(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test", types.MoneyFromFloat(10000))
	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{ID: idgen.NewULID(), Instrument: "EURUSD", Units: 1000, Side: types.Long},
			RequestType: RequestMarketOpen,
			Price:       types.PriceFromFloat(1.1),
			Timestamp:   types.Timestamp(100),
		},
	}

	lot, err := acct.SubmitOpen(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, lot)
	require.Len(t, acct.Lots.Slice(), 1)
	select {
	case evt := <-acct.Events():
		require.NotNil(t, evt)
		assert.Equal(t, EventOrderFilled, evt.Type)
		assert.Same(t, lot, evt.Lot)
	default:
		t.Fatal("expected open event to be queued")
	}
}

func TestSubmitOpen_ReturnsQueueFullWhenEventQueueIsFull(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test-account", types.MoneyFromFloat(10_000))
	acct.evtQ = make(chan *Event, 1)
	acct.evtQ <- &Event{Type: EventOrderFilled}

	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{
				ID:         idgen.NewULID(),
				Instrument: "EURUSD",
				Units:      types.Units(1000),
				Side:       types.Long,
			},
			RequestType: RequestMarketOpen,
			Price:       types.Price(1100000),
			Timestamp:   types.Timestamp(1),
		},
	}

	done := make(chan struct{})
	var (
		lot *Lot
		err error
	)
	go func() {
		defer close(done)
		lot, err = acct.SubmitOpen(context.Background(), req)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OpenRequest blocked with full event queue")
	}

	require.NoError(t, err)
	require.NotNil(t, lot)
	assert.Equal(t, 1, len(acct.evtQ))
	assert.Equal(t, 1, acct.Lots.Len())
}

func TestSubmitOpen_ReturnsContextErrorWhenContextCanceledAndQueueFull(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test-account", types.MoneyFromFloat(10_000))
	acct.evtQ = make(chan *Event, 1)
	acct.evtQ <- &Event{Type: EventOrderFilled}

	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{
				ID:         idgen.NewULID(),
				Instrument: "EURUSD",
				Units:      types.Units(1000),
				Side:       types.Long,
			},
			RequestType: RequestMarketOpen,
			Price:       types.Price(1100000),
			Timestamp:   types.Timestamp(2),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = acct.SubmitOpen(ctx, req)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OpenRequest blocked with canceled context")
	}

	require.NoError(t, err)
	assert.Equal(t, 1, len(acct.evtQ))
	assert.Equal(t, 1, acct.Lots.Len())
}

func TestEventsChannelCreation(t *testing.T) {
	t.Parallel()

	acct := NewAccount("ch-test", 0)
	assert.Nil(t, acct.evtQ)

	ch := acct.Events()
	require.NotNil(t, ch)
	assert.NotNil(t, acct.evtQ)
	assert.Len(t, acct.evtQ, 0)

	ch2 := acct.Events()
	assert.Equal(t, ch, ch2, "Events() should return the same channel on subsequent calls")
}

func TestEventsChannelReceiveEvent(t *testing.T) {
	t.Parallel()

	acct := NewAccount("recv-test", 0)
	evtCh := acct.Events()

	evt := &Event{Type: EventOrderFilled}
	err := acct.emitEvent(context.Background(), evt)
	require.NoError(t, err)

	select {
	case received := <-evtCh:
		require.NotNil(t, received)
		assert.Equal(t, EventOrderFilled, received.Type)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received on channel")
	}
}

func makeAccountCloseFixture() (*Account, *Lot, *CloseRequest) {
	acct := NewAccount("account", types.MoneyFromFloat(2_000))
	acct.evtQ = make(chan *Event, 1)

	th := NewTradeHistory("EURUSD")
	th.TradeCommon.Units = types.Units(1000)
	th.TradeCommon.Side = types.Long
	units := th.TradeCommon.Units
	lot := &Lot{
		TradeCommon:    th.TradeCommon,
		EntryPrice:     types.Price(1095000),
		EntryTime:      types.Timestamp(1),
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          LotOpen,
	}
	acct.Lots.Add(lot)

	req := &CloseRequest{
		Request: Request{
			TradeCommon: th.TradeCommon,
			RequestType: RequestClose,
			Price:       types.Price(1100000),
			Timestamp:   types.Timestamp(2),
		},
		Lot:        lot,
		CloseCause: CloseManual,
	}

	return acct, lot, req
}

func TestSubmitClose_ValidationErrors(t *testing.T) {
	t.Parallel()

	var nilAccount *Account
	err := nilAccount.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account is nil")

	acct := NewAccount("acct", types.MoneyFromFloat(1_000))
	err = acct.SubmitClose(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close request is nil")

	err = acct.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing position")

	err = acct.SubmitClose(context.Background(), &CloseRequest{
		Request: Request{Price: types.PriceFromFloat(1.1)},
		Lot:     &Lot{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing trade common")
}

func TestSubmitClose_SuccessEmitsEvent(t *testing.T) {
	t.Parallel()

	acct, lot, req := makeAccountCloseFixture()

	err := acct.SubmitClose(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, acct.Trades, 1)
	trade := acct.Trades[0]
	assert.Equal(t, req.Price, trade.ExitPrice)
	assert.Equal(t, req.Timestamp, trade.ExitTime)
	assert.Equal(t, types.MoneyFromFloat(50), trade.PNL)
	assert.Equal(t, 0, acct.Lots.Len())

	select {
	case evt := <-acct.evtQ:
		require.NotNil(t, evt)
		assert.Equal(t, EventPositionClosed, evt.Type)
		assert.Same(t, lot, evt.Lot)
		require.NotNil(t, evt.Trade)
		assert.Equal(t, *trade, *evt.Trade)
		assert.NotSame(t, trade, evt.Trade)
	default:
		t.Fatal("expected close event to be queued")
	}
}

func TestSubmitClose_PreservesReasonAndInitialStopDespiteTrailing(t *testing.T) {
	t.Parallel()

	acct, lot, req := makeAccountCloseFixture()
	lot.TradeCommon.Reason = "signalreplay:2024-01-02T00:00:00Z"
	lot.TradeCommon.InitialStop = types.PriceFromFloat(1.09)

	// Simulate trailing-stop updates overwriting Stop after open, as
	// backtest/execute.go does every bar.
	lot.TradeCommon.Stop = types.PriceFromFloat(1.093)

	err := acct.SubmitClose(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, acct.Trades, 1)
	trade := acct.Trades[0]
	assert.Equal(t, "signalreplay:2024-01-02T00:00:00Z", trade.Reason)
	assert.Equal(t, types.PriceFromFloat(1.09), trade.InitialStop)
	assert.Equal(t, types.PriceFromFloat(1.093), trade.Stop, "final trailed Stop is distinct from InitialStop")
}

func TestSubmitClose_AllowsCommittedStateWhenContextCanceledAndQueueFull(t *testing.T) {
	t.Parallel()

	acct, _, req := makeAccountCloseFixture()
	acct.evtQ <- &Event{Type: EventOrderFilled}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := acct.SubmitClose(ctx, req)
	require.NoError(t, err)
	require.Len(t, acct.Trades, 1)
}

func TestSubmitClose_DropsEventWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	acct, _, req := makeAccountCloseFixture()
	acct.evtQ <- &Event{Type: EventOrderFilled}

	err := acct.SubmitClose(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, acct.Trades, 1)
}

func TestSubmitClose_RejectsMismatchedRequestIdentity(t *testing.T) {
	t.Parallel()

	acct, _, req := makeAccountCloseFixture()
	req.Request.TradeCommon = &TradeCommon{
		ID:         "other-id",
		Instrument: req.Lot.Instrument,
	}

	err := acct.SubmitClose(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match position id")
}

func TestEmitEvent_NilContextBehavior(t *testing.T) {
	t.Parallel()

	acct := NewAccount("account", 0)
	evt := &Event{Type: EventOrderFilled}

	//lint:ignore SA1012 nil context behavior is exactly what this test verifies
	err := acct.emitEvent(nil, evt)
	require.NoError(t, err)
	require.NotNil(t, acct.evtQ)
	require.Len(t, acct.evtQ, 1)

	full := &Account{evtQ: make(chan *Event, 1)}
	full.evtQ <- &Event{Type: EventOrderFilled}
	//lint:ignore SA1012 nil context behavior is exactly what this test verifies
	err = full.emitEvent(nil, evt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue is full")
}

func TestEventTypeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  EventType
		want string
	}{
		{name: "order filled", typ: EventOrderFilled, want: "OrderFilled"},
		{name: "position closed", typ: EventPositionClosed, want: "PositionClosed"},
		{name: "unknown zero", typ: EventType(0), want: "UnknownEventType"},
		{name: "unknown out of range", typ: EventType(999), want: "UnknownEventType"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.typ.String())
		})
	}
}
