package strategies

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBroker is a simple mock for testing OpenOnceStrategy
type mockBroker struct {
	createOrderCalled bool
	createOrderErr    error
	lastRequest       broker.MarketOrderRequest
}

func (m *mockBroker) GetAccount(ctx context.Context) (broker.Account, error) {
	return broker.Account{}, nil
}

func (m *mockBroker) GetTick(ctx context.Context, instrument string) (pricing.Tick, error) {
	return pricing.Tick{}, nil
}

func (m *mockBroker) CreateMarketOrder(ctx context.Context, req broker.MarketOrderRequest) (broker.OrderFill, error) {
	m.createOrderCalled = true
	m.lastRequest = req
	if m.createOrderErr != nil {
		return broker.OrderFill{}, m.createOrderErr
	}
	return broker.OrderFill{TradeID: "test-trade-1"}, nil
}

func (m *mockBroker) UpdatePrice(p pricing.Tick) error {
	return nil
}

func TestOpenOnceStrategy_OnTick_Success(t *testing.T) {
	ctx := context.Background()
	mock := &mockBroker{}

	strat := &OpenOnceStrategy{
		Instrument: "EUR_USD",
		Units:      1000,
	}

	// First tick should open the order
	tick := pricing.Tick{
		Instrument: "EUR_USD",
		Bid:        1.0850,
		Ask:        1.0852,
		Time:       time.Now(),
	}

	err := strat.OnTick(ctx, mock, tick)
	require.NoError(t, err)
	assert.True(t, mock.createOrderCalled)
	assert.Equal(t, "EUR_USD", mock.lastRequest.Instrument)
	assert.Equal(t, 1000.0, mock.lastRequest.Units)
	assert.True(t, strat.opened)

	// Second tick should do nothing (already opened)
	mock.createOrderCalled = false
	err = strat.OnTick(ctx, mock, tick)
	require.NoError(t, err)
	assert.False(t, mock.createOrderCalled)
}

func TestOpenOnceStrategy_OnTick_WrongInstrument(t *testing.T) {
	ctx := context.Background()
	mock := &mockBroker{}

	strat := &OpenOnceStrategy{
		Instrument: "EUR_USD",
		Units:      1000,
	}

	// Tick with different instrument should be ignored
	tick := pricing.Tick{
		Instrument: "GBP_USD",
		Bid:        1.2500,
		Ask:        1.2502,
		Time:       time.Now(),
	}

	err := strat.OnTick(ctx, mock, tick)
	require.NoError(t, err)
	assert.False(t, mock.createOrderCalled)
	assert.False(t, strat.opened)
}

func TestOpenOnceStrategy_OnTick_ZeroUnits(t *testing.T) {
	ctx := context.Background()
	mock := &mockBroker{}

	strat := &OpenOnceStrategy{
		Instrument: "EUR_USD",
		Units:      0, // Zero units should cause error
	}

	tick := pricing.Tick{
		Instrument: "EUR_USD",
		Bid:        1.0850,
		Ask:        1.0852,
		Time:       time.Now(),
	}

	err := strat.OnTick(ctx, mock, tick)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "units must be non-zero")
	assert.False(t, mock.createOrderCalled)
}

func TestOpenOnceStrategy_OnTick_BrokerError(t *testing.T) {
	ctx := context.Background()
	mock := &mockBroker{
		createOrderErr: errors.New("broker error"),
	}

	strat := &OpenOnceStrategy{
		Instrument: "EUR_USD",
		Units:      1000,
	}

	tick := pricing.Tick{
		Instrument: "EUR_USD",
		Bid:        1.0850,
		Ask:        1.0852,
		Time:       time.Now(),
	}

	err := strat.OnTick(ctx, mock, tick)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker error")
	assert.True(t, mock.createOrderCalled)
	assert.False(t, strat.opened) // Should not mark as opened if order fails
}

func TestOpenOnceStrategy_OnTick_NegativeUnits(t *testing.T) {
	ctx := context.Background()
	mock := &mockBroker{}

	strat := &OpenOnceStrategy{
		Instrument: "EUR_USD",
		Units:      -500, // Negative units for short position
	}

	tick := pricing.Tick{
		Instrument: "EUR_USD",
		Bid:        1.0850,
		Ask:        1.0852,
		Time:       time.Now(),
	}

	err := strat.OnTick(ctx, mock, tick)
	require.NoError(t, err)
	assert.True(t, mock.createOrderCalled)
	assert.Equal(t, -500.0, mock.lastRequest.Units)
	assert.True(t, strat.opened)
}
