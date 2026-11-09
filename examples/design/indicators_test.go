package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEMA_Warmup(t *testing.T) {
	e := newEMA(3)

	for i, b := range closes(1, 2) {
		v, ok := e.onBar(b)
		assert.False(t, ok, "bar %d", i)
		assert.Zero(t, v)
	}
	// Seeded with the SMA of the first window closes.
	v, ok := e.onBar(closes(3)[0])
	require.True(t, ok)
	assert.Equal(t, 2.0, v)
}

func TestEMA_Recurrence(t *testing.T) {
	e := newEMA(3) // alpha = 2/(3+1) = 0.5

	var got []float64
	for _, b := range closes(1, 2, 3, 4, 2) {
		if v, ok := e.onBar(b); ok {
			got = append(got, v)
		}
	}
	// seed 2; 2+0.5*(4-2)=3; 3+0.5*(2-3)=2.5
	assert.Equal(t, []float64{2, 3, 2.5}, got)
}

func TestEMA_ConstantSeries(t *testing.T) {
	e := newEMA(5)
	var v float64
	for i := 0; i < 50; i++ {
		v, _ = e.onBar(bar{ohlc: ohlc{c: 110000}})
	}
	assert.InDelta(t, 1.1, v, 1e-12)
}

func TestEMA_WindowOfOne(t *testing.T) {
	e := newEMA(1) // alpha = 1: the EMA is the close
	for _, b := range closes(1.5, 2.5, 0.5) {
		v, ok := e.onBar(b)
		require.True(t, ok)
		assert.InDelta(t, b.c.Float64(), v, 1e-12)
	}
}

func TestEMA_InvalidWindowPanics(t *testing.T) {
	assert.Panics(t, func() { newEMA(0) })
	assert.Panics(t, func() { newEMA(-1) })
}

func hlc(h, l, c price) bar {
	return bar{ohlc: ohlc{o: l, h: h, l: l, c: c}}
}

func TestATR_TrueRangeUsesThePreviousClose(t *testing.T) {
	tests := []struct {
		name    string
		bar     bar
		wantATR float64
	}{
		{"own range is largest", hlc(1100, 800, 1000), 0.003},  // 300 > 100, 200
		{"gap up above the bar", hlc(1500, 1400, 1450), 0.005}, // 1500-1000
		{"gap down below the bar", hlc(700, 600, 650), 0.004},  // 1000-600
		{"no movement is a known zero", hlc(1000, 1000, 1000), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newATR(1)
			_, ok := a.onBar(hlc(1000, 1000, 1000))
			require.False(t, ok, "the first bar has no previous close")

			got, ok := a.onBar(tt.bar)

			require.True(t, ok)
			assert.Equal(t, tt.wantATR, got)
		})
	}
}

func TestATR_DropsTheOldestTrueRange(t *testing.T) {
	a := newATR(2)
	a.onBar(hlc(1000, 1000, 1000))
	a.onBar(hlc(1100, 1000, 1000)) // true range 100

	v, ok := a.onBar(hlc(1301, 1000, 1000)) // true range 301
	require.True(t, ok)
	assert.Equal(t, 0.002005, v) // (100+301)/2 = 200.5 points: the half point survives

	v, ok = a.onBar(hlc(1500, 1000, 1000)) // true range 500; 100 leaves
	require.True(t, ok)
	assert.Equal(t, 0.004005, v) // (301+500)/2 = 400.5 points, not an average of all three
}

func TestATR_InvalidWindowPanics(t *testing.T) {
	assert.Panics(t, func() { newATR(0) })
	assert.Panics(t, func() { newATR(-1) })
}
