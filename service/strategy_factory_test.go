package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/rustyeddy/trader/brokers/oanda"
)

func testService() *Service {
	return &Service{
		OANDA:     &oanda.Client{BaseURL: "https://api-fxpractice.oanda.com", Token: "test"},
		AccountID: "test-account",
		bots:      make(map[string]*botEntry),
	}
}

func TestBuildLiveStrategy_Pulse(t *testing.T) {
	svc := testService()
	strat, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:   "pulse",
		Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
	}, "EUR_USD")
	require.NoError(t, err)
	assert.Contains(t, strat.Name(), "pulse")
}

func TestBuildLiveStrategy_PulseDefaults(t *testing.T) {
	svc := testService()
	strat, err := svc.BuildLiveStrategy(StrategyConfig{}, "EUR_USD")
	require.NoError(t, err)
	assert.NotNil(t, strat)
}

func TestBuildLiveStrategy_Scalper(t *testing.T) {
	svc := testService()
	strat, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:        "scalper",
		Granularity: "M1",
		Params:      map[string]any{"fast_period": 3, "slow_period": 8},
	}, "EUR_USD")
	require.NoError(t, err)
	assert.NotNil(t, strat)
}

func TestBuildLiveStrategy_Stress(t *testing.T) {
	svc := testService()
	strat, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:   "stress",
		Params: map[string]any{"trade_every": 1, "stop_pct": 0.2},
	}, "EUR_USD")
	require.NoError(t, err)
	assert.Contains(t, strat.Name(), "STRESS")
}

func TestBuildLiveStrategy_UnknownKind(t *testing.T) {
	svc := testService()
	_, err := svc.BuildLiveStrategy(StrategyConfig{Kind: "bogus"}, "EUR_USD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown strategy kind")
}

func TestBuildLiveStrategy_ScalperInvalidParams(t *testing.T) {
	svc := testService()
	_, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:   "scalper",
		Params: map[string]any{"fast_period": 8, "slow_period": 3}, // fast >= slow
	}, "EUR_USD")
	require.Error(t, err)
}

func TestToInt(t *testing.T) {
	assert.Equal(t, 5, toInt(5, 1))
	assert.Equal(t, 5, toInt(5.0, 1))
	assert.Equal(t, 1, toInt("x", 1))
}

func TestToFloat(t *testing.T) {
	assert.Equal(t, 1.5, toFloat(1.5, 0))
	assert.Equal(t, 2.0, toFloat(2, 0))
	assert.Equal(t, 0.0, toFloat("x", 0))
}
