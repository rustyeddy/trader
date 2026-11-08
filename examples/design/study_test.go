package main

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// atrInstrument is deliberately not in the instruments registry: a Study must
// not need one.
const atrInstrument = "STUDYONLY"

var atrStart = time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)

func atrBar(day int, h, l, c price) bar {
	return bar{
		instrument: atrInstrument,
		Time:       atrStart.AddDate(0, 0, day),
		ohlc:       ohlc{o: l, h: h, l: l, c: c},
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
func atrFixture() []bar {
	return []bar{
		atrBar(0, 110100, 109900, 110050),
		atrBar(1, 110150, 110000, 110100),
		atrBar(2, 110500, 110400, 110450),
		atrBar(3, 110200, 109980, 110100),
		atrBar(4, 110210, 110030, 110100),
		atrBar(5, 110220, 110000, 110150),
	}
}

func studyBars(bars []bar) iter.Seq2[bar, error] {
	md := &marketData{data: bars}
	return md.bars(context.Background())
}

func TestRunATRStudy_ReportsEachCompletedWindow(t *testing.T) {
	got, err := runATRStudy(studyBars(atrFixture()), 3)

	require.NoError(t, err)
	assert.Equal(t, []atrObservation{
		{atrInstrument, atrStart.AddDate(0, 0, 3), 0.0034}, // (150+400+470)/3 = 340 points
		{atrInstrument, atrStart.AddDate(0, 0, 4), 0.0035}, // (400+470+180)/3 = 350 points
		{atrInstrument, atrStart.AddDate(0, 0, 5), 0.0029}, // (470+180+220)/3 = 290 points
	}, got)
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
	bars := func(yield func(bar, error) bool) {
		for _, b := range atrFixture() {
			if !yield(b, nil) {
				return
			}
		}
		yield(bar{}, boom)
	}

	got, err := runATRStudy(bars, 3)

	require.ErrorIs(t, err, boom)
	assert.Nil(t, got, "the fixture completes three values before the error")
}

func TestRunATRStudy_CancelledBeforeTheFirstBar(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	md := &marketData{data: atrFixture()}

	got, err := runATRStudy(md.bars(ctx), 3)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

func TestRunATRStudy_CancelledPartWayThrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	md := &marketData{data: atrFixture()}
	bars := func(yield func(bar, error) bool) {
		seen := 0
		for b, err := range md.bars(ctx) {
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

func TestRunATRStudy_InvalidWindowPanics(t *testing.T) {
	assert.Panics(t, func() { _, _ = runATRStudy(studyBars(atrFixture()), 0) })
}
