package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildMonth builds a canonical h1 month for EURUSD with one bar per given day, at
// 00:00 UTC, each good unless the row is marked incomplete.
func buildMonth(t *testing.T, year int, month time.Month, days ...int) {
	t.Helper()
	var rows []marketdata.SourceRow
	for i, d := range days {
		rows = append(rows, row(i+1, utc(year, month, d, 0)))
	}
	p := h1Source(rows...)
	p.Year, p.Month = year, month
	_, err := Build(context.Background(), p, builtAt)
	require.NoError(t, err)
}

// buildHours builds the canonical h1 months that hold n consecutive hourly bars
// from start, one month per UTC month the run touches, so the bars have no gap.
func buildHours(t *testing.T, start time.Time, n int) {
	t.Helper()
	var rows []marketdata.SourceRow
	flush := func() {
		if len(rows) == 0 {
			return
		}
		first := rows[0].Bar.Time
		p := h1Source(rows...)
		p.Year, p.Month = first.Year(), first.Month()
		_, err := Build(context.Background(), p, builtAt)
		require.NoError(t, err)
		rows = nil
	}
	for i := range n {
		at := start.Add(time.Duration(i) * time.Hour)
		if len(rows) > 0 && rows[0].Bar.Time.Month() != at.Month() {
			flush()
		}
		rows = append(rows, row(len(rows)+1, at))
	}
	flush()
}

func janToMarch(mutate ...func(*LoadRequest)) LoadRequest {
	req := LoadRequest{Source: "oanda", Instrument: "EURUSD", Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 3, 1, 0)}
	for _, m := range mutate {
		m(&req)
	}
	return req
}

func states(r LoadResult) []MonthState {
	var out []MonthState
	for _, m := range r.Months {
		out = append(out, m.State)
	}
	return out
}

// rewriteMeta reads a built month, changes its metadata and writes it back, to
// make a file from another build.
func rewriteMeta(t *testing.T, year int, month time.Month, mutate func(*canonical.Meta)) {
	t.Helper()
	key := canonical.Key{Source: "oanda", Symbol: "EURUSD", Interval: canonical.H1, Year: year, Month: month}
	f, err := canonical.Read(context.Background(), key)
	require.NoError(t, err)
	mutate(&f.Meta)
	require.NoError(t, canonical.Write(context.Background(), f))
}

func TestLoad_ReturnsTheMonthsBarsInOrderCutToTheRange(t *testing.T) {
	storetest.Root(t)
	buildHours(t, utc(2024, 1, 31, 20), 9) // Jan 31 20:00 to Feb 1 04:00, across two months
	req := janToMarch(func(r *LoadRequest) { r.From, r.To = utc(2024, 1, 31, 22), utc(2024, 2, 1, 2) })

	got, err := Load(context.Background(), req)

	require.NoError(t, err)
	var opens []time.Time
	for _, b := range got.Series.Bars {
		opens = append(opens, b.Time)
	}
	assert.Equal(t, []time.Time{utc(2024, 1, 31, 22), utc(2024, 1, 31, 23), utc(2024, 2, 1, 0), utc(2024, 2, 1, 1)}, opens,
		"[From, To) by opening time: 02:00 is the end and not included, 20:00 and 21:00 are before the start")
	assert.Equal(t, []MonthState{Loaded, Loaded}, states(got), "only the months the range touches")
	assert.Equal(t, "oanda", got.Series.Source)
	assert.Equal(t, "EURUSD", got.Series.Instrument)
	assert.Equal(t, "h1", got.Series.Interval)
	require.NoError(t, got.Series.Validate(), "Load returns a valid series across months without validating it again")
	assert.Equal(t, "EURUSD", got.Series.Bars[0].Instrument, "named from the file's key")
	assert.Equal(t, utc(2024, 1, 31, 23), got.Series.Bars[0].AvailableAt, "the stored availability")
}

func TestLoad_ReportsEveryMonthWithItsCounts(t *testing.T) {
	storetest.Root(t)
	buildMonth(t, 2024, time.January, 2, 3)

	got, err := Load(context.Background(), janToMarch(func(r *LoadRequest) { r.AllowMissing = true }))

	require.NoError(t, err)
	require.Len(t, got.Months, 2)
	assert.Equal(t, MonthStatus{Year: 2024, Month: time.January, State: Loaded, Path: "oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv", Good: 2}, got.Months[0])
	assert.Equal(t, MonthStatus{Year: 2024, Month: time.February, State: Missing, Path: "oanda/canonical/EURUSD/2024/02/EURUSD-2024-02-h1.csv"}, got.Months[1])
}

