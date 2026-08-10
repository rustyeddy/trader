package instrument

import (
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// ID is Trader's canonical, deterministic identity for one economic
// instrument. See the package doc comment for why this is a different
// identity scheme from the id package's generated identifiers.
//
// ID has no exported fields and cannot be constructed to hold an arbitrary
// value; use one of the per-kind constructors below, or obtain one from an
// existing Instrument's ID method. Two calls to the same constructor with
// the same normalized economic attributes always produce an equal ID.
//
// The zero value of ID is not a valid identity; IsZero reports true for it.
type ID struct {
	raw string
}

// String returns id's canonical text form, for example "fx:EUR/USD" or
// "fut:ES:2026-12". The zero value renders as the empty string.
func (id ID) String() string {
	return id.raw
}

// IsZero reports whether id is the zero value.
func (id ID) IsZero() bool {
	return id.raw == ""
}

// Equal reports whether id and o hold the identical canonical identity.
func (id ID) Equal(o ID) bool {
	return id.raw == o.raw
}

// CurrencyPairID returns the canonical identity for the currency pair
// base/quote. Two calls with the same base and quote always produce an
// equal ID, regardless of which provider symbol spelling — "EUR_USD",
// "EURUSD", "EUR/USD" — a caller parsed before extracting base and quote.
//
// CurrencyPairID returns the zero ID if base or quote is not structurally
// valid, or if base and quote are equal — the zero value of ID is
// reserved for exactly this: a non-zero ID is always structurally
// meaningful, never a malformed identity built from invalid input.
func CurrencyPairID(base, quote num.Currency) ID {
	if !base.IsValid() || !quote.IsValid() || base.Equal(quote) {
		return ID{}
	}
	return ID{raw: "fx:" + base.String() + "/" + quote.String()}
}

// EquityID returns the canonical identity for the equity listed under
// ticker on exchange. exchange and ticker are trimmed and upper-cased
// before joining; see the package doc comment for why exchange+ticker is
// Trader's M1 identity convention, not a claim of permanent identity.
//
// EquityID returns the zero ID if, once normalized, exchange or ticker is
// empty or contains a character outside the safe identifier alphabet — see
// isValidIdentifierPart. That alphabet excludes ':' and '/' specifically
// so EquityID("A:B", "C") and EquityID("A", "B:C") can never both collapse
// to the same canonical string; rejecting the input is preferred over
// escaping it.
func EquityID(exchange, ticker string) ID {
	exchange = normalizeIdentifierPart(exchange)
	ticker = normalizeIdentifierPart(ticker)
	if !isValidIdentifierPart(exchange) || !isValidIdentifierPart(ticker) {
		return ID{}
	}
	return ID{raw: "eq:" + exchange + ":" + ticker}
}

// ETFID is EquityID's counterpart for exchange-traded funds. It is a
// distinct constructor, not a shared one, so an ETF can never collide with
// an equity that happens to share an exchange and ticker. Its input
// validation is identical to EquityID's.
func ETFID(exchange, ticker string) ID {
	exchange = normalizeIdentifierPart(exchange)
	ticker = normalizeIdentifierPart(ticker)
	if !isValidIdentifierPart(exchange) || !isValidIdentifierPart(ticker) {
		return ID{}
	}
	return ID{raw: "etf:" + exchange + ":" + ticker}
}

// FutureID returns the canonical identity for one specific expiring
// futures contract identified by its underlying root symbol and
// expiration month. Contracts on the same root with different expiration
// months — ES December 2026 versus ES March 2027 — produce different IDs;
// see the package doc comment for why the individual contract, not the
// contract family, is the Instrument.
//
// Only expiration's year and month are significant: FutureID's canonical
// string carries "YYYY-MM", not a full date, matching standard futures
// contract-month conventions and matching what NewFuture stores as
// Instrument.Expiration — the two are always kept in exact agreement, so
// there is no month in which two differently-timed calls could produce the
// same ID but disagree about Expiration, or vice versa.
//
// FutureID returns the zero ID if, once normalized, root is empty or
// contains a character outside the safe identifier alphabet, or if
// expiration is the zero time.Time.
func FutureID(root string, expiration time.Time) ID {
	root = normalizeIdentifierPart(root)
	if !isValidIdentifierPart(root) || expiration.IsZero() {
		return ID{}
	}
	return ID{raw: "fut:" + root + ":" + expiration.UTC().Format("2006-01")}
}

// ContinuousSeriesID returns the canonical identity for the continuous,
// non-orderable research series derived from the futures family rooted at
// root. It is always distinct from any FutureID sharing the same root.
//
// ContinuousSeriesID returns the zero ID if, once normalized, root is
// empty or contains a character outside the safe identifier alphabet.
func ContinuousSeriesID(root string) ID {
	root = normalizeIdentifierPart(root)
	if !isValidIdentifierPart(root) {
		return ID{}
	}
	return ID{raw: "cont:" + root}
}

// IndexID returns the canonical identity for the non-orderable index
// name.
//
// IndexID returns the zero ID if, once normalized, name is empty or
// contains a character outside the safe identifier alphabet.
func IndexID(name string) ID {
	name = normalizeIdentifierPart(name)
	if !isValidIdentifierPart(name) {
		return ID{}
	}
	return ID{raw: "idx:" + name}
}

func normalizeIdentifierPart(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// isValidIdentifierPart reports whether s — already normalized by
// normalizeIdentifierPart — is safe to use as one segment of a canonical
// ID string: non-empty, and built only from characters that can never be
// confused with the ':' and '/' separators used to join segments together.
// Real exchange codes, tickers, and futures roots are well served by
// letters, digits, '.', and '-' (for example "BRK.B", "RDS-A", "6E"); this
// package intentionally does not attempt to escape or otherwise accept a
// wider alphabet, since rejecting an ambiguous input is safer than
// guessing how to encode it unambiguously.
func isValidIdentifierPart(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '-':
		default:
			return false
		}
	}
	return true
}
