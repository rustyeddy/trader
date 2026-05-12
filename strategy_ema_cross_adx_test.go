package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetDefaultPlanOnCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DefaultStrategyPlan = StrategyPlan{Reason: "hold"}
	})
}

func TestNewEMACrossADX_PanicGuards(t *testing.T) {
	resetDefaultPlanOnCleanup(t)

	require.Panics(t, func() {
		NewEMACrossADX(EMACrossADXConfig{FastPeriod: 0, SlowPeriod: 5, ADXPeriod: 14, Scale: PriceScale})
	})
	require.Panics(t, func() {
		NewEMACrossADX(EMACrossADXConfig{FastPeriod: 5, SlowPeriod: 5, ADXPeriod: 14, Scale: PriceScale})
	})
	require.Panics(t, func() {
		NewEMACrossADX(EMACrossADXConfig{FastPeriod: 3, SlowPeriod: 5, ADXPeriod: 0, Scale: PriceScale})
	})
	require.Panics(t, func() {
		NewEMACrossADX(EMACrossADXConfig{FastPeriod: 3, SlowPeriod: 5, ADXPeriod: 14, Scale: 0})
	})
}

func TestEMACrossADX_NameDefaultsReadyAndReset(t *testing.T) {
	resetDefaultPlanOnCleanup(t)

	x := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      2,
		SlowPeriod:      3,
		ADXPeriod:       3,
		Scale:           PriceScale,
		ADXThreshold:    0,
		RequireADXReady: true,
	})

	assert.Equal(t, 20.0, x.adxThreshold)
	assert.Contains(t, x.Name(), "EMA_CROSS_ADX(2,3,ADX3@20.0)")
	assert.False(t, x.Ready())

	for i := 0; i < 30; i++ {
		c := &CandleTime{Candle: mkCandle(int32(PriceScale), 1.0+float64(i)*0.0001, 1.0+float64(i)*0.0002, 1.0, 1.0+float64(i)*0.0001)}
		x.Update(context.Background(), c, nil)
	}
	assert.True(t, x.Ready())

	x.pendingRel = 1
	x.core.prevRel = 1
	x.Reset()
	assert.Equal(t, 0, x.pendingRel)
	assert.Equal(t, 0, x.core.prevRel)
	assert.False(t, x.Ready())
}

func TestEMACrossADX_Update_WarmupAndNil(t *testing.T) {
	resetDefaultPlanOnCleanup(t)

	x := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      2,
		SlowPeriod:      4,
		ADXPeriod:       6,
		Scale:           PriceScale,
		RequireADXReady: true,
	})

	plan := x.Update(context.Background(), nil, nil)
	require.NotNil(t, plan)
	assert.NotEmpty(t, plan.Reason)

	plan = x.Update(context.Background(), &CandleTime{Candle: mkCandle(int32(PriceScale), 1.0, 1.1, 0.9, 1.0)}, nil)
	require.NotNil(t, plan)
	assert.Equal(t, "warming up EMAs", plan.Reason)

	seenWarmingADX := false
	for i := 0; i < 20; i++ {
		plan = x.Update(context.Background(), &CandleTime{Candle: mkCandle(int32(PriceScale), 1.0, 1.1, 0.9, 1.0+float64(i)*0.0001)}, nil)
		if plan.Reason == "warming up ADX" {
			seenWarmingADX = true
			break
		}
	}
	assert.True(t, seenWarmingADX)
}

func TestEMACrossADX_Update_ThresholdAndDIAndConfirm(t *testing.T) {
	resetDefaultPlanOnCleanup(t)

	x := NewEMACrossADX(EMACrossADXConfig{
		FastPeriod:      2,
		SlowPeriod:      3,
		ADXPeriod:       3,
		Scale:           PriceScale,
		ADXThreshold:    20,
		RequireDI:       true,
		RequireADXReady: false,
	})

	x.core.fast.ready = true
	x.core.fast.seen = 10
	x.core.fast.value = 2.0
	x.core.slow.ready = true
	x.core.slow.seen = 10
	x.core.slow.value = 1.0
	x.core.prevRel = 1
	x.pendingRel = 1
	x.adx.ready = true
	x.adx.hasPrev = false
	x.adx.adx = 5
	x.adx.plusDI = 10
	x.adx.minusDI = 20

	ct := &CandleTime{Candle: mkCandle(int32(PriceScale), 2.0, 2.1, 1.9, 2.0)}
	plan := x.Update(context.Background(), ct, nil)
	require.NotNil(t, plan)
	assert.Equal(t, "waiting for ADX threshold", plan.Reason)

	x.pendingRel = 1
	x.core.prevRel = 1
	x.adx.hasPrev = false
	x.adx.adx = 40
	x.adx.plusDI = 10
	x.adx.minusDI = 20

	plan = x.Update(context.Background(), ct, nil)
	require.NotNil(t, plan)
	assert.Equal(t, "waiting for DI confirmation (buy)", plan.Reason)

	x.pendingRel = 1
	x.core.prevRel = 1
	x.adx.hasPrev = false
	x.adx.adx = 40
	x.adx.plusDI = 30
	x.adx.minusDI = 10

	plan = x.Update(context.Background(), ct, nil)
	require.NotNil(t, plan)
	assert.Equal(t, "EMA cross up + ADX confirmed", plan.Reason)
}
