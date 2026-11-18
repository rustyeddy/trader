package canonical

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const holesHeader = "source,instrument,interval,start,end\n"

func TestParseHoles_ReadsRunsSkippingCommentsAndBlankLines(t *testing.T) {
	data := "# a comment\n\n" + holesHeader +
		"oanda,EURUSD,d1,2008-10-13T21:00:00Z,2008-10-14T21:00:00Z\n" +
		"oanda,EURUSD,h1,2013-07-23T21:00:00Z,2013-07-23T22:00:00Z\n" +
		"oanda,EURUSD,h1,2014-02-27T15:00:00Z,2014-02-27T16:00:00Z\n"

	got, err := parseHoles([]byte(data))

	require.NoError(t, err)
	assert.Len(t, got[holeKey{"oanda", "EURUSD", H1}], 2)
	assert.Len(t, got[holeKey{"oanda", "EURUSD", D1}], 1)
}

func TestParseHoles_IsStrictBecauseAWrongEntryWouldExcuseARealGap(t *testing.T) {
	tests := map[string]string{
		"no header":           "oanda,EURUSD,h1,2013-07-23T21:00:00Z,2013-07-23T22:00:00Z\n",
		"a wrong header":      "source,symbol,interval,start,end\n",
		"too few fields":      holesHeader + "oanda,EURUSD,h1,2013-07-23T21:00:00Z\n",
		"too many fields":     holesHeader + "oanda,EURUSD,h1,2013-07-23T21:00:00Z,2013-07-23T22:00:00Z,x\n",
		"an empty field":      holesHeader + "oanda,,h1,2013-07-23T21:00:00Z,2013-07-23T22:00:00Z\n",
		"a padded field":      holesHeader + "oanda, EURUSD,h1,2013-07-23T21:00:00Z,2013-07-23T22:00:00Z\n",
		"an unknown interval": holesHeader + "oanda,EURUSD,h4,2013-07-23T20:00:00Z,2013-07-23T24:00:00Z\n",
		"a bad time":          holesHeader + "oanda,EURUSD,h1,yesterday,2013-07-23T22:00:00Z\n",
		"not UTC":             holesHeader + "oanda,EURUSD,h1,2013-07-23T21:00:00+01:00,2013-07-23T22:00:00Z\n",
		"end not after start": holesHeader + "oanda,EURUSD,h1,2013-07-23T22:00:00Z,2013-07-23T22:00:00Z\n",
		"off the boundary":    holesHeader + "oanda,EURUSD,h1,2013-07-23T21:30:00Z,2013-07-23T22:00:00Z\n",
		"d1 off its boundary": holesHeader + "oanda,EURUSD,d1,2008-10-13T00:00:00Z,2008-10-14T21:00:00Z\n",
		"not sorted": holesHeader +
			"oanda,EURUSD,h1,2014-02-27T15:00:00Z,2014-02-27T16:00:00Z\n" +
			"oanda,EURUSD,h1,2013-07-23T21:00:00Z,2013-07-23T22:00:00Z\n",
		"overlapping": holesHeader +
			"oanda,EURUSD,h1,2013-07-23T20:00:00Z,2013-07-23T22:00:00Z\n" +
			"oanda,EURUSD,h1,2013-07-23T21:00:00Z,2013-07-23T23:00:00Z\n",
		"empty": "",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseHoles([]byte(data))
			assert.Error(t, err)
		})
	}
}

func TestKnownHoles_TheEmbeddedListIsValidAndIsOnlyOANDAHourAndDayRuns(t *testing.T) {
	m, err := holes()

	require.NoError(t, err)
	assert.NotEmpty(t, m)
	runs := 0
	for k, rs := range m {
		assert.Equal(t, "oanda", k.source)
		assert.True(t, k.interval.Valid())
		runs += len(rs)
		for _, r := range rs {
			// A hole is never a weekend or a holiday: the calendar explains those, so an
			// entry that overlaps one would be redundant and could hide a mistake.
			for ts := r.start; ts.Before(r.end); {
				c, cerr := ClosureAt(k.interval, ts)
				require.NoError(t, cerr)
				assert.Equal(t, Open, c, "%s %s %s is not a calendar gap", k.instrument, string(k.interval), ts.Format(time.RFC3339))
				next, nerr := nextOpen(k.interval, ts)
				require.NoError(t, nerr)
				ts = next
			}
		}
	}
	assert.Greater(t, runs, 250, "the baseline list is the 2026-11-18 audit's")
}

