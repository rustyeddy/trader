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
