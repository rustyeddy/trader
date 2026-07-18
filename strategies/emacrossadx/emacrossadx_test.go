package emacrossadx

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkCandle builds a CandleTime with a symmetric spread around close so that
// the ADX indicator receives valid OHLC data.
func mkCandle(close float64) *market.CandleTime {
	toP := func(x float64) types.Price {
		return types.Price(x*float64(types.PriceScale) + 0.5)
	}
	c := toP(close)
	spread := types.Price(1000) // ~10 pips
	return &market.CandleTime{Candle: market.Candle{
		Open:  c,
		High:  c + spread,
		Low:   c - spread,
		Close: c,
	}}
}

// feedUpdates drives s with a slice of close prices and returns the signals.
func feedUpdates(s *Strategy, closes []float64) []strategy.Signal {
	sigs := make([]strategy.Signal, 0, len(closes))
	for _, c := range closes {
		sigs = append(sigs, s.Update(context.Background(), mkCandle(c), nil))
	}
	return sigs
}

// minCfg returns the smallest valid Config for signal tests.
// Uses a large ADX period so ADX never becomes ready, bypassing the
// ADX-threshold gate while still exercising the EMA cross logic.
func minCfg() Config {
	return Config{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       100, // won't be ready in our test series
		Scale:           types.PriceScale,
		RequireADXReady: false,
	}
}

// ── New / construction ────────────────────────────────────────────────────────

