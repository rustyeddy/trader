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
	units := th.TradeCommon.Units
	lot := &Lot{
		TradeCommon:    th.TradeCommon,
		EntryPrice:     Price(1095000),
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          LotOpen,
	}
	req := &CloseRequest{
		Request: Request{
			TradeCommon: th.TradeCommon,
			RequestType: RequestClose,
			Price:       Price(1100000),
			Timestamp:   Timestamp(1),
		},
		Lot: lot,
	}

	err := b.SubmitClose(context.Background(), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "queue is full")
}
