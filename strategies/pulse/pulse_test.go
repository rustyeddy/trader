package pulse

import (
	"context"
	"fmt"
	"testing"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeCandle creates a minimal candle for testing.
func makeCandle() *market.Candle {
	return &market.Candle{
		Open:  types.PriceFromFloat(1.0850),
		High:  types.PriceFromFloat(1.0860),
		Low:   types.PriceFromFloat(1.0840),
		Close: types.PriceFromFloat(1.0855),
	}
}

// makeRun builds a StrategyContext with zero or more open lots.
func makeRun(lotIDs ...string) *backtest.Backtest {
	lb := &execution.LotBook{}
	for _, id := range lotIDs {
		tc := &execution.TradeCommon{ID: id}
		tc.Instrument = "EURUSD"
		tc.Side = types.Long
		_ = lb.Add(&execution.Lot{TradeCommon: tc, State: execution.LotOpen})
	}
	return &backtest.Backtest{
		Request: &backtest.BacktestRequest{Instrument: "EURUSD"},
		State:   &backtest.BacktestRun{Lots: lb},
	}
}

// ── New validation ─────────────────────────────────────────────────────────────

func TestNew_ValidConfig(t *testing.T) {
	s, err := New(DefaultConfig())
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestNew_DefaultsTradeEvery(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 0
	s, err := New(cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, s.cfg.TradeEvery)
}

func TestNew_InvalidHoldBars(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldBars = 0
	_, err := New(cfg)
	assert.ErrorContains(t, err, "hold_bars")
}

func TestNew_InvalidMaxPositions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxPositions = -1
	_, err := New(cfg)
	assert.ErrorContains(t, err, "max_positions")
}

func TestNew_InvalidStopPips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StopPips = 0
	_, err := New(cfg)
	assert.ErrorContains(t, err, "stop_pips")
}

func TestNew_InvalidSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Side = "diagonal"
	_, err := New(cfg)
	assert.ErrorContains(t, err, "side")
}

// ── Update — no open positions ─────────────────────────────────────────────────

func TestUpdate_FirstBarNoPositions_HoldWhenNotDue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 3
	s, _ := New(cfg)

	sig := s.Update(context.Background(), makeCandle(), makeRun())
	// bar=1, 1%3 != 0 → hold
	assert.Equal(t, types.Flat, sig.Side)
	assert.False(t, sig.CloseAll)
	assert.Equal(t, "hold", sig.Reason)
}

func TestUpdate_OpensWhenDue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 2
	cfg.MaxPositions = 3
	s, _ := New(cfg)

	s.Update(context.Background(), makeCandle(), makeRun())        // bar 1 — hold
	sig := s.Update(context.Background(), makeCandle(), makeRun()) // bar 2 — open

	assert.Equal(t, types.Long, sig.Side) // first alternate = long
	assert.False(t, sig.CloseAll)
	assert.Equal(t, "pulse-open", sig.Reason)
}

// ── Update — MaxPositions cap ──────────────────────────────────────────────────

func TestUpdate_RespectsMaxPositions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 2
	cfg.HoldBars = 100
	s, _ := New(cfg)

	run := makeRun("t1", "t2")
	sig := s.Update(context.Background(), makeCandle(), run)
	// 2 open == max → no new open, no close
	assert.Equal(t, types.Flat, sig.Side)
	assert.False(t, sig.CloseAll)
}

func TestUpdate_OpensWhenUnderMaxPositions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 3
	cfg.HoldBars = 100
	s, _ := New(cfg)

	run := makeRun("t1")
	sig := s.Update(context.Background(), makeCandle(), run)
	assert.NotEqual(t, types.Flat, sig.Side)
}

// ── Update — HoldBars close logic ─────────────────────────────────────────────

func TestUpdate_ClosesAllWhenOldestExceedsHoldBars(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldBars = 3
	cfg.TradeEvery = 100 // never open
	s, _ := New(cfg)

	run := makeRun("t1", "t2")

	// Bar 1 and 2: both lots seen, not yet at HoldBars
	s.Update(context.Background(), makeCandle(), run)
	s.Update(context.Background(), makeCandle(), run)

	// Bar 3: barCount - openedAt = 3 - 1 = 2, not yet >= 3
	sig := s.Update(context.Background(), makeCandle(), run)
	assert.False(t, sig.CloseAll)

	// Bar 4: age = 4 - 1 = 3 >= HoldBars → close all
	sig = s.Update(context.Background(), makeCandle(), run)
	assert.True(t, sig.CloseAll)
	assert.Equal(t, types.Flat, sig.Side)
}

