package service

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func barsReq(from, to time.Time) BarsRequest {
	return BarsRequest{Instrument: "EURUSD", Interval: "h1", From: from, To: to}
}

func opens(bars []marketdata.Bar) []time.Time {
	var out []time.Time
	for _, b := range bars {
		out = append(out, b.Time)
	}
	return out
}

// pages follows the cursor from request to request and returns every page.
func pages(t *testing.T, req BarsRequest) []BarsPage {
	t.Helper()
	var all []BarsPage
	for {
		p, err := ReadBars(context.Background(), nil, req)
		require.NoError(t, err)
		all = append(all, p)
		if p.NextAfter.IsZero() {
			return all
		}
		require.Less(t, len(all), 1000, "pagination must end")
		req.After = p.NextAfter
	}
}

func TestReadBars_ReturnsTheRangeInOrderWithHalfOpenBounds(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 10)

	got, err := ReadBars(context.Background(), nil, barsReq(utc(2024, 1, 9, 2), utc(2024, 1, 9, 6)))

	require.NoError(t, err)
	assert.Equal(t, []time.Time{utc(2024, 1, 9, 2), utc(2024, 1, 9, 3), utc(2024, 1, 9, 4), utc(2024, 1, 9, 5)}, opens(got.Bars),
		"From is inclusive, To is exclusive, by opening time")
	assert.Equal(t, utc(2024, 1, 9, 3), got.Bars[0].AvailableAt, "the stored availability, one hour after it opens")
	assert.Equal(t, "oanda", got.Source)
	assert.Equal(t, "EURUSD", got.Instrument)
	assert.True(t, got.NextAfter.IsZero())
	assert.True(t, got.Coverage.Complete)
	assert.Equal(t, 1, got.Coverage.Summary.Loaded)
	assert.Empty(t, got.Coverage.Months)
	assert.Empty(t, got.Coverage.Gaps)
}

func TestReadBars_PagesNeitherRepeatNorOmitABarAcrossAMonthBoundary(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 31, 10), 30) // Jan 31 10:00 to Feb 1 15:00
	req := barsReq(utc(2024, 1, 1, 0), utc(2024, 3, 1, 0))
	req.Limit = 7
	whole, err := ReadBars(context.Background(), nil, BarsRequest{Instrument: "EURUSD", Interval: "h1", From: req.From, To: req.To, Limit: MaxBarsLimit})
	require.NoError(t, err)

	got := pages(t, req)

	var joined []marketdata.Bar
	for i, p := range got {
		if i < len(got)-1 {
			assert.Len(t, p.Bars, 7, "every page but the last is full")
		}
		joined = append(joined, p.Bars...)
	}
	assert.Equal(t, whole.Bars, joined, "the pages together are exactly the whole range, in order")
	assert.Len(t, got, 5, "30 bars in pages of 7")
	assert.True(t, got[len(got)-1].NextAfter.IsZero())
	for i := 1; i < len(joined); i++ {
		assert.True(t, joined[i].Time.After(joined[i-1].Time), "strictly ascending")
	}
}

func TestReadBars_TheSameRequestGivesTheSameBars(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 20)
	req := barsReq(utc(2024, 1, 9, 0), utc(2024, 1, 10, 0))
	req.Limit = 8

	first, err := ReadBars(context.Background(), nil, req)
	require.NoError(t, err)
	second, err := ReadBars(context.Background(), nil, req)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestReadBars_AMissingMonthIsServedAsPartialAndDescribedInTheCoverage(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 31, 22), true), rec(utc(2024, 1, 31, 23), true))
	putRaw(t, "EURUSD", 2024, time.March, rec(utc(2024, 3, 1, 0), true), rec(utc(2024, 3, 1, 1), true)) // February has nothing
	require.NoError(t, BuildCanonical(context.Background(), nil,
		BuildRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 4, 1, 0)}, nil))
	req := barsReq(utc(2024, 1, 1, 0), utc(2024, 4, 1, 0))
	req.Limit = MaxBarsLimit

	got, err := ReadBars(context.Background(), nil, req)

	require.NoError(t, err, "partial is the default")
	assert.Len(t, got.Bars, 4)
	assert.False(t, got.Coverage.Complete)
	assert.Equal(t, 1, got.Coverage.Summary.Missing)
	require.Len(t, got.Coverage.Months, 1)
	assert.Equal(t, MonthMissing, got.Coverage.Months[0].State)
	assert.Equal(t, time.February, got.Coverage.Months[0].Month)
	require.NotEmpty(t, got.Coverage.Gaps, "the rows between the two months are listed")
	var unexplained int
	for _, g := range got.Coverage.Gaps {
		if g.Kind == GapUnexplained {
			unexplained++
		}
	}
	assert.Positive(t, unexplained)
}

