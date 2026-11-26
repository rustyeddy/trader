package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
)

func day(m time.Month, d int) time.Time { return utc(2024, m, d, 0) }

func tradeRow(n int, date time.Time) marketdata.SourceRow {
	return marketdata.SourceRow{Row: n, Complete: true, Bar: marketdata.Bar{Instrument: "SPY", Time: date, Volume: 1000,
		Trade: marketdata.OHLC{Open: px("500.1234"), High: px("501"), Low: px("499"), Close: px("500.5")}}}
}

// snapshotOf groups rows into monthly partitions as a provider would; name makes
// the input bytes, and so the snapshot, distinct.
func snapshotOf(name string, rows ...marketdata.SourceRow) Snapshot {
	s := Snapshot{Data: []byte(name), AsOf: "2026-09-10", File: name}
	index := map[time.Month]int{}
	for m := time.January; m <= time.December; m++ {
		for _, r := range rows {
			if r.Bar.Time.Month() != m {
				continue
			}
			i, ok := index[m]
			if !ok {
				i = len(s.Partitions)
				index[m] = i
				s.Partitions = append(s.Partitions, marketdata.SourcePartition{Source: "stooq", Instrument: "SPY", Interval: "d1",
					Year: 2024, Month: m, Fingerprint: name + strconv.Itoa(int(m))})
			}
			s.Partitions[i].Rows = append(s.Partitions[i].Rows, r)
		}
	}
	return s
}

// days is a snapshot with one good row per date, numbered in the order given.
func days(name string, dates ...time.Time) Snapshot {
	var rows []marketdata.SourceRow
	for i, d := range dates {
		rows = append(rows, tradeRow(i+1, d))
	}
	return snapshotOf(name, rows...)
}

func spyFile(t *testing.T, m time.Month) canonical.File {
	t.Helper()
	f, err := canonical.Read(context.Background(), canonical.Key{Source: "stooq", Symbol: "SPY", Interval: canonical.D1, Year: 2024, Month: m})
	require.NoError(t, err)
	return f
}

func TestBuildSeries_WritesSessionTimesProvenanceAndTheArchive(t *testing.T) {
	storetest.Root(t)
	s := days("one", day(time.March, 8), day(time.March, 11), day(time.November, 29))
	s.Member, s.ArchiveSHA256 = "m", "sha256:zip"

	got, err := BuildSeries(context.Background(), s, builtAt)

	require.NoError(t, err)
	assert.Equal(t, canonical.Fingerprint([]byte("one")), got.Fingerprint)
	assert.Equal(t, "2026-09-10", got.AsOf)
	require.Len(t, got.Months, 2)
	assert.Equal(t, Result{Source: "stooq", Instrument: "SPY", Interval: "d1", Year: 2024, Month: time.March,
		Path: "stooq/canonical/SPY/2024/03/SPY-2024-03-d1.csv", Action: Built, Good: 2}, got.Months[0])
	raw, err := store.Read(got.SnapshotPath)
	require.NoError(t, err)
	assert.Equal(t, "one", string(raw))

	march := spyFile(t, time.March)
	assert.Equal(t, canonical.Meta{RawFingerprint: "one3", BuilderVersion: TradeBuilderVersion, ValidatorVersion: TradeValidatorVersion,
		AvailabilityPolicy: "us-equity-close-v1", BuiltAt: builtAt, Trade: &canonical.TradeMeta{
			PriceBasis: "trade", Currency: "USD", VolumeUnit: "shares", Adjustment: "total-return", AdjustmentEvidence: "observed",
			SnapshotFingerprint: got.Fingerprint, SnapshotAsOf: "2026-09-10", Calendar: "us-equity-calendar-v2",
		}}, march.Meta)
	assert.Equal(t, utc(2024, 3, 8, 14).Add(30*time.Minute), march.Bars[0].Time, "09:30 New York, standard time")
	assert.Equal(t, utc(2024, 3, 8, 21), march.Bars[0].AvailableAt)
	assert.Equal(t, utc(2024, 3, 11, 20), march.Bars[1].AvailableAt, "daylight time")
	assert.Equal(t, utc(2024, 11, 29, 18), spyFile(t, time.November).Bars[0].AvailableAt, "early close")
	assert.Equal(t, s.Partitions[0].Rows[0].Bar.Trade, march.Bars[0].Trade)
	assert.True(t, march.Bars[0].Mid.IsZero() && march.Bars[0].Bid.IsZero() && march.Bars[0].Ask.IsZero())
}

func TestBuildSeries_ClassifiesEveryRejectionOverTheWholeHistory(t *testing.T) {
	storetest.Root(t)
	rows := []marketdata.SourceRow{
		tradeRow(1, day(time.March, 4)),
		tradeRow(2, day(time.March, 29)), // Good Friday
		tradeRow(3, day(time.March, 30)), // Saturday
		tradeRow(4, day(time.April, 2)),
		tradeRow(5, day(time.March, 5)), // earlier than the row before, in another month
		tradeRow(6, day(time.April, 2)), // later than row 5, and a repeat
		tradeRow(7, day(time.April, 3)),
		tradeRow(8, day(time.April, 4)),
		tradeRow(9, day(time.April, 5)),
		tradeRow(10, day(time.April, 8)),
	}
	rows[6].Unrepresentable, rows[6].Bar.Trade, rows[6].Bar.Volume = true, marketdata.OHLC{}, 0
	rows[7].Bar.Trade.High = px("1") // impossible shape
	rows[8].Complete = false
	rows[9].Bar.Bid = rows[9].Bar.Trade // a quote has no place in a trade file

	got, err := BuildSeries(context.Background(), snapshotOf("bad", rows...), builtAt)

	require.NoError(t, err)
	reasons := map[int]canonical.Reason{}
	good := 0
	for _, m := range []time.Month{time.March, time.April} {
		f := spyFile(t, m)
		good += len(f.Bars)
		for _, b := range f.Bad {
			reasons[b.RawRow] = b.Reason
		}
	}
	assert.Equal(t, map[int]canonical.Reason{2: "non-session", 3: "non-session", 5: "out-of-order", 6: "duplicate",
		7: "invalid", 8: "invalid", 9: "incomplete", 10: "invalid"}, reasons)
	assert.Equal(t, 2, good)
	assert.Equal(t, len(rows), good+len(reasons), "no row is lost silently")
	assert.Equal(t, 3, got.Months[0].Bad)
}

