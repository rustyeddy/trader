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
func CurrencyPairID(base, quote num.Currency) ID {
	return ID{raw: "fx:" + base.String() + "/" + quote.String()}
}

// EquityID returns the canonical identity for the equity listed under
// ticker on exchange. exchange and ticker are trimmed and upper-cased
// before joining; see the package doc comment for why exchange+ticker is
// Trader's M1 identity convention, not a claim of permanent identity.
func EquityID(exchange, ticker string) ID {
	return ID{raw: "eq:" + normalizeIdentifierPart(exchange) + ":" + normalizeIdentifierPart(ticker)}
}

// ETFID is EquityID's counterpart for exchange-traded funds. It is a
// distinct constructor, not a shared one, so an ETF can never collide with
// an equity that happens to share an exchange and ticker.
func ETFID(exchange, ticker string) ID {
	return ID{raw: "etf:" + normalizeIdentifierPart(exchange) + ":" + normalizeIdentifierPart(ticker)}
}

// FutureID returns the canonical identity for one specific expiring
// futures contract identified by its underlying root symbol and
// expiration month. Contracts on the same root with different expirations
// — ES December 2026 versus ES March 2027 — produce different IDs; see
// the package doc comment for why the individual contract, not the
// contract family, is the Instrument.
func FutureID(root string, expiration time.Time) ID {
	return ID{raw: "fut:" + normalizeIdentifierPart(root) + ":" + expiration.UTC().Format("2006-01")}
}

// ContinuousSeriesID returns the canonical identity for the continuous,
// non-orderable research series derived from the futures family rooted at
// root. It is always distinct from any FutureID sharing the same root.
func ContinuousSeriesID(root string) ID {
	return ID{raw: "cont:" + normalizeIdentifierPart(root)}
}

// IndexID returns the canonical identity for the non-orderable index
// name.
func IndexID(name string) ID {
	return ID{raw: "idx:" + normalizeIdentifierPart(name)}
}

func normalizeIdentifierPart(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}
