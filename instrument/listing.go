package instrument

import (
	"fmt"
	"strings"
)

// Listing is one venue's tradable, or research-only non-tradable,
// representation of an Instrument: which provider and venue, under what
// provider symbol, with what trading mechanics, and whether it can
// currently be traded there.
//
// Provider and Venue are kept distinct. A provider is a broker or data
// vendor — "OANDA", "IBKR" — and a venue is an exchange or execution
// venue — "NASDAQ", "CME". They are not interchangeable: one provider can
// expose listings on several venues (IBKR can offer a NASDAQ listing), and
// the same venue's listing can be reached through several providers.
// Venue may be empty for markets with no meaningful centralized venue,
// such as spot FX.
//
// Listing's fields are unexported; construct one with NewListing.
type Listing struct {
	instrumentID ID
	provider     string
	venue        string
	symbol       string
	spec         Spec
	tradable     bool
}

// ListingParams collects NewListing's arguments.
type ListingParams struct {
	// Instrument is the Instrument this Listing represents. NewListing
	// stores only its ID, but uses the full value to validate Tradable
	// against Instrument.Kind — see Tradable below.
	Instrument Instrument

	// Provider names the broker or data vendor offering this listing, for
	// example "OANDA" or "IBKR".
	Provider string

	// Venue names the exchange or execution venue, for example "NASDAQ"
	// or "CME". Venue may be left empty for markets with no meaningful
	// centralized venue, such as spot FX.
	Venue string

	// Symbol is the provider's own symbol text, for example "EUR_USD" or
	// "AAPL". It is preserved verbatim except for trimming — it is
	// display-only and never used as identity; see the package doc
	// comment.
	Symbol string

	// Spec holds this listing's trading mechanics. It must have been
	// constructed with NewSpec.
	Spec Spec

	// Tradable reports whether this listing can currently be traded.
	// NewListing rejects Tradable: true when Instrument.Kind() is
	// KindContinuousSeries or KindIndex — both are non-orderable by
	// definition, not merely by convention — see the package doc comment.
	Tradable bool
}

// NewListing validates and returns a Listing. Instrument must be a
// constructed, non-zero Instrument; Provider and Symbol must be non-empty
// once trimmed; Venue is trimmed but may be empty; Spec must have been
// constructed with NewSpec; and Tradable must not be true when Instrument
// is a KindContinuousSeries or KindIndex.
func NewListing(p ListingParams) (Listing, error) {
	if p.Instrument.ID().IsZero() {
		return Listing{}, fmt.Errorf("%w: instrument must be constructed", ErrInvalidListing)
	}
	provider := strings.TrimSpace(p.Provider)
	if provider == "" {
		return Listing{}, fmt.Errorf("%w: provider must not be empty", ErrInvalidListing)
	}
	symbol := strings.TrimSpace(p.Symbol)
	if symbol == "" {
		return Listing{}, fmt.Errorf("%w: symbol must not be empty", ErrInvalidListing)
	}
	if p.Spec.TickSize().IsZero() {
		return Listing{}, fmt.Errorf("%w: spec must be constructed with NewSpec", ErrInvalidListing)
	}
	if p.Tradable && isNeverTradable(p.Instrument.Kind()) {
		return Listing{}, fmt.Errorf("%w: a %s listing can never be tradable", ErrInvalidListing, p.Instrument.Kind())
	}
	return Listing{
		instrumentID: p.Instrument.ID(),
		provider:     provider,
		venue:        strings.TrimSpace(p.Venue),
		symbol:       symbol,
		spec:         p.Spec,
		tradable:     p.Tradable,
	}, nil
}

// isNeverTradable reports whether k is non-orderable by definition, not
// merely by convention: a continuous research series and an index are
// never the tradable thing themselves, even though tradable instruments —
// futures contracts on a root, ETFs tracking an index — may exist
// alongside them.
func isNeverTradable(k Kind) bool {
	return k == KindContinuousSeries || k == KindIndex
}

// InstrumentID returns the Instrument l represents.
func (l Listing) InstrumentID() ID { return l.instrumentID }

// Provider returns the broker or data vendor offering l.
func (l Listing) Provider() string { return l.provider }

// Venue returns the exchange or execution venue for l, or "" if l has no
// meaningful centralized venue.
func (l Listing) Venue() string { return l.venue }

// Symbol returns the provider's own symbol text for l.
func (l Listing) Symbol() string { return l.symbol }

// Spec returns l's trading mechanics.
func (l Listing) Spec() Spec { return l.spec }

// Tradable reports whether l can currently be traded.
func (l Listing) Tradable() bool { return l.tradable }
