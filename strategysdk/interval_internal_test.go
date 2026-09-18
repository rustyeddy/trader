package strategysdk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestToWireInterval_EveryUnit(t *testing.T) {
	for _, unit := range []marketdata.Unit{marketdata.UnitMinute, marketdata.UnitHour, marketdata.UnitDay, marketdata.UnitWeek} {
		iv, err := marketdata.NewInterval(unit, 4)
		require.NoError(t, err)
		w, err := toWireInterval(iv)
		require.NoError(t, err)
		require.Equal(t, int32(4), w.GetCount())
	}
}

func TestToWireInterval_CountAboveInt32RangeRejected(t *testing.T) {
	iv, err := marketdata.NewInterval(marketdata.UnitMinute, math.MaxInt32+1)
	require.NoError(t, err)
	_, err = toWireInterval(iv)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireInterval_NilRejected(t *testing.T) {
	_, err := fromWireInterval(nil)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireInterval_InvalidCountRejected(t *testing.T) {
	_, err := fromWireInterval(&v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 0})
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireInterval_RoundTrips(t *testing.T) {
	w := &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_DAY, Count: 1}
	iv, err := fromWireInterval(w)
	require.NoError(t, err)
	require.Equal(t, marketdata.UnitDay, iv.Unit())
	require.Equal(t, 1, iv.Count())
}
