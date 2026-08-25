package main

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// simListingFlags holds "trader broker submit"'s instrument-spec flags:
// a bare FX symbol plus the simulator-specific tick size, quantity
// increment, and contract multiplier order construction validates
// against.
type simListingFlags struct {
	symbol            string
	tickSize          string
	quantityIncrement string
	multiplier        string
}

// buildSimListing constructs an instrument.Listing for the
// deterministic simulator from a bare 6-letter FX symbol plus
// simulator-specific spec flags.
//
// Unlike service/marketdata.RegisterFXInstrument — whose own doc
// comment explicitly disclaims fitness for order-validation purposes,
// since no marketdata operation ever reads a resolved Listing's Spec —
// broker order submission does validate against Spec (tick size,
// quantity increment), so this construction lives here, at the CLI/
// composition-root layer, not in service/broker: the service layer
// coordinates broker use cases, it should not know how a CLI/simulator
// turns a bare symbol into a synthetic instrument specification (see
// the M3-12 design discussion on issue #155). If a future consumer
// needs a real, trustworthy instrument catalog, that is a dedicated
// resolver/catalog abstraction, not a reason to move this helper into
// the service layer.
func buildSimListing(flags simListingFlags, provider string) (instrument.Listing, error) {
	symbol := strings.ToUpper(strings.TrimSpace(flags.symbol))
	if len(symbol) != 6 {
		return instrument.Listing{}, fmt.Errorf("invalid --symbol %q: expected a 6-letter FX pair symbol, e.g. EURUSD", symbol)
	}
	base, quote := symbol[:3], symbol[3:]

	baseCur, err := num.ParseCurrency(base)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid --symbol %q: %w", symbol, err)
	}
	quoteCur, err := num.ParseCurrency(quote)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid --symbol %q: %w", symbol, err)
	}

	inst, err := instrument.NewCurrencyPair(baseCur, quoteCur)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid --symbol %q: %w", symbol, err)
	}

	tick, err := num.ParsePrice(flags.tickSize)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid --tick-size %q: %w", flags.tickSize, err)
	}
	qtyInc, err := num.ParseQuantity(flags.quantityIncrement)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid --quantity-increment %q: %w", flags.quantityIncrement, err)
	}
	mult, err := num.ParseRate(flags.multiplier)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid --multiplier %q: %w", flags.multiplier, err)
	}

	spec, err := instrument.NewSpec(tick, qtyInc, mult, quoteCur)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid instrument spec: %w", err)
	}

	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   provider,
		Symbol:     symbol,
		Spec:       spec,
		Tradable:   true,
	})
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid listing: %w", err)
	}
	return listing, nil
}
