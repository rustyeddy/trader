package order

import "fmt"

// RejectReason classifies why a broker declined an order, cancel, or
// replace request. Like Status, it reflects external, broker-reported
// information Trader cannot fully enumerate in advance, so it reserves
// its zero value for ReasonUnknown rather than guessing or failing to
// construct. The set of named reasons is expected to grow additively as
// broker adapters are built; ReasonUnknown plus Rejection.BrokerCode
// keeps every rejection representable even before its reason has a
// dedicated case.
type RejectReason uint8

const (
	// ReasonUnknown is RejectReason's zero value: an unrecognized or
	// unclassified rejection.
	ReasonUnknown RejectReason = iota
	// ReasonInsufficientMargin means the account lacks the margin or
	// buying power the order requires.
	ReasonInsufficientMargin
	// ReasonInvalidPrice means the requested price violates the
	// listing's tick size or another price rule.
	ReasonInvalidPrice
	// ReasonInvalidQuantity means the requested quantity violates the
	// listing's quantity increment or another size rule.
	ReasonInvalidQuantity
	// ReasonMarketClosed means the listing is not currently tradable.
	ReasonMarketClosed
	// ReasonUnsupportedOrderType means the broker or account does not
	// support the requested Type/TimeInForce combination.
	ReasonUnsupportedOrderType
	// ReasonDuplicateOrderID means the broker already has an order for
	// this OrderID (or its broker-native equivalent) on file.
	ReasonDuplicateOrderID
	// ReasonRiskRejected means Trader's own risk stage rejected the
	// order before it reached a broker.
	ReasonRiskRejected
	// ReasonUnsupportedCapability means the request used a feature the
	// broker or account does not support.
	ReasonUnsupportedCapability
)

// String returns a human-readable RejectReason name.
func (r RejectReason) String() string {
	switch r {
	case ReasonUnknown:
		return "unknown"
	case ReasonInsufficientMargin:
		return "insufficient_margin"
	case ReasonInvalidPrice:
		return "invalid_price"
	case ReasonInvalidQuantity:
		return "invalid_quantity"
	case ReasonMarketClosed:
		return "market_closed"
	case ReasonUnsupportedOrderType:
		return "unsupported_order_type"
	case ReasonDuplicateOrderID:
		return "duplicate_order_id"
	case ReasonRiskRejected:
		return "risk_rejected"
	case ReasonUnsupportedCapability:
		return "unsupported_capability"
	default:
		return fmt.Sprintf("RejectReason(%d)", uint8(r))
	}
}

// Rejection explains why a broker declined a request. BrokerCode
// preserves the broker's own original rejection text even when Reason
// can only be classified as ReasonUnknown, so information is never
// silently dropped when Trader doesn't yet recognize a broker's specific
// rejection vocabulary.
type Rejection struct {
	// Reason is Trader's classification of the rejection.
	Reason RejectReason
	// Detail is a human-readable explanation, Trader- or broker-
	// authored.
	Detail string
	// BrokerCode is the broker's own rejection code or text, preserved
	// verbatim for diagnostics and future reclassification.
	BrokerCode string
}
