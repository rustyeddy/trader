package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBars_YieldsAllInOrder(t *testing.T) {
	md := &marketData{data: testBars(1, 2, 3)}
	var got []price
	for b, err := range md.bars(context.Background()) {
		require.NoError(t, err)
		got = append(got, b.c)
	}
	require.Equal(t, []price{1, 2, 3}, got)
}

func TestBars_EarlyBreak(t *testing.T) {
	md := &marketData{data: testBars(1, 2, 3)}
	n := 0
	for range md.bars(context.Background()) {
		n++
		break
	}
	require.Equal(t, 1, n)
}

func TestBars_ContextCancelled(t *testing.T) {
	md := &marketData{data: testBars(1, 2, 3)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var bars int
	var gotErr error
	for _, err := range md.bars(ctx) {
		if err != nil {
			gotErr = err
			break
		}
		bars++
		cancel() // cancel after the first bar
	}
	require.Equal(t, 1, bars)
	require.ErrorIs(t, gotErr, context.Canceled)
}

func TestBars_Empty(t *testing.T) {
	md := &marketData{}
	for range md.bars(context.Background()) {
		t.Fatal("no bars expected")
	}
}

func TestPrice_Float64(t *testing.T) {
	tests := []struct {
		p    price
		want float64
	}{
		{0, 0},
		{110000, 1.1},
		{1, 0.00001},
		{-250000, -2.5},
	}
	for _, tt := range tests {
		assert.InDelta(t, tt.want, tt.p.Float64(), 1e-12)
	}
}

// testBars builds bars whose closes are the given integer points.
func testBars(closes ...price) []bar {
	bars := make([]bar, len(closes))
	for i, c := range closes {
		bars[i] = bar{instrument: "TEST", Time: testStart.AddDate(0, 0, i), ohlc: ohlc{c: c}}
	}
	return bars
}

func TestBars_YieldsInstrumentAndTime(t *testing.T) {
	md := &marketData{data: testBars(110000, 110100, 110200)}

	var n int
	for b, err := range md.bars(context.Background()) {
		require.NoError(t, err)
		assert.Equal(t, md.data[n], b)
		assert.Equal(t, "TEST", b.instrument)
		assert.Equal(t, testStart.AddDate(0, 0, n), b.Time)
		n++
	}
	assert.Equal(t, 3, n)
}

const barsHeader = "# schema=design-bars-v1 source=synthetic instrument=EURUSD tf=d1 year=2010 month=01 scenario=test\n" +
	"time,open,high,low,close,volume\n"

func TestReadBars_ParsesExactPointsInOrder(t *testing.T) {
	md, err := readBars(strings.NewReader(barsHeader +
		"2010-01-04T00:00:00Z,1.10323,1.10650,1.09197,1.10001,1000\n" +
		"2010-01-05T00:00:00Z,1.10015,1.10130,1.10005,1.10100,0\n"))
	require.NoError(t, err)

	assert.Equal(t, "EURUSD", md.instrument)
	assert.Equal(t, "d1", md.interval)
	first := time.Date(2010, time.January, 4, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 1)
	assert.Equal(t, []bar{
		{instrument: "EURUSD", Time: first, ohlc: ohlc{o: 110323, h: 110650, l: 109197, c: 110001, v: 1000}, availableAt: second},
		{instrument: "EURUSD", Time: second, ohlc: ohlc{o: 110015, h: 110130, l: 110005, c: 110100}, availableAt: second.AddDate(0, 0, 1)},
	}, md.data)
}

func TestReadBars_AvailableWhenTheIntervalEnds(t *testing.T) {
	open := time.Date(2010, time.January, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		tf   string
		want time.Time
	}{
		{"h1", open.Add(time.Hour)},
		{"d1", open.AddDate(0, 0, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.tf, func(t *testing.T) {
			md, err := readBars(strings.NewReader("# schema=design-bars-v1 instrument=EURUSD tf=" + tt.tf + "\n" +
				"time,open,high,low,close,volume\n" +
				"2010-01-04T10:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n"))
			require.NoError(t, err)
			require.Len(t, md.data, 1)
			assert.Equal(t, open, md.data[0].Time, "the opening time is unchanged")
			assert.Equal(t, tt.want, md.data[0].availableAt)
		})
	}
}

func TestReadBars_RefusesAnUnsupportedInterval(t *testing.T) {
	for _, tf := range []string{"w1", "h2", "H1"} {
		t.Run(tf, func(t *testing.T) {
			_, err := readBars(strings.NewReader("# schema=design-bars-v1 instrument=EURUSD tf=" + tf + "\n" +
				"time,open,high,low,close,volume\n" +
				"2010-01-04T10:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n"))
			require.ErrorContains(t, err, "unsupported tf")
		})
	}
}

// mergeStart is a Monday, so bar labels read as hours from 00:00.
var mergeStart = time.Date(2010, time.January, 4, 0, 0, 0, 0, time.UTC)

// barSet builds one instrument's bars, each opening the given offset after
// mergeStart and lasting the given duration. Its closes count up from 100000.
func barSet(instrument string, lasts time.Duration, opens ...time.Duration) *marketData {
	md := &marketData{instrument: instrument}
	for i, offset := range opens {
		open := mergeStart.Add(offset)
		md.data = append(md.data, bar{
			instrument:  instrument,
			Time:        open,
			ohlc:        ohlc{c: price(100000 + i)},
			availableAt: open.Add(lasts),
		})
	}
	return md
}

// label names a bar by instrument and opening time, for readable expectations.
func label(b bar) string {
	return b.instrument + " " + b.Format("15:04")
}

func delivered(t *testing.T, sets ...*marketData) []string {
	t.Helper()
	var got []string
	for b, err := range mergedBars(context.Background(), sets...) {
		require.NoError(t, err)
		got = append(got, label(b))
	}
	return got
}

// frameLabels gives each frame as its bars' labels.
func frameLabels(t *testing.T, sets ...*marketData) [][]string {
	t.Helper()
	var got [][]string
	for f, err := range frames(mergedBars(context.Background(), sets...)) {
		require.NoError(t, err)
		var labels []string
		for _, b := range f.bars {
			assert.Equal(t, f.availableAt, b.availableAt)
			labels = append(labels, label(b))
		}
		got = append(got, labels)
	}
	return got
}

func TestMergedBars_SimultaneousBarsAreOrderedByInstrument(t *testing.T) {
	eur := barSet("EURUSD", time.Hour, 0, time.Hour)
	gbp := barSet("GBPUSD", time.Hour, 0, time.Hour)
	want := []string{"EURUSD 00:00", "GBPUSD 00:00", "EURUSD 01:00", "GBPUSD 01:00"}

	assert.Equal(t, want, delivered(t, eur, gbp))
	assert.Equal(t, want, delivered(t, gbp, eur), "the order of the sets must not matter")
}

func TestMergedBars_OffsetTimestampsInterleave(t *testing.T) {
	eur := barSet("EURUSD", time.Hour, 10*time.Hour, 11*time.Hour)
	gbp := barSet("GBPUSD", time.Hour, 10*time.Hour+30*time.Minute)

	assert.Equal(t, []string{"EURUSD 10:00", "GBPUSD 10:30", "EURUSD 11:00"}, delivered(t, gbp, eur))
}

// The D1 bar opens first and its name sorts first, but its OHLC is not known
// until the day ends. Delivering it by opening time would be look-ahead.
func TestMergedBars_ALongerBarArrivesWhenItCompletes(t *testing.T) {
	daily := barSet("AUDUSD", 24*time.Hour, 0)
	hourly := barSet("EURUSD", time.Hour, 0, 23*time.Hour)

	assert.Equal(t, []string{"EURUSD 00:00", "AUDUSD 00:00", "EURUSD 23:00"}, delivered(t, daily, hourly))
}

func TestMergedBars_Empty(t *testing.T) {
	assert.Empty(t, delivered(t))
	assert.Empty(t, delivered(t, barSet("EURUSD", time.Hour)))
}

func TestMergedBars_SingleSetIsUnchanged(t *testing.T) {
	assert.Equal(t, []string{"EURUSD 00:00", "EURUSD 01:00"}, delivered(t, barSet("EURUSD", time.Hour, 0, time.Hour)))
}

func TestMergedBars_RefusesBadInput(t *testing.T) {
	noAvailability := barSet("EURUSD", time.Hour, 0)
	noAvailability.data[0].availableAt = time.Time{}

	tests := []struct {
		name string
		sets []*marketData
		want string
	}{
		{"same instrument twice", []*marketData{barSet("EURUSD", time.Hour, 0), barSet("EURUSD", time.Hour, time.Hour)}, "given twice"},
		{"times out of order", []*marketData{barSet("EURUSD", time.Hour, time.Hour, 0)}, "not available after"},
		{"repeated time", []*marketData{barSet("EURUSD", time.Hour, 0, 0)}, "not available after"},
		{"no availability", []*marketData{noAvailability}, "no availability"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var items int
			var gotErr error
			for b, err := range mergedBars(context.Background(), tt.sets...) {
				items++
				assert.Empty(t, b.instrument, "no bar may be delivered")
				gotErr = err
			}
			assert.Equal(t, 1, items)
			require.ErrorContains(t, gotErr, tt.want)
		})
	}
}

func TestMergedBars_LeavesTheSetsUnchanged(t *testing.T) {
	gbp := barSet("GBPUSD", time.Hour, 0, time.Hour)
	eur := barSet("EURUSD", time.Hour, 0, time.Hour)
	gbp.data = slices.Grow(gbp.data, len(eur.data)) // room to merge in place, if it wrongly tried
	want := slices.Clone(gbp.data)

	delivered(t, gbp, eur)

	assert.Equal(t, want, gbp.data)
}

func TestMergedBars_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seq := mergedBars(ctx, barSet("EURUSD", time.Hour, 0, time.Hour), barSet("GBPUSD", time.Hour, 0))

	var bars int
	var gotErr error
	for _, err := range seq {
		if err != nil {
			gotErr = err
			break
		}
		bars++
		cancel()
	}

	assert.Equal(t, 1, bars)
	require.ErrorIs(t, gotErr, context.Canceled)
}