func TestBuildSeries_ARerunIsUnchangedAndKeepsTheRecordedDate(t *testing.T) {
	storetest.Root(t)
	s := days("one", day(time.March, 4))
	_, err := BuildSeries(context.Background(), s, builtAt)
	require.NoError(t, err)

	for _, asOf := range []string{"", "2027-01-01"} {
		s.AsOf = asOf
		got, err := BuildSeries(context.Background(), s, builtAt.Add(time.Hour))

		require.NoError(t, err)
		assert.Equal(t, Unchanged, got.Months[0].Action)
		assert.Equal(t, "2026-09-10", got.AsOf)
	}
}

func TestBuildSeries_ANewSnapshotReplacesEveryMonth(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	_, err := BuildSeries(ctx, days("one", day(time.March, 4)), builtAt)
	require.NoError(t, err)

	got, err := BuildSeries(ctx, days("two", day(time.March, 4), day(time.April, 1)), builtAt)

	require.NoError(t, err)
	assert.Equal(t, []Action{Replaced, Built}, []Action{got.Months[0].Action, got.Months[1].Action})
	assert.Equal(t, got.Fingerprint, spyFile(t, time.March).Meta.Trade.SnapshotFingerprint)
	old, err := store.List("stooq/snapshots/SPY")
	require.NoError(t, err)
	assert.Len(t, old, 4, "both snapshots stay archived")
}

func TestBuildSeries_RefusesASnapshotThatWouldLeaveAnOldMonthBehind(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	_, err := BuildSeries(ctx, days("one", day(time.March, 4), day(time.April, 1)), builtAt)
	require.NoError(t, err)
	before, err := store.List("stooq")
	require.NoError(t, err)

	_, err = BuildSeries(ctx, days("two", day(time.April, 1)), builtAt)

	assert.ErrorIs(t, err, ErrSnapshotConflict)
	assert.ErrorContains(t, err, "2024-03")
	after, lerr := store.List("stooq")
	require.NoError(t, lerr)
	assert.Equal(t, before, after, "nothing is written")
	assert.Equal(t, canonical.Fingerprint([]byte("one")), spyFile(t, time.April).Meta.Trade.SnapshotFingerprint)
}

func TestBuildSeries_RefusalsWriteNothing(t *testing.T) {
	good := func() Snapshot { return days("one", day(time.March, 4), day(time.April, 1)) }
	for name, tc := range map[string]struct {
		mutate func(*Snapshot)
		want   error
	}{
		"no bytes":               {func(s *Snapshot) { s.Data = nil }, ErrInvalidRequest},
		"no partitions":          {func(s *Snapshot) { s.Partitions = nil }, ErrInvalidRequest},
		"another symbol":         {func(s *Snapshot) { s.Partitions[0].Instrument = "IWM" }, ErrUnsupportedSeries},
		"another source":         {func(s *Snapshot) { s.Partitions[0].Source = "alpaca" }, ErrUnsupportedSeries},
		"mixed series":           {func(s *Snapshot) { s.Partitions[1].Interval = "h1" }, marketdata.ErrInvalidSource},
		"months unordered":       {func(s *Snapshot) { s.Partitions[0], s.Partitions[1] = s.Partitions[1], s.Partitions[0] }, marketdata.ErrInvalidSource},
		"no fingerprint":         {func(s *Snapshot) { s.Partitions[0].Fingerprint = "" }, marketdata.ErrInvalidSource},
		"row used twice":         {func(s *Snapshot) { s.Partitions[1].Rows[0].Row = 1 }, marketdata.ErrInvalidSource},
		"row of another month":   {func(s *Snapshot) { s.Partitions[0].Rows[0].Bar.Time = day(time.May, 1) }, marketdata.ErrInvalidSource},
		"row with a time of day": {func(s *Snapshot) { s.Partitions[0].Rows[0].Bar.Time = utc(2024, 3, 4, 9) }, marketdata.ErrInvalidSource},
		"outside the calendar": {func(s *Snapshot) {
			s.Partitions = s.Partitions[:1]
			s.Partitions[0].Year, s.Partitions[0].Rows[0].Bar.Time = 2027, utc(2027, 3, 4, 0)
		}, canonical.ErrOutsideCalendar},
		"no date":              {func(s *Snapshot) { s.AsOf = "" }, ErrSnapshotDate},
		"malformed date":       {func(s *Snapshot) { s.AsOf = "yesterday" }, ErrSnapshotDate},
		"date before the data": {func(s *Snapshot) { s.AsOf = "2024-03-31" }, ErrSnapshotDate},
	} {
		t.Run(name, func(t *testing.T) {
			storetest.Root(t)
			s := good()
			tc.mutate(&s)

			_, err := BuildSeries(context.Background(), s, builtAt)

			assert.ErrorIs(t, err, tc.want)
			files, lerr := store.List("")
			require.NoError(t, lerr)
			assert.Empty(t, files)
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := BuildSeries(cancelled, good(), builtAt)
	assert.ErrorIs(t, err, context.Canceled)
}
