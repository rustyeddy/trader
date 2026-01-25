package strategy

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
	"github.com/stretchr/testify/assert"
)

type fakeBroker struct {
	acct        broker.Account
	price       broker.Price
	orderErr    error
	lastRequest broker.MarketOrderRequest
}

func (f *fakeBroker) GetAccount(ctx context.Context) (broker.Account, error) {
	return f.acct, nil
}

func (f *fakeBroker) GetPrice(ctx context.Context, instrument string) (broker.Price, error) {
	p := f.price
	p.Instrument = instrument
	return p, nil
}

func (f *fakeBroker) CreateMarketOrder(ctx context.Context, req broker.MarketOrderRequest) (broker.OrderFill, error) {
	f.lastRequest = req
	return broker.OrderFill{}, f.orderErr
}

func TestTradeEURUSD_Success(t *testing.T) {
	t.Parallel()

	fb := &fakeBroker{
		acct: broker.Account{
			Equity: 10000,
		},
		price: broker.Price{
			Bid: 1.0999,
			Ask: 1.1001,
		},
	}

	err := TradeEURUSD(context.Background(), fb)
	assert.NoError(t, err)

	meta := market.Instruments["EUR_USD"]
	size := risk.Calculate(risk.Inputs{
		Equity:         fb.acct.Equity,
		RiskPct:        0.005,
		EntryPrice:     fb.price.Ask,
		StopPrice:      fb.price.Ask - 0.0020,
		PipLocation:    meta.PipLocation,
		QuoteToAccount: 1.0,
	})

	assert.Equal(t, "EUR_USD", fb.lastRequest.Instrument)
	assert.InDelta(t, size.Units, fb.lastRequest.Units, 1e-9)
}

func TestTradeEURUSD_CreateOrderError(t *testing.T) {
	t.Parallel()

	wantErr := assert.AnError

	fb := &fakeBroker{
		acct: broker.Account{
			Equity: 10000,
		},
		price: broker.Price{
			Bid: 1.0999,
			Ask: 1.1001,
		},
		orderErr: wantErr,
	}

	err := TradeEURUSD(context.Background(), fb)
	assert.ErrorIs(t, err, wantErr)
}
