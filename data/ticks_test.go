package data

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func TestTickMid(t *testing.T) {
	t.Parallel()

	tick := Tick{
		Ask: types.Price(110),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(105), tick.Mid())
}

func TestTickMidOdd(t *testing.T) {
	t.Parallel()

	// (101 + 100) / 2 = 100 (integer shift right)
	tick := Tick{
		Ask: types.Price(101),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(100), tick.Mid())
}

func TestTickSpread(t *testing.T) {
	t.Parallel()

	tick := Tick{
		Ask: types.Price(110),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(10), tick.Spread())
}

func TestTickMinute(t *testing.T) {
	t.Parallel()

	// Timemilli = 90_500 ms = 1 minute 30.5 seconds
	// FloorToMinute: (90500 / 60000) * 60000 = 60000
	tick := Tick{
		Timemilli: types.Timemilli(90_500),
	}
	require.Equal(t, types.Timemilli(60_000), tick.Minute())

	// Exactly on a minute boundary
	tick2 := Tick{
		Timemilli: types.Timemilli(60_000),
	}
	require.Equal(t, types.Timemilli(60_000), tick2.Minute())
}
