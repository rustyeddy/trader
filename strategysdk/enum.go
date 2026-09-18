package strategysdk

import (
	"fmt"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// This file converts every closed enum vocabulary the v1 schema
// shares with a domain type, in whichever direction the guest side
// actually needs it. Every function rejects an unrecognized or
// unspecified value explicitly (ErrInvalidWireValue) rather than
// defaulting to some "reasonable" value.

// toWireSide converts a required order.Side (Buy/Sell) to its v1
// counterpart — used to send DescribedIntent.side.
func toWireSide(s order.Side) (v1.Side, error) {
	switch s {
	case order.Buy:
		return v1.Side_SIDE_BUY, nil
	case order.Sell:
		return v1.Side_SIDE_SELL, nil
	default:
		return v1.Side_SIDE_UNSPECIFIED, fmt.Errorf("%w: side %v", ErrInvalidWireValue, s)
	}
}

// fromWireSide converts a received v1.Side (FillEvent.side) to its
// order.Side counterpart.
func fromWireSide(w v1.Side) (order.Side, error) {
	switch w {
	case v1.Side_SIDE_BUY:
		return order.Buy, nil
	case v1.Side_SIDE_SELL:
		return order.Sell, nil
	default:
		return 0, fmt.Errorf("%w: side %v", ErrInvalidWireValue, w)
	}
}

// fromWirePositionSide converts a received v1.PositionSide
// (PositionSnapshot.side) to its order.PositionSide counterpart.
// POSITION_SIDE_FLAT is PositionSide's own zero value, so this never
// fails for any of v1.PositionSide's three defined values.
func fromWirePositionSide(w v1.PositionSide) (order.PositionSide, error) {
	switch w {
	case v1.PositionSide_POSITION_SIDE_FLAT:
		return order.Flat, nil
	case v1.PositionSide_POSITION_SIDE_LONG:
		return order.Long, nil
	case v1.PositionSide_POSITION_SIDE_SHORT:
		return order.Short, nil
	default:
		return 0, fmt.Errorf("%w: position side %v", ErrInvalidWireValue, w)
	}
}

// toWireIntentKind converts a required order.IntentKind
// (DescribedIntent.kind) to its v1 counterpart.
func toWireIntentKind(k order.IntentKind) (v1.IntentKind, error) {
	switch k {
	case order.IntentEnter:
		return v1.IntentKind_INTENT_KIND_ENTER, nil
	case order.IntentExit:
		return v1.IntentKind_INTENT_KIND_EXIT, nil
	case order.IntentAdjustStop:
		return v1.IntentKind_INTENT_KIND_ADJUST_STOP, nil
	case order.IntentTargetExposure:
		return v1.IntentKind_INTENT_KIND_TARGET_EXPOSURE, nil
	case order.IntentEnterWithStop:
		return v1.IntentKind_INTENT_KIND_ENTER_WITH_STOP, nil
	default:
		return v1.IntentKind_INTENT_KIND_UNSPECIFIED, fmt.Errorf("%w: intent kind %v", ErrInvalidWireValue, k)
	}
}

// toWireIntervalUnit converts a required marketdata.Unit to its v1
// counterpart — used to send DataRequirement.interval/
// GetHistoryBarsRequest.interval.
func toWireIntervalUnit(u marketdata.Unit) (v1.IntervalUnit, error) {
	switch u {
	case marketdata.UnitMinute:
		return v1.IntervalUnit_INTERVAL_UNIT_MINUTE, nil
	case marketdata.UnitHour:
		return v1.IntervalUnit_INTERVAL_UNIT_HOUR, nil
	case marketdata.UnitDay:
		return v1.IntervalUnit_INTERVAL_UNIT_DAY, nil
	case marketdata.UnitWeek:
		return v1.IntervalUnit_INTERVAL_UNIT_WEEK, nil
	default:
		return v1.IntervalUnit_INTERVAL_UNIT_UNSPECIFIED, fmt.Errorf("%w: interval unit %v", ErrInvalidWireValue, u)
	}
}

// fromWireIntervalUnit converts a received v1.IntervalUnit
// (BarEvent.interval) to its marketdata.Unit counterpart.
func fromWireIntervalUnit(w v1.IntervalUnit) (marketdata.Unit, error) {
	switch w {
	case v1.IntervalUnit_INTERVAL_UNIT_MINUTE:
		return marketdata.UnitMinute, nil
	case v1.IntervalUnit_INTERVAL_UNIT_HOUR:
		return marketdata.UnitHour, nil
	case v1.IntervalUnit_INTERVAL_UNIT_DAY:
		return marketdata.UnitDay, nil
	case v1.IntervalUnit_INTERVAL_UNIT_WEEK:
		return marketdata.UnitWeek, nil
	default:
		return 0, fmt.Errorf("%w: interval unit %v", ErrInvalidWireValue, w)
	}
}
