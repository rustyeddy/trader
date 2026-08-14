package oanda

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// Sentinel errors, classifiable with errors.Is.
var (
	// ErrInstrumentOutOfScope reports a provider symbol that is recognized as
	// not an in-scope M2 FX pair — most notably XAUUSD, whose XAU leg is not
	// an in-scope currency. It is deliberately distinct from a malformed or
	// unrecognizable symbol: a caller walking the archive uses it to skip a
	// partition on purpose rather than treating it as an error in the data.
	ErrInstrumentOutOfScope = errors.New("oanda: instrument out of scope")

	// ErrUnsupportedInterval reports an interval token that is not one of the
	// raw partitions this reader supports (m1, h1, h4, d1/d). W1 is not present
	// in the raw corpus and is not invented, so it lands here.
	ErrUnsupportedInterval = errors.New("oanda: unsupported interval")

	// ErrMalformedData reports a structurally invalid file or row: a bad
	// schema line, wrong field count, or an unparseable field. It wraps the
	// specific cause for diagnostics.
	ErrMalformedData = errors.New("oanda: malformed data")
)

// inScopeFXCurrencies is the set of currencies whose pairwise combinations
// make up the 24 in-scope M2 FX pairs. A symbol is in scope only when both
// its legs are in this set; XAU (gold) is intentionally absent, which is what
// takes XAUUSD out of scope. num.ParseCurrency accepts any 3–5 uppercase
// letters and embeds no ISO table, so XAU would otherwise parse as a
// structurally valid currency — this allowlist is what keeps gold from being
// silently treated as an FX leg.
var inScopeFXCurrencies = map[string]struct{}{
	"AUD": {}, "CAD": {}, "CHF": {}, "EUR": {},
	"GBP": {}, "JPY": {}, "NZD": {}, "USD": {},
}

// resolveSymbol maps a raw OANDA provider symbol such as "EURUSD" to its
// canonical instrument.ID through instrument's currency-pair facility. It
// reports ErrInstrumentOutOfScope for a well-formed six-letter symbol whose
// legs are not both in-scope FX currencies (for example XAUUSD), and
// ErrMalformedData for a symbol that is not six uppercase letters at all.
func resolveSymbol(symbol string) (instrument.ID, error) {
	if len(symbol) != 6 || !isUpperAlpha(symbol) {
		return instrument.ID{}, fmt.Errorf("%w: symbol %q is not six uppercase letters", ErrMalformedData, symbol)
	}
	base, quote := symbol[:3], symbol[3:]
	if _, ok := inScopeFXCurrencies[base]; !ok {
		return instrument.ID{}, fmt.Errorf("%w: %q (base %q)", ErrInstrumentOutOfScope, symbol, base)
	}
	if _, ok := inScopeFXCurrencies[quote]; !ok {
		return instrument.ID{}, fmt.Errorf("%w: %q (quote %q)", ErrInstrumentOutOfScope, symbol, quote)
	}
	id := instrument.CurrencyPairID(num.MustParseCurrency(base), num.MustParseCurrency(quote))
	if id.IsZero() {
		// Unreachable for two distinct in-scope currencies, but never return a
		// zero ID as if it were a real identity.
		return instrument.ID{}, fmt.Errorf("%w: %q produced a zero instrument id", ErrMalformedData, symbol)
	}
	return id, nil
}

// resolveInterval maps a raw partition's interval token to a marketdata
// Interval. Both the path token "d1" and the schema comment's "d" map to D1;
// "m1", "h1", and "h4" map to their obvious intervals. Anything else,
// including "w1", reports ErrUnsupportedInterval.
func resolveInterval(token string) (marketdata.Interval, error) {
	switch token {
	case "m1":
		return marketdata.M1, nil
	case "h1":
		return marketdata.H1, nil
	case "h4":
		return marketdata.H4, nil
	case "d1", "d":
		return marketdata.D1, nil
	default:
		return marketdata.Interval{}, fmt.Errorf("%w: %q", ErrUnsupportedInterval, token)
	}
}

// isUpperAlpha reports whether s consists solely of ASCII A–Z.
func isUpperAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return len(s) > 0
}
