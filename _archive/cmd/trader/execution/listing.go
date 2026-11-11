package execution

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// simListingFlags holds the instrument-spec flags every leaf command
// here needs: a bare FX symbol plus the simulator-specific tick size,
// quantity increment, and contract multiplier order construction
// validates against — the same shape cmd/trader/broker's own
// simListingFlags establishes, duplicated here rather than shared
// across command-family packages (issue #201).
type simListingFlags struct {
	symbol            string
	tickSize          string
	quantityIncrement string
	multiplier        string
}

// normalizeSymbol upper-cases and trims s, the same normalization
// buildSimListing applies to flags.symbol before constructing a
// Listing. Exposed separately so submit.go can resolve its own
// simbroker.FillPriceSource against the exact symbol the eventual
// Listing.Symbol() will report, matching cmd/trader/broker's own
// submit command, which resolves its price source from the already-
// built Listing's Symbol() rather than the raw flag.
func normalizeSymbol(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// buildSimListing constructs an instrument.Listing for the
// deterministic simulator from a bare 6-letter FX symbol plus
// simulator-specific spec flags. See cmd/trader/broker's own
// buildSimListing doc comment for why this lives at the CLI/
// composition-root layer rather than in service/execution.
func buildSimListing(flags simListingFlags, provider string) (instrument.Listing, error) {
	symbol := normalizeSymbol(flags.symbol)
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