func TestMergedBars_EarlyBreak(t *testing.T) {
	var bars int
	for range mergedBars(context.Background(), barSet("EURUSD", time.Hour, 0, time.Hour)) {
		bars++
		break
	}
	assert.Equal(t, 1, bars)
}

func TestFrames_GroupsSimultaneousBars(t *testing.T) {
	got := frameLabels(t, barSet("GBPUSD", time.Hour, 0, time.Hour), barSet("EURUSD", time.Hour, 0, time.Hour))

	assert.Equal(t, [][]string{
		{"EURUSD 00:00", "GBPUSD 00:00"},
		{"EURUSD 01:00", "GBPUSD 01:00"},
	}, got)
}

func TestFrames_AMissingBarIsAbsentNotCarried(t *testing.T) {
	got := frameLabels(t,
		barSet("EURUSD", time.Hour, 0, time.Hour, 2*time.Hour),
		barSet("GBPUSD", time.Hour, 0, 2*time.Hour)) // no 01:00 bar

	assert.Equal(t, [][]string{
		{"EURUSD 00:00", "GBPUSD 00:00"},
		{"EURUSD 01:00"},
		{"EURUSD 02:00", "GBPUSD 02:00"},
	}, got)
}

func TestFrames_AGapInEveryInstrumentProducesNoFrame(t *testing.T) {
	got := frameLabels(t,
		barSet("EURUSD", time.Hour, 0, 3*time.Hour),
		barSet("GBPUSD", time.Hour, 0, 3*time.Hour))

	assert.Equal(t, [][]string{
		{"EURUSD 00:00", "GBPUSD 00:00"},
		{"EURUSD 03:00", "GBPUSD 03:00"},
	}, got)
}

