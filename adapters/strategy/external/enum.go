package external

import (
	"fmt"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// This file converts every closed enum vocabulary the v1 schema
// shares with a domain type. Every function rejects an unrecognized
// or unspecified value explicitly (ErrInvalidWireValue) rather than
// defaulting to some "reasonable" domain value — matching order.Side/
// order.PositionSide/order.IntentKind/marketdata.Unit's own closed,
// Trader-controlled vocabularies, none of which have an "unknown but
// still valid" member.

// toWireSide converts a required order.Side (Buy/Sell) to its v1
// counterpart.
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

// fromWireSide converts a required v1.Side to its order.Side
// counterpart. SIDE_UNSPECIFIED is rejected: every DescribedIntent
// kind that carries a side requires one.
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

// toWirePositionSide converts an order.PositionSide to its v1
// counterpart. Flat maps to POSITION_SIDE_FLAT, the same zero value
// PositionSide.String's own doc comment documents (strategy.proto's
// own "PositionSide is deliberately the same zero value" comment), so
// this never fails for any of order.PositionSide's three defined
// values.
func toWirePositionSide(s order.PositionSide) (v1.PositionSide, error) {
	switch s {
	case order.Flat:
		return v1.PositionSide_POSITION_SIDE_FLAT, nil
	case order.Long:
		return v1.PositionSide_POSITION_SIDE_LONG, nil
	case order.Short:
		return v1.PositionSide_POSITION_SIDE_SHORT, nil
	default:
		return v1.PositionSide_POSITION_SIDE_FLAT, fmt.Errorf("%w: position side %v", ErrInvalidWireValue, s)
	}
}

// fromWireIntentKind converts a required v1.IntentKind to its
// order.IntentKind counterpart. INTENT_KIND_UNSPECIFIED is rejected —
// a guest must always name one of the five defined kinds.
func fromWireIntentKind(w v1.IntentKind) (order.IntentKind, error) {
	switch w {
	case v1.IntentKind_INTENT_KIND_ENTER:
		return order.IntentEnter, nil
	case v1.IntentKind_INTENT_KIND_EXIT:
		return order.IntentExit, nil
	case v1.IntentKind_INTENT_KIND_ADJUST_STOP:
		return order.IntentAdjustStop, nil
	case v1.IntentKind_INTENT_KIND_TARGET_EXPOSURE:
		return order.IntentTargetExposure, nil
	case v1.IntentKind_INTENT_KIND_ENTER_WITH_STOP:
		return order.IntentEnterWithStop, nil
	default:
		return 0, fmt.Errorf("%w: intent kind %v", ErrInvalidWireValue, w)
	}
}

// toWireIntervalUnit converts a required marketdata.Unit to its v1
// counterpart.
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

// fromWireIntervalUnit converts a required v1.IntervalUnit to its
// marketdata.Unit counterpart.
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
