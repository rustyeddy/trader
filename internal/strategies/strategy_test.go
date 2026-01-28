package strategies

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/broker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStrategy is a simple mock for testing
type mockStrategy struct {
	tickCount int
}

func (m *mockStrategy) OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error {
	m.tickCount++
	return nil
}

func TestRegister(t *testing.T) {
	// Clear registry before test
	registry = make(map[string]TickStrategy)

	mock := &mockStrategy{}
	Register("test-strategy", mock)

	strat := GetStrategy("test-strategy")
	assert.NotNil(t, strat)
	assert.Equal(t, mock, strat)
}

func TestGetStrategy_NotFound(t *testing.T) {
	// Clear registry before test
	registry = make(map[string]TickStrategy)

	strat := GetStrategy("nonexistent")
	assert.Nil(t, strat)
}

func TestStrategyByName_Noop(t *testing.T) {
	tests := []struct {
		name     string
		stratKey string
	}{
		{"noop lowercase", "noop"},
		{"none lowercase", "none"},
		{"NOOP uppercase", "NOOP"},
		{"NONE uppercase", "NONE"},
		{"Noop mixed case", "Noop"},
		{"noop with spaces", "  noop  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strat, err := StrategyByName(tt.stratKey, "EUR_USD", 100, 20, 50, 0.01, 20, 2.0)
			require.NoError(t, err)
			assert.NotNil(t, strat)
			_, ok := strat.(NoopStrategy)
			assert.True(t, ok, "expected NoopStrategy")
		})
	}
}

func TestStrategyByName_OpenOnce(t *testing.T) {
	strat, err := StrategyByName("open-once", "EUR_USD", 1000, 20, 50, 0.01, 20, 2.0)
	require.NoError(t, err)
	assert.NotNil(t, strat)

	openOnce, ok := strat.(*OpenOnceStrategy)
	require.True(t, ok, "expected *OpenOnceStrategy")
	assert.Equal(t, "EUR_USD", openOnce.Instrument)
	assert.Equal(t, 1000.0, openOnce.Units)
}

func TestStrategyByName_EmaCross(t *testing.T) {
	tests := []struct {
		name     string
		stratKey string
	}{
		{"ema-cross", "ema-cross"},
		{"emacross", "emacross"},
		{"EMA-CROSS uppercase", "EMA-CROSS"},
		{"EmaCross mixed", "EmaCross"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strat, err := StrategyByName(tt.stratKey, "EUR_USD", 1000, 20, 50, 0.01, 20, 2.0)
			require.NoError(t, err)
			assert.NotNil(t, strat)

			emaCross, ok := strat.(*EmaCrossStrategy)
			require.True(t, ok, "expected *EmaCrossStrategy")
			assert.Equal(t, "EUR_USD", emaCross.Instrument)
			assert.Equal(t, 20, emaCross.FastPeriod)
			assert.Equal(t, 50, emaCross.SlowPeriod)
			assert.Equal(t, 0.01, emaCross.RiskPct)
			assert.Equal(t, 20.0, emaCross.StopPips)
			assert.Equal(t, 2.0, emaCross.RR)
		})
	}
}

func TestStrategyByName_Unknown(t *testing.T) {
	_, err := StrategyByName("unknown-strategy", "EUR_USD", 1000, 20, 50, 0.01, 20, 2.0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown strategy")
}
