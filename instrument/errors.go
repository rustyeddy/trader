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
	// validation: a zero instrument ID, an empty venue/symbol, or a Spec
	// that was not constructed with NewSpec.
	ErrInvalidListing = errors.New("instrument: invalid listing")
)
