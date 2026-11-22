package trader

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/rustyeddy/trader/indicators"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// atrInstrument is deliberately not in the instruments registry: a Study must
// not need one.
const atrInstrument = "STUDYONLY"

var atrStart = time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)

func atrBar(day int, hN int64, lN int64, cN int64) marketdata.Bar {
	h, l, c := px(hN), px(lN), px(cN)
	return marketdata.Bar{
		Instrument: atrInstrument,
		Time:       atrStart.AddDate(0, 0, day),
		Mid:        marketdata.OHLC{Open: l, High: h, Low: l, Close: c},
	}
}

// atrFixture has hand-worked true ranges, in price points:
//
//	day  high    low     close   prev    h-l  h-prev  prev-l  TR
//	0    110100  109900  110050  -       -    -       -       none
//	1    110150  110000  110100  110050  150  100     50      150  range
//	2    110500  110400  110450  110100  100  400     -300    400  gap up
//	3    110200  109980  110100  110450  220  -250    470     470  gap down
//	4    110210  110030  110100  110100  180  110     70      180  range
//	5    110220  110000  110150  110100  220  120     100     220  range
func atrFixture() []marketdata.Bar {
	return []marketdata.Bar{
		atrBar(0, 110100, 109900, 110050),
		atrBar(1, 110150, 110000, 110100),
		atrBar(2, 110500, 110400, 110450),
		atrBar(3, 110200, 109980, 110100),
		atrBar(4, 110210, 110030, 110100),
		atrBar(5, 110220, 110000, 110150),
	}
}

func studyBars(bars []marketdata.Bar) iter.Seq2[marketdata.Bar, error] {
	md := marketdata.Series{Bars: bars}
	return seriesBars(context.Background(), md)
}

func TestRunATRStudy_ReportsEachCompletedWindow(t *testing.T) {
	got, err := runATRStudy(studyBars(atrFixture()), 3)

	require.NoError(t, err)
	// The ATR is a float, the ADR-045 boundary: compared to the float's resolution.
	want := []atrObservation{
		{atrInstrument, atrStart.AddDate(0, 0, 3), 0.0034}, // (150+400+470)/3 = 340 points
		{atrInstrument, atrStart.AddDate(0, 0, 4), 0.0035}, // (400+470+180)/3 = 350 points
		{atrInstrument, atrStart.AddDate(0, 0, 5), 0.0029}, // (470+180+220)/3 = 290 points
	}
	require.Len(t, got, len(want))
	for i := range want {
		assert.Equal(t, want[i].instrument, got[i].instrument)
		assert.Equal(t, want[i].opened, got[i].opened)
		assert.InDelta(t, want[i].value, got[i].value, 1e-12)
	}
}

func TestRunATRStudy_Warmup(t *testing.T) {
	tests := []struct {
		name string
		bars int
		want int
	}{
		{"no bars", 0, 0},
		{"first bar only supplies a close", 1, 0},
		{"one bar short of a full window", 3, 0},
		{"exactly window plus one", 4, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runATRStudy(studyBars(atrFixture()[:tt.bars]), 3)
			require.NoError(t, err)
			assert.Len(t, got, tt.want)
		})
	}
}

// A value is fixed once its bar has been seen. The study never revises one, and
// no bar can influence a measurement stamped before it.
func TestRunATRStudy_LaterBarsDoNotChangeEarlierValues(t *testing.T) {
	bars := atrFixture()
	full, err := runATRStudy(studyBars(bars), 3)
	require.NoError(t, err)
	require.Len(t, full, 3)

	for n := 4; n < len(bars); n++ {
		prefix, err := runATRStudy(studyBars(bars[:n]), 3)
		require.NoError(t, err)
		assert.Equal(t, full[:n-3], prefix, "first %d bars", n)
	}
}

func TestRunATRStudy_RepeatedRunsAreIdentical(t *testing.T) {
	first, err := runATRStudy(studyBars(atrFixture()), 2)
	require.NoError(t, err)
	second, err := runATRStudy(studyBars(atrFixture()), 2)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestRunATRStudy_ReturnsNoPartialResultsOnADataError(t *testing.T) {
	boom := errors.New("bad row")
	bars := func(yield func(marketdata.Bar, error) bool) {
		for _, b := range atrFixture() {
			if !yield(b, nil) {
				return
			}
		}
		yield(marketdata.Bar{}, boom)
	}

	got, err := runATRStudy(bars, 3)

	require.ErrorIs(t, err, boom)
	assert.Nil(t, got, "the fixture completes three values before the error")
}

func TestRunATRStudy_CancelledBeforeTheFirstBar(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	md := marketdata.Series{Bars: atrFixture()}

	got, err := runATRStudy(seriesBars(ctx, md), 3)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

func TestRunATRStudy_CancelledPartWayThrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	md := marketdata.Series{Bars: atrFixture()}
	bars := func(yield func(marketdata.Bar, error) bool) {
		seen := 0
		for b, err := range seriesBars(ctx, md) {
			if !yield(b, err) {
				return
			}
			if seen++; seen == 5 { // four are enough for one value
				cancel()
			}
		}
	}

	got, err := runATRStudy(bars, 3)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

// The indicator reports a bad window as an error (it used to panic in the root
// package); the study passes it on, and returns no measurements.
func TestRunATRStudy_InvalidWindowIsAnError(t *testing.T) {
	got, err := runATRStudy(studyBars(atrFixture()), 0)

	require.ErrorIs(t, err, indicators.ErrInvalidPeriod)
	assert.Nil(t, got)
}
