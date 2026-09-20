package jsonl

import (
	"fmt"

	"github.com/rustyeddy/trader/internal/broker"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
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

func parseType(s string) (runtimeorder.Type, error) {
	switch s {
	case "market":
		return runtimeorder.Market, nil
	case "limit":
		return runtimeorder.Limit, nil
	case "stop":
		return runtimeorder.Stop, nil
	case "stop_limit":
		return runtimeorder.StopLimit, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized order type %q", ErrCorruptEntry, s)
	}
}

func parseTimeInForce(s string) (runtimeorder.TimeInForce, error) {
	switch s {
	case "gtc":
		return runtimeorder.GTC, nil
	case "day":
		return runtimeorder.DAY, nil
	case "ioc":
		return runtimeorder.IOC, nil
	case "fok":
		return runtimeorder.FOK, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized time in force %q", ErrCorruptEntry, s)
	}
}

func parseStatus(s string) (runtimeorder.Status, error) {
	switch s {
	case "unknown":
		return runtimeorder.StatusUnknown, nil
	case "pending_submit":
		return runtimeorder.StatusPendingSubmit, nil
	case "working":
		return runtimeorder.StatusWorking, nil
	case "partially_filled":
		return runtimeorder.StatusPartiallyFilled, nil
	case "filled":
		return runtimeorder.StatusFilled, nil
	case "pending_cancel":
		return runtimeorder.StatusPendingCancel, nil
	case "canceled":
		return runtimeorder.StatusCanceled, nil
	case "pending_replace":
		return runtimeorder.StatusPendingReplace, nil
	case "rejected":
		return runtimeorder.StatusRejected, nil
	case "expired":
		return runtimeorder.StatusExpired, nil
	default:
		return 0, fmt.Errorf("%w: unrecognized order status %q", ErrCorruptEntry, s)
	}
}

func parseRejectReason(s string) (runtimeorder.RejectReason, error) {
	switch s {
	case "unknown":
		return runtimeorder.ReasonUnknown, nil
	case "insufficient_margin":
		return runtimeorder.ReasonInsufficientMargin, nil
	case "invalid_price":
		return runtimeorder.ReasonInvalidPrice, nil
	case "invalid_quantity":
		return runtimeorder.ReasonInvalidQuantity, nil
	case "market_closed":
		return runtimeorder.ReasonMarketClosed, nil
	case "unsupported_order_type":
		return runtimeorder.ReasonUnsupportedOrderType, nil
	case "duplicate_order_id":
		return runtimeorder.ReasonDuplicateOrderID, nil
	case "risk_rejected":
		return runtimeorder.ReasonRiskRejected, nil
	case "unsupported_capability":
		return runtimeorder.ReasonUnsupportedCapability, nil
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
	case "enter_with_stop":
		return order.IntentEnterWithStop, nil
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
