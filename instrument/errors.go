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

	// ErrUnknownSymbol reports a Resolver lookup — by provider/venue/symbol
	// or by Instrument — that matched no registered Listing.
	ErrUnknownSymbol = errors.New("instrument: unknown symbol")

	// ErrAmbiguousSymbol reports a Resolver lookup that matched more than
	// one registered Listing: the supplied provider/venue context was not
	// enough to identify exactly one. Resolver never guesses; the caller
	// must narrow the lookup.
	ErrAmbiguousSymbol = errors.New("instrument: ambiguous symbol")

	// ErrDuplicateListing reports an attempt to register a Listing or
	// alias under a provider/venue/symbol combination that is already
	// registered on that Resolver.
	ErrDuplicateListing = errors.New("instrument: duplicate listing registration")

	// ErrInvalidResolution reports a Resolver query missing required
	// context: ResolveSymbol requires a non-empty provider and symbol
	// (venue alone may be left unconstrained).
	ErrInvalidResolution = errors.New("instrument: invalid resolution query")
)
