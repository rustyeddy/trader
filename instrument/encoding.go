package instrument

import (
	"encoding/json"
	"fmt"
)

// JSON encoding for ID (issue #215 review): ID's own String already
// documents its return value as "canonical text form," the same
// round-trip guarantee id.ID[K] and every num package type already
// provide via their own MarshalJSON/UnmarshalJSON, so ID needs no
// separate wire format — it marshals as that same canonical string.
//
// ErrInvalidID marks JSON input that does not name a well-formed ID:
// empty text. UnmarshalJSON accepts only text an ID constructor itself
// could have produced — it never re-derives raw from separate typed
// components, since raw already *is* the canonical identity.

// MarshalJSON implements json.Marshaler, encoding id as its own
// canonical text form (String). The zero value marshals as an empty
// JSON string, mirroring String's own zero-value behavior.
func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.raw)
}

// UnmarshalJSON implements json.Unmarshaler, decoding a JSON string
// holding an ID's own canonical text form. An empty string decodes to
// the zero ID, matching MarshalJSON's own zero-value round trip.
func (id *ID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidID, err)
	}
	id.raw = s
	return nil
}
