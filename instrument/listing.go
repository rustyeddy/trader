package instrument

import (
	"fmt"
	"strings"
)

// Listing is one venue's tradable, or research-only non-tradable,
// representation of an Instrument: which venue, under what provider
// symbol, with what trading mechanics, and whether it can currently be
// traded there.
//
// Listing's fields are unexported; construct one with NewListing. A
// Listing's InstrumentID names an Instrument by ID but does not itself
// reference or validate against a constructed Instrument value — Listing
// has no dependency on a catalog or registry; composing the two, if a
// caller needs that, is left to the caller.
type Listing struct {
	instrumentID ID
	venue        string
	symbol       string
	spec         Spec
	tradable     bool
}

// ListingParams collects NewListing's arguments.
type ListingParams struct {
	// InstrumentID is the Instrument this Listing represents.
	InstrumentID ID

	// Venue names the provider or exchange offering this listing, for
	// example "OANDA" or "NASDAQ".
	Venue string

	// Symbol is the venue's own symbol text, for example "EUR_USD" or
	// "AAPL". It is preserved verbatim except for trimming — it is
	// display-only and never used as identity; see the package doc
	// comment.
	Symbol string

	// Spec holds this listing's trading mechanics. It must have been
	// constructed with NewSpec.
	Spec Spec

	// Tradable reports whether this listing can currently be traded.
	// KindContinuousSeries instruments are never tradable in practice,
	// but Listing does not enforce that against Kind — see the package
	// doc comment for why that distinction is Instrument.Kind's
	// responsibility, not a Listing-level flag combination.
	Tradable bool
}

// NewListing validates and returns a Listing. InstrumentID must be
// non-zero, Venue and Symbol must be non-empty once trimmed, and Spec must
// have been constructed with NewSpec.
func NewListing(p ListingParams) (Listing, error) {
	if p.InstrumentID.IsZero() {
		return Listing{}, fmt.Errorf("%w: instrument ID must not be zero", ErrInvalidListing)
	}
	venue := strings.TrimSpace(p.Venue)
	if venue == "" {
		return Listing{}, fmt.Errorf("%w: venue must not be empty", ErrInvalidListing)
	}
	symbol := strings.TrimSpace(p.Symbol)
	if symbol == "" {
		return Listing{}, fmt.Errorf("%w: symbol must not be empty", ErrInvalidListing)
	}
	if p.Spec.TickSize().IsZero() {
		return Listing{}, fmt.Errorf("%w: spec must be constructed with NewSpec", ErrInvalidListing)
	}
	return Listing{
		instrumentID: p.InstrumentID,
		venue:        venue,
		symbol:       symbol,
		spec:         p.Spec,
		tradable:     p.Tradable,
	}, nil
}

// InstrumentID returns the Instrument l represents.
func (l Listing) InstrumentID() ID { return l.instrumentID }

// Venue returns the provider or exchange offering l.
func (l Listing) Venue() string { return l.venue }

// Symbol returns the venue's own symbol text for l.
func (l Listing) Symbol() string { return l.symbol }

// Spec returns l's trading mechanics.
func (l Listing) Spec() Spec { return l.spec }

// Tradable reports whether l can currently be traded.
func (l Listing) Tradable() bool { return l.tradable }
