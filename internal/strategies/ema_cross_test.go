package strategies

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
)

func TestNewEmaCross(t *testing.T) {
	t.Run("with valid rr", func(t *testing.T) {
		strat := NewEmaCross("EUR_USD", 20, 50, 0.01, 20, 2.5)
		assert.NotNil(t, strat)
		assert.Equal(t, "EUR_USD", strat.Instrument)
		assert.Equal(t, 20, strat.FastPeriod)
		assert.Equal(t, 50, strat.SlowPeriod)
		assert.Equal(t, 0.01, strat.RiskPct)
		assert.Equal(t, 20.0, strat.StopPips)
		assert.Equal(t, 2.5, strat.RR)
		assert.NotNil(t, strat.fast)
		assert.NotNil(t, strat.slow)
		assert.False(t, strat.haveLastDiff)
		assert.Equal(t, "", strat.openTradeID)
		assert.Equal(t, 0.0, strat.openUnits)
	})

	t.Run("with zero or negative rr defaults to 2.0", func(t *testing.T) {
		tests := []float64{0, -1, -2.5}
		for _, rr := range tests {
			strat := NewEmaCross("EUR_USD", 20, 50, 0.01, 20, rr)
			assert.Equal(t, 2.0, strat.RR, "RR should default to 2.0 when <= 0")
		}
	})
}

func TestEmaCrossStrategy_OnTick_WrongInstrument(t *testing.T) {
	strat := NewEmaCross("EUR_USD", 20, 50, 0.01, 20, 2.0)
	ctx := context.Background()

	// Tick with different instrument should be ignored
	tick := broker.Price{
		Instrument: "GBP_USD",
		Bid:        1.2500,
		Ask:        1.2502,
		Time:       time.Now(),
	}

	err := strat.OnTick(ctx, nil, tick)
	assert.NoError(t, err)
}

func TestEmaCrossStrategy_OnTick_WarmupPhase(t *testing.T) {
	strat := NewEmaCross("EUR_USD", 3, 5, 0.01, 20, 2.0)
	ctx := context.Background()

	// During warmup, EMAs are not ready yet
	// Should process ticks without errors but not generate signals
	for i := 0; i < 4; i++ {
		tick := broker.Price{
			Instrument: "EUR_USD",
			Bid:        1.0850 + float64(i)*0.0001,
			Ask:        1.0852 + float64(i)*0.0001,
			Time:       time.Now().Add(time.Duration(i) * time.Minute),
		}
		err := strat.OnTick(ctx, nil, tick)
		assert.NoError(t, err)
	}

	// After a few ticks, fast EMA might be ready but slow might not be
	// or both might not be ready. Either way, no signal should be generated
	assert.Equal(t, "", strat.openTradeID)
	assert.Equal(t, 0.0, strat.openUnits)
}

func TestEmaCrossStrategy_OnTick_BuildsLastDiff(t *testing.T) {
	strat := NewEmaCross("EUR_USD", 2, 3, 0.01, 20, 2.0)
	ctx := context.Background()

	// Feed enough ticks to warm up EMAs
	for i := 0; i < 5; i++ {
		tick := broker.Price{
			Instrument: "EUR_USD",
			Bid:        1.0850 + float64(i)*0.0001,
			Ask:        1.0852 + float64(i)*0.0001,
			Time:       time.Now().Add(time.Duration(i) * time.Minute),
		}
		err := strat.OnTick(ctx, nil, tick)
		assert.NoError(t, err)
	}

	// After warmup, haveLastDiff should be true
	assert.True(t, strat.haveLastDiff)
}

// mockBrokerForEmaCross is a more comprehensive mock for testing EmaCross
type mockBrokerForEmaCross struct {
	account        broker.Account
	price          broker.Price
	accountErr     error
	priceErr       error
	createOrderErr error
	orderFills     []broker.OrderFill
	closeCalled    bool
	closeTradeID   string
	closeReason    string
}

func (m *mockBrokerForEmaCross) GetAccount(ctx context.Context) (broker.Account, error) {
	if m.accountErr != nil {
		return broker.Account{}, m.accountErr
	}
	return m.account, nil
}

func (m *mockBrokerForEmaCross) GetPrice(ctx context.Context, instrument string) (broker.Price, error) {
	if m.priceErr != nil {
		return broker.Price{}, m.priceErr
	}
	return m.price, nil
}

func (m *mockBrokerForEmaCross) CreateMarketOrder(ctx context.Context, req broker.MarketOrderRequest) (broker.OrderFill, error) {
	if m.createOrderErr != nil {
		return broker.OrderFill{}, m.createOrderErr
	}
	fill := broker.OrderFill{
		TradeID: "test-trade",
		Units:   req.Units,
	}
	m.orderFills = append(m.orderFills, fill)
	return fill, nil
}

func (m *mockBrokerForEmaCross) CloseTrade(ctx context.Context, tradeID string, reason string) error {
	m.closeCalled = true
	m.closeTradeID = tradeID
	m.closeReason = reason
	return nil
}

