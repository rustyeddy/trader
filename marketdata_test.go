package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBars_YieldsAllInOrder(t *testing.T) {
	md := &marketData{data: testBars(px(1), px(2), px(3))}
	var got []price
	for b, err := range md.bars(context.Background()) {
		require.NoError(t, err)
		got = append(got, b.c)
	}
	require.Equal(t, []price{px(1), px(2), px(3)}, got)
}

func TestBars_EarlyBreak(t *testing.T) {
	md := &marketData{data: testBars(px(1), px(2), px(3))}
	n := 0
	for range md.bars(context.Background()) {
		n++
		break
	}
	require.Equal(t, 1, n)
}

func TestBars_ContextCancelled(t *testing.T) {
	md := &marketData{data: testBars(px(1), px(2), px(3))}
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

func TestBars_CancelledAfterTheLastBar(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	md := &marketData{data: testBars(px(1))}

	var bars, errs int
	var gotErr error
	for _, err := range md.bars(ctx) {
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
	md := &marketData{data: testBars(px(1))}

	var items int
	for range md.bars(ctx) {
		items++
		cancel()
		break
	}

	assert.Equal(t, 1, items)
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
		{px(0), 0},
		{px(110000), 1.1},
		{px(1), 0.00001},
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
	md := &marketData{data: testBars(px(110000), px(110100), px(110200))}

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