func TestFrames_OffsetBarsAreSeparateFrames(t *testing.T) {
	got := frameLabels(t,
		barSet("EURUSD", time.Hour, 0),
		barSet("GBPUSD", time.Hour, 30*time.Minute))

	assert.Equal(t, [][]string{{"EURUSD 00:00"}, {"GBPUSD 00:30"}}, got)
}

func TestFrames_Empty(t *testing.T) {
	assert.Empty(t, frameLabels(t))
}

func TestFrames_ReportsAvailability(t *testing.T) {
	seq := frames(mergedBars(context.Background(), barSet("EURUSD", time.Hour, 0)))

	for f, err := range seq {
		require.NoError(t, err)
		assert.Equal(t, mergeStart.Add(time.Hour), f.availableAt)
	}
}

// The second frame has one bar when the error arrives, and might have had more.
func TestFrames_AnErrorDropsTheFrameBeingBuilt(t *testing.T) {
	boom := errors.New("bad row")
	sets := barSet("EURUSD", time.Hour, 0, time.Hour)
	seq := func(yield func(bar, error) bool) {
		for _, b := range sets.data {
			if !yield(b, nil) {
				return
			}
		}
		yield(bar{}, boom)
	}

	var got []frame
	var gotErr error
	for f, err := range frames(seq) {
		if err != nil {
			gotErr = err
			continue
		}
		got = append(got, f)
	}

	require.ErrorIs(t, gotErr, boom)
	require.Len(t, got, 1)
	assert.Equal(t, mergeStart, got[0].bars[0].Time)
}

