package instrument

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON encoding for ID (issue #215 review): ID's own String already
// documents its return value as "canonical text form," the same
// round-trip guarantee id.ID[K] and every num package type already
// provide via their own MarshalJSON/UnmarshalJSON, so ID needs no
// separate wire format — it marshals as that same canonical string.
//
// ErrInvalidID marks JSON input that UnmarshalJSON refuses: input that
// is not a JSON string, or a non-empty string not prefixed by one of
// idKindPrefixes — the same "raw already is the canonical identity"
// contract every per-kind constructor in id.go establishes, so
// UnmarshalJSON accepts only text one of those constructors could
// plausibly have produced, never an arbitrary string smuggled in as an
// ID. An empty string is accepted and decodes to the zero ID, matching
// MarshalJSON's own zero-value round trip.

// idKindPrefixes are every "<kind>:" prefix a per-kind ID constructor
// in id.go produces. UnmarshalJSON checks against this set rather than
// re-deriving raw from typed components: it is a structural sanity
// check (this looks like an ID our own constructors could have built),
// not a claim that the referenced instrument actually exists.
var idKindPrefixes = []string{"fx:", "eq:", "etf:", "fut:", "cont:", "idx:"}

// MarshalJSON implements json.Marshaler, encoding id as its own
// canonical text form (String). The zero value marshals as an empty
// JSON string, mirroring String's own zero-value behavior.
func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.raw)
}

// UnmarshalJSON implements json.Unmarshaler, decoding a JSON string
// holding an ID's own canonical text form. See ErrInvalidID's own doc
// comment for exactly what is accepted.
func (id *ID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidID, err)
	}
	if s != "" && !hasKnownIDKindPrefix(s) {
		return fmt.Errorf("%w: %q has no recognized instrument ID kind prefix", ErrInvalidID, s)
	}
	id.raw = s
	return nil
}

func hasKnownIDKindPrefix(s string) bool {
	for _, prefix := range idKindPrefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
