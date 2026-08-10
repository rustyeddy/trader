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

// Expiration returns i's contract expiration and true when i.Kind() is
// KindFuture; otherwise it returns the zero time.Time and false.
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
// exchange. exchange and ticker must both be non-empty once trimmed.
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
// ticker on exchange. exchange and ticker must both be non-empty once
// trimmed.
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
	if exchange == "" {
		return "", "", fmt.Errorf("%w: exchange must not be empty", ErrInvalidInstrument)
	}
	if ticker == "" {
		return "", "", fmt.Errorf("%w: ticker must not be empty", ErrInvalidInstrument)
	}
	return exchange, ticker, nil
}

// NewFuture returns the Instrument for one specific expiring futures
// contract identified by its underlying root symbol and expiration. root
// must be non-empty once trimmed and expiration must be non-zero.
// Contracts on the same root with different expirations are different
// Instruments — see the package doc comment.
func NewFuture(root string, expiration time.Time) (Instrument, error) {
	root = normalizeIdentifierPart(root)
	if root == "" {
		return Instrument{}, fmt.Errorf("%w: root must not be empty", ErrInvalidInstrument)
	}
	if expiration.IsZero() {
		return Instrument{}, fmt.Errorf("%w: expiration must not be zero", ErrInvalidInstrument)
	}
	return Instrument{
		id:         FutureID(root, expiration),
		kind:       KindFuture,
		root:       root,
		expiration: expiration.UTC(),
	}, nil
}

// NewContinuousSeries returns the Instrument for the continuous,
// non-orderable research series derived from the futures family rooted at
// root. root must be non-empty once trimmed.
func NewContinuousSeries(root string) (Instrument, error) {
	root = normalizeIdentifierPart(root)
	if root == "" {
		return Instrument{}, fmt.Errorf("%w: root must not be empty", ErrInvalidInstrument)
	}
	return Instrument{
		id:   ContinuousSeriesID(root),
		kind: KindContinuousSeries,
		root: root,
	}, nil
}

// NewIndex returns the Instrument for the non-orderable index name. name
// must be non-empty once trimmed.
func NewIndex(name string) (Instrument, error) {
	trimmed := normalizeIdentifierPart(name)
	if trimmed == "" {
		return Instrument{}, fmt.Errorf("%w: name must not be empty", ErrInvalidInstrument)
	}
	return Instrument{
		id:   IndexID(trimmed),
		kind: KindIndex,
		name: trimmed,
	}, nil
}
