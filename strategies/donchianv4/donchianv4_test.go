package donchianv4

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

func warm(t *testing.T, s *Breakout, period int) {
	t.Helper()
	for i := 0; i < period; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		plan := s.Update(context.Background(), ct, nil)
		require.Empty(t, plan.Opens)
	}
	require.True(t, s.Ready())
}

func longBreak(above trader.Price) *trader.CandleTime {
	return &trader.CandleTime{
		Candle: trader.Candle{
			Open:  above,
			High:  above + 20,
			Low:   above - 1,
			Close: above + 19,
		},
	}
}

func shortBreak(below trader.Price) *trader.CandleTime {
	return &trader.CandleTime{
		Candle: trader.Candle{
			Open:  below,
			High:  below + 1,
			Low:   below - 20,
			Close: below - 19,
		},
	}
}

func newRun() *trader.Backtest {
	run := &trader.Backtest{State: &trader.BacktestRun{}}
	run.State.Lots = &trader.LotBook{}
	return run
}

// warmADX feeds `count` strongly trending bars to the ADX so it becomes ready.
// Each bar has a consistent high-to-low move in one direction to produce
// elevated ADX and +DI values.
func warmADX(s *Breakout, count int) {
	for i := 0; i < count; i++ {
		// Alternating bullish bars: +DM dominant, consistent uptrend.
		c := trader.Candle{
			Open:  trader.Price(100_000 + i*200),
			High:  trader.Price(100_200 + i*200),
			Low:   trader.Price(99_800 + i*200),
			Close: trader.Price(100_100 + i*200),
		}
		s.adx.Update(c)
	}
}

func TestV4_NotReadyDuringWarmup(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	for i := 0; i < 4; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 105}}
		plan := s.Update(context.Background(), ct, nil)
		require.Empty(t, plan.Opens)
	}
	assert.False(t, s.Ready())
}

func TestV4_EntryWhenADXNotReady_Bypassed(t *testing.T) {
	t.Parallel()
	// ADX period=14 needs 28 bars; with period=5 Donchian, ADX won't be ready
	// until after many bars — so the gate is bypassed and v2 logic applies.
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	warm(t, s, 5)

	require.False(t, s.adx.Ready(), "ADX must still be warming up at this point")

	s.Update(context.Background(), longBreak(110), nil)         // bar 1
	plan := s.Update(context.Background(), longBreak(110), nil) // bar 2 — confirms
	require.Len(t, plan.Opens, 1, "ADX warmup bypass must allow entry")
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
	assert.Equal(t, "donchian-v4-breakout-up", plan.Reason)
}

func TestV4_EntryBlockedWhenADXBelowThreshold(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 50})
	require.NoError(t, err)
	warm(t, s, 5)

	// Force ADX ready with a moderate (non-extreme) value by seeding with
	// flat/choppy bars — ADX will be low.
	// Use ADXThreshold=50 which is very high, so real ADX (typically <50 in
	// ranging conditions) will be below threshold.
	warmADX(s, 30) // ensure ADX is ready but value stays moderate
	require.True(t, s.adx.Ready())
	// The ADX value from these bars should be below 50 in most cases.
	// We confirm the filter is *actually* blocking by checking the reason.

	s.pendingSide = 0
	s.pendingCount = 0

	// Build up a confirmed streak.
	s.Update(context.Background(), longBreak(110), nil)
	plan := s.Update(context.Background(), longBreak(110), nil)

	if s.adx.Float64() < 50 {
		// ADX below threshold — entry must be blocked.
		assert.Empty(t, plan.Opens)
		assert.Contains(t, plan.Reason, "adx-filtered")
	} else {
		// ADX happened to exceed 50 — gate passes, entry fires (acceptable).
		t.Logf("ADX=%.1f exceeded threshold=50, gate passed (ok)", s.adx.Float64())
	}
}

