package id

import "time"

// Source identifies the component, strategy, adapter, broker, or service
// that produced an event — for example "strategy.macd_cross" or
// "broker.oanda". It is a plain label, not a generated identifier, so it
// marshals as an ordinary JSON string with no custom encoding needed.
type Source string

// IsZero reports whether s is empty.
func (s Source) IsZero() bool {
	return s == ""
}

// Metadata is the correlation and causation context carried alongside a
// domain event, tracing it through a multi-stage workflow. A typical
// intent-to-fill chain shares one CorrelationID throughout, while each
// stage's CausationID points at the EventID immediately before it — see
// the package doc comment for the full worked example.
type Metadata struct {
	// EventID identifies this event.
	EventID EventID

	// CorrelationID is shared across every stage of the workflow this
	// event belongs to.
	CorrelationID CorrelationID

	// CausationID is the EventID of the event that directly caused this
	// one. It is the zero value for an event nothing caused — the first
	// event in a workflow.
	CausationID EventID

	// Timestamp is this event's canonical event time. It should be UTC;
	// every Now from this module's clock.Clock implementations already is.
	Timestamp time.Time

	// Source names the component that produced this event.
	Source Source
}
