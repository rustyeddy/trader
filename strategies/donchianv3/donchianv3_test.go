package donchianv3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

// warm fills the strategy with `period` neutral bars (high=110, low=90).
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
	run := &trader.Backtest{BacktestRun: &trader.BacktestRun{}}
	run.Lots = &trader.LotBook{}
	return run
}

// candleAt constructs a CandleTime with the given unix-second timestamp.
func candleAt(c trader.Candle, ts int64) *trader.CandleTime {
	return &trader.CandleTime{Candle: c, Timestamp: trader.Timestamp(ts)}
}

func TestV3_NotReadyDuringWarmup(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: true})
	for i := 0; i < 4; i++ {
		ct := &trader.CandleTime{Candle: trader.Candle{Open: 100, High: 110, Low: 90, Close: 105}}
		plan := s.Update(context.Background(), ct, nil)
		require.Empty(t, plan.Opens)
	}
	assert.False(t, s.Ready())
}

func TestV3_EntryOnTwoConfirmBars(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: false})
	warm(t, s, 5)

	s.Update(context.Background(), longBreak(110), nil)
	plan := s.Update(context.Background(), longBreak(110), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
	assert.Equal(t, "donchian-v3-breakout-up", plan.Reason)
}

func TestV3_ConfirmBarsOne_BehavesLikeV1(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 1, SameDayBlock: false})
	warm(t, s, 5)

	plan := s.Update(context.Background(), longBreak(110), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
}

// TestV3_BlockAfterStopOut verifies that when the strategy's tracked lot
// disappears from the LotBook without manualClose=true, entries are blocked
// for the rest of the same calendar day.
func TestV3_BlockAfterStopOut(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: true})
	warm(t, s, 5)

	day := int64(19723)   // 2024-01-01 UTC (unix day number)
	ts := day * 86400

	// Trigger a confirmed long entry.
	ct1 := candleAt(longBreak(110).Candle, ts)
	s.Update(context.Background(), ct1, nil)
	run := newRun()
	ct2 := candleAt(longBreak(110).Candle, ts+3600)
	plan := s.Update(context.Background(), ct2, run)
	require.Len(t, plan.Opens, 1)

	openID := s.openLotID
	require.NotEmpty(t, openID, "strategy must record the new lot ID")

	// Simulate broker fill then stop-out: add then delete from LotBook.
	run.Lots.Add(&trader.Lot{
		TradeCommon: &trader.TradeCommon{ID: openID, Side: trader.Long},
		State:       trader.LotOpen,
	})
	run.Lots.Delete(openID)

	// Next bar same day: strategy detects stop-out, blocks.
	ct3 := candleAt(longBreak(130).Candle, ts+7200)
	plan = s.Update(context.Background(), ct3, run)
	assert.Equal(t, "same-day block", plan.Reason)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, day, s.blockedDay)
}

// TestV3_NoBlockAfterManualClose verifies that when manualClose=true the
// disappearance of the tracked lot does not trigger the same-day block.
func TestV3_NoBlockAfterManualClose(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: true})
	warm(t, s, 5)

	day := int64(19723)
	ts := day * 86400

	// Directly set up the "we manually closed our lot" scenario.
	s.openLotID = "fake-lot"
	s.manualClose = true

	// LotBook is empty — fake-lot is not there. Detection fires but manualClose=true.
	run := newRun()
	ct := candleAt(longBreak(130).Candle, ts)
	// At top of Update(), openLotID="fake-lot" not found → manualClose path → no block.
	s.Update(context.Background(), ct, run)

	assert.Equal(t, int64(0), s.blockedDay, "manual close must not set blockedDay")
	assert.Empty(t, s.openLotID, "openLotID should be cleared after detection")
}

// TestV3_BlockLiftsNextDay verifies that blockedDay on day N does not prevent
// entries on day N+1.
func TestV3_BlockLiftsNextDay(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: true})
	warm(t, s, 5)

	day := int64(19723) // 2024-01-01
	ts := day * 86400

	// Inject a block directly on day N.
	s.blockedDay = day

	// Same-day bar: should be blocked.
	ct1 := candleAt(longBreak(130).Candle, ts+3600)
	plan := s.Update(context.Background(), ct1, nil)
	assert.Equal(t, "same-day block", plan.Reason)

	// Next-day bar: block should not apply.
	nextTs := (day + 1) * 86400
	ct2 := candleAt(longBreak(130).Candle, nextTs)
	plan = s.Update(context.Background(), ct2, nil)
	assert.NotEqual(t, "same-day block", plan.Reason)
}

// TestV3_SameDayBlockDisabled verifies that when SameDayBlock=false, even
// a simulated stop-out never sets the block.
func TestV3_SameDayBlockDisabled(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: false})
	warm(t, s, 5)

	day := int64(19723)
	ts := day * 86400

	// Directly set openLotID as if an entry was made; manualClose=false.
	s.openLotID = "lot-x"
	run := newRun() // lot-x not in book → looks like stop-out

	ct := candleAt(longBreak(130).Candle, ts)
	s.Update(context.Background(), ct, run)

	assert.Equal(t, int64(0), s.blockedDay, "SameDayBlock=false must never set blockedDay")
}

func TestV3_Reset_ClearsAllState(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: true})
	warm(t, s, 5)

	// Set all fields to non-zero state.
	s.blockedDay = 19723
	s.openLotID = "some-id"
	s.manualClose = true
	s.pendingSide = trader.Long
	s.pendingCount = 1
	s.pendingLevel = 110

	s.Reset()

	assert.Equal(t, int64(0), s.blockedDay)
	assert.Equal(t, "", s.openLotID)
	assert.False(t, s.manualClose)
	assert.Equal(t, 0, s.pendingCount)
	assert.Equal(t, trader.Side(0), s.pendingSide)
	assert.Equal(t, trader.Price(0), s.pendingLevel)
	assert.False(t, s.Ready())
}

func TestV3_NilCandleTime_ReturnsSafely(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: true})
	plan := s.Update(context.Background(), nil, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
}

func TestV3_ShortEntryOnTwoConfirmBars(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: false})
	warm(t, s, 5)

	s.Update(context.Background(), shortBreak(90), nil)
	plan := s.Update(context.Background(), shortBreak(90), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
	assert.Equal(t, "donchian-v3-breakout-down", plan.Reason)
}

func TestV3_ReverseClosesOppositeAndOpens(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: false})
	warm(t, s, 5)

	lot := &trader.Lot{
		TradeCommon: &trader.TradeCommon{ID: "existing-long", Side: trader.Long},
		State:       trader.LotOpen,
	}
	run := newRun()
	run.Lots.Add(lot)

	s.Update(context.Background(), shortBreak(90), nil)
	plan := s.Update(context.Background(), shortBreak(90), run)
	require.Len(t, plan.Closes, 1, "must close existing long on reversal")
	require.Len(t, plan.Opens, 1, "must open new short")
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
}

func TestV3_BlockDoesNotPreventNilRunUpdate(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, SameDayBlock: true})
	warm(t, s, 5)

	day := int64(19723)
	s.blockedDay = day

	ct := candleAt(longBreak(130).Candle, day*86400+3600)
	// Should not panic even with nil run.
	plan := s.Update(context.Background(), ct, nil)
	assert.Equal(t, "same-day block", plan.Reason)
}
