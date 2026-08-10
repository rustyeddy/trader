package instrument

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/num"
)

// Instrument is one economic instrument: what the thing is, independent of
// any provider, venue, or symbol spelling. See Listing for how a specific
// provider or venue exposes an Instrument for trading.
//
// Instrument's fields are unexported; construct one with NewCurrencyPair,
// NewEquity, NewETF, NewFuture, NewContinuousSeries, or NewIndex, and read
// it back through ID, Kind, and the kind-specific accessors below. There is
// no way to construct an Instrument holding a value inconsistent with its
// Kind: each accessor's second return value reports whether it applies to
// i's Kind, so callers cannot mistake a zero value for a legitimate one.
type Instrument struct {
	id   ID
	kind Kind

	base, quote num.Currency // KindCurrencyPair only
	exchange    string       // KindEquity, KindETF
	ticker      string       // KindEquity, KindETF
	root        string       // KindFuture, KindContinuousSeries
	expiration  time.Time    // KindFuture only
	name        string       // KindIndex only
}

// ID returns i's canonical, deterministic identity.
func (i Instrument) ID() ID { return i.id }

// Kind returns the category of economic instrument i represents.
func (i Instrument) Kind() Kind { return i.kind }

// Base returns i's base currency and true when i.Kind() is
// KindCurrencyPair; otherwise it returns the zero Currency and false.
func (i Instrument) Base() (num.Currency, bool) {
	return i.base, i.kind == KindCurrencyPair
}

// Quote returns i's quote currency and true when i.Kind() is
// KindCurrencyPair; otherwise it returns the zero Currency and false.
func (i Instrument) Quote() (num.Currency, bool) {
	return i.quote, i.kind == KindCurrencyPair
}

// Exchange returns i's exchange and true when i.Kind() is KindEquity or
// KindETF; otherwise it returns "" and false.
func (i Instrument) Exchange() (string, bool) {
	return i.exchange, i.kind == KindEquity || i.kind == KindETF
}

// Ticker returns i's ticker and true when i.Kind() is KindEquity or
// KindETF; otherwise it returns "" and false.
func (i Instrument) Ticker() (string, bool) {
	return i.ticker, i.kind == KindEquity || i.kind == KindETF
}

// Root returns i's underlying root symbol and true when i.Kind() is
// KindFuture or KindContinuousSeries; otherwise it returns "" and false.
func (i Instrument) Root() (string, bool) {
	return i.root, i.kind == KindFuture || i.kind == KindContinuousSeries
}

// Expiration returns i's contract month and true when i.Kind() is
// KindFuture; otherwise it returns the zero time.Time and false. The
// returned value is always the first instant of the contract's expiration
// month in UTC, not necessarily the exact time.Time NewFuture was called
// with — see NewFuture and FutureID for why only the year and month are
// significant to a futures contract's identity.
func (i Instrument) Expiration() (time.Time, bool) {
	return i.expiration, i.kind == KindFuture
}

// Name returns i's index name and true when i.Kind() is KindIndex;
// otherwise it returns "" and false.
func (i Instrument) Name() (string, bool) {
	return i.name, i.kind == KindIndex
}

// NewCurrencyPair returns the Instrument for the currency pair base/quote.
// base and quote must both be structurally valid and must differ.
func NewCurrencyPair(base, quote num.Currency) (Instrument, error) {
	if !base.IsValid() || !quote.IsValid() {
		return Instrument{}, fmt.Errorf("%w: base and quote currencies must be valid", ErrInvalidInstrument)
	}
	if base.Equal(quote) {
		return Instrument{}, fmt.Errorf("%w: base and quote currencies must differ", ErrInvalidInstrument)
	}
	return Instrument{
		id:    CurrencyPairID(base, quote),
		kind:  KindCurrencyPair,
		base:  base,
		quote: quote,
	}, nil
}

// NewEquity returns the Instrument for the equity listed under ticker on
// exchange. Once trimmed and upper-cased, exchange and ticker must both be
// non-empty and contain only letters, digits, '.', or '-'.
func NewEquity(exchange, ticker string) (Instrument, error) {
	exchange, ticker, err := validateExchangeTicker(exchange, ticker)
	if err != nil {
		return Instrument{}, err
	}
	return Instrument{
		id:       EquityID(exchange, ticker),
		kind:     KindEquity,
		exchange: exchange,
		ticker:   ticker,
	}, nil
}

