package trader

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func TestRawTickMid(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: types.Price(110),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(105), tick.Mid())
}

func TestRawTickMidOdd(t *testing.T) {
	t.Parallel()

	// (101 + 100) / 2 = 100 (integer shift right)
	tick := RawTick{
		Ask: types.Price(101),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(100), tick.Mid())
}

func TestRawTickSpread(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: types.Price(110),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(10), tick.Spread())
}

func TestRawTickMinute(t *testing.T) {
	t.Parallel()

	// Timemilli = 90_500 ms = 1 minute 30.5 seconds
	// FloorToMinute: (90500 / 60000) * 60000 = 60000
	tick := RawTick{
		Timemilli: types.Timemilli(90_500),
	}
	require.Equal(t, types.Timemilli(60_000), tick.Minute())

	// Exactly on a minute boundary
	tick2 := RawTick{
		Timemilli: types.Timemilli(60_000),
	}
	require.Equal(t, types.Timemilli(60_000), tick2.Minute())
}
