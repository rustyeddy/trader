package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrokerSubmitOpen_Guards(t *testing.T) {
	t.Parallel()

	req := &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: NewULID(), Instrument: "EURUSD", Units: 1000, Side: Long}, Price: PriceFromFloat(1.1)}}

	var nilBroker *Broker
	res, err := nilBroker.SubmitOpen(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "nil broker")

	b := &Broker{OpenOrders: OpenOrders{Orders: map[string]*order{}}}
	res, err = b.SubmitOpen(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "broker account is nil")

	b.Account = NewAccount("test", MoneyFromFloat(10000))
	res, err = b.SubmitOpen(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "nil open request")

	badReq := &OpenRequest{Request: Request{TradeCommon: nil, Price: PriceFromFloat(1.1)}}
	res, err = b.SubmitOpen(context.Background(), badReq)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "missing trade common")
}

func TestBrokerSubmitOpen_InitializesOpenOrdersMap(t *testing.T) {
	t.Parallel()

	b := &Broker{Account: NewAccount("test", MoneyFromFloat(10000))}
	req := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{ID: NewULID(), Instrument: "EURUSD", Units: 1000, Side: Long},
			RequestType: RequestMarketOpen,
			Price:       PriceFromFloat(1.1),
			Timestamp:   Timestamp(100),
		},
	}

	res, err := b.SubmitOpen(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, b.OpenOrders.Orders)
	assert.NotNil(t, b.OpenOrders.Get(req.ID))
}
