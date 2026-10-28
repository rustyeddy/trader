package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func closes(vals ...float64) []ohlc {
	bars := make([]ohlc, len(vals))
	for i, v := range vals {
		bars[i] = ohlc{c: price(v * priceScale)}
	}
	return bars
}

func TestEmaxHoldsUntilBothWarm(t *testing.T) {
	e := &emax{short: 2, long: 4}
	require.NoError(t, e.init(nil))

	// Falling prices, so short < long once warm; no signal and no sell
	// while the long EMA is still warming up (first 3 bars).
	ctx := context.Background()
	for i, b := range closes(10, 9, 8, 7)[:3] {
		require.Equal(t, "hold", e.onBar(ctx, b).side, "bar %d", i)
	}
}

func TestEmaxLongWarmsOnSameBars(t *testing.T) {
	e := &emax{short: 2, long: 4}
	require.NoError(t, e.init(nil))

	ctx := context.Background()
	for _, b := range closes(1, 2, 3, 4) {
		e.onBar(ctx, b)
	}
	// Long EMA seeded from bars 1-4 means it is valid on the 4th bar with
	// value 2.5. Had it been fed late it would not be valid yet.
	v, ok := e.emaLong.onBar(ctx, ohlc{c: price(4 * priceScale)})
	require.True(t, ok)
	require.InDelta(t, 2.5+0.4*(4-2.5), v, 1e-9)
}

func TestEmaxCrossovers(t *testing.T) {
	e := &emax{short: 2, long: 4}
	require.NoError(t, e.init(nil))

	// Down, then sharply up (short crosses above long), then sharply down.
	series := closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2)
	var sides []string
	ctx := context.Background()
	for _, b := range series {
		sides = append(sides, e.onBar(ctx, b).side)
	}

	var buys, sells int
	for _, s := range sides {
		switch s {
		case "buy":
			buys++
		case "sell":
			sells++
		}
	}
	require.Equal(t, 1, buys, "sides: %v", sides)
	require.Equal(t, 1, sells, "sides: %v", sides)
	require.Less(t, indexOf(sides, "buy"), indexOf(sides, "sell"))
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

func TestEmaxInitValidation(t *testing.T) {
	require.Error(t, (&emax{short: 0, long: 5}).init(nil))
	require.Error(t, (&emax{short: 5, long: 5}).init(nil))
	require.Error(t, (&emax{short: 6, long: 5}).init(nil))
	require.NoError(t, (&emax{short: 2, long: 5}).init(nil))
}