func TestFrames_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var gotErr error
	for f, err := range frames(mergedBars(ctx, barSet("EURUSD", time.Hour, 0))) {
		assert.Empty(t, f.bars)
		gotErr = err
	}

	require.ErrorIs(t, gotErr, context.Canceled)
}

func TestFrames_EarlyBreak(t *testing.T) {
	var n int
	for range frames(mergedBars(context.Background(), barSet("EURUSD", time.Hour, 0, time.Hour, 2*time.Hour))) {
		n++
		break
	}
	assert.Equal(t, 1, n)
}

// A consumer of single bars acts between the two bars of a moment, so it can see
// one instrument's new close beside the other's old one. A consumer of frames
// never does. This is why trading needs frames and a per-instrument consumer
// does not.
func TestDelivery_BarsCanMixMomentsAndFramesCannot(t *testing.T) {
	sets := func() []*marketData {
		return []*marketData{barSet("EURUSD", time.Hour, 0, time.Hour), barSet("GBPUSD", time.Hour, 0, time.Hour)}
	}
	closeSum := func(latest map[string]price) price {
		return latest["EURUSD"] + latest["GBPUSD"]
	}

	var byBar []price
	latest := map[string]price{}
	for b, err := range mergedBars(context.Background(), sets()...) {
		require.NoError(t, err)
		latest[b.instrument] = b.c
		if len(latest) == 2 {
			byBar = append(byBar, closeSum(latest))
		}
	}

	var byFrame []price
	latest = map[string]price{}
	for f, err := range frames(mergedBars(context.Background(), sets()...)) {
		require.NoError(t, err)
		for _, b := range f.bars {
			latest[b.instrument] = b.c
		}
		byFrame = append(byFrame, closeSum(latest))
	}

	// Closes are 100000 then 100001 for each instrument.
	assert.Equal(t, []price{200000, 200001, 200002}, byBar, "200001 is EUR's 01:00 close beside GBP's 00:00 close")
	assert.Equal(t, []price{200000, 200002}, byFrame)
}

