package service

import (
	"bytes"
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	mdservice "github.com/rustyeddy/trader/marketdata/service"
)

const stooqHeader = "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\r\n"

func stooqRow(date, volume string) string {
	return "SPY.US,D," + date + ",000000,500.1234,501,499,500.5," + volume + ",0\r\n"
}

// putStooq writes synthetic input and returns the request that builds it.
func putStooq(t *testing.T, rows string) StooqBuildRequest {
	t.Helper()
	require.NoError(t, store.Write(context.Background(), "stooq/raw/spy.us.txt", []byte(stooqHeader+rows)))
	return StooqBuildRequest{Instrument: "SPY", Interval: "d1", Input: "stooq/raw/spy.us.txt", SnapshotAsOf: "2026-09-10"}
}

func TestBuildStooq_BuildsEveryMonthAndReadBarsServesItWithItsData(t *testing.T) {
	storetest.Root(t)
	req := putStooq(t, stooqRow("20240328", "10")+stooqRow("20240401", "20"))
	var got []PartitionResult

	built, err := BuildStooq(context.Background(), nil, req, func(r PartitionResult) { got = append(got, r) })

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, PartitionResult{Instrument: "SPY", Year: 2024, Month: time.April,
		Path: "stooq/canonical/SPY/2024/04/SPY-2024-04-d1.csv", Action: Built, Good: 1}, got[1])
	assert.Equal(t, "2026-09-10", built.SnapshotAsOf)
	raw, err := store.Read(built.SnapshotPath)
	require.NoError(t, err)
	input, err := store.Read(req.Input)
	require.NoError(t, err)
	assert.Equal(t, input, raw)

	page, err := ReadBars(context.Background(), nil, BarsRequest{Source: "stooq", Instrument: "SPY", Interval: "d1",
		From: utc(2024, 3, 1, 0), To: utc(2024, 5, 1, 0), Strict: true})
	require.NoError(t, err)
	require.Len(t, page.Bars, 2)
	assert.Equal(t, "500.1234", page.Bars[0].Trade.Open.String())
	assert.True(t, page.Coverage.Complete)
	require.NotNil(t, page.Data)
	assert.Equal(t, built.Fingerprint, page.Data.SnapshotFingerprint)

	asOf, err := ReadBars(context.Background(), nil, BarsRequest{Source: "stooq", Instrument: "SPY", Interval: "d1",
		From: utc(2024, 3, 1, 0), To: utc(2024, 5, 1, 0), AsOf: page.Bars[1].AvailableAt.Add(-time.Nanosecond)})
	require.NoError(t, err)
	assert.Len(t, asOf.Bars, 1, "a bar is not served before it is available")
}

func TestBuildStooq_Errors(t *testing.T) {
	storetest.Root(t)
	good := putStooq(t, stooqRow("20240328", "10"))
	for name, tc := range map[string]struct {
		mutate func(*StooqBuildRequest)
		want   string
		is     error
	}{
		"another instrument": {mutate: func(r *StooqBuildRequest) { r.Instrument = "IWM" }, want: "only SPY and QQQ d1"},
		"another interval":   {mutate: func(r *StooqBuildRequest) { r.Interval = "h1" }, want: "only SPY and QQQ d1"},
		"no input":           {mutate: func(r *StooqBuildRequest) { r.Input = "" }, want: "no input"},
		"missing input":      {mutate: func(r *StooqBuildRequest) { r.Input = "stooq/raw/none" }, is: fs.ErrNotExist},
		"no date":            {mutate: func(r *StooqBuildRequest) { r.SnapshotAsOf = "" }, is: mdservice.ErrSnapshotDate},
	} {
		t.Run(name, func(t *testing.T) {
			req := good
			tc.mutate(&req)

			_, err := BuildStooq(context.Background(), nil, req, nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is)
			}
		})
	}
	bad := putStooq(t, "junk\r\n")
	_, err := BuildStooq(context.Background(), nil, bad, nil)
	assert.ErrorContains(t, err, "malformed")
}

