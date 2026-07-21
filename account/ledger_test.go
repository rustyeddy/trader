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

func TestBrokerSubmitOpen_Guards(t *testing.T) {
	t.Parallel()

	req := &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: idgen.NewULID(), Instrument: "EURUSD", Units: 1000, Side: types.Long}, Price: types.PriceFromFloat(1.1)}}

	var nilBroker *Ledger
	lot, err := nilBroker.SubmitOpen(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "broker is nil")

	b := &Ledger{}
	lot, err = b.SubmitOpen(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "broker account is nil")

	b.Account = NewAccount("test", types.MoneyFromFloat(10000))
	lot, err = b.SubmitOpen(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "open request is nil")

	badReq := &OpenRequest{Request: Request{TradeCommon: nil, Price: types.PriceFromFloat(1.1)}}
	lot, err = b.SubmitOpen(context.Background(), badReq)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "missing trade common")

	badReq = &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: idgen.NewULID(), Units: 1000, Side: types.Long}, Price: types.PriceFromFloat(1.1)}}
	lot, err = b.SubmitOpen(context.Background(), badReq)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "instrument must not be empty")

	badReq = &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: idgen.NewULID(), Instrument: "EURUSD", Units: 1000, Side: types.Long}}}
	lot, err = b.SubmitOpen(context.Background(), badReq)
	require.Error(t, err)
	assert.Nil(t, lot)
	assert.Contains(t, err.Error(), "price must be > 0")
}

func TestBrokerSubmitOpen_QueuesFilledLotEvent(t *testing.T) {
	t.Parallel()

	b := &Ledger{Account: NewAccount("test", types.MoneyFromFloat(10000))}
	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{ID: idgen.NewULID(), Instrument: "EURUSD", Units: 1000, Side: types.Long},
			RequestType: RequestMarketOpen,
			Price:       types.PriceFromFloat(1.1),
			Timestamp:   types.Timestamp(100),
		},
	}

	lot, err := b.SubmitOpen(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, lot)
	require.Len(t, b.Account.Lots.Slice(), 1)
	select {
	case evt := <-b.Events():
		require.NotNil(t, evt)
		assert.Equal(t, EventOrderFilled, evt.Type)
		assert.Same(t, lot, evt.Lot)
	default:
		t.Fatal("expected open event to be queued")
	}
}

func TestBrokerOpenRequestReturnsQueueFullWhenEventQueueIsFull(t *testing.T) {
	t.Parallel()

	b := &Ledger{
		Account: NewAccount("test-account", types.MoneyFromFloat(10_000)),
		evtQ:    make(chan *Event, 1),
	}
	b.evtQ <- &Event{Type: EventOrderFilled}

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
		lot, err = b.SubmitOpen(context.Background(), req)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OpenRequest blocked with full event queue")
	}

	require.NoError(t, err)
	require.NotNil(t, lot)
	assert.Equal(t, 1, len(b.evtQ))
	assert.Equal(t, 1, b.Account.Lots.Len())
}

func TestBrokerOpenRequestReturnsContextErrorWhenContextCanceledAndQueueFull(t *testing.T) {
	t.Parallel()

	b := &Ledger{
		Account: NewAccount("test-account", types.MoneyFromFloat(10_000)),
		evtQ:    make(chan *Event, 1),
	}
	b.evtQ <- &Event{Type: EventOrderFilled}

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
		_, err = b.SubmitOpen(ctx, req)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OpenRequest blocked with canceled context")
	}

	require.NoError(t, err)
	assert.Equal(t, 1, len(b.evtQ))
	assert.Equal(t, 1, b.Account.Lots.Len())
}

func TestNewBroker(t *testing.T) {
	t.Parallel()

	b := NewLedger("test-broker")
	require.NotNil(t, b)
	assert.Equal(t, "test-broker", b.Name)
	assert.Nil(t, b.Account)
	assert.Nil(t, b.evtQ)
}

func TestBrokerEventsChannelCreation(t *testing.T) {
	t.Parallel()

	b := NewLedger("ch-test")
	assert.Nil(t, b.evtQ)

	ch := b.Events()
	require.NotNil(t, ch)
	assert.NotNil(t, b.evtQ)
	assert.Len(t, b.evtQ, 0)

	ch2 := b.Events()
	assert.Equal(t, ch, ch2, "Events() should return the same channel on subsequent calls")
}