func TestInKnownHole_StartIsInsideAndEndIsOutsideAndOnlyTheNamedSeriesHasIt(t *testing.T) {
	// EURUSD h1 2007-01-08 23:00 to 2007-01-09 21:00 is one listed run, and no other
	// pair has it.
	start, end := utc(2007, time.January, 8, 23), utc(2007, time.January, 9, 21)
	tests := []struct {
		name         string
		source, inst string
		interval     Interval
		open         time.Time
		want         bool
	}{
		{"the first hour of the run", "oanda", "EURUSD", H1, start, true},
		{"the hour before the run", "oanda", "EURUSD", H1, start.Add(-time.Hour), false},
		{"the last hour of the run", "oanda", "EURUSD", H1, end.Add(-time.Hour), true},
		{"the end is exclusive", "oanda", "EURUSD", H1, end, false},
		{"another instrument", "oanda", "GBPUSD", H1, start, false},
		{"another source", "other", "EURUSD", H1, start, false},
		{"another interval", "oanda", "EURUSD", D1, start, false},
		{"a time in another zone is the same instant", "oanda", "EURUSD", H1, start.In(time.FixedZone("x", 3600)), true},
		{"before the list", "oanda", "EURUSD", H1, utc(1999, time.January, 1, 0), false},
		{"after the list", "oanda", "EURUSD", H1, utc(2099, time.January, 1, 0), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InKnownHole(tc.source, tc.inst, tc.interval, tc.open)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInKnownHole_AMultiHourRunCoversEveryHourOfIt(t *testing.T) {
	// EURUSD h1 2007-01-08 23:00 to 2007-01-09 21:00 is one listed run.
	for h := range 22 {
		got, err := InKnownHole("oanda", "EURUSD", H1, utc(2007, time.January, 8, 23).Add(time.Duration(h)*time.Hour))
		require.NoError(t, err)
		assert.True(t, got, "hour %d of the run", h)
	}
	after, err := InKnownHole("oanda", "EURUSD", H1, utc(2007, time.January, 9, 21))
	require.NoError(t, err)
	assert.False(t, after)
}

func TestGapsWith_AListedRunIsAKnownHoleAndAnythingElseStaysUnexplained(t *testing.T) {
	in := utc(2013, time.July, 23, 21) // a listed single hour for EURUSD
	known := func(open time.Time) (bool, error) { return InKnownHole("oanda", "EURUSD", H1, open) }

	// bars at 20:00 and 22:00 miss 21:00 (listed); bars at 22:00 and 02:00 next day miss 23:00..01:00 (not listed).
	got, err := GapsWith(H1, barsAt(t, in.Add(-time.Hour), in.Add(time.Hour), in.Add(5*time.Hour)), known)

	require.NoError(t, err)
	assert.Equal(t, []Gap{
		{Start: in, End: in.Add(time.Hour), Bars: 1, Closure: KnownHole},
		{Start: in.Add(2 * time.Hour), End: in.Add(5 * time.Hour), Bars: 3, Closure: Open},
	}, got)
}

func TestGapsWith_TheCalendarWinsAndARunThatOutgrowsTheListIsSplit(t *testing.T) {
	// A listed run [21:00, 22:00): a gap that also misses 22:00 splits into the hole and one unexplained hour.
	in := utc(2013, time.July, 23, 21)
	known := func(open time.Time) (bool, error) { return InKnownHole("oanda", "EURUSD", H1, open) }
	got, err := GapsWith(H1, barsAt(t, in.Add(-time.Hour), in.Add(2*time.Hour)), known)
	require.NoError(t, err)
	assert.Equal(t, []Gap{
		{Start: in, End: in.Add(time.Hour), Bars: 1, Closure: KnownHole},
		{Start: in.Add(time.Hour), End: in.Add(2 * time.Hour), Bars: 1, Closure: Open},
	}, got)

	// A hole predicate that says yes to everything never turns a weekend or a holiday into a hole.
	always := func(time.Time) (bool, error) { return true, nil }
	weekend, err := GapsWith(H1, barsAt(t, at(t, 2024, time.March, 1, 16), at(t, 2024, time.March, 3, 17)), always)
	require.NoError(t, err)
	assert.Empty(t, weekend)
	holiday, err := GapsWith(H1, barsAt(t, at(t, 2024, time.December, 24, 16), at(t, 2024, time.December, 25, 17)), always)
	require.NoError(t, err)
	require.Len(t, holiday, 1)
	assert.Equal(t, Holiday, holiday[0].Closure, "the calendar explains it first")
}

func TestGapsWith_ANilPredicateIsGapsAndAnErrorFromItStops(t *testing.T) {
	bars := barsAt(t, utc(2024, time.March, 5, 0), utc(2024, time.March, 5, 5))
	plain, err := Gaps(H1, bars)
	require.NoError(t, err)
	nilKnown, err := GapsWith(H1, bars, nil)
	require.NoError(t, err)
	assert.Equal(t, plain, nilKnown)

	_, err = GapsWith(H1, bars, func(time.Time) (bool, error) { return false, assert.AnError })
	assert.ErrorIs(t, err, assert.AnError)
}

func TestKnownHolesHeader_NamesWhatBelongsInTheList(t *testing.T) {
	head := string(knownHolesCSV)
	assert.True(t, strings.HasPrefix(head, "# known-provider-holes-v1"), "the list is versioned")
	assert.Contains(t, head, "downloaded again")
}
