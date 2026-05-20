package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeBrokerCloseFixture() (*Broker, *Lot, *CloseRequest) {
	acct := NewAccount("account", MoneyFromFloat(2_000))
	broker := &Broker{
		Account: acct,
		OpenOrders: OpenOrders{
			Orders: make(map[string]*order),
		},
		evtQ: make(chan *Event, 1),
	}

	th := NewTradeHistory("EURUSD")
	th.TradeCommon.Units = Units(1000)
	th.TradeCommon.Side = Long
	units := th.TradeCommon.Units
	lot := &Lot{
		TradeCommon:    th.TradeCommon,
		EntryPrice:     Price(1095000),
		EntryTime:      Timestamp(1),
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          LotOpen,
	}
	acct.Lots.Add(lot)

	req := &CloseRequest{
		Request: Request{
			TradeCommon: th.TradeCommon,
			RequestType: RequestClose,
			Price:       Price(1100000),
			Timestamp:   Timestamp(2),
		},
		Lot:        lot,
		CloseCause: CloseManual,
	}

	return broker, lot, req
}

func TestBrokerSubmitCloseValidationErrors(t *testing.T) {
	t.Parallel()

	var nilBroker *Broker
	err := nilBroker.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil broker")

	broker := NewBroker("broker")
	err = broker.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker account is nil")

	broker.Account = NewAccount("acct", MoneyFromFloat(1_000))
	err = broker.SubmitClose(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil close request")

	err = broker.SubmitClose(context.Background(), &CloseRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing position")
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
	assert.Equal(t, MoneyFromFloat(50), trade.PNL)
	assert.Equal(t, 0, broker.Account.Lots.Len())

	select {
	case evt := <-broker.evtQ:
		require.NotNil(t, evt)
		assert.Equal(t, EventPositionClosed, evt.Type)
		assert.Equal(t, req.Request.ID, evt.ClientOrderID)
		assert.Equal(t, "lowest low", evt.Reason)
		assert.Equal(t, CloseManual, evt.Cause)
		assert.Same(t, lot, evt.Lot)
		assert.Same(t, trade, evt.Trade)
	default:
		t.Fatal("expected close event to be queued")
	}
}

func TestBrokerSubmitCloseReturnsContextErrorWhenContextCanceledAndQueueFull(t *testing.T) {
	t.Parallel()

	broker, _, req := makeBrokerCloseFixture()
	broker.evtQ <- &Event{Type: EventOrderFilled}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := broker.SubmitClose(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, broker.Account.Trades, 1)
}

func TestBrokerEmitEventNilContextBehavior(t *testing.T) {
	t.Parallel()

	broker := NewBroker("broker")
	evt := &Event{Type: EventOrderAccepted, Time: Timestamp(10)}

	err := broker.emitEvent(nil, evt)
	require.NoError(t, err)
	require.NotNil(t, broker.evtQ)
	require.Len(t, broker.evtQ, 1)

	full := &Broker{evtQ: make(chan *Event, 1)}
	full.evtQ <- &Event{Type: EventOrderFilled}
	err = full.emitEvent(nil, evt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue is full")
}
