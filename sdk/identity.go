package sdk

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// ErrInvalidWireValue reports a wire message field that is malformed,
// missing, or outside what this v1 surface defines. Conversion never
// guesses or repairs an invalid value; it fails explicitly.
var ErrInvalidWireValue = errors.New("sdk: invalid wire value")

// parseInstrumentID reconstructs an instrument.ID from its own
// canonical text form (for example "fx:EUR/USD"), the same text every
// instrument_id wire field carries. It reconstructs through the
// matching public per-Kind constructor and requires an exact String()
// round trip, rather than accepting anything with a merely recognized
// "<kind>:" prefix — the identical discipline
// adapters/strategy/external's own parseInstrumentID uses (this
// package's own translation logic is not shared code with that one;
// see the package doc comment), since each side of the v1 boundary
// owns and tests its own half of the wire mapping independently.
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

func parseExchangeTickerID(rest string, ctor func(exchange, ticker string) instrument.ID) (instrument.ID, error) {
	exchange, ticker, ok := strings.Cut(rest, ":")
	if !ok {
		return instrument.ID{}, fmt.Errorf("id must have the form EXCHANGE:TICKER")
	}
	return ctor(exchange, ticker), nil
}

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
