package datamanager

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// TestSlotIndexForTime_H4MatchesSlotBoundaries_AcrossManyDays is the
// regression case for a bug found while validating #179's regen:
// SlotIndexForTime's per-day increment for H4 used floor(dayWidth/step),
// undercounting by one on the 23-hour spring-forward day (floor(23h/4h)=5),
// while SlotBoundaries' emission loop (a slot counts if it starts before
// the day ends, even if it doesn't finish before the day ends) actually
// emits 6 slots for that same day. The mismatch permanently shifted every
// later H4 index in the file by -1 relative to SlotBoundaries, causing
// DeriveCanonicalFromRaw to misplace every raw row after a March DST
// transition — silently dropping one real candle and colliding two
// different slots onto the same timestamp.
//
// This walks 400 days (more than a year, crossing both the March
// spring-forward and November fall-back transitions) and asserts
// SlotIndexForTime(start, tf, SlotBoundaries(start, tf, n)[i]) == i for
// every single slot — i.e. the two functions must stay exact inverses of
// each other for as long as the underlying data might span.
func TestSlotIndexForTime_MatchesSlotBoundaries_AcrossManyDays(t *testing.T) {
	t.Parallel()

	for _, tf := range []types.Timeframe{types.D1, types.H4} {
		start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		n := 400
		if tf == types.H4 {
			n = 400 * 6
		}
		boundaries := SlotBoundaries(start, tf, n)

		for i, b := range boundaries {
			got := SlotIndexForTime(start, tf, b)
			require.Equal(t, i, got, "tf=%v slot %d (%s): SlotIndexForTime must invert SlotBoundaries", tf, i, b)
		}
	}
}

// TestH4Slots_NYLocalWallClockRule_DSTTransitions pins the H4 grid rule
// discovered while investigating #182: OANDA H4 candles open at fixed
// America/New_York WALL-CLOCK hours (1/5/9/13/17/21:00 local), so the UTC
// phase switches at the DST transition instant (2am local) — mid-session —
// not at the next 17:00 session boundary. The expected times below are
// transcribed from real raw OANDA archive rows (USDJPY 2011-03,
// AUDCAD 2007-03, EURGBP 2011-11), not computed from the code under test,
// so this test fails if the implementation regresses to fixed 4h-UTC
// strides from the session open (which agreed with these times on normal
// days but drifted an hour after each transition instant).
func TestH4Slots_NYLocalWallClockRule_DSTTransitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		monthStart time.Time
		want       []time.Time // consecutive run of expected slot opens
	}{
		{
			// US spring-forward 2011-03-13 02:00 EST (07:00 UTC).
			// Session opened Sat 22:00 UTC (17:00 EST); pre-transition
			// slots on the EST phase (02:00, 06:00 UTC), post-transition
			// on the EDT phase (09:00, 13:00, 17:00 UTC) — the 06:00
			// slot is 3 real hours wide. Matches raw USDJPY 2011-03.
			name:       "spring-forward 2011",
			monthStart: time.Date(2011, 3, 1, 0, 0, 0, 0, time.UTC),
			want: []time.Time{
				time.Date(2011, 3, 13, 2, 0, 0, 0, time.UTC),
				time.Date(2011, 3, 13, 6, 0, 0, 0, time.UTC),
				time.Date(2011, 3, 13, 9, 0, 0, 0, time.UTC),
				time.Date(2011, 3, 13, 13, 0, 0, 0, time.UTC),
				time.Date(2011, 3, 13, 17, 0, 0, 0, time.UTC),
				time.Date(2011, 3, 13, 21, 0, 0, 0, time.UTC),
			},
		},
		{
			// US spring-forward 2007-03-11. Post-transition EDT-phase
			// slots 17:00, 21:00 UTC match raw AUDCAD 2007-03.
			name:       "spring-forward 2007",
			monthStart: time.Date(2007, 3, 1, 0, 0, 0, 0, time.UTC),
			want: []time.Time{
				time.Date(2007, 3, 11, 17, 0, 0, 0, time.UTC),
				time.Date(2007, 3, 11, 21, 0, 0, 0, time.UTC),
				time.Date(2007, 3, 12, 1, 0, 0, 0, time.UTC),
			},
		},
		{
			// US fall-back 2011-11-06 02:00 EDT (06:00 UTC). Local 1:00
			// occurs twice (05:00 and 06:00 UTC); only the first opens a
			// candle and that slot is 5 real hours wide (05:00→10:00 UTC).
			// Post-transition EST-phase slots 10:00, 14:00, 18:00 UTC —
			// 14:00 and 18:00 match raw EURGBP 2011-11.
			name:       "fall-back 2011",
			monthStart: time.Date(2011, 11, 1, 0, 0, 0, 0, time.UTC),
			want: []time.Time{
				time.Date(2011, 11, 6, 1, 0, 0, 0, time.UTC),
				time.Date(2011, 11, 6, 5, 0, 0, 0, time.UTC),
				time.Date(2011, 11, 6, 10, 0, 0, 0, time.UTC),
				time.Date(2011, 11, 6, 14, 0, 0, 0, time.UTC),
				time.Date(2011, 11, 6, 18, 0, 0, 0, time.UTC),
				time.Date(2011, 11, 6, 22, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			monthEnd := tc.monthStart.AddDate(0, 1, 0)
			got := MonthSlotBoundaries(tc.monthStart, monthEnd, types.H4)

			start := -1
			for i, b := range got {
				if b.Equal(tc.want[0]) {
					start = i
					break
				}
			}
			require.GreaterOrEqual(t, start, 0, "first expected slot %s not found in grid", tc.want[0])
			require.LessOrEqual(t, start+len(tc.want), len(got))
			for i, w := range tc.want {
				require.Equal(t, w, got[start+i], "slot %d after %s", i, tc.want[0])
			}
		})
	}
}

