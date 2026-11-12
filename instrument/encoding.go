package instrument

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// JSON encoding for ID (issue #215 review): ID's own String already
// documents its return value as "canonical text form," the same
// round-trip guarantee id.ID[K] and every num package type already
// provide via their own MarshalJSON/UnmarshalJSON, so ID needs no
// separate wire format — it marshals as that same canonical string.
//
// ErrInvalidID marks JSON input that UnmarshalJSON refuses: input that is not
// a JSON string, or a non-empty string that is not exactly the canonical text
// a per-kind constructor in id.go produces. UnmarshalJSON parses the text
// back into the components its kind needs (currencies, exchange and ticker,
// root and contract month, ...), rebuilds the ID with the same constructor,
// and accepts the input only if that gives back the identical string. A
// recognized prefix alone is not enough: "fx:eur/usd", "fx:EUR/EUR",
// "eq:NASDAQ", "fut:ES:2026-13" and "cont:A B" are all refused, so
// UnmarshalJSON accepts only text a constructor could have built, never an
// arbitrary string smuggled in as an ID. It does not claim the referenced
// instrument exists. An empty string is accepted and decodes to the zero ID,
// matching MarshalJSON's own zero-value round trip.

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
	// Decode into a *string: json.Unmarshal turns a JSON null into a nil
	// pointer, which a plain string would swallow as "", silently clearing an
	// existing ID.
	var p *string
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidID, err)
	}
	if p == nil {
		return fmt.Errorf("%w: null is not an ID", ErrInvalidID)
	}
	s := *p
	if s == "" {
		*id = ID{}
		return nil
	}
	parsed, ok := parseID(s)
	if !ok {
		return fmt.Errorf("%w: %q is not the canonical text of any instrument ID", ErrInvalidID, s)
	}
	*id = parsed
	return nil
}

// parseID rebuilds the ID whose canonical text is s, or reports false. It
// goes through the per-kind constructors, so what it accepts can never drift
// from what they produce, and compares the result with s so that anything a
// constructor would normalize (case, surrounding space) is refused.
func parseID(s string) (ID, bool) {
	kind, rest, ok := strings.Cut(s, ":")
	if !ok {
		return ID{}, false
	}
	var id ID
	switch kind {
	case "fx":
		base, quote, ok := strings.Cut(rest, "/")
		if !ok {
			return ID{}, false
		}
		b, err := num.ParseCurrency(base)
		if err != nil {
			return ID{}, false
		}
		q, err := num.ParseCurrency(quote)
		if err != nil {
			return ID{}, false
		}
		id = CurrencyPairID(b, q)
	case "eq", "etf":
		exchange, ticker, ok := strings.Cut(rest, ":")
		if !ok {
			return ID{}, false
		}
		if kind == "eq" {
			id = EquityID(exchange, ticker)
		} else {
			id = ETFID(exchange, ticker)
		}
	case "fut":
		root, month, ok := strings.Cut(rest, ":")
		if !ok {
			return ID{}, false
		}
		expiration, err := time.Parse(contractMonthLayout, month)
		if err != nil {
			return ID{}, false
		}
		id = FutureID(root, expiration)
	case "cont":
		id = ContinuousSeriesID(rest)
	case "idx":
		id = IndexID(rest)
	default:
		return ID{}, false
	}
	if id.raw != s {
		return ID{}, false
	}
	return id, true
}
