package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
	"github.com/rustyeddy/trader/store"
	"github.com/rustyeddy/trader/store/storetest"
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
	buildMonth(t, 2024, time.January, 2, 15, 31)
	buildMonth(t, 2024, time.February, 1, 10)
	req := janToMarch(func(r *LoadRequest) { r.From, r.To = utc(2024, 1, 15, 0), utc(2024, 2, 10, 0) })

	got, err := Load(context.Background(), req)

	require.NoError(t, err)
	var opens []time.Time
	for _, b := range got.Series.Bars {
		opens = append(opens, b.Time)
	}
	assert.Equal(t, []time.Time{utc(2024, 1, 15, 0), utc(2024, 1, 31, 0), utc(2024, 2, 1, 0)}, opens,
		"[From, To) by opening time: the 10th is the end and not included, the 2nd is before the start")
	assert.Equal(t, []MonthState{Loaded, Loaded}, states(got), "only the months the range touches")
	assert.Equal(t, "oanda", got.Series.Source)
	assert.Equal(t, "EURUSD", got.Series.Instrument)
	assert.Equal(t, "h1", got.Series.Interval)
	require.NoError(t, got.Series.Validate())
	assert.Equal(t, "EURUSD", got.Series.Bars[0].Instrument, "named from the file's key")
	assert.Equal(t, utc(2024, 1, 15, 1), got.Series.Bars[0].AvailableAt, "the stored availability")
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
