package id

import (
	"encoding/json"
	"time"
)

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

// metadataWire is Metadata's JSON wire shape. Every ID field is a
// pointer, omitted when zero, because ID[K].MarshalJSON deliberately
// errors on a zero ID (id/encoding.go) — correct for a value that is
// always required to be a real identity, but wrong for Metadata, where
// a zero EventID/CorrelationID/CausationID is a legitimate value (most
// notably CausationID for the first event in a workflow, per this
// type's own doc comment). Marshaling Metadata directly via reflection
// would therefore fail on exactly the common case of an event nothing
// caused, rather than merely omitting the field that doesn't apply.
type metadataWire struct {
	EventID       *EventID       `json:"event_id,omitempty"`
	CorrelationID *CorrelationID `json:"correlation_id,omitempty"`
	CausationID   *EventID       `json:"causation_id,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
	Source        Source         `json:"source,omitempty"`
}

// MarshalJSON implements json.Marshaler, omitting any of
// EventID/CorrelationID/CausationID that are the zero value instead of
// erroring on them.
func (m Metadata) MarshalJSON() ([]byte, error) {
	w := metadataWire{Timestamp: m.Timestamp, Source: m.Source}
	if !m.EventID.IsZero() {
		w.EventID = &m.EventID
	}
	if !m.CorrelationID.IsZero() {
		w.CorrelationID = &m.CorrelationID
	}
	if !m.CausationID.IsZero() {
		w.CausationID = &m.CausationID
	}
	return json.Marshal(w)
}

// UnmarshalJSON implements json.Unmarshaler, the inverse of
// MarshalJSON: an omitted/null ID field decodes to the zero ID.
func (m *Metadata) UnmarshalJSON(data []byte) error {
	var w metadataWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*m = Metadata{Timestamp: w.Timestamp, Source: w.Source}
	if w.EventID != nil {
		m.EventID = *w.EventID
	}
	if w.CorrelationID != nil {
		m.CorrelationID = *w.CorrelationID
	}
	if w.CausationID != nil {
		m.CausationID = *w.CausationID
	}
	return nil
}
