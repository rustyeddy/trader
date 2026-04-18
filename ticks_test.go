package trader

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRawTickMid(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: Price(110),
		Bid: Price(100),
	}
	require.Equal(t, Price(105), tick.Mid())
}

func TestRawTickMidOdd(t *testing.T) {
	t.Parallel()

	// (101 + 100) / 2 = 100 (integer shift right)
	tick := RawTick{
		Ask: Price(101),
		Bid: Price(100),
	}
	require.Equal(t, Price(100), tick.Mid())
}

func TestRawTickSpread(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: Price(110),
		Bid: Price(100),
	}
	require.Equal(t, Price(10), tick.Spread())
}

func TestRawTickMinute(t *testing.T) {
	t.Parallel()

	// Timemilli = 90_500 ms = 1 minute 30.5 seconds
	// FloorToMinute: (90500 / 60000) * 60000 = 60000
	tick := RawTick{
		Timemilli: Timemilli(90_500),
	}
	require.Equal(t, Timemilli(60_000), tick.Minute())

	// Exactly on a minute boundary
	tick2 := RawTick{
		Timemilli: Timemilli(60_000),
	}
	require.Equal(t, Timemilli(60_000), tick2.Minute())
}
