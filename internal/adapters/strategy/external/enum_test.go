package external_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestToWireInterval_EveryUnit(t *testing.T) {
	tests := []struct {
		unit marketdata.Unit
		want v1.IntervalUnit
	}{
		{marketdata.UnitMinute, v1.IntervalUnit_INTERVAL_UNIT_MINUTE},
		{marketdata.UnitHour, v1.IntervalUnit_INTERVAL_UNIT_HOUR},
		{marketdata.UnitDay, v1.IntervalUnit_INTERVAL_UNIT_DAY},
		{marketdata.UnitWeek, v1.IntervalUnit_INTERVAL_UNIT_WEEK},
	}
	for _, tt := range tests {
		iv, err := marketdata.NewInterval(tt.unit, 4)
		require.NoError(t, err)

		w, err := external.ToWireInterval(iv)
		require.NoError(t, err)
		require.Equal(t, tt.want, w.GetUnit())
		require.Equal(t, int32(4), w.GetCount())

		back, err := external.FromWireInterval(w)
		require.NoError(t, err)
		require.True(t, back.Valid())
		require.Equal(t, tt.unit, back.Unit())
		require.Equal(t, 4, back.Count())
	}
}

func TestFromWireInterval_NilRejected(t *testing.T) {
	_, err := external.FromWireInterval(nil)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestFromWireInterval_UnspecifiedUnitRejected(t *testing.T) {
	_, err := external.FromWireInterval(&v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_UNSPECIFIED, Count: 1})
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestFromWireInterval_InvalidCountRejected(t *testing.T) {
	_, err := external.FromWireInterval(&v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 0})
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

// TestToWireInterval_CountAboveInt32RangeRejected is the review
// finding: a domain Interval.Count above math.MaxInt32 must fail
// explicitly rather than silently wrapping through the int32 cast.
func TestToWireInterval_CountAboveInt32RangeRejected(t *testing.T) {
	iv, err := marketdata.NewInterval(marketdata.UnitMinute, math.MaxInt32+1)
	require.NoError(t, err)

	_, err = external.ToWireInterval(iv)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}
