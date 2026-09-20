package marketdata

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// RegisterFXInstrument registers a Listing for a 6-letter FX pair
// symbol (for example "EURUSD") under provider into resolver, and
// returns the resulting instrument.ID. This is the one place M2.5
// constructs a Listing from a bare symbol string on a caller's
// behalf — deliberately living here, in the service layer, rather
// than in a transport adapter: ADR-022 assigns "translate application
// requests into domain types" to services, and a CLI (or future REST
// adapter) should describe what dataset it wants — an instrument
// symbol, an interval, a range — not invent domain/execution facts
// like tick size or quantity increment itself.
//
// Those Spec fields are exactly that kind of fact, and this function
// does invent conservative FX-convention values for them (a 0.001
// tick for JPY-quoted pairs, 0.00001 otherwise; quantity increment and
// multiplier both 1) — but doing so is safe specifically because no
// *marketdata.Manager operation (Bars, Coverage, Plan, and, via
// Service, Sync/Build/Update) ever reads a resolved Listing's Spec;
// every one of them only ever calls Listing.Symbol(). Registering a
// Listing at all is a mechanical requirement of instrument.Resolver's
// contract (ADR-016), not evidence that these particular values are
// fit for any execution, sizing, or order-validation purpose. A
// caller must not read Spec back off a Listing this function
// registered and treat it as authoritative for anything beyond
// satisfying that one constructor requirement — if a future M3
// consumer needs a real, trustworthy Spec, it needs a real instrument
// catalog, not this function.
//
// This is a deliberately narrow, FX-only v0 convention (M2's only
// asset class so far), not a general instrument catalog or
// symbol-resolution service.
func RegisterFXInstrument(resolver *instrument.MemoryResolver, provider, symbol string) (instrument.ID, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if len(symbol) != 6 {
		return instrument.ID{}, fmt.Errorf(
			"invalid instrument %q: expected a 6-letter FX pair symbol, e.g. EURUSD", symbol)
	}
	base, quote := symbol[:3], symbol[3:]

	baseCur, err := num.ParseCurrency(base)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid instrument %q: %w", symbol, err)
	}
	quoteCur, err := num.ParseCurrency(quote)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid instrument %q: %w", symbol, err)
	}

	inst, err := instrument.NewCurrencyPair(baseCur, quoteCur)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid instrument %q: %w", symbol, err)
	}

	tick := "0.00001"
	if quote == "JPY" {
		tick = "0.001"
	}
	spec, err := instrument.NewSpec(
		num.MustParsePrice(tick),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		quoteCur,
	)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid instrument %q: %w", symbol, err)
	}

	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   provider,
		Symbol:     symbol,
		Spec:       spec,
		Tradable:   true,
	})
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid instrument %q: %w", symbol, err)
	}

	if err := resolver.Register(listing); err != nil {
		return instrument.ID{}, fmt.Errorf("registering instrument %q: %w", symbol, err)
	}

	return listing.InstrumentID(), nil
}
