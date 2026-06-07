package donchianv5

import (
	"context"
	"os"
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

func candleAt(c trader.Candle, ts int64) *trader.CandleTime {
	return &trader.CandleTime{Candle: c, Timestamp: trader.Timestamp(ts)}
}

func TestV5_NewsDayBlock_BlocksEntry(t *testing.T) {
	t.Parallel()
	// 2024-01-05 (NFP) = unix day 19727
	newsDay := int64(19727)
	s, err := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockedDays:   map[int64]bool{newsDay: true},
	})
	require.NoError(t, err)
	warm(t, s, 5)

	ts := newsDay * 86400
	ct1 := candleAt(longBreak(110).Candle, ts)
	plan := s.Update(context.Background(), ct1, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "news-day-block", plan.Reason)

	ct2 := candleAt(longBreak(110).Candle, ts+3600)
	plan = s.Update(context.Background(), ct2, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "news-day-block", plan.Reason)
}

// TestV5_NewsDayBlock_StreakPreservedAcrossBlock verifies that a pending
// breakout streak is kept alive through a blocked day and fires on the next
// unblocked bar (ADX warmup bypass applies since ADX is not yet ready).
func TestV5_NewsDayBlock_StreakPreservedAcrossBlock(t *testing.T) {
	t.Parallel()
	// 19724 = 2024-01-02, 19725 = 2024-01-03 (blocked), 19726 = 2024-01-04
	blockedDay := int64(19725)
	s, err := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockedDays:   map[int64]bool{blockedDay: true},
	})
	require.NoError(t, err)
	warm(t, s, 5)

	// Bar 1 on day 19724: streak starts (pendingCount=1).
	ct1 := candleAt(longBreak(110).Candle, 19724*86400)
	plan := s.Update(context.Background(), ct1, nil)
	assert.Empty(t, plan.Opens)
	assert.Equal(t, "confirming break (1/2)", plan.Reason)

	// Bar 2 on blocked day: news-day block; streak must survive.
	ct2 := candleAt(longBreak(110).Candle, blockedDay*86400)
	plan = s.Update(context.Background(), ct2, nil)
	assert.Equal(t, "news-day-block", plan.Reason)
	assert.Equal(t, 1, s.pendingCount, "streak must be preserved on news-day block")
	assert.Equal(t, trader.Long, s.pendingSide)

	// Bar 3 on day 19726: block lifted, second confirmation fires entry.
	ct3 := candleAt(longBreak(110).Candle, 19726*86400)
	plan = s.Update(context.Background(), ct3, nil)
	require.Len(t, plan.Opens, 1, "entry must fire on first unblocked confirmation bar")
	assert.Equal(t, trader.Long, plan.Opens[0].Side)
}

func TestV5_NoBlockOnNonNewsDays(t *testing.T) {
	t.Parallel()
	// Only day 19727 is blocked; day 19728 should be tradeable.
	s, err := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockedDays:   map[int64]bool{19727: true},
	})
	require.NoError(t, err)
	warm(t, s, 5)

	ts := int64(19728) * 86400
	ct1 := candleAt(longBreak(110).Candle, ts)
	s.Update(context.Background(), ct1, nil)
	ct2 := candleAt(longBreak(110).Candle, ts+3600)
	plan := s.Update(context.Background(), ct2, nil)
	require.Len(t, plan.Opens, 1)
	assert.NotEqual(t, "news-day-block", plan.Reason)
}

func TestV5_EmptyBlockedDays_NoBlocking(t *testing.T) {
	t.Parallel()
	s, err := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		// BlockedDays nil → no blocking
	})
	require.NoError(t, err)
	require.NoError(t, err)
	warm(t, s, 5)

	ct1 := &trader.CandleTime{Candle: longBreak(110).Candle}
	s.Update(context.Background(), ct1, nil)
	ct2 := &trader.CandleTime{Candle: longBreak(110).Candle}
	plan := s.Update(context.Background(), ct2, nil)
	require.Len(t, plan.Opens, 1)
}

func TestV5_LoadNewsDays_ParsesFile(t *testing.T) {
	t.Parallel()
	content := "# comment\n2024-01-05\n2024-02-02\n\n2024-03-01   # NFP\n"
	f, err := os.CreateTemp("", "newsdays*.txt")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	days, err := LoadNewsDays(f.Name())
	require.NoError(t, err)
	require.Len(t, days, 3)

	// Verified via python: 2024-01-05=19727, 2024-02-02=19755, 2024-03-01=19783
	assert.True(t, days[19727], "2024-01-05 must be in set")
	assert.True(t, days[19755], "2024-02-02 must be in set")
	assert.True(t, days[19783], "2024-03-01 must be in set")
}

func TestV5_LoadNewsDays_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := LoadNewsDays("/nonexistent/path/news.txt")
	assert.Error(t, err)
}

func TestV5_LoadNewsDays_InvalidDate(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp("", "newsdays*.txt")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, _ = f.WriteString("not-a-date\n")
	_ = f.Close()

	_, err = LoadNewsDays(f.Name())
	assert.Error(t, err)
}

func TestV5_Reset_ClearsState(t *testing.T) {
	t.Parallel()
	s, err := New(Config{
		Period:        5,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
	})
	require.NoError(t, err)
	require.NoError(t, err)
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

func TestV5_ShortEntry(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	require.NoError(t, err)
	warm(t, s, 5)

	s.Update(context.Background(), shortBreak(90), nil)
	plan := s.Update(context.Background(), shortBreak(90), nil)
	require.Len(t, plan.Opens, 1)
	assert.Equal(t, trader.Short, plan.Opens[0].Side)
}

func TestV5_NilCandleTime_ReturnsSafely(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Period: 5, CloseStrength: 0.6, ConfirmBars: 2, ADXPeriod: 14, ADXThreshold: 25})
	require.NoError(t, err)
	require.NoError(t, err)
	plan := s.Update(context.Background(), nil, nil)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Opens)
}

func TestV5_Name_IncludesNewsDayCount(t *testing.T) {
	t.Parallel()
	s, err := New(Config{
		Period:        20,
		CloseStrength: 0.6,
		ConfirmBars:   2,
		ADXPeriod:     14,
		ADXThreshold:  25,
		BlockedDays:   map[int64]bool{19727: true, 19755: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "DONCHIAN-V5(20,cs=0.60,cb=2,adx=14/25.0,nd=2)", s.Name())
}
