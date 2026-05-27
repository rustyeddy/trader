package donchian

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

func TestBreakout_NotReadyDuringWarmup(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 4; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 105}}
		plan := s.Update(context.Background(), ct, nil)
		require.Empty(t, plan.Opens, "no entries while warming up")
	}
	assert.False(t, s.Ready())
}

func TestBreakout_LongOnUpperBreak(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6})

	for i := 0; i < 5; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	require.True(t, s.Ready())

	break_ := &trader.CandleTime{Candle: trader.Candle{Open: 110, High: 120, Low: 109, Close: 119}}
	plan := s.Update(context.Background(), break_, nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].TradeCommon.Side)
}

func TestBreakout_ShortOnLowerBreak(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	break_ := &trader.CandleTime{Candle: trader.Candle{Open: 90, High: 91, Low: 80, Close: 81}}
	plan := s.Update(context.Background(), break_, nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].TradeCommon.Side)
}

func TestBreakout_WeakCloseRejected(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	weak := &trader.CandleTime{Candle: trader.Candle{Open: 110, High: 130, Low: 109, Close: 111}}
	plan := s.Update(context.Background(), weak, nil)
	assert.Empty(t, plan.Opens, "weak close should be rejected by confirmation filter")
	assert.Equal(t, "weak close", plan.Reason)
}

func TestBreakout_NoBreakNoEntry(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	inside := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 109, Low: 91, Close: 105}}
	plan := s.Update(context.Background(), inside, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "no breakout", plan.Reason)
}

// makeOpenLot returns a minimal Backtest with one open lot on the given side,
// used to simulate an already-open position when calling emitOpen.
func makeOpenLot(side trader.Side) *trader.Backtest {
	lot := &trader.Lot{
		TradeCommon: &trader.TradeCommon{ID: "test-lot", Side: side},
		State:       trader.LotOpen,
	}
	run := &trader.Backtest{BacktestRun: &trader.BacktestRun{}}
	run.Lots = &trader.LotBook{}
	run.Lots.Add(lot)
	return run
}

func TestBreakout_NoSecondEntryInSameDirection(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}

	// Simulate a long already open; a second breakout bar should not add another.
	run := makeOpenLot(trader.Long)
	break2 := &trader.CandleTime{Candle: trader.Candle{Open: 110, High: 125, Low: 109, Close: 124}}
	plan := s.Update(context.Background(), break2, run)
	assert.Empty(t, plan.Opens, "should not stack a second long on top of existing long")
	assert.Empty(t, plan.Closes, "should not close the existing long either")
}

func TestBreakout_ReverseClosesOppositeAndOpens(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}

	// Long open, then a short breakout: should close the long and open a short.
	run := makeOpenLot(trader.Long)
	rev := &trader.CandleTime{Candle: trader.Candle{Open: 90, High: 91, Low: 78, Close: 79}}
	plan := s.Update(context.Background(), rev, run)
	require.Len(t, plan.Closes, 1, "should close existing long on reversal")
	require.Len(t, plan.Opens, 1, "should open new short on reversal")
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
}