func TestReadBars_RefusesMalformedInput(t *testing.T) {
	const row = "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n"
	for name, in := range map[string]string{
		"empty":            "",
		"header only line": "# schema=design-bars-v1 instrument=EURUSD tf=d1",
		"not a comment":    "schema=design-bars-v1 instrument=EURUSD tf=d1\n" + row,
		"wrong schema":     "# schema=canonical-v1 instrument=EURUSD tf=d1\ntime,open,high,low,close,volume\n" + row,
		"no instrument":    "# schema=design-bars-v1 tf=d1\ntime,open,high,low,close,volume\n" + row,
		"no interval":      "# schema=design-bars-v1 instrument=EURUSD\ntime,open,high,low,close,volume\n" + row,
		"no columns":       "# schema=design-bars-v1 instrument=EURUSD tf=d1\n",
		"wrong columns":    "# schema=design-bars-v1 instrument=EURUSD tf=d1\ntime,high,open,low,close,volume\n" + row,
		"quoted five-field columns": "# schema=design-bars-v1 instrument=EURUSD tf=d1\n\"time,open\",high,low,close,volume\n" +
			"2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050\n",
		"quoted one-field columns": "# schema=design-bars-v1 instrument=EURUSD tf=d1\n\"time,open,high,low,close,volume\"\n" +
			"2010-01-04T00:00:00Z\n",
		"no rows":          barsHeader,
		"short row":        barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100\n",
		"date only":        barsHeader + "2010-01-04,1.10000,1.10100,1.09900,1.10050,1000\n",
		"offset time":      barsHeader + "2010-01-04T00:00:00+01:00,1.10000,1.10100,1.09900,1.10050,1000\n",
		"four decimals":    barsHeader + "2010-01-04T00:00:00Z,1.1000,1.10100,1.09900,1.10050,1000\n",
		"six decimals":     barsHeader + "2010-01-04T00:00:00Z,1.100000,1.10100,1.09900,1.10050,1000\n",
		"no whole part":    barsHeader + "2010-01-04T00:00:00Z,.10000,1.10100,1.09900,1.10050,1000\n",
		"signed price":     barsHeader + "2010-01-04T00:00:00Z,-1.10000,1.10100,1.09900,1.10050,1000\n",
		"signed fraction":  barsHeader + "2010-01-04T00:00:00Z,1.+1000,1.10100,1.09900,1.10050,1000\n",
		"zero price":       barsHeader + "2010-01-04T00:00:00Z,0.00000,1.10100,1.09900,1.10050,1000\n",
		"overflow":         barsHeader + "2010-01-04T00:00:00Z,99999999999999999.00000,1.10100,1.09900,1.10050,1000\n",
		"above the bound":  barsHeader + "2010-01-04T00:00:00Z,10000.00001,10000.00002,10000.00001,10000.00001,1000\n",
		"high below open":  barsHeader + "2010-01-04T00:00:00Z,1.10200,1.10100,1.09900,1.10050,1000\n",
		"high below close": barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10150,1000\n",
		"low above open":   barsHeader + "2010-01-04T00:00:00Z,1.09800,1.10100,1.09900,1.10050,1000\n",
		"low above close":  barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.09800,1000\n",
		"bad volume":       barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050,many\n",
		"negative volume":  barsHeader + "2010-01-04T00:00:00Z,1.10000,1.10100,1.09900,1.10050,-1\n",
		"duplicate time":   barsHeader + row + row,
		"out of order":     barsHeader + row + "2010-01-03T00:00:00Z,1.10000,1.10100,1.09900,1.10050,1000\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readBars(strings.NewReader(in))
			require.Error(t, err)
		})
	}
}

func TestLoadBars_NamesTheFileInErrors(t *testing.T) {
	dir := t.TempDir()

	_, err := loadBars(filepath.Join(dir, "missing.csv"))
	require.ErrorIs(t, err, os.ErrNotExist)

	path := filepath.Join(dir, "bad.csv")
	require.NoError(t, os.WriteFile(path, []byte(barsHeader), 0o600))
	_, err = loadBars(path)
	require.ErrorContains(t, err, path)
}
