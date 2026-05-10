package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePriceSource struct {
	price          Tick
	err            error
	called         int
	lastInstrument string
}

func (f *fakePriceSource) GetTick(ctx context.Context, instrument string) (Tick, error) {
	f.called++
	f.lastInstrument = instrument
	return f.price, f.err
}

func findByQuote(account string) (string, bool) {
	for k, v := range Instruments {
		if v.QuoteCurrency == account {
			return k, true
		}
	}
	return "", false
}

func findByBase(account string) (string, bool) {
	for k, v := range Instruments {
		if v.BaseCurrency == account {
			return k, true
		}
	}
	return "", false
}

func findCross(account string) (string, bool) {
	for k, v := range Instruments {
		if v.BaseCurrency != account && v.QuoteCurrency != account {
			return k, true
		}
	}
	return "", false
}

func TestQuoteToAccountRate_UnknownInstrument(t *testing.T) {
	t.Parallel()

	ps := &fakePriceSource{}
	rate, err := QuoteToAccountRate("NO_SUCH_INSTRUMENT", "USD", ps)
	assert.Error(t, err)
	assert.Equal(t, 0.0, rate)
}

func TestQuoteToAccountRate_QuoteEqualsAccount(t *testing.T) {
	t.Parallel()

	instrument, ok := findByQuote("USD")
	if !ok {
		t.Skip("no instrument with quote currency USD")
	}

	ps := &fakePriceSource{}
	rate, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.NoError(t, err)
	assert.Equal(t, 1.0, rate)
	assert.Equal(t, 0, ps.called)
}

func TestQuoteToAccountRate_BaseEqualsAccount(t *testing.T) {
	t.Parallel()

	instrument, ok := findByBase("USD")
	if !ok {
		t.Skip("no instrument with base currency USD")
	}

	ps := &fakePriceSource{
		price: Tick{
			BA: BA{
				Bid: 2.0,
				Ask: 4.0,
			},
		},
	}
	rate, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.NoError(t, err)

	expected := 1.0 / (float64(ps.price.Mid()) / float64(PriceScale))
	assert.InDelta(t, expected, rate, 1e-9)
	assert.Equal(t, 1, ps.called)
	assert.Equal(t, instrument, ps.lastInstrument)
}

func TestQuoteToAccountRate_CrossNotImplemented(t *testing.T) {
	t.Parallel()

	instrument, ok := findCross("USD")
	if !ok {
		t.Skip("no cross instrument found")
	}

	ps := &fakePriceSource{}
	_, err := QuoteToAccountRate(instrument, "USD", ps)
	assert.Error(t, err)
}

func TestAccountPrint(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test", MoneyFromFloat(50_000))
	acct.Balance = MoneyFromFloat(50_500)
	acct.Equity = MoneyFromFloat(50_750)

	require.NotPanics(t, func() {
		acct.Print()
	})
}

func TestAccountOpenPosition(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test", MoneyFromFloat(10_000))

	c := &CandleTime{
		Candle: Candle{
			Open:  PriceFromFloat(1.1000),
			High:  PriceFromFloat(1.1010),
			Low:   PriceFromFloat(1.0990),
			Close: PriceFromFloat(1.1005),
		},
		Timestamp: Timestamp(1000),
	}

	tr := NewTradeHistory("EURUSD")
	tr.Side = Long
	tr.Units = 100_000
	tr.Stop = PriceFromFloat(1.0950)

	openReq := &OpenRequest{
		Request: Request{
			TradeCommon: tr.TradeCommon,
			Price:       c.Close,
			Timestamp:   c.Timestamp,
		},
	}

	assert.Equal(t, 0, acct.Positions.Len(), "initial account should have no positions")

	acct.OpenPosition(Timestamp(1000), c.Candle, openReq)

	assert.Equal(t, 1, acct.Positions.Len(), "account should have 1 position after OpenPosition")

	positions := acct.Positions.Positions()
	require.Len(t, positions, 1)
	pos := positions[openReq.ID]
	require.NotNil(t, pos, "position should exist in account")
	assert.Equal(t, openReq.ID, pos.ID)
	assert.Equal(t, c.Close, pos.FillPrice)
	assert.Equal(t, Timestamp(1000), pos.FillTime)
	assert.Equal(t, Long, pos.Side)
	assert.Equal(t, Units(100_000), pos.Units)
}
