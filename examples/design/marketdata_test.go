package main

import (
	"context"
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
