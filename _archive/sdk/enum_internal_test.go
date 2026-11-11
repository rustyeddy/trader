package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestToWireSide_EveryValue(t *testing.T) {
	w, err := toWireSide(order.Buy)
	require.NoError(t, err)
	require.Equal(t, v1.Side_SIDE_BUY, w)

	w, err = toWireSide(order.Sell)
	require.NoError(t, err)
	require.Equal(t, v1.Side_SIDE_SELL, w)
}

func TestToWireSide_InvalidRejected(t *testing.T) {
	_, err := toWireSide(order.Side(99))
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireSide_EveryValue(t *testing.T) {
	s, err := fromWireSide(v1.Side_SIDE_BUY)
	require.NoError(t, err)
	require.Equal(t, order.Buy, s)

	s, err = fromWireSide(v1.Side_SIDE_SELL)
	require.NoError(t, err)
	require.Equal(t, order.Sell, s)
}

func TestFromWireSide_UnspecifiedRejected(t *testing.T) {
	_, err := fromWireSide(v1.Side_SIDE_UNSPECIFIED)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWirePositionSide_EveryValue(t *testing.T) {
	tests := []struct {
		w    v1.PositionSide
		want order.PositionSide
	}{
		{v1.PositionSide_POSITION_SIDE_FLAT, order.Flat},
		{v1.PositionSide_POSITION_SIDE_LONG, order.Long},
		{v1.PositionSide_POSITION_SIDE_SHORT, order.Short},
	}
	for _, tt := range tests {
		got, err := fromWirePositionSide(tt.w)
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}
}

func TestFromWirePositionSide_InvalidRejected(t *testing.T) {
	_, err := fromWirePositionSide(v1.PositionSide(99))
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireIntentKind_EveryValue(t *testing.T) {
	tests := []struct {
		k    order.IntentKind
		want v1.IntentKind
	}{
		{order.IntentEnter, v1.IntentKind_INTENT_KIND_ENTER},
		{order.IntentExit, v1.IntentKind_INTENT_KIND_EXIT},
		{order.IntentAdjustStop, v1.IntentKind_INTENT_KIND_ADJUST_STOP},
		{order.IntentTargetExposure, v1.IntentKind_INTENT_KIND_TARGET_EXPOSURE},
		{order.IntentEnterWithStop, v1.IntentKind_INTENT_KIND_ENTER_WITH_STOP},
	}
	for _, tt := range tests {
		got, err := toWireIntentKind(tt.k)
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}
}

func TestToWireIntentKind_InvalidRejected(t *testing.T) {
	_, err := toWireIntentKind(order.IntentKind(99))
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireIntervalUnit_EveryValue(t *testing.T) {
	tests := []struct {
		u    marketdata.Unit
		want v1.IntervalUnit
	}{
		{marketdata.UnitMinute, v1.IntervalUnit_INTERVAL_UNIT_MINUTE},
		{marketdata.UnitHour, v1.IntervalUnit_INTERVAL_UNIT_HOUR},
		{marketdata.UnitDay, v1.IntervalUnit_INTERVAL_UNIT_DAY},
		{marketdata.UnitWeek, v1.IntervalUnit_INTERVAL_UNIT_WEEK},
	}
	for _, tt := range tests {
		got, err := toWireIntervalUnit(tt.u)
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}
}

func TestToWireIntervalUnit_InvalidRejected(t *testing.T) {
	_, err := toWireIntervalUnit(marketdata.Unit(99))
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireIntervalUnit_EveryValue(t *testing.T) {
	tests := []struct {
		w    v1.IntervalUnit
		want marketdata.Unit
	}{
		{v1.IntervalUnit_INTERVAL_UNIT_MINUTE, marketdata.UnitMinute},
		{v1.IntervalUnit_INTERVAL_UNIT_HOUR, marketdata.UnitHour},
		{v1.IntervalUnit_INTERVAL_UNIT_DAY, marketdata.UnitDay},
		{v1.IntervalUnit_INTERVAL_UNIT_WEEK, marketdata.UnitWeek},
	}
	for _, tt := range tests {
		got, err := fromWireIntervalUnit(tt.w)
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}
}

func TestFromWireIntervalUnit_InvalidRejected(t *testing.T) {
	_, err := fromWireIntervalUnit(v1.IntervalUnit_INTERVAL_UNIT_UNSPECIFIED)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}
