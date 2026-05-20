package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDonchianBreakout_NotReadyDuringWarmup(t *testing.T) {
	t.Parallel()
	s := NewDonchianBreakout(DonchianBreakoutConfig{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 4; i++ {
		ct := &CandleTime{Candle: Candle{Open: 100, High: 110, Low: 90, Close: 105}}
		plan := s.Update(context.Background(), ct, nil)
		require.Empty(t, plan.Opens, "no entries while warming up")
	}
	assert.False(t, s.Ready())
}

func TestDonchianBreakout_LongOnUpperBreak(t *testing.T) {
	t.Parallel()
	s := NewDonchianBreakout(DonchianBreakoutConfig{Period: 5, CloseStrength: 0.6})

	// Fill window with closes inside [90, 110].
	for i := 0; i < 5; i++ {
		ct := &CandleTime{Candle: Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	require.True(t, s.Ready())

	// Breakout bar: close above 110, strong close (top of range).
	break_ := &CandleTime{Candle: Candle{Open: 110, High: 120, Low: 109, Close: 119}}
	plan := s.Update(context.Background(), break_, nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, Long, plan.Opens[0].TradeCommon.Side)
}

func TestDonchianBreakout_ShortOnLowerBreak(t *testing.T) {
	t.Parallel()
	s := NewDonchianBreakout(DonchianBreakoutConfig{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &CandleTime{Candle: Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	break_ := &CandleTime{Candle: Candle{Open: 90, High: 91, Low: 80, Close: 81}}
	plan := s.Update(context.Background(), break_, nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, Short, plan.Opens[0].TradeCommon.Side)
}

func TestDonchianBreakout_WeakCloseRejected(t *testing.T) {
	t.Parallel()
	s := NewDonchianBreakout(DonchianBreakoutConfig{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &CandleTime{Candle: Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	// Bar with break (close 111 above 110) but weak — close near the low of bar.
	weak := &CandleTime{Candle: Candle{Open: 110, High: 130, Low: 109, Close: 111}}
	plan := s.Update(context.Background(), weak, nil)
	assert.Empty(t, plan.Opens, "weak close should be rejected by confirmation filter")
	assert.Equal(t, "weak close", plan.Reason)
}

func TestDonchianBreakout_NoBreakNoEntry(t *testing.T) {
	t.Parallel()
	s := NewDonchianBreakout(DonchianBreakoutConfig{Period: 5, CloseStrength: 0.6})
	for i := 0; i < 5; i++ {
		ct := &CandleTime{Candle: Candle{Open: 100, High: 110, Low: 90, Close: 100}}
		s.Update(context.Background(), ct, nil)
	}
	// Inside-range close
	inside := &CandleTime{Candle: Candle{Open: 100, High: 109, Low: 91, Close: 105}}
	plan := s.Update(context.Background(), inside, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "no breakout", plan.Reason)
}