func TestLoad_AMissingMonthFailsUnlessAllowed(t *testing.T) {
	storetest.Root(t)
	buildMonth(t, 2024, time.January, 2)
	buildMonth(t, 2024, time.March, 2)
	req := janToMarch(func(r *LoadRequest) { r.To = utc(2024, 4, 1, 0) })

	strict, err := Load(context.Background(), req)
	req.AllowMissing = true
	allowed, aerr := Load(context.Background(), req)

	require.ErrorIs(t, err, ErrIncomplete)
	var inc *IncompleteError
	require.True(t, errors.As(err, &inc))
	assert.Equal(t, "EURUSD", inc.Instrument)
	require.Len(t, inc.Months, 1)
	assert.Equal(t, Missing, inc.Months[0].State)
	assert.Equal(t, time.February, inc.Months[0].Month)
	assert.Empty(t, strict.Series.Bars, "nothing is returned for a refused load")
	assert.Equal(t, []MonthState{Loaded, Missing, Loaded}, states(strict), "but every month's status is")
	require.NoError(t, aerr)
	assert.Len(t, allowed.Series.Bars, 2, "the series has a gap where the month is missing")
}

func TestLoad_ABadMonthFailsUnlessAllowedAndIsStillReportedBad(t *testing.T) {
	storetest.Root(t)
	bad := row(2, utc(2024, 1, 2, 1))
	bad.Complete = false
	p := h1Source(row(1, utc(2024, 1, 2, 0)), bad)
	_, err := Build(context.Background(), p, builtAt)
	require.NoError(t, err)
	req := janToMarch(func(r *LoadRequest) { r.To = utc(2024, 2, 1, 0) })

	_, strictErr := Load(context.Background(), req)
	req.AllowBad = true
	allowed, aerr := Load(context.Background(), req)

	require.ErrorIs(t, strictErr, ErrIncomplete)
	require.NoError(t, aerr)
	assert.Len(t, allowed.Series.Bars, 1, "the good bar of a known-bad month")
	assert.Equal(t, MonthStatus{Year: 2024, Month: time.January, State: Bad, Path: allowed.Months[0].Path, Good: 1, Bad: 1, Reason: "1 bad records"}, allowed.Months[0])
}

func TestLoad_AStaleMonthIsNeverUsed(t *testing.T) {
	for name, mutate := range map[string]func(*canonical.Meta){
		"builder":             func(m *canonical.Meta) { m.BuilderVersion = "builder-v0" },
		"validator":           func(m *canonical.Meta) { m.ValidatorVersion = "validator-v0" },
		"availability policy": func(m *canonical.Meta) { m.AvailabilityPolicy = "scheduled-close-v0" },
	} {
		t.Run(name, func(t *testing.T) {
			storetest.Root(t)
			buildMonth(t, 2024, time.January, 2)
			rewriteMeta(t, 2024, time.January, mutate)
			req := janToMarch(func(r *LoadRequest) { r.To = utc(2024, 2, 1, 0); r.AllowBad, r.AllowMissing = true, true })

			got, err := Load(context.Background(), req)

			require.ErrorIs(t, err, ErrIncomplete, "no allow flag admits a stale month")
			assert.Equal(t, Stale, got.Months[0].State)
			assert.NotEmpty(t, got.Months[0].Reason)
			assert.Empty(t, got.Series.Bars)
		})
	}
}

func TestLoad_AFileThatIsNotCanonicalV2IsStale(t *testing.T) {
	storetest.Root(t)
	path := "oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv"
	require.NoError(t, store.Write(context.Background(), path, []byte("# schema=canonical-v1\nold,format\n")))

	got, err := Load(context.Background(), janToMarch(func(r *LoadRequest) { r.To = utc(2024, 2, 1, 0) }))

	require.ErrorIs(t, err, ErrIncomplete)
	assert.Equal(t, Stale, got.Months[0].State)
	assert.Contains(t, got.Months[0].Reason, "canonical-v2")
}

