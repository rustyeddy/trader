package donchianv2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

// warm fills the strategy with `period` neutral bars (high=110, low=90) and
// asserts the strategy reports Ready.
func warm(t *testing.T, s *Breakout, period int) {
	t.Helper()
	for i := 0; i < period; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		plan := s.Update(context.Background(), ct, nil)
		require.Empty(t, plan.Opens)
	}
	require.True(t, s.Ready())
}

// longBreak returns a strong long breakout bar: close clearly above `above`
// with close strength well above 0.6 (close in top 95% of bar range).
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

// shortBreak returns a strong short breakout bar: close clearly below `below`.
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

func TestBreakout_NotReadyDuringWarmup(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	for i := 0; i < 4; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 105}}
		plan := s.Update(context.Background(), ct, nil)
		require.Empty(t, plan.Opens)
	}
	assert.False(t, s.Ready())
}

func TestBreakout_NoEntryOnFirstBreakBar(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5) // channel hi=110, lo=90

	// Bar 1 closes above 110 — streak starts, no entry yet.
	plan := s.Update(context.Background(), longBreak(110), nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "confirming break (1/2)", plan.Reason)
	assert.Equal(t, 1, s.pendingCount)
}

func TestBreakout_EntryOnSecondConsecutiveBreakBar(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5) // channel hi=110

	// Bar 1: streak starts, pendingLevel=110.
	s.Update(context.Background(), longBreak(110), nil)
	// Bar 2: close still above pendingLevel=110 → confirmed, entry.
	// (The live channel hi is now 130 from bar 1, but we compare against 110.)
	plan := s.Update(context.Background(), longBreak(110), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
	assert.Equal(t, "donchian-v2-breakout-up", plan.Reason)
}

func TestBreakout_StreakResetByInsideBar(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5) // channel hi=110

	// Bar 1: pendingLevel=110, streak starts.
	s.Update(context.Background(), longBreak(110), nil)

	// Inside bar: close=105 is not above pendingLevel=110 → streak reset.
	inside := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 109, Low: 91, Close: 105}}
	plan := s.Update(context.Background(), inside, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "no breakout", plan.Reason)
	assert.Equal(t, 0, s.pendingCount)

	// Channel now includes bar1's high (130), so the new streak needs close>130.
	// longBreak(130) = {H=150, close=149} which clearly exceeds 130.
	plan = s.Update(context.Background(), longBreak(130), nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "confirming break (1/2)", plan.Reason)
}

func TestBreakout_StreakResetByWeakFirstBar(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5)

	// Closes above channel but weak (close near bottom of bar range).
	weak := &trader.CandleTime{
		Candle: trader.Candle{Open: 110, High: 130, Low: 109, Close: 111},
	}
	plan := s.Update(context.Background(), weak, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "weak close", plan.Reason)
	assert.Equal(t, 0, s.pendingCount)
}

func TestBreakout_ShortEntryOnTwoConsecutiveBreaks(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5) // channel lo=90

	// Bar 1: pendingLevel=90.
	s.Update(context.Background(), shortBreak(90), nil)
	// Bar 2: close still below pendingLevel=90 (71<90) → confirmed, entry.
	plan := s.Update(context.Background(), shortBreak(90), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
	assert.Equal(t, "donchian-v2-breakout-down", plan.Reason)
}

func TestBreakout_ConfirmBarsOne_BehavesLikeV1(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 1})
	require.NoError(t, err)
	warm(t, s, 5)

	// With confirm_bars=1 the first strong breakout bar should enter immediately.
	plan := s.Update(context.Background(), longBreak(110), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
}

func TestBreakout_DirectionFlipResetsStreak(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5) // channel hi=110, lo=90

	// Long streak starts.
	s.Update(context.Background(), longBreak(110), nil)
	assert.Equal(t, trader.Long, s.pendingSide)
	assert.Equal(t, 1, s.pendingCount)

	// Short breakout kills the long streak and starts a short one.
	plan := s.Update(context.Background(), shortBreak(90), nil)
	assert.Empty(t, plan.Opens, "need 2 bars, only 1 short bar so far")
	assert.Equal(t, trader.Short, s.pendingSide)
	assert.Equal(t, 1, s.pendingCount)
}

func TestBreakout_NoSecondEntryInSameDirection(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5)

	lot := &trader.Lot{
		TradeCommon: &trader.TradeCommon{ID: "test-lot", Side: trader.Long},
		State:       trader.LotOpen,
	}
	run := &trader.Backtest{State: &trader.BacktestRun{}}
	run.State.Lots = &trader.LotBook{}
	run.State.Lots.Add(lot)

	s.Update(context.Background(), longBreak(110), nil)         // bar 1
	plan := s.Update(context.Background(), longBreak(110), run) // bar 2 — confirm
	assert.Empty(t, plan.Opens, "should not stack long on existing long")
	assert.Empty(t, plan.Closes, "should not close existing long")
}

func TestBreakout_ReverseClosesOppositeAndOpens(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5)

	lot := &trader.Lot{
		TradeCommon: &trader.TradeCommon{ID: "test-lot", Side: trader.Long},
		State:       trader.LotOpen,
	}
	run := &trader.Backtest{State: &trader.BacktestRun{}}
	run.State.Lots = &trader.LotBook{}
	run.State.Lots.Add(lot)

	// Two consecutive short bars with a long position open → close long, open short.
	s.Update(context.Background(), shortBreak(90), nil)         // bar 1, no entry
	plan := s.Update(context.Background(), shortBreak(90), run) // bar 2, entry
	require.Len(t, plan.Closes, 1, "should close existing long on confirmed reversal")
	require.Len(t, plan.Opens, 1, "should open new short")
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
}

func TestBreakout_Reset(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2})
	require.NoError(t, err)
	warm(t, s, 5)
	s.Update(context.Background(), longBreak(110), nil) // start streak

	s.Reset()
	assert.Equal(t, 0, s.pendingCount)
	assert.Equal(t, trader.Side(0), s.pendingSide)
	assert.Equal(t, trader.Price(0), s.pendingLevel)
	assert.False(t, s.Ready())
}

func TestBreakout_ThreeConfirmBars(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 3})
	require.NoError(t, err)
	warm(t, s, 5) // channel hi=110

	// All three bars close above pendingLevel=110.
	s.Update(context.Background(), longBreak(110), nil)         // bar 1
	plan := s.Update(context.Background(), longBreak(110), nil) // bar 2
	assert.Empty(t, plan.Opens, "need 3 bars, only 2 confirmed")
	plan = s.Update(context.Background(), longBreak(110), nil) // bar 3
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
}
