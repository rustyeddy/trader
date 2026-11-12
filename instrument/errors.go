package instrument

import "errors"

var (
	// ErrInvalidInstrument reports an Instrument constructor argument that
	// fails validation: an invalid or matching currency pair, an empty
	// exchange/ticker/root/name, or a missing futures expiration.
	ErrInvalidInstrument = errors.New("instrument: invalid instrument")

	// ErrInvalidSpec reports a Spec constructor argument that fails
	// validation, or a price/quantity that is not an exact multiple of a
	// Spec's tick size or quantity increment.
	ErrInvalidSpec = errors.New("instrument: invalid spec")

	// ErrInvalidListing reports a Listing constructor argument that fails
	// validation: an unconstructed Instrument, an empty provider/symbol, a
	// Spec that was not constructed with NewSpec, or Tradable: true for an
	// Instrument that is non-orderable by Kind.
	ErrInvalidListing = errors.New("instrument: invalid listing")

	// ErrInvalidID reports JSON input to ID.UnmarshalJSON that either is
	// not a JSON string, or is a non-empty string that is not the exact
	// canonical text one of the per-kind ID constructors produces — see
	// ID.UnmarshalJSON's own doc comment for the full contract.
	ErrInvalidID = errors.New("instrument: invalid id")
)