func TestNew_Valid(t *testing.T) {
	s, err := New(minCfg())
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestNew_ZeroPeriods(t *testing.T) {
	cfgs := []Config{
		{FastPeriod: 0, SlowPeriod: 5, ADXPeriod: 14, Scale: types.PriceScale},
		{FastPeriod: 3, SlowPeriod: 0, ADXPeriod: 14, Scale: types.PriceScale},
		{FastPeriod: 3, SlowPeriod: 5, ADXPeriod: 0, Scale: types.PriceScale},
	}
	for _, cfg := range cfgs {
		_, err := New(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "periods must be > 0")
	}
}

func TestNew_FastMustBeLessThanSlow(t *testing.T) {
	_, err := New(Config{FastPeriod: 5, SlowPeriod: 3, ADXPeriod: 14, Scale: types.PriceScale})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FastPeriod must be < SlowPeriod")
}

func TestNew_ZeroScaleReturnsError(t *testing.T) {
	_, err := New(Config{FastPeriod: 3, SlowPeriod: 5, ADXPeriod: 14, Scale: 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Scale must be > 0")
}

func TestNew_DefaultsADXThresholdTo20(t *testing.T) {
	cfg := minCfg()
	cfg.ADXThreshold = 0 // should default to 20.0
	s, err := New(cfg)
	require.NoError(t, err)
	assert.Equal(t, types.Units(20_000_000), s.adxThreshold)
}

func TestNew_WithATRPeriod(t *testing.T) {
	cfg := minCfg()
	cfg.ATRPeriod = 14
	s, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, s.core.ATR)
	assert.InDelta(t, 1.5, s.core.ATRMultiplier, 1e-9) // default multiplier
}

// ── build (params constructor) ────────────────────────────────────────────────

func TestBuild_ValidParams(t *testing.T) {
	s, err := build(map[string]any{"fast": int32(9), "slow": int32(21)})
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestBuild_MissingFastReturnsError(t *testing.T) {
	_, err := build(map[string]any{"slow": int32(21)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fast")
}

func TestBuild_MissingSlowReturnsError(t *testing.T) {
	_, err := build(map[string]any{"fast": int32(9)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slow")
}

func TestBuild_FastGteSlowReturnsError(t *testing.T) {
	_, err := build(map[string]any{"fast": int32(21), "slow": int32(9)})
	require.Error(t, err)
}

// ── Name / Ready / Reset ──────────────────────────────────────────────────────

func TestStrategy_Name(t *testing.T) {
	s, err := New(Config{
		FastPeriod: 9, SlowPeriod: 21, ADXPeriod: 14,
		Scale: types.PriceScale, ADXThreshold: 25.0,
	})
	require.NoError(t, err)
	assert.Contains(t, s.Name(), "EMA_CROSS_ADX")
	assert.Contains(t, s.Name(), "9")
	assert.Contains(t, s.Name(), "21")
}

func TestStrategy_Ready_FalseBeforeWarmup(t *testing.T) {
	s, err := New(minCfg())
	require.NoError(t, err)
	assert.False(t, s.Ready())
}

func TestStrategy_Ready_TrueAfterEMAWarmup(t *testing.T) {
	cfg := minCfg()
	s, err := New(cfg)
	require.NoError(t, err)
	for i := 0; i < cfg.SlowPeriod; i++ {
		s.Update(context.Background(), mkCandle(1.0), nil)
	}
	assert.True(t, s.Ready())
}

func TestStrategy_RequireADXReady_DelaysReadiness(t *testing.T) {
	cfg := Config{
		FastPeriod: 3, SlowPeriod: 5, ADXPeriod: 14,
		Scale:           types.PriceScale,
		RequireADXReady: true,
	}
	s, err := New(cfg)
	require.NoError(t, err)
	for i := 0; i < cfg.SlowPeriod; i++ {
		s.Update(context.Background(), mkCandle(1.0), nil)
	}
	assert.False(t, s.Ready())
}

func TestStrategy_Reset_ClearsState(t *testing.T) {
	s, err := New(minCfg())
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		s.Update(context.Background(), mkCandle(1.0+float64(i)*0.0001), nil)
	}
	s.Reset()
	assert.Equal(t, 0, s.core.PrevRel)
	assert.Equal(t, 0, s.pendingRel)
	assert.False(t, s.core.Fast.Ready())
}

// ── Update ────────────────────────────────────────────────────────────────────

func TestStrategy_Update_NilCandleReturnsHold(t *testing.T) {
	s, err := New(minCfg())
	require.NoError(t, err)
	sig := s.Update(context.Background(), nil, nil)
	assert.Equal(t, types.Flat, sig.Side)
}

func TestStrategy_Update_WarmupProducesNoOpens(t *testing.T) {
	s, err := New(minCfg())
	require.NoError(t, err)
	sigs := feedUpdates(s, []float64{1.0, 1.0, 1.0})
	for _, sig := range sigs {
		assert.Equal(t, types.Flat, sig.Side)
	}
}

func TestStrategy_Update_CrossUpEmitsLongSignal(t *testing.T) {
	s, err := New(minCfg())
	require.NoError(t, err)

	closes := make([]float64, 0, 120)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	p := 1.0000
	for i := 0; i < 20; i++ {
		p -= 0.0002
		closes = append(closes, p)
	}
	for i := 0; i < 30; i++ {
		p += 0.0003
		closes = append(closes, p)
	}

	sigs := feedUpdates(s, closes)
	var longs []strategy.Signal
	for _, sig := range sigs {
		if sig.Side == types.Long {
			longs = append(longs, sig)
		}
	}
	require.NotEmpty(t, longs, "expected at least one long signal after EMA cross up")
	assert.Equal(t, types.Long, longs[0].Side)
}

func TestStrategy_Update_CrossDownEmitsShortSignal(t *testing.T) {
	s, err := New(minCfg())
	require.NoError(t, err)

	closes := make([]float64, 0, 150)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	p := 1.0000
	for i := 0; i < 20; i++ {
		p += 0.0002
		closes = append(closes, p)
	}
	for i := 0; i < 30; i++ {
		p -= 0.0003
		closes = append(closes, p)
	}

	sigs := feedUpdates(s, closes)
	var shorts []strategy.Signal
	for _, sig := range sigs {
		if sig.Side == types.Short {
			shorts = append(shorts, sig)
		}
	}
	require.NotEmpty(t, shorts, "expected at least one short signal after EMA cross down")
	assert.Equal(t, types.Short, shorts[0].Side)
}

func TestStrategy_Update_ADXGateFiltersWhenBelowThreshold(t *testing.T) {
	cfg := Config{
		FastPeriod:      3,
		SlowPeriod:      5,
		ADXPeriod:       5,
		Scale:           types.PriceScale,
		ADXThreshold:    99.0,
		RequireADXReady: false,
	}
	s, err := New(cfg)
	require.NoError(t, err)

	closes := make([]float64, 0, 120)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	p := 1.0000
	for i := 0; i < 20; i++ {
		p -= 0.0002
		closes = append(closes, p)
	}
	for i := 0; i < 30; i++ {
		p += 0.0003
		closes = append(closes, p)
	}

	sigs := feedUpdates(s, closes)
	for _, sig := range sigs {
		assert.Equal(t, types.Flat, sig.Side, "ADX gate should suppress all opens when ADX < 99")
	}
}

// ── absPriceSum ───────────────────────────────────────────────────────────────

func TestAbsPriceSum_Positive(t *testing.T) {
	assert.Equal(t, types.PriceSum(5), absPriceSum(5))
}

func TestAbsPriceSum_Negative(t *testing.T) {
	assert.Equal(t, types.PriceSum(5), absPriceSum(-5))
}

func TestAbsPriceSum_Zero(t *testing.T) {
	assert.Equal(t, types.PriceSum(0), absPriceSum(0))
}
