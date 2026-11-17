package trader

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBars_YieldsAllInOrder(t *testing.T) {
	md := marketdata.Series{Bars: testBars(px(1), px(2), px(3))}
	var got []price
	for b, err := range seriesBars(context.Background(), md) {
		require.NoError(t, err)
		got = append(got, b.OHLC().Close)
	}
	require.Equal(t, []price{px(1), px(2), px(3)}, got)
}

func TestBars_EarlyBreak(t *testing.T) {
	md := marketdata.Series{Bars: testBars(px(1), px(2), px(3))}
	n := 0
	for range seriesBars(context.Background(), md) {
		n++
		break
	}
	require.Equal(t, 1, n)
}

func TestBars_ContextCancelled(t *testing.T) {
	md := marketdata.Series{Bars: testBars(px(1), px(2), px(3))}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var bars int
	var gotErr error
	for _, err := range seriesBars(ctx, md) {
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

func TestBars_CancelledAfterTheLastBar(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	md := marketdata.Series{Bars: testBars(px(1))}

	var bars, errs int
	var gotErr error
	for _, err := range seriesBars(ctx, md) {
		if err != nil {
			errs++
			gotErr = err
			continue
		}
		bars++
		cancel()
	}

	assert.Equal(t, 1, bars)
	assert.Equal(t, 1, errs, "the error is yielded once")
	require.ErrorIs(t, gotErr, context.Canceled)
}

func TestBars_EarlyBreakOnTheLastBarAfterCancelYieldsNothingMore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	md := marketdata.Series{Bars: testBars(px(1))}

	var items int
	for range seriesBars(ctx, md) {
		items++
		cancel()
		break
	}

	assert.Equal(t, 1, items)
}

func TestBars_Empty(t *testing.T) {
	md := marketdata.Series{}
	for range seriesBars(context.Background(), md) {
		t.Fatal("no bars expected")
	}
}

// testBars builds bars whose closes are the given integer points.
func testBars(closes ...price) []marketdata.Bar {
	bars := make([]marketdata.Bar, len(closes))
	for i, c := range closes {
		bars[i] = marketdata.Bar{Instrument: "TEST", Time: testStart.AddDate(0, 0, i), Mid: marketdata.OHLC{Close: c}}
	}
	return bars
}

func TestBars_YieldsInstrumentAndTime(t *testing.T) {
	md := marketdata.Series{Bars: testBars(px(110000), px(110100), px(110200))}

	var n int
	for b, err := range seriesBars(context.Background(), md) {
		require.NoError(t, err)
		assert.Equal(t, md.Bars[n], b)
		assert.Equal(t, "TEST", b.Instrument)
		assert.Equal(t, testStart.AddDate(0, 0, n), b.Time)
		n++
	}
	assert.Equal(t, 3, n)
}

// mergeStart is a Monday, so bar labels read as hours from 00:00.
var mergeStart = time.Date(2010, time.January, 4, 0, 0, 0, 0, time.UTC)

// barSet builds one instrument's bars, each opening the given offset after
// mergeStart and lasting the given duration. Its closes count up from 100000.
func barSet(instrument string, lasts time.Duration, opens ...time.Duration) marketdata.Series {
	md := &marketdata.Series{Instrument: instrument}
	for i, offset := range opens {
		open := mergeStart.Add(offset)
		md.Bars = append(md.Bars, marketdata.Bar{
			Instrument:  instrument,
			Time:        open,
			Mid:         marketdata.OHLC{Close: px(int64(100000 + i))},
			AvailableAt: open.Add(lasts),
		})
	}
	return *md
}

// label names a bar by instrument and opening time, for readable expectations.
func label(b marketdata.Bar) string {
	return b.Instrument + " " + b.Time.Format("15:04")
}

func delivered(t *testing.T, sets ...marketdata.Series) []string {
	t.Helper()
	var got []string
	for b, err := range mergedBars(context.Background(), sets...) {
		require.NoError(t, err)
		got = append(got, label(b))
	}
	return got
}

// frameLabels gives each frame as its bars' labels.
func frameLabels(t *testing.T, sets ...marketdata.Series) [][]string {
	t.Helper()
	var got [][]string
	for f, err := range frames(mergedBars(context.Background(), sets...)) {
		require.NoError(t, err)
		var labels []string
		for _, b := range f.bars {
			assert.Equal(t, f.availableAt, b.AvailableAt)
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
	noAvailability.Bars[0].AvailableAt = time.Time{}

	daily := barSet("GBPUSD", time.Hour, 0)
	daily.Interval = "d1"
	hourly := barSet("EURUSD", time.Hour, 0)
	hourly.Interval = "h1"
	stray := barSet("EURUSD", time.Hour, 0)
	stray.Bars[0].Instrument = "GBPUSD"

	tests := []struct {
		name string
		sets []marketdata.Series
		want string
	}{
		{"mixed intervals", []marketdata.Series{hourly, daily}, "one interval"},
		{"a bar of another instrument", []marketdata.Series{stray}, "in the \"EURUSD\" series"},
		{"same instrument twice", []marketdata.Series{barSet("EURUSD", time.Hour, 0), barSet("EURUSD", time.Hour, time.Hour)}, "given twice"},
		{"times out of order", []marketdata.Series{barSet("EURUSD", time.Hour, time.Hour, 0)}, "not available after"},
		{"repeated time", []marketdata.Series{barSet("EURUSD", time.Hour, 0, 0)}, "not available after"},
		{"no availability", []marketdata.Series{noAvailability}, "no availability"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var items int
			var gotErr error
			for b, err := range mergedBars(context.Background(), tt.sets...) {
				items++
				assert.Empty(t, b.Instrument, "no bar may be delivered")
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
	gbp.Bars = slices.Grow(gbp.Bars, len(eur.Bars)) // room to merge in place, if it wrongly tried
	want := slices.Clone(gbp.Bars)

	delivered(t, gbp, eur)

	assert.Equal(t, want, gbp.Bars)
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
	seq := func(yield func(marketdata.Bar, error) bool) {
		for _, b := range sets.Bars {
			if !yield(b, nil) {
				return
			}
		}
		yield(marketdata.Bar{}, boom)
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

// frames has already read the last bar when it delivers the frame before it, so
// the end of the data must not publish that bar after a cancel.
func TestFrames_CancelledAfterTheFrameBeforeTheLast(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seq := frames(mergedBars(ctx, barSet("EURUSD", time.Hour, 0, time.Hour)))

	var got []frame
	var gotErr error
	for f, err := range seq {
		if err != nil {
			gotErr = err
			continue
		}
		got = append(got, f)
		cancel()
	}

	require.ErrorIs(t, gotErr, context.Canceled)
	require.Len(t, got, 1, "the last frame must not be delivered after the cancel")
	assert.Equal(t, mergeStart, got[0].bars[0].Time)
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
	sets := func() []marketdata.Series {
		return []marketdata.Series{barSet("EURUSD", time.Hour, 0, time.Hour), barSet("GBPUSD", time.Hour, 0, time.Hour)}
	}
	closeSum := func(latest map[string]price) price {
		return must1(latest["EURUSD"].Add(latest["GBPUSD"]))
	}

	var byBar []price
	latest := map[string]price{}
	for b, err := range mergedBars(context.Background(), sets()...) {
		require.NoError(t, err)
		latest[b.Instrument] = b.OHLC().Close
		if len(latest) == 2 {
			byBar = append(byBar, closeSum(latest))
		}
	}

	var byFrame []price
	latest = map[string]price{}
	for f, err := range frames(mergedBars(context.Background(), sets()...)) {
		require.NoError(t, err)
		for _, b := range f.bars {
			latest[b.Instrument] = b.OHLC().Close
		}
		byFrame = append(byFrame, closeSum(latest))
	}

	// Closes are 100000 then 100001 for each instrument.
	assert.Equal(t, []price{px(200000), px(200001), px(200002)}, byBar, "200001 is EUR's 01:00 close beside GBP's 00:00 close")
	assert.Equal(t, []price{px(200000), px(200002)}, byFrame)
}
