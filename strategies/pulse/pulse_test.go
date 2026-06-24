package pulse

import (
	"context"
	"fmt"
	"testing"

	"github.com/rustyeddy/trader/live"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var price = live.LivePrice{Instrument: "EUR_USD", Bid: 1.0850, Ask: 1.0852}

func makeTrade(id string, ticksOpen int) live.LiveTrade {
	return live.LiveTrade{ID: id, Instrument: "EUR_USD", Units: 1000, TicksOpen: ticksOpen}
}

// ── New validation ─────────────────────────────────────────────────────────────

func TestNew_ValidConfig(t *testing.T) {
	s, err := New(DefaultConfig())
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestNew_DefaultsTradeEvery(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 0 // should be clamped to 1
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

func TestNew_DefaultsRiskPct(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RiskPct = 0 // should default to 0.1
	s, err := New(cfg)
	require.NoError(t, err)
	assert.Equal(t, 0.1, s.cfg.RiskPct)
}

// ── Tick — no open positions ───────────────────────────────────────────────────

func TestTick_FirstTickNoTrades_HoldWhenNotDue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 3
	s, _ := New(cfg)

	plan := s.Tick(context.Background(), price, nil)
	// tick=1, 1%3 != 0 → hold
	assert.Nil(t, plan.Open)
	assert.Empty(t, plan.CloseIDs)
	assert.Equal(t, "hold", plan.Reason)
}

func TestTick_OpensWhenDue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 2
	cfg.MaxPositions = 3
	s, _ := New(cfg)

	s.Tick(context.Background(), price, nil)         // tick 1 — hold
	plan := s.Tick(context.Background(), price, nil) // tick 2 — open

	require.NotNil(t, plan.Open)
	assert.Equal(t, "long", plan.Open.Side) // first alternate = long
	assert.Equal(t, cfg.StopPips, plan.Open.StopPips)
	assert.Equal(t, cfg.RiskPct, plan.Open.RiskPct)
}

// ── Tick — MaxPositions cap ────────────────────────────────────────────────────

func TestTick_RespectsMaxPositions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1 // open every tick
	cfg.MaxPositions = 2
	cfg.HoldBars = 100 // don't close
	s, _ := New(cfg)

	openTrades := []live.LiveTrade{
		makeTrade("t1", 5),
		makeTrade("t2", 3),
	}
	plan := s.Tick(context.Background(), price, openTrades)
	// 2 open, max=2 → no new open
	assert.Nil(t, plan.Open)
}

func TestTick_OpensWhenUnderMaxPositions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 3
	cfg.HoldBars = 100
	s, _ := New(cfg)

	openTrades := []live.LiveTrade{makeTrade("t1", 2)}
	plan := s.Tick(context.Background(), price, openTrades)
	require.NotNil(t, plan.Open)
}

// ── Tick — HoldBars close logic ────────────────────────────────────────────────

func TestTick_ClosesTradeAfterHoldBars(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldBars = 5
	cfg.TradeEvery = 100 // never open
	s, _ := New(cfg)

	openTrades := []live.LiveTrade{
		makeTrade("t1", 5),  // exactly HoldBars → close
		makeTrade("t2", 4),  // one short → keep
		makeTrade("t3", 10), // over HoldBars → close
	}
	plan := s.Tick(context.Background(), price, openTrades)
	assert.ElementsMatch(t, []string{"t1", "t3"}, plan.CloseIDs)
}

func TestTick_ClosesAndOpensInSameTick(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldBars = 3
	cfg.TradeEvery = 1
	cfg.MaxPositions = 2
	s, _ := New(cfg)

	// 2 trades open, both at HoldBars → close both → active=0 after close → open allowed
	openTrades := []live.LiveTrade{
		makeTrade("t1", 3),
		makeTrade("t2", 3),
	}
	plan := s.Tick(context.Background(), price, openTrades)
	assert.Len(t, plan.CloseIDs, 2)
	require.NotNil(t, plan.Open, "should open after closing brings active below max")
}

