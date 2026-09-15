package external

import (
	"encoding/json"
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
)

// parseInstrumentID reconstructs an instrument.ID from its own
// canonical text form (for example "fx:EUR/USD" — instrument.ID.
// String's own documented example), the same text every
// instrument_id wire field carries (every *.proto message using one
// says so explicitly).
//
// This goes through instrument.ID's own UnmarshalJSON rather than a
// per-Kind parser: instrument/encoding.go's own doc comment states
// "ID needs no separate wire format — it marshals as that same
// canonical string," and UnmarshalJSON already validates the result
// against every known Kind prefix. This is a different situation from
// adapters/journal/jsonl's own instrument.Listing reconstruction
// (which needs a per-Kind parser because a full Instrument's identity
// depends on Kind-specific fields a bare ID string does not carry) —
// here we only need the ID itself, which UnmarshalJSON already
// round-trips losslessly for every Kind, not FX alone.
func parseInstrumentID(s string) (instrument.ID, error) {
	if s == "" {
		return instrument.ID{}, fmt.Errorf("%w: instrument id must not be empty", ErrInvalidWireValue)
	}
	quoted, err := json.Marshal(s)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("%w: %v", ErrInvalidWireValue, err)
	}
	var out instrument.ID
	if err := out.UnmarshalJSON(quoted); err != nil {
		return instrument.ID{}, fmt.Errorf("%w: %v", ErrInvalidWireValue, err)
	}
	return out, nil
}

// correlationIDOrEmpty formats v for a wire field: the empty string
// for the zero id.CorrelationID (the "not applicable" convention every
// exact-value/identity wire field documents), its canonical text
// otherwise. There is no wire-to-domain counterpart in this package:
// the host never receives a raw CorrelationID/EventID string to parse
// back (see doc.go — FillEvent is ToWire-only, and a DescribedIntent's
// correlation_token is an opaque grouping key, never a real
// CorrelationID, per IntentsFromWire's own doc comment).
func correlationIDOrEmpty(v id.CorrelationID) string {
	if v.IsZero() {
		return ""
	}
	return v.String()
}

// eventIDOrEmpty is correlationIDOrEmpty's id.EventID counterpart.
func eventIDOrEmpty(v id.EventID) string {
	if v.IsZero() {
		return ""
	}
	return v.String()
}
