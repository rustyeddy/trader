package trader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceMid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bid      Price
		ask      Price
		expected Price
	}{
		{"simple", 10, 30, 20},
		{"same", 25, 25, 25},
		{"zero", 00, 00, 00},
		{"negative", -20, 20, 00},
		{"fractional", 11, 13, 12},
	}

	const tol = 0

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := Tick{
				BA: BA{Bid: tt.bid, Ask: tt.ask},
			}
			got := p.Mid()
			v := got - tt.expected
			if v < 0 {
				v = -v
			}

			if v > tol {
				t.Fatalf("Mid() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestBrokerOpenRequestReturnsQueueFullWhenEventQueueIsFull(t *testing.T) {
	t.Parallel()

	b := &Broker{
		Account: NewAccount("test-account", MoneyFromFloat(10_000)),
		evtQ: make(chan *Event, 1),
		OpenOrders: OpenOrders{
			Orders: make(map[string]*order),
		},
	}
	b.evtQ <- &Event{Type: EventOrderAccepted}

	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{
				ID:         NewULID(),
				Instrument: "EURUSD",
				Units:      Units(1000),
				Side:       Long,
			},
			RequestType: RequestMarketOpen,
			Price:       Price(1100000),
			Timestamp:   Timestamp(1),
		},
	}

	done := make(chan struct{})
	var (
		res *openResult
		err error
	)
	go func() {
		defer close(done)
		res, err = b.SubmitOpen(context.Background(), req)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OpenRequest blocked with full event queue")
	}

	require.Error(t, err)
	require.Contains(t, err.Error(), "queue is full")
	require.NotNil(t, res)
	require.NotNil(t, res.order)
	require.NotNil(t, res.Lot)
	assert.Equal(t, 1, len(b.evtQ))
}

func TestBrokerOpenRequestReturnsContextErrorWhenContextCanceledAndQueueFull(t *testing.T) {
	t.Parallel()

	b := &Broker{
		Account: NewAccount("test-account", MoneyFromFloat(10_000)),
		evtQ: make(chan *Event, 1),
		OpenOrders: OpenOrders{
			Orders: make(map[string]*order),
		},
	}
	b.evtQ <- &Event{Type: EventOrderAccepted}

	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{
				ID:         NewULID(),
				Instrument: "EURUSD",
				Units:      Units(1000),
				Side:       Long,
			},
			RequestType: RequestMarketOpen,
			Price:       Price(1100000),
			Timestamp:   Timestamp(2),
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

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, len(b.evtQ))
}

func TestNewBroker(t *testing.T) {
	t.Parallel()

	b := NewBroker("test-broker")
	require.NotNil(t, b)
	assert.Equal(t, "test-broker", b.ID)
	assert.Nil(t, b.Account)
	assert.NotNil(t, b.OpenOrders.Orders)
	assert.Equal(t, 0, len(b.OpenOrders.Orders))
	assert.Nil(t, b.evtQ)
}

func TestBrokerEventsChannelCreation(t *testing.T) {
	t.Parallel()

	b := NewBroker("ch-test")
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

	b := NewBroker("recv-test")
	evtCh := b.Events()

	evt := &Event{Type: EventOrderAccepted, Time: Timestamp(100)}
	err := b.emitEvent(context.Background(), evt)
	require.NoError(t, err)

	select {
	case received := <-evtCh:
		require.NotNil(t, received)
		assert.Equal(t, EventOrderAccepted, received.Type)
		assert.Equal(t, Timestamp(100), received.Time)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received on channel")
	}
}
