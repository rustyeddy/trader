package lifecycle

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minCandle returns a CandleTime with a valid close price for EURUSD.
func minCandle(close float64) *market.CandleTime {
	toP := func(x float64) market.Price {
		return market.Price(x*float64(market.PriceScale) + 0.5)
	}
	c := toP(close)
	return &market.CandleTime{Candle: market.Candle{
		Open:  c,
		High:  c + 1000,
		Low:   c - 1000,
		Close: c,
	}}
}

// minRun builds a minimal Backtest sufficient for the lifecycle strategy.
func minRun(lots *execution.LotBook) *backtest.Backtest {
	if lots == nil {
		lots = &execution.LotBook{}
	}
	return &backtest.Backtest{
		Request: &backtest.BacktestRequest{Instrument: "EURUSD"},
		State:   &backtest.BacktestRun{Lots: lots},
	}
}

// openLot builds and adds a minimal open lot to a fresh LotBook.
func openLot() *execution.LotBook {
	lb := &execution.LotBook{}
	lot := &execution.Lot{
		TradeCommon: &execution.TradeCommon{
			ID:         "lot-1",
			Instrument: "EURUSD",
			Side:       market.Long,
			Units:      1000,
		},
		EntryPrice:     market.PriceFromFloat(1.1),
		EntryTime:      1000,
		OriginalUnits:  1000,
		RemainingUnits: 1000,
		State:          execution.LotOpen,
	}
	_ = lb.Add(lot)
	return lb
}

// ── Interface / metadata ──────────────────────────────────────────────────────

func TestStrategy_Name(t *testing.T) {
	s := &Strategy{}
	assert.Equal(t, "lifecycle-test", s.Name())
}

func TestStrategy_StopDescription(t *testing.T) {
	s := &Strategy{}
	assert.Equal(t, "", s.StopDescription())
}

func TestStrategy_Ready_AlwaysTrue(t *testing.T) {
	s := &Strategy{}
	assert.True(t, s.Ready())
}

func TestStrategy_Reset_ClearsBarAndFlags(t *testing.T) {
	s := &Strategy{bar: 5, opened: true, closed: true}
	s.Reset()
	assert.Equal(t, 0, s.bar)
	assert.False(t, s.opened)
	assert.False(t, s.closed)
}

// ── Build ─────────────────────────────────────────────────────────────────────

func TestBuild_ReturnsStrategy(t *testing.T) {
	strat, err := build(nil)
	require.NoError(t, err)
	require.NotNil(t, strat)
	assert.Equal(t, "lifecycle-test", strat.Name())
}

// ── Update edge cases ─────────────────────────────────────────────────────────

func TestStrategy_Update_NilCandleReturnsHold(t *testing.T) {
	s := &Strategy{}
	sig := s.Update(context.Background(), nil, minRun(nil))
	assert.Equal(t, market.Flat, sig.Side)
}

func TestStrategy_Update_NilRunReturnsHold(t *testing.T) {
	s := &Strategy{}
	sig := s.Update(context.Background(), minCandle(1.1), nil)
	assert.Equal(t, market.Flat, sig.Side)
}

// ── Bar-by-bar lifecycle ──────────────────────────────────────────────────────

func TestStrategy_Update_Bar1_OpensLong(t *testing.T) {
	s := &Strategy{}
	sig := s.Update(context.Background(), minCandle(1.10), minRun(nil))
	assert.Equal(t, market.Long, sig.Side)
	assert.Equal(t, "lifecycle-test-open-long", sig.Reason)
	assert.True(t, s.opened)
}

func TestStrategy_Update_Bar2_Holds(t *testing.T) {
	s := &Strategy{bar: 1, opened: true}
	sig := s.Update(context.Background(), minCandle(1.10), minRun(nil))
	assert.Equal(t, market.Flat, sig.Side)
	assert.Equal(t, "hold", sig.Reason)
}

func TestStrategy_Update_Bar3_EmitsCloseAll(t *testing.T) {
	s := &Strategy{bar: 2, opened: true}
	sig := s.Update(context.Background(), minCandle(1.11), minRun(openLot()))
	assert.True(t, sig.CloseAll)
	assert.Equal(t, "lifecycle-test-close-long", sig.Reason)
	assert.True(t, s.closed)
}

func TestStrategy_Update_Bar3_AlreadyClosed_Holds(t *testing.T) {
	s := &Strategy{bar: 2, opened: true, closed: true}
	sig := s.Update(context.Background(), minCandle(1.11), minRun(openLot()))
	assert.Equal(t, market.Flat, sig.Side)
	assert.False(t, sig.CloseAll)
}

func TestStrategy_Update_DefaultsUnitsAndStopPips(t *testing.T) {
	s := &Strategy{Units: 0, StopPips: 0}
	_ = s.Update(context.Background(), minCandle(1.10), minRun(nil))
	assert.Equal(t, market.Units(1000), s.Units)
	assert.Equal(t, float64(20), s.StopPips)
}
