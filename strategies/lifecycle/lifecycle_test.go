package lifecycle

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/execution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minCandle returns a CandleTime with a valid close price for EURUSD.
func minCandle(close float64) *trader.CandleTime {
	toP := func(x float64) trader.Price {
		return trader.Price(x*float64(trader.PriceScale) + 0.5)
	}
	c := toP(close)
	return &trader.CandleTime{Candle: trader.Candle{
		Open:  c,
		High:  c + 1000,
		Low:   c - 1000,
		Close: c,
	}}
}

// minRun builds a minimal Backtest sufficient for the lifecycle strategy.
func minRun(lots *execution.LotBook) *trader.Backtest {
	if lots == nil {
		lots = &execution.LotBook{}
	}
	return &trader.Backtest{
		Request: &trader.BacktestRequest{Instrument: "EURUSD"},
		State:   &trader.BacktestRun{Lots: lots},
	}
}

// openLot builds and adds a minimal open lot to a fresh LotBook.
func openLot() *execution.LotBook {
	lb := &execution.LotBook{}
	lot := &execution.Lot{
		TradeCommon: &execution.TradeCommon{
			ID:         "lot-1",
			Instrument: "EURUSD",
			Side:       trader.Long,
			Units:      1000,
		},
		EntryPrice:     trader.PriceFromFloat(1.1),
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
	plan := s.Update(context.Background(), nil, minRun(nil))
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
	assert.Empty(t, plan.Closes)
}

func TestStrategy_Update_NilRunReturnsHold(t *testing.T) {
	s := &Strategy{}
	plan := s.Update(context.Background(), minCandle(1.1), nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
}

// ── Bar-by-bar lifecycle ──────────────────────────────────────────────────────

func TestStrategy_Update_Bar1_OpensLong(t *testing.T) {
	s := &Strategy{}
	plan := s.Update(context.Background(), minCandle(1.10), minRun(nil))
	require.NotNil(t, plan)
	require.Len(t, plan.Opens, 1, "bar 1 should emit one long open")
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
	assert.Equal(t, "lifecycle-test-open-long", plan.Reason)
	assert.True(t, s.opened)
}

func TestStrategy_Update_Bar2_Holds(t *testing.T) {
	s := &Strategy{bar: 1, opened: true}
	plan := s.Update(context.Background(), minCandle(1.10), minRun(nil))
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
	assert.Empty(t, plan.Closes)
	assert.Equal(t, "hold", plan.Reason)
}

func TestStrategy_Update_Bar3_NoLotsYieldsNoClose(t *testing.T) {
	s := &Strategy{bar: 2, opened: true}
	// State has empty LotBook — strategy cannot find lot to close.
	plan := s.Update(context.Background(), minCandle(1.11), minRun(nil))
	require.NotNil(t, plan)
	assert.Empty(t, plan.Closes)
	assert.Equal(t, "lifecycle-test-no-position-to-close", plan.Reason)
}

func TestStrategy_Update_Bar3_WithOpenLot_EmitsClose(t *testing.T) {
	s := &Strategy{bar: 2, opened: true}
	run := minRun(openLot())
	plan := s.Update(context.Background(), minCandle(1.11), run)
	require.NotNil(t, plan)
	require.Len(t, plan.Closes, 1, "bar 3 with open lot should emit one close")
	assert.Equal(t, "lifecycle-test-close-long", plan.Reason)
	assert.True(t, s.closed)
}

func TestStrategy_Update_Bar3_AlreadyClosed_Holds(t *testing.T) {
	s := &Strategy{bar: 2, opened: true, closed: true}
	plan := s.Update(context.Background(), minCandle(1.11), minRun(openLot()))
	require.NotNil(t, plan)
	assert.Empty(t, plan.Closes)
}

func TestStrategy_Update_DefaultsUnitsAndStopPips(t *testing.T) {
	s := &Strategy{Units: 0, StopPips: 0}
	plan := s.Update(context.Background(), minCandle(1.10), minRun(nil))
	require.NotNil(t, plan)
	// After bar 1 the strategy should have applied defaults.
	assert.Equal(t, trader.Units(1000), s.Units)
	assert.Equal(t, float64(20), s.StopPips)
	_ = plan
}
