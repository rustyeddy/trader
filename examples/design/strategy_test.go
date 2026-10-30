package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func closes(vals ...float64) []ohlc {
	bars := make([]ohlc, len(vals))
	for i, v := range vals {
		bars[i] = ohlc{c: price(v * priceScale)}
	}
	return bars
}

func newTestEmax(t *testing.T, short, long int) *emax {
	t.Helper()
	e := &emax{short: short, long: long}
	require.NoError(t, e.init(nil))
	return e
}

func TestEmax_HoldsUntilBothWarm(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	// Falling prices: once warm, short < long. No signal, and especially no
	// close, while the long EMA is still warming up (first 3 bars).
	for i, b := range closes(10, 9, 8) {
		assert.Equal(t, hold, e.onBar(ctx, b).action, "bar %d", i)
	}
}

func TestEmax_LongWarmsOnSameBars(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	for _, b := range closes(1, 2, 3, 4) {
		e.onBar(ctx, b)
	}
	// Regression: the long EMA must be seeded from bars 1-4 (value 2.5). If it
	// were only fed after the short EMA warmed, the next value would differ.
	v, ok := e.emaLong.onBar(ctx, ohlc{c: price(4 * priceScale)})
	require.True(t, ok)
	assert.InDelta(t, 2.5+0.4*(4-2.5), v, 1e-9)
}

func TestEmax_Crossovers(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	// Down, then sharply up (short crosses above long), then sharply down.
	series := closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2)
	var got []action
	for _, b := range series {
		got = append(got, e.onBar(ctx, b).action)
	}

	// Warm on bar index 3. The short EMA crosses above the long one on the
	// jump to 12 (index 5) and back below on the drop to 8 (index 8).
	want := []action{hold, hold, hold, hold, hold, open, hold, hold, close, hold, hold}
	assert.Equal(t, want, got)
}

func TestEmax_NoRepeatedOpen(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var opens int
	for _, b := range closes(10, 9, 8, 7, 6, 12, 14, 16, 18, 20, 22, 24) {
		if e.onBar(ctx, b).action == open {
			opens++
		}
	}
	assert.Equal(t, 1, opens, "a sustained bullish state must not re-emit open")
}

func TestEmax_FirstValidBarAlreadyBullish(t *testing.T) {
	// Documents current behavior: with short already above long on the first
	// valid bar there is no crossover, but emax still emits open.
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var got []action
	for _, b := range closes(1, 2, 3, 4) {
		got = append(got, e.onBar(ctx, b).action)
	}
	assert.Equal(t, []action{hold, hold, hold, open}, got)
}

func TestEmax_Init(t *testing.T) {
	tests := []struct {
		name        string
		short, long int
		wantErr     bool
	}{
		{"valid", 2, 5, false},
		{"zero short", 0, 5, true},
		{"zero long", 2, 0, true},
		{"negative short", -1, 5, true},
		{"equal windows", 5, 5, true},
		{"short above long", 6, 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &emax{short: tt.short, long: tt.long}
			err := e.init(nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, e.emaShort)
				assert.Nil(t, e.emaLong)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, e.emaShort)
			assert.NotNil(t, e.emaLong)
		})
	}
}
