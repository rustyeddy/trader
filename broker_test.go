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
		res, err = b.OpenRequest(context.Background(), req)
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
	require.NotNil(t, res.Position)
	assert.Equal(t, 1, len(b.evtQ))
}

func TestBrokerOpenRequestReturnsContextErrorWhenContextCanceledAndQueueFull(t *testing.T) {
	t.Parallel()

	b := &Broker{
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
		_, err = b.OpenRequest(ctx, req)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OpenRequest blocked with canceled context")
	}

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, len(b.evtQ))
}

func TestBrokerSubmitOrderAndReadOrderResponsesContract(t *testing.T) {
	t.Parallel()

	b := &Broker{
		OpenOrders: OpenOrders{
			Orders: make(map[string]*order),
		},
	}

	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{
				ID:         NewULID(),
				Instrument: "EURUSD",
				Units:      Units(1000),
				Side:       Long,
			},
			RequestType: RequestMarketOpen,
			Price:       Price(1090000),
			Timestamp:   Timestamp(10),
		},
	}
	ord := &order{
		TradeCommon: req.TradeCommon,
		orderType:   OrderMarket,
		orderStatus: OrderPending,
	}

	pos, err := b.SubmitOrder(context.Background(), ord)
	require.NoError(t, err)
	require.NotNil(t, pos)
	assert.Equal(t, req.ID, pos.ID)
	assert.Equal(t, req.Instrument, pos.Instrument)
	assert.Equal(t, req.Units, pos.Units)
	assert.Equal(t, req.Side, pos.Side)
	assert.Empty(t, b.OpenOrders.Orders, "SubmitOrder should not create open-order responses")

	b.ReadOrderResponses(req)
	require.Len(t, b.OpenOrders.Orders, 1)
	stored := b.OpenOrders.Get(req.ID)
	require.NotNil(t, stored)
	assert.Equal(t, req.ID, stored.ID)
	assert.Equal(t, OrderMarket, stored.orderType)
	assert.Equal(t, OrderPending, stored.orderStatus)
}