func TestReadBars_StrictTurnsAnIncompleteRangeIntoAnErrorWithTheSamePage(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 3)
	req := barsReq(utc(2024, 1, 1, 0), utc(2024, 3, 1, 0)) // February is missing
	partial, perr := ReadBars(context.Background(), nil, req)
	req.Strict = true

	strict, err := ReadBars(context.Background(), nil, req)

	require.NoError(t, perr)
	require.ErrorIs(t, err, ErrBarsIncomplete)
	assert.Equal(t, partial, strict, "what was read and how it is described do not change")
	assert.False(t, strict.Coverage.Complete)
}

func TestReadBars_ABadMonthContributesItsGoodBarsAndIsReported(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), true), rec(utc(2024, 1, 9, 2), false))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	req := barsReq(utc(2024, 1, 1, 0), utc(2024, 2, 1, 0))

	got, err := ReadBars(context.Background(), nil, req)

	require.NoError(t, err)
	assert.Len(t, got.Bars, 2, "the good bars of a known-bad month")
	assert.False(t, got.Coverage.Complete)
	require.Len(t, got.Coverage.Months, 1)
	assert.Equal(t, MonthBad, got.Coverage.Months[0].State)
	assert.Equal(t, 1, got.Coverage.Months[0].Bad)
}

func TestReadBars_AStaleMonthIsNeverServedAndIsReported(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 3)
	require.NoError(t, store.Write(context.Background(), "oanda/canonical/EURUSD/2024/02/EURUSD-2024-02-h1.csv", []byte("# schema=canonical-v1\nold\n")))
	req := barsReq(utc(2024, 1, 1, 0), utc(2024, 3, 1, 0))

	got, err := ReadBars(context.Background(), nil, req)

	require.NoError(t, err)
	assert.Len(t, got.Bars, 3, "only January: a stale file is never used")
	assert.Equal(t, 1, got.Coverage.Summary.Stale)
	var stale []MonthStatus
	for _, m := range got.Coverage.Months {
		if m.State == MonthStale {
			stale = append(stale, m)
		}
	}
	require.Len(t, stale, 1)
	assert.Contains(t, stale[0].Reason, "not readable as canonical-v2")
}

func TestReadBars_ARangeWithNoFileAtAllIsNoData(t *testing.T) {
	storetest.Root(t)

	got, err := ReadBars(context.Background(), nil, barsReq(utc(2024, 1, 1, 0), utc(2024, 3, 1, 0)))

	require.ErrorIs(t, err, ErrNoData)
	assert.Empty(t, got.Bars)
	assert.Equal(t, 2, got.Coverage.Summary.Missing, "the page still says what is missing")
}

func TestReadBars_ACoverageGapIsListedOnExactlyOnePage(t *testing.T) {
	storetest.Root(t)
	// 20 hourly bars with one hour missing at 10:00 on a Tuesday: a gap the calendar
	// does not explain. Pages of 5 put the gap's neighbours on different pages.
	putRaw(t, "EURUSD", 2024, time.January, func() []oanda.Record {
		var out []oanda.Record
		for h := range 21 {
			if h == 10 {
				continue
			}
			out = append(out, rec(utc(2024, 1, 9, h), true))
		}
		return out
	}()...)
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	req := barsReq(utc(2024, 1, 9, 0), utc(2024, 1, 10, 0))
	req.Limit = 5

	got := pages(t, req)

	var listed int
	for _, p := range got {
		for _, g := range p.Coverage.Gaps {
			listed++
			assert.Equal(t, GapUnexplained, g.Kind)
			assert.Equal(t, utc(2024, 1, 9, 10), g.Start)
		}
	}
	assert.Equal(t, 1, listed, "listed once across all pages, not zero and not twice")
	assert.False(t, got[0].Coverage.Complete, "the first page knows the range is not complete")
	assert.False(t, got[2].Coverage.Complete, "so does a page that still has the gap ahead of it or just behind its start")
	assert.True(t, got[len(got)-1].Coverage.Complete, "a page's coverage describes the range from its own start on: nothing is missing after it")
}

