package donchianv6

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
)

// Unix day epoch: 1970-01-01 = Thursday.
// 2024-01-01 = Monday = unix day 19723  (19723 % 7 == 4)
// 2024-01-02 = Tuesday = 19724
// 2024-01-05 = Friday  = 19727  (19727 % 7 == 1)

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

func candleAt(c trader.Candle, ts int64) *trader.CandleTime {
	return &trader.CandleTime{Candle: c, Timestamp: trader.Timestamp(ts)}
}

func TestV6_MondayBlock_BlocksEntryOnMonday(t *testing.T) {
	t.Parallel()
	s := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockMonday:   true,
	})
	warm(t, s, 5)

	// 2024-01-01 = Monday = unix day 19723
	monday := int64(19723)
	require.Equal(t, int64(4), monday%7, "sanity: 19723 must be a Monday")

	ts := monday * 86400
	ct1 := candleAt(longBreak(110).Candle, ts)
	plan := s.Update(context.Background(), ct1, nil)
	assert.Equal(t, "monday-block", plan.Reason)
	assert.Empty(t, plan.Opens)

	ct2 := candleAt(longBreak(110).Candle, ts+3600)
	plan = s.Update(context.Background(), ct2, nil)
	assert.Equal(t, "monday-block", plan.Reason)
}

func TestV6_MondayBlock_AllowsEntryOnTuesday(t *testing.T) {
	t.Parallel()
	s := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockMonday:   true,
	})
	warm(t, s, 5)

	// 2024-01-02 = Tuesday = unix day 19724
	tuesday := int64(19724)
	require.Equal(t, int64(5), tuesday%7, "sanity: 19724 must be a Tuesday")

	ts := tuesday * 86400
	ct1 := candleAt(longBreak(110).Candle, ts)
	s.Update(context.Background(), ct1, nil)
	ct2 := candleAt(longBreak(110).Candle, ts+3600)
	plan := s.Update(context.Background(), ct2, nil)

	// ADX not ready → gate bypassed; entry fires on second confirm bar.
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, "donchian-v6-breakout-up", plan.Reason)
}

func TestV6_MondayBlock_StreakPreservedAcrossMonday(t *testing.T) {
	t.Parallel()
	// Friday streak starts → Monday blocked → Tuesday fires.
	// 2024-01-05 = Friday = day 19727, 2024-01-08 = Monday = 19730, 2024-01-09 = Tuesday = 19731
	friday := int64(19727)
	monday := int64(19730)
	tuesday := int64(19731)
	require.Equal(t, int64(1), friday%7, "sanity: 19727 is Friday")
	require.Equal(t, int64(4), monday%7, "sanity: 19730 is Monday")

	s := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockMonday:   true,
	})
	warm(t, s, 5)

	// Bar 1 on Friday: streak starts.
	ct1 := candleAt(longBreak(110).Candle, friday*86400)
	plan := s.Update(context.Background(), ct1, nil)
	assert.Equal(t, "confirming break (1/2)", plan.Reason)
	assert.Equal(t, 1, s.pendingCount)

	// Monday: blocked; streak must survive.
	ct2 := candleAt(longBreak(110).Candle, monday*86400)
	plan = s.Update(context.Background(), ct2, nil)
	assert.Equal(t, "monday-block", plan.Reason)
	assert.Equal(t, 1, s.pendingCount, "streak must survive monday block")
	assert.Equal(t, trader.Long, s.pendingSide)

	// Tuesday: block lifted, second confirmation fires entry.
	ct3 := candleAt(longBreak(110).Candle, tuesday*86400)
	plan = s.Update(context.Background(), ct3, nil)
	require.Len(t, plan.Opens, 1, "entry must fire on Tuesday after weekend skip")
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
}

func TestV6_MondayBlockDisabled_AllowsEntryOnMonday(t *testing.T) {
	t.Parallel()
	s := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockMonday:   false,
	})
	warm(t, s, 5)

	monday := int64(19723)
	ts := monday * 86400
	ct1 := candleAt(longBreak(110).Candle, ts)
	s.Update(context.Background(), ct1, nil)
	ct2 := candleAt(longBreak(110).Candle, ts+3600)
	plan := s.Update(context.Background(), ct2, nil)
	require.Len(t, plan.Opens, 1, "monday block disabled: entry must fire")
}

func TestV6_FridayBlock_BlocksEntryOnFriday(t *testing.T) {
	t.Parallel()
	s := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockFriday:   true,
	})
	warm(t, s, 5)

	// 2024-01-05 = Friday = unix day 19727
	friday := int64(19727)
	require.Equal(t, int64(1), friday%7, "sanity: 19727 is Friday")

	ct1 := candleAt(longBreak(110).Candle, friday*86400)
	plan := s.Update(context.Background(), ct1, nil)
	assert.Equal(t, "friday-block", plan.Reason)
	assert.Empty(t, plan.Opens)
}

func TestV6_NewsDayBlock_StillWorksInV6(t *testing.T) {
	t.Parallel()
	// 2024-01-11 = Thursday = day 19733 (CPI day)
	cpiDay := int64(19733)
	require.Equal(t, int64(0), cpiDay%7, "sanity: 19733 is Thursday")
	s := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockMonday:   true,
		BlockedDays:   map[int64]bool{cpiDay: true},
	})
	warm(t, s, 5)

	ct := candleAt(longBreak(110).Candle, cpiDay*86400)
	plan := s.Update(context.Background(), ct, nil)
	assert.Equal(t, "news-day-block", plan.Reason)
}

func TestV6_Reset_ClearsState(t *testing.T) {
	t.Parallel()
	s := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockMonday:   true,
	})
	warm(t, s, 5)
	s.pendingSide = trader.Long
	s.pendingCount = 2
	s.pendingLevel = 110

	s.Reset()
	assert.Equal(t, 0, s.pendingCount)
	assert.Equal(t, trader.Side(0), s.pendingSide)
	assert.Equal(t, trader.Price(0), s.pendingLevel)
	assert.False(t, s.adx.Ready())
	assert.False(t, s.Ready())
}

func TestV6_ShortEntry(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	warm(t, s, 5)

	s.Update(context.Background(), shortBreak(90), nil)
	plan := s.Update(context.Background(), shortBreak(90), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
}

func TestV6_NilCandleTime_ReturnsSafely(t *testing.T) {
	t.Parallel()
	s := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	plan := s.Update(context.Background(), nil, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
}