func TestReadBars_RefusesOtherStooqSeriesAndMixedSnapshots(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	req := BarsRequest{Source: "stooq", Instrument: "SPY", Interval: "d1", From: utc(2024, 3, 1, 0), To: utc(2024, 5, 1, 0)}
	for _, mutate := range []func(*BarsRequest){
		func(r *BarsRequest) { r.Instrument = "IWM" }, func(r *BarsRequest) { r.Interval = "h1" },
		func(r *BarsRequest) { r.Source = "alpaca" },
	} {
		other := req
		mutate(&other)
		_, err := ReadBars(ctx, nil, other)
		assert.ErrorIs(t, err, ErrInvalidBarsRequest)
	}

	_, err := BuildStooq(ctx, nil, putStooq(t, stooqRow("20240328", "10")+stooqRow("20240401", "20")), nil)
	require.NoError(t, err)
	march := "stooq/canonical/SPY/2024/03/SPY-2024-03-d1.csv"
	old, err := store.Read(march)
	require.NoError(t, err)
	_, err = BuildStooq(ctx, nil, putStooq(t, stooqRow("20240328", "11")+stooqRow("20240401", "20")), nil)
	require.NoError(t, err)
	require.NoError(t, store.Write(ctx, march, old)) // as a rebuild stopped part way would leave it

	_, err = ReadBars(ctx, nil, req)

	assert.ErrorIs(t, err, ErrBarsMixed)
}

func TestBuildStooq_QQQFromItsFirstYearAndAnOldCalendarIsRebuilt(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	row := "QQQ.US,D,19990310,000000,43.3466,43.3734,42.6342,43.2932,12341014,0\n"
	require.NoError(t, store.Write(ctx, "stooq/raw/qqq.us.txt", []byte(stooqHeader+row)))
	req := StooqBuildRequest{Instrument: "QQQ", Interval: "d1", Input: "stooq/raw/qqq.us.txt", SnapshotAsOf: "2026-09-10"}
	_, err := BuildStooq(ctx, nil, req, nil)
	require.NoError(t, err)
	read := BarsRequest{Source: "stooq", Instrument: "QQQ", Interval: "d1", From: utc(1999, 3, 1, 0), To: utc(1999, 4, 1, 0)}
	page, err := ReadBars(ctx, nil, read)
	require.NoError(t, err)
	require.Len(t, page.Bars, 1)
	assert.Equal(t, "total-return", page.Data.Adjustment)

	// A file of the retired calendar is never served, and a build replaces it.
	path := "stooq/canonical/QQQ/1999/03/QQQ-1999-03-d1.csv"
	data, err := store.Read(path)
	require.NoError(t, err)
	old := bytes.Replace(data, []byte("us-equity-calendar-v2"), []byte("us-equity-calendar-v1"), 1)
	require.NotEqual(t, data, old)
	require.NoError(t, store.Write(ctx, path, old))

	page, err = ReadBars(ctx, nil, read)
	require.NoError(t, err)
	assert.Empty(t, page.Bars)
	assert.Equal(t, 1, page.Coverage.Summary.Stale)
	var got []PartitionResult
	_, err = BuildStooq(ctx, nil, req, func(r PartitionResult) { got = append(got, r) })
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, Replaced, got[0].Action)
}

// A replay just after the 16:00 stock close of a 2000 session must not see that
// day's bar: the fund still traded (#741 review).
func TestReadBars_A2000BarIsNotAvailableAtTheStockClose(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	rows := "QQQ.US,D,20000301,000000,100,101,99,100.5,10,0\nQQQ.US,D,20000302,000000,100,101,99,100.5,10,0\n"
	require.NoError(t, store.Write(ctx, "stooq/raw/qqq.us.txt", []byte(stooqHeader+rows)))
	_, err := BuildStooq(ctx, nil, StooqBuildRequest{Instrument: "QQQ", Interval: "d1", Input: "stooq/raw/qqq.us.txt", SnapshotAsOf: "2026-09-10"}, nil)
	require.NoError(t, err)
	req := BarsRequest{Source: "stooq", Instrument: "QQQ", Interval: "d1", From: utc(2000, 3, 1, 0), To: utc(2000, 4, 1, 0)}
	stockClose := utc(2000, 3, 2, 21) // 16:00 New York on the second session

	for asOf, bars := range map[time.Time]int{
		stockClose.Add(time.Minute):                      1,
		stockClose.Add(15*time.Minute - time.Nanosecond): 1,
		stockClose.Add(15 * time.Minute):                 2,
	} {
		req.AsOf = asOf
		page, err := ReadBars(ctx, nil, req)
		require.NoError(t, err)
		assert.Len(t, page.Bars, bars, asOf.Format(time.RFC3339Nano))
		assert.Equal(t, "us-equity-close-v2", page.Data.AvailabilityPolicy)
	}
}
