package service

import (
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Register strategies used in these tests.
	_ "github.com/rustyeddy/trader/strategies/bollingerfade"
	_ "github.com/rustyeddy/trader/strategies/donchian"
	_ "github.com/rustyeddy/trader/strategies/pulse"
)

func testService() *Service {
	return &Service{
		OANDA:       &oanda.Client{BaseURL: "https://api-fxpractice.oanda.com", Token: "test"},
		AccountID:   "test-account",
		bots:        make(map[string]*botEntry),
		tradeBotMap: make(map[string]string),
	}
}

func TestBuildLiveStrategy_Pulse(t *testing.T) {
	svc := testService()
	// pulse is now a strategy.Strategy wrapped in CandleStrategyAdapter; its
	// Name() is "pulse/<instrument>/<granularity>".
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

// ── generic backtest strategy fallback ───────────────────────────────────────

func TestBuildLiveStrategy_Donchian(t *testing.T) {
	svc := testService()
	strat, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:        "donchian-breakout",
		Granularity: "D",
		Params:      map[string]any{"period": 55, "close_strength": 0.6, "confirm_bars": 1},
		Exit:        strategy.ExitConfig{Kind: "chandelier", Params: map[string]any{"atr_period": 14, "multiplier": 6.0}},
	}, "USD_JPY")
	require.NoError(t, err)
	assert.NotNil(t, strat)
}

func TestBuildLiveStrategy_DonchianWithRegime(t *testing.T) {
	svc := testService()
	strat, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:        "donchian-breakout",
		Granularity: "H1",
		Params:      map[string]any{"period": 20, "close_strength": 0.6, "confirm_bars": 2, "adx_period": 14, "adx_threshold": 25.0},
		Exit:        strategy.ExitConfig{Kind: "chandelier", Params: map[string]any{"atr_period": 14, "multiplier": 3.0}},
		Regime:      strategy.RegimeConfig{Kind: "session", Params: map[string]any{"session_start": 7, "session_end": 17}},
	}, "GBP_USD")
	require.NoError(t, err)
	assert.NotNil(t, strat)
}

func TestBuildLiveStrategy_BBFade(t *testing.T) {
	svc := testService()
	strat, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:        "bb-fade",
		Granularity: "D",
		Params:      map[string]any{"period": 20, "multiplier": 2.0, "atr_period": 14, "atr_mult": 2.5},
	}, "EUR_GBP")
	require.NoError(t, err)
	assert.NotNil(t, strat)
}

func TestBuildLiveStrategy_BadExit(t *testing.T) {
	svc := testService()
	_, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:   "donchian-breakout",
		Params: map[string]any{"period": 20},
		Exit:   strategy.ExitConfig{Kind: "bad-exit"},
	}, "USD_JPY")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit strategy")
}

func TestBuildLiveStrategy_BadRegime(t *testing.T) {
	svc := testService()
	_, err := svc.BuildLiveStrategy(StrategyConfig{
		Kind:   "donchian-breakout",
		Params: map[string]any{"period": 20},
		Regime: strategy.RegimeConfig{Kind: "bad-regime"},
	}, "USD_JPY")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "regime filter")
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
