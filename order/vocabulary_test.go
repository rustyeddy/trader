package order_test

import (
	"testing"

	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/require"
)

func TestPositionSideVocabulary(t *testing.T) {
	for _, tc := range []struct {
		value order.PositionSide
		text  string
		valid bool
	}{
		{order.Flat, "flat", true}, {order.Long, "long", true}, {order.Short, "short", true}, {order.PositionSide(255), "PositionSide(255)", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			require.Equal(t, tc.text, tc.value.String())
			require.Equal(t, tc.valid, tc.value.Valid())
		})
	}
}

func TestIntentKindVocabulary(t *testing.T) {
	for _, tc := range []struct {
		value order.IntentKind
		text  string
		valid bool
	}{
		{0, "IntentKind(0)", false},
		{order.IntentEnter, "enter", true}, {order.IntentExit, "exit", true},
		{order.IntentAdjustStop, "adjust_stop", true}, {order.IntentTargetExposure, "target_exposure", true},
		{order.IntentEnterWithStop, "enter_with_stop", true}, {order.IntentKind(255), "IntentKind(255)", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			require.Equal(t, tc.text, tc.value.String())
			require.Equal(t, tc.valid, tc.value.Valid())
		})
	}
}