func TestTick_MaxPositionsAccountsForPendingCloses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldBars = 1
	cfg.TradeEvery = 1
	cfg.MaxPositions = 2
	s, _ := New(cfg)

	// 2 trades open: both hit HoldBars (close both), leaving 0 active
	// → can open (0 < 2)
	openTrades := []live.LiveTrade{
		makeTrade("t1", 1),
		makeTrade("t2", 1),
	}
	plan := s.Tick(context.Background(), price, openTrades)
	assert.Len(t, plan.CloseIDs, 2)
	require.NotNil(t, plan.Open)
}

// ── Side alternation ───────────────────────────────────────────────────────────

func TestTick_AlternateSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 10
	cfg.HoldBars = 100
	cfg.Side = "alternate"
	s, _ := New(cfg)

	sides := make([]string, 6)
	for i := range sides {
		plan := s.Tick(context.Background(), price, nil)
		if plan.Open != nil {
			sides[i] = plan.Open.Side
		}
	}
	// Should alternate long/short/long/short...
	assert.Equal(t, "long", sides[0])
	assert.Equal(t, "short", sides[1])
	assert.Equal(t, "long", sides[2])
	assert.Equal(t, "short", sides[3])
}

func TestTick_FixedLongSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 10
	cfg.HoldBars = 100
	cfg.Side = "long"
	s, _ := New(cfg)

	for i := 0; i < 4; i++ {
		plan := s.Tick(context.Background(), price, nil)
		require.NotNil(t, plan.Open)
		assert.Equal(t, "long", plan.Open.Side, "tick %d", i)
	}
}

func TestTick_FixedShortSide(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 10
	cfg.HoldBars = 100
	cfg.Side = "short"
	s, _ := New(cfg)

	plan := s.Tick(context.Background(), price, nil)
	require.NotNil(t, plan.Open)
	assert.Equal(t, "short", plan.Open.Side)
}

// ── Name ──────────────────────────────────────────────────────────────────────

func TestStrategy_Name(t *testing.T) {
	s, _ := New(DefaultConfig())
	assert.Equal(t, "pulse", s.Name())
}

// ── Reason field ───────────────────────────────────────────────────────────────

func TestTick_ReasonHold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 100
	s, _ := New(cfg)
	plan := s.Tick(context.Background(), price, nil)
	assert.Equal(t, "hold", plan.Reason)
}

func TestTick_ReasonIncludesActions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.MaxPositions = 5
	cfg.HoldBars = 1
	s, _ := New(cfg)

	plan := s.Tick(context.Background(), price, []live.LiveTrade{makeTrade("t1", 1)})
	assert.Contains(t, plan.Reason, "close")
	assert.Contains(t, plan.Reason, "open")
}

// ── TakePips propagated ────────────────────────────────────────────────────────

func TestTick_TakePipsPropagatedToOpen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TradeEvery = 1
	cfg.TakePips = 40
	s, _ := New(cfg)

	plan := s.Tick(context.Background(), price, nil)
	require.NotNil(t, plan.Open)
	assert.Equal(t, 40.0, plan.Open.TakePips)
}

// ── Table-driven open/close schedule ──────────────────────────────────────────

func TestTick_OpenSchedule(t *testing.T) {
	cases := []struct {
		tick      int
		every     int
		positions int
		max       int
		wantOpen  bool
	}{
		{1, 3, 0, 2, false}, // not due
		{3, 3, 0, 2, true},  // due, space
		{6, 3, 2, 2, false}, // due, but max hit
		{6, 3, 1, 2, true},  // due, space available
		{5, 5, 0, 1, true},  // due, space
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("tick%d_every%d_pos%d_max%d", tc.tick, tc.every, tc.positions, tc.max), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.TradeEvery = tc.every
			cfg.MaxPositions = tc.max
			cfg.HoldBars = 1000
			s, _ := New(cfg)
			s.tick = tc.tick - 1 // pre-set so next Tick() is tc.tick

			open := make([]live.LiveTrade, tc.positions)
			for i := range open {
				open[i] = makeTrade(fmt.Sprintf("t%d", i), 1)
			}
			plan := s.Tick(context.Background(), price, open)
			if tc.wantOpen {
				assert.NotNil(t, plan.Open, "expected open at tick %d", tc.tick)
			} else {
				assert.Nil(t, plan.Open, "expected no open at tick %d", tc.tick)
			}
		})
	}
}
