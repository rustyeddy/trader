package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSMA_Warmup(t *testing.T) {
	s := newSMA(3)
	ctx := context.Background()

	for i, b := range closes(1, 2) {
		v, ok := s.onBar(ctx, b)
		assert.False(t, ok, "bar %d", i)
		assert.Zero(t, v)
	}
	// Regression: valid on the window-th bar, not one bar later.
	v, ok := s.onBar(ctx, closes(3)[0])
	require.True(t, ok)
	assert.Equal(t, 2.0, v)
}

func TestSMA_RollingWindow(t *testing.T) {
	s := newSMA(3)
	ctx := context.Background()

	var got []float64
	for _, b := range closes(1, 2, 3, 4, 5, 2) {
		if v, ok := s.onBar(ctx, b); ok {
			got = append(got, v)
		}
	}
	// windows: (1,2,3) (2,3,4) (3,4,5) (4,5,2)
	assert.Equal(t, []float64{2, 3, 4, 11.0 / 3}, got)
}

func TestSMA_WindowOfOne(t *testing.T) {
	s := newSMA(1)
	for _, b := range closes(1.5, 2.5, 0.5) {
		v, ok := s.onBar(context.Background(), b)
		require.True(t, ok)
		assert.Equal(t, b.c.Float64(), v)
	}
}

func TestSMA_NoDriftOverLongRun(t *testing.T) {
	// Integer sums mean the average of a constant series stays exact no
	// matter how many bars have rolled through the window.
	s := newSMA(7)
	var v float64
	for i := 0; i < 100000; i++ {
		v, _ = s.onBar(context.Background(), bar{ohlc: ohlc{c: 123457}})
	}
	assert.Equal(t, price(123457).Float64(), v)
}

func TestSMA_InvalidWindowPanics(t *testing.T) {
	assert.Panics(t, func() { newSMA(0) })
	assert.Panics(t, func() { newSMA(-1) })
}

func TestEMA_Warmup(t *testing.T) {
	e := newEMA(3)
	ctx := context.Background()

	for i, b := range closes(1, 2) {
		v, ok := e.onBar(ctx, b)
		assert.False(t, ok, "bar %d", i)
		assert.Zero(t, v)
	}
	// Seeded with the SMA of the first window closes.
	v, ok := e.onBar(ctx, closes(3)[0])
	require.True(t, ok)
	assert.Equal(t, 2.0, v)
}

func TestEMA_Recurrence(t *testing.T) {
	e := newEMA(3) // alpha = 2/(3+1) = 0.5
	ctx := context.Background()

	var got []float64
	for _, b := range closes(1, 2, 3, 4, 2) {
		if v, ok := e.onBar(ctx, b); ok {
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
		v, _ = e.onBar(context.Background(), bar{ohlc: ohlc{c: 110000}})
	}
	assert.InDelta(t, 1.1, v, 1e-12)
}

func TestEMA_WindowOfOne(t *testing.T) {
	e := newEMA(1) // alpha = 1: the EMA is the close
	for _, b := range closes(1.5, 2.5, 0.5) {
		v, ok := e.onBar(context.Background(), b)
		require.True(t, ok)
		assert.InDelta(t, b.c.Float64(), v, 1e-12)
	}
}

func TestEMA_InvalidWindowPanics(t *testing.T) {
	assert.Panics(t, func() { newEMA(0) })
	assert.Panics(t, func() { newEMA(-1) })
}
