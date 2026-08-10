package id

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/id/internal/ulid"
)

// ID is a Trader-owned identifier of kind K. RunID, OrderID, and the other
// aliases in kind.go are each a distinct instantiation of this generic
// type, giving compile-time distinctness between kinds without hand-writing
// ten nearly identical types. Its representation — a 128-bit ULID value —
// is unexported; callers can never construct one holding an arbitrary or
// invalid value, only through Parse, MustParse, or a Generator.
//
// The zero value of ID is not a valid identifier: it is treated as unset,
// not as the identifier that happens to encode sixteen zero bytes. IsZero
// reports true for it, String names it visibly rather than returning
// something that could pass for real output, and marshaling it fails.
// Parse rejects any input that would decode to the all-zero value for the
// same reason: that bit pattern is reserved for "unset," never assigned to
// a real identifier.
type ID[K Kind] struct {
	value [16]byte
	set   bool
}

// String returns id's canonical text form: "<prefix>_<26-character
// Crockford Base32>", for example "run_01J8Z3K3R2N4XG9YB6HFA1V7ZQ". The
// zero value renders as "<unset id>" rather than an empty or otherwise
// plausible-looking string; see MarshalText for the form used at
// serialization boundaries, which rejects the zero value outright instead
// of rendering it as text at all.
func (id ID[K]) String() string {
	if !id.set {
		return "<unset id>"
	}
	var k K
	return k.Prefix() + "_" + ulid.Encode(id.value)
}

// IsZero reports whether id is the unset zero value.
func (id ID[K]) IsZero() bool {
	return !id.set
}

// Equal reports whether id and o hold the identical value. Two zero values
// are equal to each other.
func (id ID[K]) Equal(o ID[K]) bool {
	return id.set == o.set && id.value == o.value
}

// Time returns the creation instant encoded in id, in UTC. It reports
// ErrZeroValue if id is the zero value.
func (id ID[K]) Time() (time.Time, error) {
	if !id.set {
		return time.Time{}, ErrZeroValue
	}
	return ulid.Timestamp(id.value), nil
}

// Parse validates s as a canonical ID[K] string — the correct kind prefix
// followed by a well-formed, non-zero ULID body — and returns the
// identifier it encodes. Malformed input, a wrong prefix, and a body that
// decodes to the reserved all-zero value are all rejected with
// ErrInvalidID; nothing is repaired or guessed.
func Parse[K Kind](s string) (ID[K], error) {
	var k K
	prefix := k.Prefix()

	rest, ok := strings.CutPrefix(s, prefix+"_")
	if !ok {
		return ID[K]{}, fmt.Errorf("%w: %q does not have the %q prefix", ErrInvalidID, s, prefix)
	}

	v, err := ulid.Decode(rest)
	if err != nil {
		return ID[K]{}, fmt.Errorf("%w: %v", ErrInvalidID, err)
	}
	if v == ([16]byte{}) {
		return ID[K]{}, fmt.Errorf("%w: the all-zero value is reserved for the unset zero value", ErrInvalidID)
	}

	return ID[K]{value: v, set: true}, nil
}

// MustParse is like Parse but panics on error. It is intended for
// programmer-controlled constants, fixtures, and tests, not for parsing
// external or generated input.
func MustParse[K Kind](s string) ID[K] {
	v, err := Parse[K](s)
	if err != nil {
		panic(err)
	}
	return v
}
