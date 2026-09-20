package oanda

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// Sentinel errors, classifiable with errors.Is.
var (
	// ErrInstrumentOutOfScope reports a provider symbol that is well-formed
	// but is not one of the 24 in-scope M2 FX pairs — XAUUSD (gold is not an
	// FX leg), and also any valid-looking pair such as USDEUR that is simply
	// not part of the preserved corpus. It is deliberately distinct from a
	// malformed symbol: a caller walking the archive uses it to skip a
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

// inScopeFXPairs is the exact set of 24 FX pairs preserved in the archive and
// in scope for M2. It is an audited symbol set, not a currency-combination
// rule: two in-scope currencies do not by themselves make an in-scope pair
// (there is no USDEUR or CADNZD partition), and gold (XAU) is not present at
// all. A symbol is in scope only if it appears here verbatim.
var inScopeFXPairs = map[string]struct{}{
	"AUDCAD": {}, "AUDCHF": {}, "AUDJPY": {}, "AUDNZD": {}, "AUDUSD": {},
	"CADJPY": {}, "CHFJPY": {},
	"EURAUD": {}, "EURCAD": {}, "EURCHF": {}, "EURGBP": {}, "EURJPY": {}, "EURNZD": {}, "EURUSD": {},
	"GBPAUD": {}, "GBPCAD": {}, "GBPJPY": {}, "GBPNZD": {}, "GBPUSD": {},
	"NZDJPY": {}, "NZDUSD": {},
	"USDCAD": {}, "USDCHF": {}, "USDJPY": {},
}

// resolveSymbol maps a raw OANDA provider symbol such as "EURUSD" to its
// canonical instrument.ID through instrument's currency-pair facility. It
// reports ErrInstrumentOutOfScope for a well-formed six-letter symbol that is
// not one of the 24 in-scope pairs (for example XAUUSD or USDEUR), and
// ErrMalformedData for a symbol that is not six uppercase letters at all.
func resolveSymbol(symbol string) (instrument.ID, error) {
	if len(symbol) != 6 || !isUpperAlpha(symbol) {
		return instrument.ID{}, fmt.Errorf("%w: symbol %q is not six uppercase letters", ErrMalformedData, symbol)
	}
	if _, ok := inScopeFXPairs[symbol]; !ok {
		return instrument.ID{}, fmt.Errorf("%w: %q", ErrInstrumentOutOfScope, symbol)
	}
	id := instrument.CurrencyPairID(num.MustParseCurrency(symbol[:3]), num.MustParseCurrency(symbol[3:]))
	if id.IsZero() {
		// Unreachable for an in-scope pair, but never return a zero ID as if
		// it were a real identity.
		return instrument.ID{}, fmt.Errorf("%w: %q produced a zero instrument id", ErrMalformedData, symbol)
	}
	return id, nil
}

// resolveInterval maps a raw partition's interval token to a RawInterval. Both
// the path token "d1" and the schema comment's "d" map to RawD1; "m1", "h1",
// and "h4" map to their obvious tokens. Anything else, including "w1", reports
// ErrUnsupportedInterval.
func resolveInterval(token string) (RawInterval, error) {
	switch token {
	case "m1":
		return RawM1, nil
	case "h1":
		return RawH1, nil
	case "h4":
		return RawH4, nil
	case "d1", "d":
		return RawD1, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedInterval, token)
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
