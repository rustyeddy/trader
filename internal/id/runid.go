package id

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/internal/id/internal/ulid"
)

// runPrefix starts the text form of every RunID, so a stray one in a log line or a
// directory listing is recognisable on sight.
const runPrefix = "run_"

// RunID identifies one run. Its text form is "run_" and a ULID: 26 Crockford
// Base32 characters holding the millisecond the ID was made and 80 bits of
// entropy, so run directories sort by start time and two never collide.
//
// The zero value is unset, not the ID that happens to encode sixteen zero bytes:
// IsZero reports true, String says so, and marshaling it is an error. A RunID
// comes only from a Generator or from ParseRunID, so it is always well formed.
type RunID struct {
	value [16]byte
	set   bool
}

// String is the canonical text form, or "<unset id>" for the zero value.
func (id RunID) String() string {
	if !id.set {
		return "<unset id>"
	}
	return runPrefix + ulid.Encode(id.value)
}

// IsZero reports whether id is the unset zero value.
func (id RunID) IsZero() bool { return !id.set }

// Equal reports whether id and o are the same ID. Two zero values are equal.
func (id RunID) Equal(o RunID) bool { return id.set == o.set && id.value == o.value }

// Time is the instant the ID was made, in UTC, to the millisecond.
func (id RunID) Time() (time.Time, error) {
	if !id.set {
		return time.Time{}, ErrZeroValue
	}
	return ulid.Timestamp(id.value), nil
}

// ParseRunID checks that s is a RunID in its canonical form and returns it. It
// refuses anything else (a wrong prefix, a malformed body, the reserved all-zero
// value) and repairs nothing.
func ParseRunID(s string) (RunID, error) {
	rest, ok := strings.CutPrefix(s, runPrefix)
	if !ok {
		return RunID{}, fmt.Errorf("%w: %q does not start with %q", ErrInvalidID, s, runPrefix)
	}
	v, err := ulid.Decode(rest)
	if err != nil {
		return RunID{}, fmt.Errorf("%w: %v", ErrInvalidID, err)
	}
	if v == ([16]byte{}) {
		return RunID{}, fmt.Errorf("%w: the all-zero value is reserved for the unset zero value", ErrInvalidID)
	}
	return RunID{value: v, set: true}, nil
}

// MustParseRunID is ParseRunID for constants and tests; it panics on error.
func MustParseRunID(s string) RunID {
	id, err := ParseRunID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// MarshalText encodes id as its text form. The zero value is an error: it is not
// a real ID, and a caller that forgot to set one should hear about it.
func (id RunID) MarshalText() ([]byte, error) {
	if !id.set {
		return nil, ErrZeroValue
	}
	return []byte(id.String()), nil
}

// UnmarshalText decodes the text form, refusing anything ParseRunID refuses.
func (id *RunID) UnmarshalText(text []byte) error {
	v, err := ParseRunID(string(text))
	if err != nil {
		return err
	}
	*id = v
	return nil
}

// MarshalJSON encodes id as a JSON string holding its text form.
func (id RunID) MarshalJSON() ([]byte, error) {
	if !id.set {
		return nil, ErrZeroValue
	}
	return json.Marshal(id.String())
}

// UnmarshalJSON decodes a JSON string holding the text form.
func (id *RunID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return id.UnmarshalText([]byte(s))
}