// TestMonthSlotBoundaries_H4IncludesEarlySessionSlots is the regression
// case for the H4 month-start data-loss bug found while independently
// validating #179's regen with a Python oracle that reads the raw tree
// directly: OANDA's trading day opens at 17:00 America/New_York (~21:00 or
// 22:00 UTC), so a session is almost always already in progress at UTC
// midnight on the 1st. SlotBoundaries(monthStart, tf, n) anchors to the
// first boundary AT OR AFTER monthStart, silently skipping that
// in-progress session's early H4 sub-slots entirely — not written, not
// even reported as missing, because DeriveCanonicalFromRaw's reporting
// loop iterated the same incomplete boundary set. Those sub-slots' raw
// data lives ONLY in this month's own raw file (OANDA's date-scoped fetch
// means the prior month's raw fetch never reaches a timestamp in this
// month), so the data was permanently lost with zero error surfaced
// anywhere in the Go tooling.
//
// July 2026 is EDT (UTC-4): the session already in progress at July 1
// 00:00 UTC opened June 30 21:00 UTC, so its remaining sub-slots within
// July are 01:00, 05:00, 09:00, 13:00, 17:00 UTC, before the next full
// session opens at July 1 21:00 UTC. These expected times are computed
// independently of MonthSlotBoundaries (not just re-deriving its own
// output) so this test can actually catch a regression in it.
func TestMonthSlotBoundaries_H4IncludesEarlySessionSlots(t *testing.T) {
	t.Parallel()

	monthStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	want := []time.Time{
		time.Date(2026, time.July, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 5, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 13, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 17, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 21, 0, 0, 0, time.UTC),
	}

	got := MonthSlotBoundaries(monthStart, monthEnd, types.H4)
	require.GreaterOrEqual(t, len(got), len(want))
	require.Equal(t, want, got[:len(want)])

	// Every returned slot must genuinely belong to July.
	for i, b := range got {
		require.False(t, b.Before(monthStart), "slot %d %s before monthStart", i, b)
		require.True(t, b.Before(monthEnd), "slot %d %s not before monthEnd", i, b)
	}
}