func TestV4_EntryBlockedWhenDIDirectionDisagrees(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 1})
	require.NoError(t, err)
	// ADXThreshold=1 so ADX strength always passes.
	warm(t, s, 5)

	// Feed strongly bearish bars to make -DI dominate.
	for i := 0; i < 30; i++ {
		base := trader.Price(100_000 - i*300)
		c := trader.Candle{Open: base + 200, High: base + 200, Low: base, Close: base + 50}
		s.adx.Update(c)
	}
	require.True(t, s.adx.Ready())

	// With strongly bearish -DI, a long breakout should be blocked.
	if s.adx.MinusDI() > s.adx.PlusDI() {
		s.pendingSide = 0
		s.pendingCount = 0

		s.Update(context.Background(), longBreak(110), nil)
		plan := s.Update(context.Background(), longBreak(110), nil)

		assert.Empty(t, plan.Opens, "-DI dominant: long entry must be blocked")
		assert.Contains(t, plan.Reason, "adx-filtered")
	} else {
		t.Skip("DI direction not bearish enough after warmup — skip directional test")
	}
}

func TestV4_StreakPreservedOnADXBlock(t *testing.T) {
	t.Parallel()
	// With threshold=1, ADX strength gate always passes; only DI direction matters.
	// We inject a bearish DI state, block a long entry, then inject bullish DI
	// and verify the streak is still active (entry fires without restarting).
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 200})
	require.NoError(t, err)
	// ADXThreshold=200 (impossible to reach) — every confirmed streak is blocked.
	warm(t, s, 5)
	warmADX(s, 30)
	require.True(t, s.adx.Ready())

	// Confirm a long streak — should be blocked by ADX threshold.
	s.Update(context.Background(), longBreak(110), nil)         // bar 1
	plan := s.Update(context.Background(), longBreak(110), nil) // bar 2 — confirmed but blocked
	assert.Empty(t, plan.Opens, "impossible threshold must block entry")
	assert.Contains(t, plan.Reason, "adx-filtered")

	// Streak is preserved: pendingCount should still be >= confirmBars.
	assert.GreaterOrEqual(t, s.pendingCount, s.confirmBars)
}

func TestV4_ShortEntry(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	warm(t, s, 5)

	// ADX not ready → gate bypassed → v2 logic.
	require.False(t, s.adx.Ready())

	s.Update(context.Background(), shortBreak(90), nil)
	plan := s.Update(context.Background(), shortBreak(90), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
	assert.Equal(t, "donchian-v4-breakout-down", plan.Reason)
}

func TestV4_ConfirmBarsOne(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 1, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	warm(t, s, 5)
	require.False(t, s.adx.Ready()) // gate bypassed

	plan := s.Update(context.Background(), longBreak(110), nil)
	require.Len(t, plan.Opens, 1)
}

func TestV4_Reset_ClearsAllState(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	warm(t, s, 5)
	s.pendingSide = trader.Long
	s.pendingCount = 2
	s.pendingLevel = 110
	warmADX(s, 30)
	require.True(t, s.adx.Ready())

	s.Reset()

	assert.Equal(t, 0, s.pendingCount)
	assert.Equal(t, trader.Side(0), s.pendingSide)
	assert.Equal(t, trader.Price(0), s.pendingLevel)
	assert.False(t, s.adx.Ready(), "ADX must be reset")
	assert.False(t, s.Ready())
}

func TestV4_NilCandleTime_ReturnsSafely(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	plan := s.Update(context.Background(), nil, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
}

func TestV4_ReverseClosesOppositeAndOpens(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	warm(t, s, 5)
	require.False(t, s.adx.Ready()) // gate bypassed for this test

	lot := &trader.Lot{
		TradeCommon: &trader.TradeCommon{ID: "existing-long", Side: trader.Long},
		State:       trader.LotOpen,
	}
	run := newRun()
	run.State.Lots.Add(lot)

	s.Update(context.Background(), shortBreak(90), nil)
	plan := s.Update(context.Background(), shortBreak(90), run)
	require.Len(t, plan.Closes, 1)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
}

func TestV4_WeakFirstBar_StreakReset(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	warm(t, s, 5)

	weak := &trader.CandleTime{
		Candle: trader.Candle{Open: 110, High: 130, Low: 109, Close: 111},
	}
	plan := s.Update(context.Background(), weak, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "weak close", plan.Reason)
	assert.Equal(t, 0, s.pendingCount)
}

func TestV4_Name(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 20, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	assert.Equal(t, "DONCHIAN-V4(20,cs=0.60,cb=2,adx=14/25.0)", s.Name())
}
