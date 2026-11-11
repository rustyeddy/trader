package accountactivity

import (
	"fmt"
	"strings"
)

// Basis says where a Value came from. The zero value is BasisUnknown.
type Basis uint8

const (
	// BasisUnknown means no source supplied the value. It is not zero: a
	// Value with this basis holds nothing, and a reported or derived zero is
	// a different, known fact.
	BasisUnknown Basis = iota
	// BasisReported means the broker reported the value; Trader did not
	// compute it.
	BasisReported
	// BasisDerived means Trader computed the value from other data, by a
	// named method.
	BasisDerived
)

// String returns the basis's lowercase name.
func (b Basis) String() string {
	switch b {
	case BasisReported:
		return "reported"
	case BasisDerived:
		return "derived"
	default:
		return "unknown"
	}
}

// Value is one datum together with where it came from (ADR-079: unknown is
// not zero, and reported is not derived). The zero Value is unknown and
// holds nothing. Construct known values with Reported, ReportedFrom or
// Derived.
//
// Provenance is per value, not per record: one Activity can carry a
// broker-reported price and spread beside a Trader-derived slippage.
type Value[T any] struct {
	v      T
	basis  Basis
	source string
}

// Reported returns a value the broker reported. The Activity's provider
// identity is its provenance; use ReportedFrom to record the provider's own
// field name as well.
func Reported[T any](v T) Value[T] {
	return Value[T]{v: v, basis: BasisReported}
}

// ReportedFrom returns a broker-reported value and the provider field it
// came from (for example "halfSpreadCost"). source must be non-empty.
func ReportedFrom[T any](v T, source string) (Value[T], error) {
	if strings.TrimSpace(source) == "" {
		return Value[T]{}, fmt.Errorf("%w: reported source must not be empty", ErrInvalidValue)
	}
	return Value[T]{v: v, basis: BasisReported, source: source}, nil
}

// Derived returns a value Trader computed. method names the derivation (or,
// for a benchmark, where it was observed) and must be non-empty: a derived
// number with no stated method is an estimate in disguise.
func Derived[T any](v T, method string) (Value[T], error) {
	if strings.TrimSpace(method) == "" {
		return Value[T]{}, fmt.Errorf("%w: derived value needs a method", ErrInvalidValue)
	}
	return Value[T]{v: v, basis: BasisDerived, source: method}, nil
}

// Known reports whether any source supplied the value.
func (v Value[T]) Known() bool { return v.basis != BasisUnknown }

// Basis returns where the value came from.
func (v Value[T]) Basis() Basis { return v.basis }

// Source returns the derivation method or the provider field name; it is
// empty for an unknown value or a reported one without a field name.
func (v Value[T]) Source() string { return v.source }

// Get returns the value and whether it is known. An unknown value returns
// T's zero value and false; callers must not treat that as a real zero.
func (v Value[T]) Get() (T, bool) {
	if v.basis == BasisUnknown {
		var zero T
		return zero, false
	}
	return v.v, true
}
