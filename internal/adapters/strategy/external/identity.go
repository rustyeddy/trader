package external

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/num"
)

// parseInstrumentID reconstructs an instrument.ID from its own
// canonical text form (for example "fx:EUR/USD" — instrument.ID.
// String's own documented example), the same text every
// instrument_id wire field carries (every *.proto message using one
// says so explicitly).
//
// This does not go through instrument.ID's own UnmarshalJSON: that
// method only checks for a recognized "<kind>:" prefix (instrument/
// encoding.go's own doc comment — "a structural sanity check ...
// not a claim that the referenced instrument actually exists"), so it
// accepts non-canonical strings no instrument constructor could ever
// have produced, such as "fx:" or "fx:eur/usd" (review finding).
// Instead, this parses each Kind's own documented text shape (the
// exact per-Kind format instrument.ID's own doc comment names) and
// reconstructs the ID through the matching public constructor —
// CurrencyPairID, EquityID, ETFID, FutureID, ContinuousSeriesID, or
// IndexID — the same constructors any other package uses to build
// one, so this can never accept a shape none of them would produce.
// Every constructor already returns the zero ID for invalid
// arguments; this rejects that outright, and additionally rejects a
// well-formed reconstruction whose own String() disagrees with s
// (case or whitespace variants of an otherwise-valid input, for
// example) rather than silently normalizing it.
func parseInstrumentID(s string) (instrument.ID, error) {
	if s == "" {
		return instrument.ID{}, fmt.Errorf("%w: instrument id must not be empty", ErrInvalidWireValue)
	}

	out, err := reconstructInstrumentID(s)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("%w: instrument id %q: %v", ErrInvalidWireValue, s, err)
	}
	if out.IsZero() {
		return instrument.ID{}, fmt.Errorf("%w: instrument id %q is not constructible", ErrInvalidWireValue, s)
	}
	if out.String() != s {
		return instrument.ID{}, fmt.Errorf("%w: instrument id %q is not in canonical form (canonical: %q)", ErrInvalidWireValue, s, out.String())
	}
	return out, nil
}

// reconstructInstrumentID dispatches s to the constructor matching
// its own "<kind>:" prefix. An unrecognized prefix, or a prefix whose
// own remainder does not match that Kind's documented shape, is an
// error here rather than a silently zero ID, so parseInstrumentID's
// own caller sees exactly what was wrong.
func reconstructInstrumentID(s string) (instrument.ID, error) {
	switch {
	case strings.HasPrefix(s, "fx:"):
		return parseCurrencyPairID(strings.TrimPrefix(s, "fx:"))
	case strings.HasPrefix(s, "etf:"):
		return parseExchangeTickerID(strings.TrimPrefix(s, "etf:"), instrument.ETFID)
	case strings.HasPrefix(s, "eq:"):
		return parseExchangeTickerID(strings.TrimPrefix(s, "eq:"), instrument.EquityID)
	case strings.HasPrefix(s, "fut:"):
		return parseFutureID(strings.TrimPrefix(s, "fut:"))
	case strings.HasPrefix(s, "cont:"):
		return instrument.ContinuousSeriesID(strings.TrimPrefix(s, "cont:")), nil
	case strings.HasPrefix(s, "idx:"):
		return instrument.IndexID(strings.TrimPrefix(s, "idx:")), nil
	default:
		return instrument.ID{}, fmt.Errorf("no recognized instrument id kind prefix")
	}
}

// parseCurrencyPairID parses "BASE/QUOTE" (the remainder after "fx:")
// into instrument.CurrencyPairID's two currencies.
func parseCurrencyPairID(rest string) (instrument.ID, error) {
	base, quote, ok := strings.Cut(rest, "/")
	if !ok {
		return instrument.ID{}, fmt.Errorf("fx id must have the form BASE/QUOTE")
	}
	baseCur, err := num.ParseCurrency(base)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("base currency: %w", err)
	}
	quoteCur, err := num.ParseCurrency(quote)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("quote currency: %w", err)
	}
	return instrument.CurrencyPairID(baseCur, quoteCur), nil
}

// parseExchangeTickerID parses "EXCHANGE:TICKER" (the remainder after
// "eq:" or "etf:") and reconstructs through ctor (instrument.EquityID
// or instrument.ETFID).
func parseExchangeTickerID(rest string, ctor func(exchange, ticker string) instrument.ID) (instrument.ID, error) {
	exchange, ticker, ok := strings.Cut(rest, ":")
	if !ok {
		return instrument.ID{}, fmt.Errorf("id must have the form EXCHANGE:TICKER")
	}
	return ctor(exchange, ticker), nil
}

// parseFutureID parses "ROOT:YYYY-MM" (the remainder after "fut:")
// into instrument.FutureID's root and expiration month.
func parseFutureID(rest string) (instrument.ID, error) {
	root, yearMonth, ok := strings.Cut(rest, ":")
	if !ok {
		return instrument.ID{}, fmt.Errorf("fut id must have the form ROOT:YYYY-MM")
	}
	expiration, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return instrument.ID{}, fmt.Errorf("expiration month: %w", err)
	}
	return instrument.FutureID(root, expiration), nil
}

// correlationIDOrEmpty formats v for a wire field: the empty string
// for the zero id.CorrelationID (the "not applicable" convention every
// exact-value/identity wire field documents), its canonical text
// otherwise. There is no wire-to-domain counterpart in this package:
// the host never receives a raw CorrelationID/EventID string to parse
// back (see doc.go — FillEvent is ToWire-only, and a DescribedIntent's
// correlation_token is an opaque grouping key, never a real
// CorrelationID, per IntentsFromWire's own doc comment).
func correlationIDOrEmpty(v id.CorrelationID) string {
	if v.IsZero() {
		return ""
	}
	return v.String()
}

// eventIDOrEmpty is correlationIDOrEmpty's id.EventID counterpart.
func eventIDOrEmpty(v id.EventID) string {
	if v.IsZero() {
		return ""
	}
	return v.String()
}
