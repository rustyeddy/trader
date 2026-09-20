package marketdata

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// EquityRegistration groups the identity fields needed to register one
// US equity or ETF Listing (issue #295, EQ-02). A struct, rather than
// RegisterFXInstrument's positional strings, because equity
// registration genuinely needs more independent fields than a single
// symbol can decompose (exchange, ticker, and a provider-native symbol
// that can legitimately differ from the ticker — Stooq's own SPY file
// is filed as "spy.us", not "SPY") — positional strings at that count
// invite silently swapped arguments.
type EquityRegistration struct {
	// Provider is the data provider this Listing is registered under
	// (for example "stooq" or "alpaca").
	Provider string
	// Exchange is the instrument's canonical listing exchange (for
	// example "NASDAQ", or "ARCA" for NYSE Arca — see ADR-047's own
	// identity requirement that Phase 1's reference instruments use
	// their actual listing exchange, not a placeholder). Normalized
	// and validated by instrument.NewEquity/NewETF.
	Exchange string
	// Ticker is the instrument's canonical ticker (for example
	// "AAPL"). Normalized and validated by instrument.NewEquity/NewETF.
	Ticker string
	// ProviderSymbol is the provider's own native symbol for this
	// instrument, used as the registered Listing's Symbol(). It is
	// deliberately independent of Ticker: two providers can spell the
	// same instrument differently (Stooq: "spy.us", Alpaca: "SPY").
	// If empty, Ticker is used verbatim.
	ProviderSymbol string
	// Currency is the instrument's settlement/quote currency (for
	// example num.MustParseCurrency("USD")). Unlike RegisterFXInstrument
	// (which derives its FX pair's settlement currency from the symbol
	// itself), an equity ticker carries no currency information, so
	// this is a required, explicit field — Phase 1 callers pass USD for
	// every reference instrument, but nothing here assumes that.
	Currency num.Currency
}

// RegisterEquityInstrument registers reg as a common-stock Listing
// (instrument.KindEquity) under resolver, and returns the resulting
// instrument.ID. See RegisterETFInstrument for the exchange-traded-fund
// equivalent, and EquityRegistration's own doc comment for why this
// takes a struct rather than positional strings.
//
// Like RegisterFXInstrument, this invents conservative Spec defaults
// (a $0.01 tick, whole-share quantity increment, and multiplier of 1 —
// ADR-047's own Phase 1 defaults) rather than looking up the
// instrument's real trading mechanics. This is safe for the same
// reason it is safe for FX: no *marketdata.Manager operation (Bars,
// Coverage, Plan, Sync, Build, Update) ever reads a resolved Listing's
// Spec — every one of them only ever calls Listing.Symbol(). A caller
// must not read Spec back off a Listing this function registered and
// treat it as authoritative for execution, sizing, or order
// validation — a real instrument catalog is required for that,
// exactly as RegisterFXInstrument's own doc comment already states for
// FX.
func RegisterEquityInstrument(resolver *instrument.MemoryResolver, reg EquityRegistration) (instrument.ID, error) {
	inst, err := instrument.NewEquity(reg.Exchange, reg.Ticker)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid equity %s:%s: %w", reg.Exchange, reg.Ticker, err)
	}
	return registerEquityLikeListing(resolver, reg, inst)
}

// RegisterETFInstrument registers reg as an exchange-traded-fund
// Listing (instrument.KindETF) under resolver, and returns the
// resulting instrument.ID. See RegisterEquityInstrument's own doc
// comment — every consideration there (Spec defaults, provenance,
// caller responsibility) applies identically here.
func RegisterETFInstrument(resolver *instrument.MemoryResolver, reg EquityRegistration) (instrument.ID, error) {
	inst, err := instrument.NewETF(reg.Exchange, reg.Ticker)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid ETF %s:%s: %w", reg.Exchange, reg.Ticker, err)
	}
	return registerEquityLikeListing(resolver, reg, inst)
}

// registerEquityLikeListing builds and registers the Listing shared by
// RegisterEquityInstrument and RegisterETFInstrument, which differ only
// in which instrument.New* constructor produced inst.
func registerEquityLikeListing(resolver *instrument.MemoryResolver, reg EquityRegistration, inst instrument.Instrument) (instrument.ID, error) {
	providerSymbol := strings.TrimSpace(reg.ProviderSymbol)
	if providerSymbol == "" {
		providerSymbol = strings.ToUpper(strings.TrimSpace(reg.Ticker))
	}

	// ADR-047's Phase 1 defaults: a cent tick, whole-share quantity
	// increment, and a multiplier of 1 (equities have no meaningful
	// contract multiplier, per instrument.NewSpec's own doc comment).
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		reg.Currency,
	)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid equity registration %s:%s: %w", reg.Exchange, reg.Ticker, err)
	}

	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   reg.Provider,
		// Venue is meaningful for equities/ETFs (unlike spot FX, which
		// RegisterFXInstrument leaves empty per Venue's own doc
		// comment): it is the exchange itself, exactly the value
		// already validated into inst.Exchange() above. Leaving it
		// empty would let two same-symbol listings on different
		// exchanges either collide or become impossible for a caller
		// to disambiguate via venue during resolution.
		Venue:    reg.Exchange,
		Symbol:   providerSymbol,
		Spec:     spec,
		Tradable: true,
	})
	if err != nil {
		return instrument.ID{}, fmt.Errorf("invalid equity registration %s:%s: %w", reg.Exchange, reg.Ticker, err)
	}

	if err := resolver.Register(listing); err != nil {
		return instrument.ID{}, fmt.Errorf("registering equity %s:%s: %w", reg.Exchange, reg.Ticker, err)
	}

	return listing.InstrumentID(), nil
}
