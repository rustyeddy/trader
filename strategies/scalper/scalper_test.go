package scalper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

func TestNew_Valid(t *testing.T) {
	s, err := New(Config{FastPeriod: 3, SlowPeriod: 8})
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestNew_InvalidFastPeriod(t *testing.T) {
	_, err := New(Config{FastPeriod: 0, SlowPeriod: 8})
	assert.ErrorContains(t, err, "fast_period")
}

func TestNew_InvalidSlowPeriod(t *testing.T) {
	_, err := New(Config{FastPeriod: 3, SlowPeriod: 0})
	assert.ErrorContains(t, err, "slow_period")
}

func TestNew_FastGeqSlow(t *testing.T) {
	_, err := New(Config{FastPeriod: 8, SlowPeriod: 3})
	assert.ErrorContains(t, err, "must be <")
}

func TestNew_FastEqualSlow(t *testing.T) {
	_, err := New(Config{FastPeriod: 5, SlowPeriod: 5})
	assert.ErrorContains(t, err, "must be <")
}

func TestName(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	assert.Equal(t, "SCALPER(ema3/8)", s.Name())
}

func TestReady_AlwaysTrue(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	assert.True(t, s.Ready())
}

func TestReset_IsNoop(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	require.NotPanics(t, s.Reset)
}

func TestStopDescription_Empty(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	assert.Equal(t, "", s.StopDescription())
}

func TestUpdate_NilCandleTime(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	plan := s.Update(context.Background(), nil, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
	assert.Empty(t, plan.Closes)
}

func TestUpdate_ReturnsDefaultPlan(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	ct := &trader.CandleTime{
		Candle: trader.Candle{Close: trader.Price(1.0850 * float64(trader.PriceScale))},
	}
	plan := s.Update(context.Background(), ct, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
	assert.Empty(t, plan.Closes)
}