// NewETF returns the Instrument for the exchange-traded fund listed under
// ticker on exchange. Once trimmed and upper-cased, exchange and ticker
// must both be non-empty and contain only letters, digits, '.', or '-'.
func NewETF(exchange, ticker string) (Instrument, error) {
	exchange, ticker, err := validateExchangeTicker(exchange, ticker)
	if err != nil {
		return Instrument{}, err
	}
	return Instrument{
		id:       ETFID(exchange, ticker),
		kind:     KindETF,
		exchange: exchange,
		ticker:   ticker,
	}, nil
}

func validateExchangeTicker(exchange, ticker string) (string, string, error) {
	exchange = normalizeIdentifierPart(exchange)
	ticker = normalizeIdentifierPart(ticker)
	if !isValidIdentifierPart(exchange) {
		return "", "", fmt.Errorf("%w: exchange must be non-empty and contain only letters, digits, '.', or '-'", ErrInvalidInstrument)
	}
	if !isValidIdentifierPart(ticker) {
		return "", "", fmt.Errorf("%w: ticker must be non-empty and contain only letters, digits, '.', or '-'", ErrInvalidInstrument)
	}
	return exchange, ticker, nil
}

// NewFuture returns the Instrument for one specific expiring futures
// contract identified by its underlying root symbol and expiration
// month. Once trimmed and upper-cased, root must be non-empty and contain
// only letters, digits, '.', or '-'; expiration must be non-zero.
// Contracts on the same root with different expiration months are
// different Instruments — see the package doc comment.
//
// Only expiration's year and month are significant to a futures
// contract's identity, matching standard contract-month conventions: two
// calls with expirations in the same month, even on different days,
// produce the same ID. Expiration() returns the canonicalized first
// instant of that month, never the exact time.Time passed in, so it can
// never disagree with what FutureID encoded into the ID.
func NewFuture(root string, expiration time.Time) (Instrument, error) {
	root = normalizeIdentifierPart(root)
	if !isValidIdentifierPart(root) {
		return Instrument{}, fmt.Errorf("%w: root must be non-empty and contain only letters, digits, '.', or '-'", ErrInvalidInstrument)
	}
	if expiration.IsZero() {
		return Instrument{}, fmt.Errorf("%w: expiration must not be zero", ErrInvalidInstrument)
	}
	contractMonth := canonicalContractMonth(expiration)
	return Instrument{
		id:         FutureID(root, contractMonth),
		kind:       KindFuture,
		root:       root,
		expiration: contractMonth,
	}, nil
}

// canonicalContractMonth truncates t to the first instant of its month in
// UTC — the only granularity that is significant to a futures contract's
// identity. Keeping FutureID and Instrument.Expiration derived from this
// same canonicalization is what keeps them from ever disagreeing.
func canonicalContractMonth(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// NewContinuousSeries returns the Instrument for the continuous,
// non-orderable research series derived from the futures family rooted at
// root. Once trimmed and upper-cased, root must be non-empty and contain
// only letters, digits, '.', or '-'.
func NewContinuousSeries(root string) (Instrument, error) {
	root = normalizeIdentifierPart(root)
	if !isValidIdentifierPart(root) {
		return Instrument{}, fmt.Errorf("%w: root must be non-empty and contain only letters, digits, '.', or '-'", ErrInvalidInstrument)
	}
	return Instrument{
		id:   ContinuousSeriesID(root),
		kind: KindContinuousSeries,
		root: root,
	}, nil
}

// NewIndex returns the Instrument for the non-orderable index name. Once
// trimmed and upper-cased, name must be non-empty and contain only
// letters, digits, '.', or '-'.
func NewIndex(name string) (Instrument, error) {
	name = normalizeIdentifierPart(name)
	if !isValidIdentifierPart(name) {
		return Instrument{}, fmt.Errorf("%w: name must be non-empty and contain only letters, digits, '.', or '-'", ErrInvalidInstrument)
	}
	return Instrument{
		id:   IndexID(name),
		kind: KindIndex,
		name: name,
	}, nil
}
