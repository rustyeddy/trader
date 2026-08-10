package instrument

import (
	"fmt"
	"sync"
)

// Resolver resolves provider-specific symbols and Instrument+venue
// combinations to Listings, without ever treating an alias or a provider
// symbol as instrument identity — see the package doc comment.
//
// Resolver is intentionally read-only: how a Resolver's mappings are
// populated — in-memory registration (MemoryResolver), a loaded config
// file, a future persistent catalog — is an implementation concern, not
// part of the contract callers depend on.
//
// Both methods treat an empty provider, venue, or symbol argument as
// meaning "unconstrained on this axis," never as a literal empty value to
// match against. A Listing legitimately registered with an empty Venue —
// spot FX has no meaningful centralized venue, see Listing — is still
// found by a query that passes "" for venue; "" is a wildcard on the
// query side, not a distinct registered value to search for.
//
// Both methods resolve to exactly one Listing or fail: zero matches after
// applying whatever context was supplied reports ErrUnknownSymbol, and
// more than one reports ErrAmbiguousSymbol. A Resolver never guesses which
// Listing a caller means; the caller must supply enough provider/venue
// context to narrow the match to one.
type Resolver interface {
	// ResolveSymbol resolves the Listing registered under provider, venue,
	// and symbol. provider and symbol should normally be supplied; venue
	// may be left "" when it does not disambiguate — but if a provider
	// exposes the same symbol on more than one venue, an unconstrained
	// venue will report ErrAmbiguousSymbol rather than pick one.
	ResolveSymbol(provider, venue, symbol string) (Listing, error)

	// ResolveInstrument resolves the orderable Listing for instrumentID,
	// optionally narrowed by provider and/or venue.
	ResolveInstrument(instrumentID ID, provider, venue string) (Listing, error)
}

// providerSymbolKey indexes MemoryResolver's registrations by provider and
// symbol only — not venue — because a provider is not guaranteed to expose
// a symbol on at most one venue; venue narrows within the indexed slice at
// resolution time instead. Both fields are pre-normalized with
// normalizeIdentifierPart so lookups are case-insensitive.
type providerSymbolKey struct {
	provider string
	symbol   string
}

// MemoryResolver is an in-memory, mutable Resolver: the reference
// implementation this package provides. It holds no package-level state —
// every MemoryResolver returned by NewMemoryResolver is independent of
// every other one, so multiple resolvers (per environment, per test) can
// be configured without interfering with each other. It is safe for
// concurrent use: registration and resolution are both expected to happen
// from multiple goroutines in real Trader use.
type MemoryResolver struct {
	mu           sync.RWMutex
	bySymbol     map[providerSymbolKey][]Listing
	byInstrument map[ID][]Listing
}

var _ Resolver = (*MemoryResolver)(nil)

// NewMemoryResolver returns an empty MemoryResolver.
func NewMemoryResolver() *MemoryResolver {
	return &MemoryResolver{
		bySymbol:     make(map[providerSymbolKey][]Listing),
		byInstrument: make(map[ID][]Listing),
	}
}