// TestDeriveCanonicalFromRaw_H4WritesEarlySessionSlots is the end-to-end
// regression test for the same bug via the real production path: raw rows
// for the early sub-slots of a session already in progress at monthStart
// must be written to the canonical file, not silently dropped.
func TestDeriveCanonicalFromRaw_H4WritesEarlySessionSlots(t *testing.T) {
	rawDir := t.TempDir()
	UseTempDataDir(t)

	key := Key{
		Kind:       KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         types.H4,
		Year:       2026,
		Month:      7,
	}

	monthStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	earlySlots := []time.Time{
		time.Date(2026, time.July, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 5, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 13, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 17, 0, 0, 0, time.UTC),
	}

	var rows []RawCandleRow
	for _, ts := range earlySlots {
		rows = append(rows, RawCandleRow{
			Time:    ts,
			BidOpen: 1.1000, BidHigh: 1.1010, BidLow: 1.0990, BidClose: 1.1005,
			AskOpen: 1.1002, AskHigh: 1.1012, AskLow: 1.0992, AskClose: 1.1007,
			Volume:   100,
			Complete: true,
		})
	}
	require.NoError(t, writeRawMonth(rawDir, key, monthStart, rows))
	rawPath := monthlyCandle(rawDir, key)

	dm := NewDataManager([]string{"EURUSD"}, monthStart, monthEnd)
	result, err := dm.DeriveCanonicalFromRaw(context.Background(), rawPath, key)
	require.NoError(t, err)
	require.Equal(t, len(earlySlots), result.CandlesWritten,
		"every early-session raw row must be written, not silently dropped")

	cs, err := getStore().ReadCSV(key)
	require.NoError(t, err)
	found := 0
	for i := 0; i < len(cs.Candles); i++ {
		if !cs.IsValid(i) {
			continue
		}
		ct := cs.Time(i)
		for _, ts := range earlySlots {
			if ct.Equal(ts) {
				found++
			}
		}
	}
	require.Equal(t, len(earlySlots), found, "every early-session slot must be present and valid in the canonical file")
}

// TestDeriveCanonicalFromRaw_H4SpringForwardNoCollision is an end-to-end
// regression test for the same bug via the real production path: a raw
// H4 month spanning the March 2020 spring-forward transition must derive
// with every real candle in its own slot, no collisions, and no
// off-by-one loss of a real trading day's candle.
func TestDeriveCanonicalFromRaw_H4SpringForwardNoCollision(t *testing.T) {
	rawDir := t.TempDir()
	UseTempDataDir(t)

	key := Key{
		Kind:       KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         types.H4,
		Year:       2020,
		Month:      3,
	}

	monthStart := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	// MonthSlotBoundaries, not SlotBoundaries(monthStart, tf, n): every
	// entry is already scoped to [monthStart, monthEnd), including any
	// early sub-slots of a session in progress at monthStart, matching
	// exactly what DeriveCanonicalFromRaw now writes.
	boundaries := MonthSlotBoundaries(monthStart, monthEnd, types.H4)

	var rows []RawCandleRow
	for _, b := range boundaries {
		rows = append(rows, RawCandleRow{
			Time:    b,
			BidOpen: 1.1000, BidHigh: 1.1010, BidLow: 1.0990, BidClose: 1.1005,
			AskOpen: 1.1002, AskHigh: 1.1012, AskLow: 1.0992, AskClose: 1.1007,
			Volume:   100,
			Complete: true,
		})
	}
	require.NoError(t, writeRawMonth(rawDir, key, monthStart, rows))
	rawPath := monthlyCandle(rawDir, key)

	dm := NewDataManager([]string{"EURUSD"}, monthStart, monthEnd)
	_, err := dm.DeriveCanonicalFromRaw(context.Background(), rawPath, key)
	require.NoError(t, err)

	cs, err := getStore().ReadCSV(key)
	require.NoError(t, err)
	require.Equal(t, len(boundaries), cs.CountValid(), "every slot with a raw row must land in its own slot, no collisions or losses")
	for i := range boundaries {
		require.True(t, cs.IsValid(i), "slot %d", i)
		require.Equal(t, boundaries[i], cs.Time(i), "slot %d", i)
	}
}
