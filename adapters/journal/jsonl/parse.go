package jsonl

import (
	"fmt"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/order"
)

// This file inverts every .String() method wire.go relies on, so
// Reader can reconstruct real domain enum values from the wire's
// human-readable string form rather than a raw integer — a plain
// ErrCorruptEntry is returned for any string that isn't one of the
// exact values the corresponding toXxxWire ever writes.

func parseSide(s string) (order.Side, error) {
	switch s {
	case "":
		return order.Side(0), nil
	case "buy":
		return order.Buy, nil
	case "sell":
		return order.Sell, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized side %q", ErrCorruptEntry, s)
	}
}

func parseType(s string) (order.Type, error) {
	switch s {
	case "market":
		return order.Market, nil
	case "limit":
		return order.Limit, nil
	case "stop":
		return order.Stop, nil
	case "stop_limit":
		return order.StopLimit, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized order type %q", ErrCorruptEntry, s)
	}
}

func parseTimeInForce(s string) (order.TimeInForce, error) {
	switch s {
	case "gtc":
		return order.GTC, nil
	case "day":
		return order.DAY, nil
	case "ioc":
		return order.IOC, nil
	case "fok":
		return order.FOK, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized time in force %q", ErrCorruptEntry, s)
	}
}

func parseStatus(s string) (order.Status, error) {
	switch s {
	case "unknown":
		return order.StatusUnknown, nil
	case "pending_submit":
		return order.StatusPendingSubmit, nil
	case "working":
		return order.StatusWorking, nil
	case "partially_filled":
		return order.StatusPartiallyFilled, nil
	case "filled":
		return order.StatusFilled, nil
	case "pending_cancel":
		return order.StatusPendingCancel, nil
	case "canceled":
		return order.StatusCanceled, nil
	case "pending_replace":
		return order.StatusPendingReplace, nil
	case "rejected":
		return order.StatusRejected, nil
	case "expired":
		return order.StatusExpired, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized order status %q", ErrCorruptEntry, s)
	}
}

func parseRejectReason(s string) (order.RejectReason, error) {
	switch s {
	case "unknown":
		return order.ReasonUnknown, nil
	case "insufficient_margin":
		return order.ReasonInsufficientMargin, nil
	case "invalid_price":
		return order.ReasonInvalidPrice, nil
	case "invalid_quantity":
		return order.ReasonInvalidQuantity, nil
	case "market_closed":
		return order.ReasonMarketClosed, nil
	case "unsupported_order_type":
		return order.ReasonUnsupportedOrderType, nil
	case "duplicate_order_id":
		return order.ReasonDuplicateOrderID, nil
	case "risk_rejected":
		return order.ReasonRiskRejected, nil
	case "unsupported_capability":
		return order.ReasonUnsupportedCapability, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized reject reason %q", ErrCorruptEntry, s)
	}
}

func parsePositionSide(s string) (order.PositionSide, error) {
	switch s {
	case "flat":
		return order.Flat, nil
	case "long":
		return order.Long, nil
	case "short":
		return order.Short, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized position side %q", ErrCorruptEntry, s)
	}
}

func parseIntentKind(s string) (order.IntentKind, error) {
	switch s {
	case "enter":
		return order.IntentEnter, nil
	case "exit":
		return order.IntentExit, nil
	case "adjust_stop":
		return order.IntentAdjustStop, nil
	case "target_exposure":
		return order.IntentTargetExposure, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized intent kind %q", ErrCorruptEntry, s)
	}
}

func parseAccountStatus(s string) (broker.AccountStatus, error) {
	switch s {
	case "unknown":
		return broker.AccountStatusUnknown, nil
	case "active":
		return broker.AccountStatusActive, nil
	case "degraded":
		return broker.AccountStatusDegraded, nil
	case "disconnected":
		return broker.AccountStatusDisconnected, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized account status %q", ErrCorruptEntry, s)
	}
}