// Register indexes listing under its own Provider/Venue/Symbol and under
// its InstrumentID. listing must be a constructed, non-zero Listing.
// Register reports ErrDuplicateListing if a Listing with the same
// Provider, Venue, and Symbol (all case-insensitive) is already
// registered on r.
func (r *MemoryResolver) Register(listing Listing) error {
	if listing.InstrumentID().IsZero() {
		return fmt.Errorf("%w: listing must be constructed", ErrInvalidListing)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := newProviderSymbolKey(listing.Provider(), listing.Symbol())
	if findByVenue(r.bySymbol[key], listing.Venue()) != nil {
		return fmt.Errorf("%w: provider %q venue %q symbol %q", ErrDuplicateListing, listing.Provider(), listing.Venue(), listing.Symbol())
	}

	r.bySymbol[key] = append(r.bySymbol[key], listing)
	r.byInstrument[listing.InstrumentID()] = append(r.byInstrument[listing.InstrumentID()], listing)
	return nil
}

// RegisterAlias registers an additional provider/venue/symbol lookup key —
// aliasProvider, aliasVenue, aliasSymbol — that resolves to the Listing
// already registered under canonicalProvider/canonicalVenue/
// canonicalSymbol. The canonical reference is resolved with the same
// zero/one/many logic ResolveSymbol uses, so an underspecified canonical
// reference reports ErrAmbiguousSymbol here too, at registration time
// rather than silently aliasing the wrong Listing.
//
// The Listing an alias resolves to is always the canonical Listing value,
// unchanged: its own Provider, Venue, and Symbol remain whatever it was
// registered with. An alias is never itself indexed as a new Listing, and
// never gains its own entry in the instrument index — aliasing a symbol
// twice must never make ResolveInstrument see two listings where there is
// only one.
//
// RegisterAlias reports ErrUnknownSymbol or ErrAmbiguousSymbol if the
// canonical reference does not resolve to exactly one Listing, and
// ErrDuplicateListing if the alias's own provider/venue/symbol is already
// registered.
func (r *MemoryResolver) RegisterAlias(aliasProvider, aliasVenue, aliasSymbol, canonicalProvider, canonicalVenue, canonicalSymbol string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canonical, err := r.lookupSymbol(canonicalProvider, canonicalVenue, canonicalSymbol)
	if err != nil {
		return err
	}

	key := newProviderSymbolKey(aliasProvider, aliasSymbol)
	if findByVenue(r.bySymbol[key], aliasVenue) != nil {
		return fmt.Errorf("%w: provider %q venue %q symbol %q", ErrDuplicateListing, aliasProvider, aliasVenue, aliasSymbol)
	}

	r.bySymbol[key] = append(r.bySymbol[key], canonical)
	return nil
}

// ResolveSymbol implements Resolver.
func (r *MemoryResolver) ResolveSymbol(provider, venue, symbol string) (Listing, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lookupSymbol(provider, venue, symbol)
}

// lookupSymbol is ResolveSymbol's implementation, factored out so
// RegisterAlias can call it while already holding the write lock — it
// assumes the caller holds whatever lock is appropriate and never locks
// itself.
func (r *MemoryResolver) lookupSymbol(provider, venue, symbol string) (Listing, error) {
	key := newProviderSymbolKey(provider, symbol)
	candidates := filterByVenue(r.bySymbol[key], venue)
	return exactlyOne(candidates, fmt.Sprintf("provider %q venue %q symbol %q", provider, venue, symbol))
}

// ResolveInstrument implements Resolver.
func (r *MemoryResolver) ResolveInstrument(instrumentID ID, provider, venue string) (Listing, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidates := r.byInstrument[instrumentID]
	if provider != "" {
		candidates = filterByProvider(candidates, provider)
	}
	candidates = filterByVenue(candidates, venue)
	return exactlyOne(candidates, fmt.Sprintf("instrument %s provider %q venue %q", instrumentID, provider, venue))
}

func newProviderSymbolKey(provider, symbol string) providerSymbolKey {
	return providerSymbolKey{
		provider: normalizeIdentifierPart(provider),
		symbol:   normalizeIdentifierPart(symbol),
	}
}

// findByVenue returns the first Listing in candidates whose Venue matches
// venue (case-insensitively, including two empty venues matching each
// other), or nil if none does. Unlike filterByVenue, an empty venue here
// is an exact value to match, not a wildcard — findByVenue answers "is
// this exact provider/venue/symbol combination already registered,"
// which is a different question from "which listings match this query."
func findByVenue(candidates []Listing, venue string) *Listing {
	venue = normalizeIdentifierPart(venue)
	for i, l := range candidates {
		if normalizeIdentifierPart(l.Venue()) == venue {
			return &candidates[i]
		}
	}
	return nil
}

// filterByVenue narrows candidates to those whose Venue matches venue
// case-insensitively. An empty venue is a wildcard here — it leaves
// candidates unfiltered — matching Resolver's documented "" semantics.
func filterByVenue(candidates []Listing, venue string) []Listing {
	if venue == "" {
		return candidates
	}
	venue = normalizeIdentifierPart(venue)

	var out []Listing
	for _, l := range candidates {
		if normalizeIdentifierPart(l.Venue()) == venue {
			out = append(out, l)
		}
	}
	return out
}

// filterByProvider narrows candidates to those whose Provider matches
// provider case-insensitively. Callers only invoke this when provider is
// non-empty; there is no wildcard case to handle here.
func filterByProvider(candidates []Listing, provider string) []Listing {
	provider = normalizeIdentifierPart(provider)

	var out []Listing
	for _, l := range candidates {
		if normalizeIdentifierPart(l.Provider()) == provider {
			out = append(out, l)
		}
	}
	return out
}

// exactlyOne implements Resolver's shared zero/one/many resolution
// outcome: ErrUnknownSymbol, the single match, or ErrAmbiguousSymbol. Both
// ResolveSymbol and ResolveInstrument funnel through this so the two
// resolution directions cannot drift apart on failure semantics.
func exactlyOne(candidates []Listing, desc string) (Listing, error) {
	switch len(candidates) {
	case 0:
		return Listing{}, fmt.Errorf("%w: %s", ErrUnknownSymbol, desc)
	case 1:
		return candidates[0], nil
	default:
		return Listing{}, fmt.Errorf("%w: %s matches %d listings", ErrAmbiguousSymbol, desc, len(candidates))
	}
}
