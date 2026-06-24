package scalper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/market"
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
	assert.Equal(t, "SCALPER(ema3/8,atr14)", s.Name())
}

func TestReady_AfterIndicatorsWarmUp(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	for i := 0; i < 14; i++ {
		s.Update(context.Background(), scalperCT(1.1000, 1.1010, 1.0990, 1.1000), nil)
	}
	assert.False(t, s.Ready())

	s.Update(context.Background(), scalperCT(1.1000, 1.1010, 1.0990, 1.1000), nil)
	assert.True(t, s.Ready())
}

func TestReset_IsNoop(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	require.NotPanics(t, s.Reset)
}

func TestStopDescription(t *testing.T) {
	s, _ := New(Config{FastPeriod: 3, SlowPeriod: 8})
	assert.Equal(t, "ATR(14)×1.0", s.StopDescription())
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
	ct := &market.CandleTime{
		Candle: market.Candle{Close: market.Price(1.0850 * float64(market.PriceScale))},
	}
	plan := s.Update(context.Background(), ct, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
	assert.Empty(t, plan.Closes)
}

func TestUpdate_BuyTheDipRecoveryOpensLong(t *testing.T) {
	s, err := New(Config{FastPeriod: 2, SlowPeriod: 3, ATRPeriod: 2, StopMultiplier: 1.0})
	require.NoError(t, err)

	run := &backtest.Backtest{
		Request: &backtest.BacktestRequest{Instrument: "EURUSD"},
		State:   &backtest.BacktestRun{},
	}

	for _, ct := range []*market.CandleTime{
		scalperCT(1.0000, 1.0010, 0.9990, 1.0000),
		scalperCT(1.0100, 1.0110, 1.0090, 1.0100),
		scalperCT(1.0200, 1.0210, 1.0190, 1.0200),
		scalperCT(1.0100, 1.0110, 0.9990, 1.0000),
	} {
		plan := s.Update(context.Background(), ct, run)
		require.Empty(t, plan.Opens)
	}

	recovery := scalperCT(1.0000, 1.0310, 0.9990, 1.0300)
	plan := s.Update(context.Background(), recovery, run)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, "buy-the-dip", plan.Reason)
	assert.Equal(t, "buy-the-dip", plan.Opens[0].Reason)
	assert.Equal(t, market.Long, plan.Opens[0].Side)
	assert.Equal(t, "EURUSD", plan.Opens[0].Instrument)
	assert.Less(t, plan.Opens[0].Stop, recovery.Close)
}

func scalperCT(open, high, low, close float64) *market.CandleTime {
	return &market.CandleTime{
		Candle: market.Candle{
			Open:  market.PriceFromFloat(open),
			High:  market.PriceFromFloat(high),
			Low:   market.PriceFromFloat(low),
			Close: market.PriceFromFloat(close),
		},
	}
}