func TestReadBars_ClosuresAndKnownHolesAreListedAndStillComplete(t *testing.T) {
	storetest.Root(t)
	// EURUSD h1 2007-01-08 23:00 to 2007-01-09 21:00 is on the list of known provider holes.
	putRaw(t, "EURUSD", 2007, time.January, rec(utc(2007, 1, 8, 22), true), rec(utc(2007, 1, 9, 21), true))
	require.NoError(t, BuildCanonical(context.Background(), nil,
		BuildRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2007, 1, 1, 0), To: utc(2007, 2, 1, 0)}, nil))

	got, err := ReadBars(context.Background(), nil, barsReq(utc(2007, 1, 1, 0), utc(2007, 2, 1, 0)))

	require.NoError(t, err)
	assert.True(t, got.Coverage.Complete, "a known provider hole is explained")
	require.Len(t, got.Coverage.Gaps, 1)
	assert.Equal(t, GapKnownHole, got.Coverage.Gaps[0].Kind, "but it is still listed")
	assert.Equal(t, 1, got.Coverage.Summary.GapsKnownHole)
}

func TestReadBars_AsOfLeavesOutBarsNotYetAvailable(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 10)
	req := barsReq(utc(2024, 1, 9, 0), utc(2024, 1, 10, 0))
	req.AsOf = utc(2024, 1, 9, 4) // the 03:00 bar closes at 04:00, the 04:00 bar at 05:00

	got, err := ReadBars(context.Background(), nil, req)

	require.NoError(t, err)
	assert.Equal(t, []time.Time{utc(2024, 1, 9, 0), utc(2024, 1, 9, 1), utc(2024, 1, 9, 2), utc(2024, 1, 9, 3)}, opens(got.Bars),
		"a bar is usable from its AvailableAt: no look-ahead")
	for _, b := range got.Bars {
		assert.False(t, b.AvailableAt.After(req.AsOf))
	}
	assert.True(t, got.NextAfter.IsZero())
	assert.Equal(t, utc(2024, 1, 9, 4), got.AsOf)
}

func TestReadBars_RefusesARequestItCannotRun(t *testing.T) {
	storetest.Root(t)
	good := barsReq(utc(2024, 1, 1, 0), utc(2024, 2, 1, 0))
	tests := map[string]func(*BarsRequest){
		"another source":          func(r *BarsRequest) { r.Source = "alpaca" },
		"a negative limit":        func(r *BarsRequest) { r.Limit = -1 },
		"a limit over the max":    func(r *BarsRequest) { r.Limit = MaxBarsLimit + 1 },
		"an empty range":          func(r *BarsRequest) { r.To = r.From },
		"a reversed range":        func(r *BarsRequest) { r.From, r.To = r.To, r.From },
		"a position before":       func(r *BarsRequest) { r.After = r.From.Add(-time.Hour) },
		"a position at the end":   func(r *BarsRequest) { r.After = r.To },
		"an unsupported interval": func(r *BarsRequest) { r.Interval = "h4" },
		"a path in the name":      func(r *BarsRequest) { r.Instrument = "../x" },
		"no instrument":           func(r *BarsRequest) { r.Instrument = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			req := good
			mutate(&req)

			_, err := ReadBars(context.Background(), nil, req)

			assert.ErrorIs(t, err, ErrInvalidBarsRequest)
		})
	}
}

func TestReadBars_ACancelledContextReturnsItsError(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 9, 0), 3)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ReadBars(cancelled, nil, barsReq(utc(2024, 1, 1, 0), utc(2024, 2, 1, 0)))

	assert.ErrorIs(t, err, context.Canceled)
}