func TestLoad_ARebuildMakesAStaleMonthLoadable(t *testing.T) {
	storetest.Root(t)
	buildMonth(t, 2024, time.January, 2)
	rewriteMeta(t, 2024, time.January, func(m *canonical.Meta) { m.BuilderVersion = "builder-v0" })
	req := janToMarch(func(r *LoadRequest) { r.To = utc(2024, 2, 1, 0) })
	_, err := Load(context.Background(), req)
	require.ErrorIs(t, err, ErrIncomplete)

	buildMonth(t, 2024, time.January, 2) // Build replaces a file from another build
	got, err := Load(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, []MonthState{Loaded}, states(got))
}

func TestLoad_RefusesARequestItCannotRun(t *testing.T) {
	storetest.Root(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, tc := range map[string]struct {
		ctx    context.Context
		mutate func(*LoadRequest)
		want   error
	}{
		"no source":     {context.Background(), func(r *LoadRequest) { r.Source = "" }, ErrInvalidRequest},
		"no instrument": {context.Background(), func(r *LoadRequest) { r.Instrument = "" }, ErrInvalidRequest},
		"h4":            {context.Background(), func(r *LoadRequest) { r.Interval = "h4" }, ErrUnsupportedInterval},
		"empty range":   {context.Background(), func(r *LoadRequest) { r.To = r.From }, ErrInvalidRequest},
		"reversed":      {context.Background(), func(r *LoadRequest) { r.From, r.To = r.To, r.From }, ErrInvalidRequest},
		"traversal":     {context.Background(), func(r *LoadRequest) { r.Instrument = "../x" }, ErrInvalidRequest},
		"cancelled":     {cancelled, func(*LoadRequest) {}, context.Canceled},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(tc.ctx, janToMarch(tc.mutate))

			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestIncompleteError_NamesTheInstrumentAndMonths(t *testing.T) {
	err := &IncompleteError{Instrument: "EURUSD", Months: []MonthStatus{
		{Year: 2024, Month: time.February, State: Missing}, {Year: 2024, Month: time.March, State: Stale},
	}}

	assert.Contains(t, err.Error(), "EURUSD")
	assert.Contains(t, err.Error(), "2024-02 missing")
	assert.Contains(t, err.Error(), "2024-03 stale")
	assert.ErrorIs(t, err, ErrIncomplete)
}

// gapped builds bars on Tuesday January 9 2024 at the given hours (UTC), one
// build, so what lies between them is a gap. Not January 2: the year-end holiday
// window runs to then.
func gapped(t *testing.T, hours ...int) {
	t.Helper()
	var rows []marketdata.SourceRow
	for i, h := range hours {
		rows = append(rows, row(i+1, utc(2024, 1, 9, h)))
	}
	p := h1Source(rows...)
	p.Year, p.Month = 2024, time.January
	_, err := Build(context.Background(), p, builtAt)
	require.NoError(t, err)
}

func TestLoad_AnInteriorGapTheCalendarDoesNotExplainFailsUnlessAllowed(t *testing.T) {
	storetest.Root(t)
	gapped(t, 0, 1, 5, 6) // 02:00 to 04:00 are missing
	req := janToMarch(func(r *LoadRequest) { r.To = utc(2024, 2, 1, 0) })

	strict, err := Load(context.Background(), req)
	req.AllowMissing = true
	allowed, aerr := Load(context.Background(), req)

	require.ErrorIs(t, err, ErrIncomplete)
	var inc *IncompleteError
	require.True(t, errors.As(err, &inc))
	assert.Empty(t, inc.Months, "every month loaded; the gap is what stopped it")
	assert.Equal(t, []Gap{{Kind: Unexplained, Start: utc(2024, 1, 9, 2), End: utc(2024, 1, 9, 5), Bars: 3}}, inc.Gaps)
	assert.Contains(t, err.Error(), "gap unexplained 2024-01-09T02:00Z 2024-01-09T05:00Z, 3 bars")
	assert.Empty(t, strict.Series.Bars, "nothing is returned for a refused load")
	assert.Equal(t, []MonthState{Loaded}, states(strict), "but the months are reported")
	assert.Equal(t, inc.Gaps, strict.Gaps)
	require.NoError(t, aerr)
	assert.Len(t, allowed.Series.Bars, 4)
	assert.Equal(t, inc.Gaps, allowed.Gaps, "an allowed gap is still reported")
}

func TestLoad_AGapAcrossAMissingMonthIsAnInteriorGap(t *testing.T) {
	storetest.Root(t)
	buildHours(t, utc(2024, 1, 31, 22), 2)
	buildHours(t, utc(2024, 3, 1, 0), 2)
	req := janToMarch(func(r *LoadRequest) { r.To = utc(2024, 3, 2, 0); r.AllowMissing = true })

	got, err := Load(context.Background(), req)

	require.NoError(t, err)
	// February 2024 has 29*24 hours, 4 weekends of 48 closed hours among them; the
	// weekends split it into five gaps and are not themselves gaps.
	require.Len(t, got.Gaps, 5)
	bars := 0
	for _, g := range got.Gaps {
		assert.Equal(t, Unexplained, g.Kind)
		bars += g.Bars
	}
	assert.Equal(t, utc(2024, 2, 1, 0), got.Gaps[0].Start)
	assert.Equal(t, utc(2024, 3, 1, 0), got.Gaps[4].End)
	assert.Equal(t, 29*24-4*48, bars)
}

func TestLoad_TheWeeklyCloseIsNotAGapAndTheEndsOfTheRangeAreNotJudged(t *testing.T) {
	storetest.Root(t)
	// Friday Jan 5 2024 21:00 UTC is 16:00 in New York, the last hour before the
	// close; the market reopens Sunday 17:00 New York, 22:00 UTC.
	var rows []marketdata.SourceRow
	for i, at := range []time.Time{utc(2024, 1, 5, 20), utc(2024, 1, 5, 21), utc(2024, 1, 7, 22), utc(2024, 1, 7, 23)} {
		rows = append(rows, row(i+1, at))
	}
	jan := h1Source(rows...)
	jan.Year, jan.Month = 2024, time.January
	_, err := Build(context.Background(), jan, builtAt)
	require.NoError(t, err)
	req := janToMarch(func(r *LoadRequest) { r.From, r.To = utc(2024, 1, 1, 0), utc(2024, 1, 31, 0) })

	got, err := Load(context.Background(), req)

	require.NoError(t, err, "the bars before the first and after the last are not judged, and the weekend is not a gap")
	assert.Len(t, got.Series.Bars, 4)
	assert.Empty(t, got.Gaps, "a weekend is not reported")
}

func TestLoad_AYearEndHolidayIsReportedAsAKnownClosure(t *testing.T) {
	storetest.Root(t)
	// Christmas 2024: the market closed after Tuesday December 24 16:00 New York
	// (21:00 UTC) and reopened Wednesday 17:00 (22:00 UTC).
	var rows []marketdata.SourceRow
	for i, at := range []time.Time{utc(2024, 12, 24, 20), utc(2024, 12, 24, 21), utc(2024, 12, 25, 22), utc(2024, 12, 25, 23)} {
		rows = append(rows, row(i+1, at))
	}
	dec := h1Source(rows...)
	dec.Year, dec.Month = 2024, time.December
	_, err := Build(context.Background(), dec, builtAt)
	require.NoError(t, err)
	req := LoadRequest{Source: "oanda", Instrument: "EURUSD", Interval: "h1", From: utc(2024, 12, 1, 0), To: utc(2025, 1, 1, 0)}

	got, err := Load(context.Background(), req)

	require.NoError(t, err, "a holiday closure is never an error")
	assert.Len(t, got.Series.Bars, 4)
	assert.Equal(t, []Gap{{Kind: KnownClosure, Start: utc(2024, 12, 24, 22), End: utc(2024, 12, 25, 22), Bars: 24}}, got.Gaps)
}

// holeMonth builds EURUSD h1 January 2007 with bars at the given times only. The
// reviewed list of known provider holes has EURUSD h1 from 2007-01-08 23:00 to
// 2007-01-09 21:00 (OANDA has no candles for it).
func holeMonth(t *testing.T, opens ...time.Time) {
	t.Helper()
	var rows []marketdata.SourceRow
	for i, at := range opens {
		rows = append(rows, row(i+1, at))
	}
	p := h1Source(rows...)
	p.Year, p.Month = 2007, time.January
	_, err := Build(context.Background(), p, builtAt)
	require.NoError(t, err)
}

func TestLoad_ARunOnTheKnownProviderHolesListIsReportedAndIsNotAnError(t *testing.T) {
	storetest.Root(t)
	holeMonth(t, utc(2007, 1, 8, 22), utc(2007, 1, 9, 21))
	req := LoadRequest{Source: "oanda", Instrument: "EURUSD", Interval: "h1", From: utc(2007, 1, 1, 0), To: utc(2007, 2, 1, 0)}

	got, err := Load(context.Background(), req)

	require.NoError(t, err, "a known provider hole never fails a strict load")
	assert.Len(t, got.Series.Bars, 2)
	assert.Equal(t, []Gap{{Kind: KnownHole, Start: utc(2007, 1, 8, 23), End: utc(2007, 1, 9, 21), Bars: 22}}, got.Gaps)
}

func TestLoad_AGapThatOutgrowsTheListedRunStillFailsForTheRest(t *testing.T) {
	storetest.Root(t)
	holeMonth(t, utc(2007, 1, 8, 22), utc(2007, 1, 9, 22)) // 21:00 is missing too, and is not listed
	req := LoadRequest{Source: "oanda", Instrument: "EURUSD", Interval: "h1", From: utc(2007, 1, 1, 0), To: utc(2007, 2, 1, 0)}

	strict, err := Load(context.Background(), req)
	req.AllowMissing = true
	allowed, aerr := Load(context.Background(), req)

	require.ErrorIs(t, err, ErrIncomplete)
	var inc *IncompleteError
	require.True(t, errors.As(err, &inc))
	assert.Equal(t, []Gap{{Kind: Unexplained, Start: utc(2007, 1, 9, 21), End: utc(2007, 1, 9, 22), Bars: 1}}, inc.Gaps, "only the unlisted hour fails")
	assert.Equal(t, KnownHole, strict.Gaps[0].Kind, "the listed part is still reported")
	require.NoError(t, aerr)
	assert.Len(t, allowed.Gaps, 2)
}

func TestLoad_AnotherInstrumentDoesNotInheritAListedRun(t *testing.T) {
	storetest.Root(t)
	var rows []marketdata.SourceRow
	for i, at := range []time.Time{utc(2007, 1, 8, 22), utc(2007, 1, 9, 21)} {
		rows = append(rows, row(i+1, at))
	}
	p := h1Source(rows...)
	p.Instrument, p.Year, p.Month = "GBPUSD", 2007, time.January
	_, err := Build(context.Background(), p, builtAt)
	require.NoError(t, err)

	_, lerr := Load(context.Background(), LoadRequest{Source: "oanda", Instrument: "GBPUSD", Interval: "h1", From: utc(2007, 1, 1, 0), To: utc(2007, 2, 1, 0)})

	assert.ErrorIs(t, lerr, ErrIncomplete, "the list names EURUSD, not GBPUSD")
}

func TestLoad_SkipStaleServesTheOtherMonthsAndStillReportsTheStaleOne(t *testing.T) {
	storetest.Root(t)
	buildHours(t, utc(2024, 1, 31, 22), 4) // Jan 31 22:00 to Feb 1 01:00: one bar pair in each month
	rewriteMeta(t, 2024, time.February, func(m *canonical.Meta) { m.BuilderVersion = "builder-v0" })
	req := janToMarch(func(r *LoadRequest) { r.To = utc(2024, 3, 1, 0); r.AllowMissing = true })

	strict, err := Load(context.Background(), req)
	req.SkipStale = true
	skipped, serr := Load(context.Background(), req)

	require.ErrorIs(t, err, ErrIncomplete, "a stale month fails the load unless it is skipped")
	assert.Equal(t, []MonthState{Loaded, Stale}, states(strict))
	require.NoError(t, serr)
	assert.Equal(t, []MonthState{Loaded, Stale}, states(skipped), "still reported as stale")
	assert.Len(t, skipped.Series.Bars, 2, "only the January bars: a stale month's bars are never used")
	assert.Equal(t, utc(2024, 1, 31, 23), skipped.Series.Bars[1].Time)
}

func spyLoad(from, to time.Time) LoadRequest {
	return LoadRequest{Source: "stooq", Instrument: "SPY", Interval: "d1", From: from, To: to}
}

func TestLoad_ATradeSeriesComesWithItsDataAndNoGapForClosures(t *testing.T) {
	storetest.Root(t)
	// 28 March, Good Friday the 29th, a weekend, then 1 April.
	_, err := BuildSeries(context.Background(), days("one", day(time.March, 28), day(time.April, 1)), builtAt)
	require.NoError(t, err)

	got, err := Load(context.Background(), spyLoad(day(time.March, 1), day(time.May, 1)))

	require.NoError(t, err)
	require.Len(t, got.Series.Bars, 2)
	assert.Empty(t, got.Gaps)
	assert.Equal(t, &DataInfo{AvailabilityPolicy: "us-equity-close-v2", Calendar: "us-equity-calendar-v2", PriceBasis: "trade",
		Currency: "USD", VolumeUnit: "shares", Adjustment: "total-return", AdjustmentEvidence: "observed",
		SnapshotFingerprint: canonical.Fingerprint([]byte("one")), SnapshotAsOf: "2026-09-10"}, got.Data)
	require.NoError(t, got.Series.Validate())
}

func TestLoad_AMissingSessionIsAnUnexplainedGap(t *testing.T) {
	storetest.Root(t)
	_, err := BuildSeries(context.Background(), days("one", day(time.March, 4), day(time.March, 6)), builtAt)
	require.NoError(t, err)
	req := spyLoad(day(time.March, 1), day(time.April, 1))

	got, err := Load(context.Background(), req)

	assert.ErrorIs(t, err, ErrIncomplete)
	assert.NotNil(t, got.Data)
	require.Len(t, got.Gaps, 1)
	assert.Equal(t, Gap{Kind: Unexplained, Start: utc(2024, 3, 5, 14).Add(30 * time.Minute),
		End: utc(2024, 3, 6, 14).Add(30 * time.Minute), Bars: 1}, got.Gaps[0])
	req.AllowMissing = true
	got, err = Load(context.Background(), req)
	require.NoError(t, err)
	assert.Len(t, got.Series.Bars, 2)
}

func TestLoad_MonthsOfDifferentSeriesNeverLoadWhateverIsAllowed(t *testing.T) {
	april := canonical.Key{Source: "stooq", Symbol: "SPY", Interval: canonical.D1, Year: 2024, Month: time.April}
	for name, mutate := range map[string]func(*canonical.Meta){
		"snapshot":         func(m *canonical.Meta) { m.Trade.SnapshotFingerprint = canonical.Fingerprint([]byte("two")) },
		"currency":         func(m *canonical.Meta) { m.Trade.Currency = "EUR" },
		"adjustment":       func(m *canonical.Meta) { m.Trade.Adjustment = canonical.AdjustmentSplit },
		"data description": func(m *canonical.Meta) { m.Trade.SnapshotAsOf = "2026-09-11" },
		"stale as well": func(m *canonical.Meta) {
			m.BuilderVersion, m.Trade.SnapshotFingerprint = "old", canonical.Fingerprint([]byte("two"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			storetest.Root(t)
			ctx := context.Background()
			_, err := BuildSeries(ctx, days("one", day(time.March, 28), day(time.April, 1)), builtAt)
			require.NoError(t, err)
			f, err := canonical.Read(ctx, april)
			require.NoError(t, err)
			mutate(&f.Meta)
			require.NoError(t, canonical.Write(ctx, f))
			req := spyLoad(day(time.March, 1), day(time.May, 1))
			req.AllowBad, req.AllowMissing, req.SkipStale = true, true, true

			got, err := Load(ctx, req)

			assert.ErrorIs(t, err, ErrMixedSeries)
			assert.Empty(t, got.Series.Bars)
		})
	}
}

func TestLoad_ATradeFileFromAnotherBuildIsStale(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	_, err := BuildSeries(ctx, days("one", day(time.March, 4)), builtAt)
	require.NoError(t, err)
	req := spyLoad(day(time.March, 1), day(time.April, 1))
	for want, mutate := range map[string]func(*canonical.Meta){
		`builder "builder-v1"`:     func(m *canonical.Meta) { m.BuilderVersion = BuilderVersion },
		`validator "validator-v1"`: func(m *canonical.Meta) { m.ValidatorVersion = ValidatorVersion },
	} {
		f := spyFile(t, time.March)
		original := f.Meta
		mutate(&f.Meta)
		require.NoError(t, canonical.Write(ctx, f))

		got, err := Load(ctx, req)

		assert.ErrorIs(t, err, ErrIncomplete)
		assert.Equal(t, Stale, got.Months[0].State)
		assert.Contains(t, got.Months[0].Reason, want)
		f.Meta = original
		require.NoError(t, canonical.Write(ctx, f))
	}
	require.NoError(t, store.Write(ctx, "stooq/canonical/SPY/2024/03/SPY-2024-03-d1.csv", []byte("junk")))
	got, err := Load(ctx, req)
	assert.ErrorIs(t, err, ErrIncomplete)
	assert.Contains(t, got.Months[0].Reason, "not readable as canonical-trade-v1")
}
