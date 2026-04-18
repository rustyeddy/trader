package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrokerSubmitCloseReturnsErrorWhenEventQueueIsFull(t *testing.T) {
	t.Parallel()

	b := &Broker{evtQ: make(chan *Event, 1)}
	b.evtQ <- &Event{Type: EventOrderFilled}

	th := NewTradeHistory("EURUSD")
	pos := &Position{TradeCommon: th.TradeCommon, State: PositionOpen}
	req := &CloseRequest{
		Request: Request{
			TradeCommon: th.TradeCommon,
			RequestType: RequestClose,
			Price:       Price(1100000),
			Timestamp:   Timestamp(1),
		},
		Position: pos,
	}

	err := b.SubmitClose(context.Background(), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "queue is full")
}
