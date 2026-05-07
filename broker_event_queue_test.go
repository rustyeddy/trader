package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrokerSubmitCloseReturnsErrorWhenEventQueueIsFull(t *testing.T) {
	t.Parallel()

	b := &Broker{
		evtQ:    make(chan *Event, 1),
		Account: NewAccount("account", MoneyFromFloat(2000.00)),
	}
	b.evtQ <- &Event{Type: EventOrderFilled}

	th := NewTradeHistory("EURUSD")
	th.TradeCommon.Units = Units(1000)
	th.TradeCommon.Side = Long
	pos := &Position{
		TradeCommon: th.TradeCommon,
		FillPrice:   Price(1095000),
		State:       PositionOpen,
	}
	req := &closeRequest{
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
