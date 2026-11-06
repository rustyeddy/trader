package main

import (
	"context"
	"os"
	"path/filepath"
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

func synthMD(t *testing.T, start, end string) *marketData {
	t.Helper()
	s, err := time.Parse("2006-01-02", start)
	require.NoError(t, err)
	e, err := time.Parse("2006-01-02", end)
	require.NoError(t, err)
	return &marketData{timerange: timeRange{start: s, end: e}}
}

func TestSynthesize_WeekdaysOnly(t *testing.T) {
	// 2010-01-01 is a Friday; through the following Monday is Fri + Mon.
	md := synthMD(t, "2010-01-01", "2010-01-04")
	md.synthesize(1, 110000)
	assert.Len(t, md.data, 2)

	// A full week Mon-Sun has five bars.
	md = synthMD(t, "2010-01-04", "2010-01-10")
	md.synthesize(1, 110000)
	assert.Len(t, md.data, 5)
}

func TestSynthesize_InclusiveRange(t *testing.T) {
	md := synthMD(t, "2010-01-04", "2010-01-04") // single Monday
	md.synthesize(1, 110000)
	assert.Len(t, md.data, 1)
}

func TestSynthesize_EmptyRange(t *testing.T) {
	md := synthMD(t, "2010-01-05", "2010-01-04") // end before start
	md.synthesize(1, 110000)
	assert.Empty(t, md.data)

	md = synthMD(t, "2010-01-02", "2010-01-03") // Sat-Sun only
	md.synthesize(1, 110000)
	assert.Empty(t, md.data)
}

func TestSynthesize_Deterministic(t *testing.T) {
	a := synthMD(t, "2010-01-01", "2010-12-31")
	b := synthMD(t, "2010-01-01", "2010-12-31")
	a.synthesize(42, 110000)
	b.synthesize(42, 110000)
	require.NotEmpty(t, a.data)
	assert.Equal(t, a.data, b.data)

	c := synthMD(t, "2010-01-01", "2010-12-31")
	c.synthesize(43, 110000)
	assert.NotEqual(t, a.data, c.data)
}

func TestSynthesize_BarInvariants(t *testing.T) {
	md := synthMD(t, "2010-01-01", "2011-12-31")
	md.synthesize(1, 110000)
	require.Len(t, md.data, 521)

	assert.Equal(t, price(110000), md.data[0].o, "first bar opens at start price")
	for i, b := range md.data {
		assert.GreaterOrEqual(t, b.h, max(b.o, b.c), "bar %d high", i)
		assert.LessOrEqual(t, b.l, min(b.o, b.c), "bar %d low", i)
		assert.GreaterOrEqual(t, b.v, 1000, "bar %d volume", i)
		assert.Less(t, b.v, 10000, "bar %d volume", i)
		if i > 0 {
			assert.Equal(t, md.data[i-1].c, b.o, "bar %d opens at previous close", i)
		}
	}
}

func TestSynthesize_ResetsPreviousData(t *testing.T) {
	md := synthMD(t, "2010-01-04", "2010-01-08")
	md.synthesize(1, 110000)
	first := append([]bar(nil), md.data...)
	md.synthesize(1, 110000)
	assert.Equal(t, first, md.data, "re-synthesizing must replace, not append")
}

// testBars builds bars whose closes are the given integer points.
func testBars(closes ...price) []bar {
	bars := make([]bar, len(closes))
	for i, c := range closes {
		bars[i] = bar{instrument: "TEST", Time: testStart.AddDate(0, 0, i), ohlc: ohlc{c: c}}
	}
	return bars
}

func TestSynthesize_StampsInstrumentAndTime(t *testing.T) {
	md := synthMD(t, "2010-01-01", "2010-01-12") // Fri .. Tue, two weeks
	md.instrument = "EUR/USD"
	md.synthesize(1, 110000)
	require.NotEmpty(t, md.data)

	assert.Equal(t, time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC), md.data[0].Time)
	for i, b := range md.data {
		assert.Equal(t, "EUR/USD", b.instrument, "bar %d", i)
		assert.NotEqual(t, time.Saturday, b.Weekday(), "bar %d", i)
		assert.NotEqual(t, time.Sunday, b.Weekday(), "bar %d", i)
		if i > 0 {
			assert.True(t, b.After(md.data[i-1].Time), "bar %d must be later than the previous", i)
		}
	}
	last := md.data[len(md.data)-1]
	assert.Equal(t, time.Date(2010, 1, 12, 0, 0, 0, 0, time.UTC), last.Time)
}

func TestBars_YieldsInstrumentAndTime(t *testing.T) {
	md := synthMD(t, "2010-01-04", "2010-01-08")
	md.instrument = "GBP/USD"
	md.synthesize(1, 110000)

	var n int
	for b, err := range md.bars(context.Background()) {
		require.NoError(t, err)
		assert.Equal(t, md.data[n], b)
		assert.Equal(t, "GBP/USD", b.instrument)
		n++
	}
	assert.Equal(t, 5, n)
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
	assert.Equal(t, timeRange{start: first, end: second}, md.timerange)
	assert.Equal(t, []bar{
		{instrument: "EURUSD", Time: first, ohlc: ohlc{o: 110323, h: 110650, l: 109197, c: 110001, v: 1000}},
		{instrument: "EURUSD", Time: second, ohlc: ohlc{o: 110015, h: 110130, l: 110005, c: 110100}},
	}, md.data)
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