func TestBrokerEventsChannelReceiveEvent(t *testing.T) {
	t.Parallel()

	b := NewLedger("recv-test")
	evtCh := b.Events()

	evt := &Event{Type: EventOrderFilled}
	err := b.emitEvent(context.Background(), evt)
	require.NoError(t, err)

	select {
	case received := <-evtCh:
		require.NotNil(t, received)
		assert.Equal(t, EventOrderFilled, received.Type)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received on channel")
	}
}

func makeBrokerCloseFixture() (*Ledger, *Lot, *CloseRequest) {
	acct := NewAccount("account", types.MoneyFromFloat(2_000))
	broker := &Ledger{
		Account: acct,
		evtQ:    make(chan *Event, 1),
	}

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

	return broker, lot, req
}

func TestBrokerSubmitCloseValidationErrors(t *testing.T) {
	t.Parallel()

	var nilBroker *Ledger
	err := nilBroker.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker is nil")

	broker := NewLedger("broker")
	err = broker.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker account is nil")

	broker.Account = NewAccount("acct", types.MoneyFromFloat(1_000))
	err = broker.SubmitClose(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close request is nil")

	err = broker.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing position")

	err = broker.SubmitClose(context.Background(), &CloseRequest{
		Request: Request{Price: types.PriceFromFloat(1.1)},
		Lot:     &Lot{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing trade common")
}

func TestBrokerSubmitCloseSuccessEmitsEvent(t *testing.T) {
	t.Parallel()

	broker, lot, req := makeBrokerCloseFixture()

	err := broker.SubmitClose(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, broker.Account.Trades, 1)
	trade := broker.Account.Trades[0]
	assert.Equal(t, req.Price, trade.ExitPrice)
	assert.Equal(t, req.Timestamp, trade.ExitTime)
	assert.Equal(t, types.MoneyFromFloat(50), trade.PNL)
	assert.Equal(t, 0, broker.Account.Lots.Len())

	select {
	case evt := <-broker.evtQ:
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

func TestBrokerSubmitClose_PreservesReasonAndInitialStopDespiteTrailing(t *testing.T) {
	t.Parallel()

	broker, lot, req := makeBrokerCloseFixture()
	lot.TradeCommon.Reason = "signalreplay:2024-01-02T00:00:00Z"
	lot.TradeCommon.InitialStop = types.PriceFromFloat(1.09)

	// Simulate trailing-stop updates overwriting Stop after open, as
	// backtest/execute.go does every bar.
	lot.TradeCommon.Stop = types.PriceFromFloat(1.093)

	err := broker.SubmitClose(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, broker.Account.Trades, 1)
	trade := broker.Account.Trades[0]
	assert.Equal(t, "signalreplay:2024-01-02T00:00:00Z", trade.Reason)
	assert.Equal(t, types.PriceFromFloat(1.09), trade.InitialStop)
	assert.Equal(t, types.PriceFromFloat(1.093), trade.Stop, "final trailed Stop is distinct from InitialStop")
}

func TestBrokerSubmitCloseAllowsCommittedStateWhenContextCanceledAndQueueFull(t *testing.T) {
	t.Parallel()

	broker, _, req := makeBrokerCloseFixture()
	broker.evtQ <- &Event{Type: EventOrderFilled}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := broker.SubmitClose(ctx, req)
	require.NoError(t, err)
	require.Len(t, broker.Account.Trades, 1)
}

func TestBrokerSubmitCloseDropsEventWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	broker, _, req := makeBrokerCloseFixture()
	broker.evtQ <- &Event{Type: EventOrderFilled}

	err := broker.SubmitClose(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, broker.Account.Trades, 1)
}

func TestBrokerSubmitCloseRejectsMismatchedRequestIdentity(t *testing.T) {
	t.Parallel()

	broker, _, req := makeBrokerCloseFixture()
	req.Request.TradeCommon = &TradeCommon{
		ID:         "other-id",
		Instrument: req.Lot.Instrument,
	}

	err := broker.SubmitClose(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match position id")
}

func TestBrokerEmitEventNilContextBehavior(t *testing.T) {
	t.Parallel()

	broker := NewLedger("broker")
	evt := &Event{Type: EventOrderFilled}

	//lint:ignore SA1012 nil context behavior is exactly what this test verifies
	err := broker.emitEvent(nil, evt)
	require.NoError(t, err)
	require.NotNil(t, broker.evtQ)
	require.Len(t, broker.evtQ, 1)

	full := &Ledger{evtQ: make(chan *Event, 1)}
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