func (m *mockBrokerForEmaCross) UpdatePrice(p broker.Price) error {
	return nil
}

func TestEmaCrossStrategy_Integration_NoCross(t *testing.T) {
	// Test that no trades are opened when EMAs don't cross
	strat := NewEmaCross("EUR_USD", 2, 3, 0.01, 20, 2.0)
	ctx := context.Background()

	mock := &mockBrokerForEmaCross{
		account: broker.Account{
			ID:       "test-account",
			Currency: "USD",
			Balance:  100000,
			Equity:   100000,
		},
		price: broker.Price{
			Instrument: "EUR_USD",
			Bid:        1.0850,
			Ask:        1.0852,
		},
	}

	// Save original market instruments and restore after test
	originalInstruments := market.Instruments
	t.Cleanup(func() {
		market.Instruments = originalInstruments
	})

	// Initialize market instruments for test
	if market.Instruments == nil {
		market.Instruments = make(map[string]market.InstrumentMeta)
	}
	market.Instruments["EUR_USD"] = market.InstrumentMeta{
		Name:          "EUR_USD",
		BaseCurrency:  "EUR",
		QuoteCurrency: "USD",
		PipLocation:   -4,
	}

	// Feed ticks with gradually increasing prices (no cross)
	for i := 0; i < 10; i++ {
		tick := broker.Price{
			Instrument: "EUR_USD",
			Bid:        1.0850 + float64(i)*0.0001,
			Ask:        1.0852 + float64(i)*0.0001,
			Time:       time.Now().Add(time.Duration(i) * time.Minute),
		}
		err := strat.OnTick(ctx, mock, tick)
		assert.NoError(t, err)
	}

	// No orders should be created without a clear cross
	assert.Equal(t, "", strat.openTradeID)
	assert.Equal(t, 0.0, strat.openUnits)
}

func TestEmaCrossStrategy_OnTick_CrossDetection(t *testing.T) {
	// Test cross detection logic with minimal periods
	strat := NewEmaCross("EUR_USD", 2, 4, 0.01, 20, 2.0)
	ctx := context.Background()

	// Feed decreasing prices first to establish EMAs with fast < slow
	prices := []float64{1.0900, 1.0890, 1.0880, 1.0870, 1.0860}
	for i, price := range prices {
		tick := broker.Price{
			Instrument: "EUR_USD",
			Bid:        price,
			Ask:        price + 0.0002,
			Time:       time.Now().Add(time.Duration(i) * time.Minute),
		}
		err := strat.OnTick(ctx, nil, tick)
		assert.NoError(t, err)
	}

	// At this point both EMAs should be ready and haveLastDiff should be true
	assert.True(t, strat.haveLastDiff)
	
	// Verify that the strategy is tracking state properly
	// After decreasing prices, fast EMA should be below slow EMA (negative diff)
	// Note: We're testing internal state here to verify EMA calculation is working.
	// This ensures the cross detection logic will function correctly in real scenarios.
	assert.True(t, strat.lastDiff < 0, "Expected negative diff after decreasing prices")
	
	// Feed one more decreasing price - should not trigger a cross since trend continues
	tick := broker.Price{
		Instrument: "EUR_USD",
		Bid:        1.0850,
		Ask:        1.0852,
		Time:       time.Now().Add(time.Duration(len(prices)) * time.Minute),
	}
	err := strat.OnTick(ctx, nil, tick)
	assert.NoError(t, err)
	
	// Should still have lastDiff tracking enabled and no position opened
	assert.True(t, strat.haveLastDiff)
	assert.Equal(t, "", strat.openTradeID)
}

func TestEmaCrossStrategy_OnTradeClosed(t *testing.T) {
	// Test that OnTradeClosed properly clears internal state
	strat := NewEmaCross("EUR_USD", 20, 50, 0.01, 20, 2.0)
	
	// Simulate a trade being opened
	strat.openTradeID = "test-trade-123"
	strat.openUnits = 100000.0
	
	// Verify state is set
	assert.Equal(t, "test-trade-123", strat.openTradeID)
	assert.Equal(t, 100000.0, strat.openUnits)
	
	// Call OnTradeClosed
	strat.OnTradeClosed("test-trade-123", "StopLoss")
	
	// Verify state is cleared
	assert.Equal(t, "", strat.openTradeID)
	assert.Equal(t, 0.0, strat.openUnits)
}

func TestEmaCrossStrategy_OnTradeClosed_WrongTradeID(t *testing.T) {
	// Test that OnTradeClosed with wrong trade ID doesn't clear state
	strat := NewEmaCross("EUR_USD", 20, 50, 0.01, 20, 2.0)
	
	// Simulate a trade being opened
	strat.openTradeID = "test-trade-123"
	strat.openUnits = 100000.0
	
	// Call OnTradeClosed with different trade ID
	strat.OnTradeClosed("different-trade-456", "StopLoss")
	
	// Verify state is NOT cleared (because it's a different trade)
	assert.Equal(t, "test-trade-123", strat.openTradeID)
	assert.Equal(t, 100000.0, strat.openUnits)
}