func TestUpdate_ClosesAndOpensInSameBar(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldBars = 2
	cfg.TradeEvery = 1
	cfg.MaxPositions = 2
	s, _ := New(cfg)

	run := makeRun("t1")
	s.Update(context.Background(), makeCandle(), run) // bar 1: lot opened at bar 1

	// Bar 3: age = 3 - 1 = 2 >= HoldBars → close + open (bar 3 % 1 == 0)
	s.Update(context.Background(), makeCandle(), run)        // bar 2
	sig := s.Update(context.Background(), makeCandle(), run) // bar 3
	assert.True(t, sig.CloseAll)
	assert.NotEqual(t, types.Flat, sig.Side)
	assert.Equal(t, "pulse-close-reopen", sig.Reason)
}

func TestUpdate_MaxPositionsAccountsForPendingClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldBars = 2
	cfg.TradeEvery = 1
	cfg.MaxPositions = 2
	s, _ := New(cfg)

	run := makeRun("t1", "t2")
	s.Update(context.Background(), makeCandle(), run) // bar 1: both lots opened

	// Bar 3: both lots age = 3-1=2 >= HoldBars → close all → 0 active → can open
	s.Update(context.Background(), makeCandle(), run)
	sig := s.Update(context.Background(), makeCandle(), run)
	assert.True(t, sig.CloseAll)
	assert.NotEqual(t, types.Flat, sig.Side)
}

// ── Side alternation ───────────────────────────────────────────────────────────

func TestUpdate_AlternateSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 10
	cfg.HoldBars = 100
	cfg.Side = "alternate"
	s, _ := New(cfg)

	sides := make([]types.Side, 6)
	for i := range sides {
		sig := s.Update(context.Background(), makeCandle(), makeRun())
		sides[i] = sig.Side
	}
	assert.Equal(t, types.Long, sides[0])
	assert.Equal(t, types.Short, sides[1])
	assert.Equal(t, types.Long, sides[2])
	assert.Equal(t, types.Short, sides[3])
}

func TestUpdate_FixedLongSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 10
	cfg.HoldBars = 100
	cfg.Side = "long"
	s, _ := New(cfg)

	for i := 0; i < 4; i++ {
		sig := s.Update(context.Background(), makeCandle(), makeRun())
		assert.Equal(t, types.Long, sig.Side, "bar %d", i+1)
	}
}

func TestUpdate_FixedShortSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 10
	cfg.HoldBars = 100
	cfg.Side = "short"
	s, _ := New(cfg)

	sig := s.Update(context.Background(), makeCandle(), makeRun())
	assert.Equal(t, types.Short, sig.Side)
}

// ── Name / Ready / Reset ───────────────────────────────────────────────────────

func TestStrategy_Name(t *testing.T) {
	s, _ := New(DefaultConfig())
	assert.Equal(t, "pulse", s.Name())
}

func TestStrategy_Ready(t *testing.T) {
	s, _ := New(DefaultConfig())
	assert.True(t, s.Ready())
}

func TestStrategy_Reset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.HoldBars = 100
	s, _ := New(cfg)

	run := makeRun("t1")
	s.Update(context.Background(), makeCandle(), run)
	s.Update(context.Background(), makeCandle(), run)
	require.Greater(t, s.barCount, 0)

	s.Reset()
	assert.Equal(t, 0, s.barCount)
	assert.Nil(t, s.lotOpenedAt)
}

// ── NilCandle and NilContext ───────────────────────────────────────────────────

func TestUpdate_NilCandle_ReturnsHold(t *testing.T) {
	s, _ := New(DefaultConfig())
	sig := s.Update(context.Background(), nil, makeRun())
	assert.Equal(t, types.Flat, sig.Side)
	assert.Equal(t, "no candle", sig.Reason)
}

func TestUpdate_NilContext_StillOpens(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 5
	cfg.HoldBars = 100
	s, _ := New(cfg)

	sig := s.Update(context.Background(), makeCandle(), nil)
	assert.NotEqual(t, types.Flat, sig.Side)
}

// ── Table-driven open schedule ─────────────────────────────────────────────────

func TestUpdate_OpenSchedule(t *testing.T) {
	cases := []struct {
		bar      int
		every    int
		lots     int
		max      int
		wantOpen bool
	}{
		{1, 3, 0, 2, false},
		{3, 3, 0, 2, true},
		{6, 3, 2, 2, false}, // at max
		{6, 3, 1, 2, true},  // space available
		{5, 5, 0, 1, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("bar%d_every%d_lots%d_max%d", tc.bar, tc.every, tc.lots, tc.max), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.TradeEvery = tc.every
			cfg.MaxPositions = tc.max
			cfg.HoldBars = 1000
			s, _ := New(cfg)
			s.barCount = tc.bar - 1 // next Update() will be bar tc.bar

			ids := make([]string, tc.lots)
			for i := range ids {
				ids[i] = fmt.Sprintf("t%d", i)
			}
			sig := s.Update(context.Background(), makeCandle(), makeRun(ids...))
			if tc.wantOpen {
				assert.NotEqual(t, types.Flat, sig.Side, "expected open at bar %d", tc.bar)
			} else {
				assert.Equal(t, types.Flat, sig.Side, "expected no open at bar %d", tc.bar)
			}
		})
	}
}
