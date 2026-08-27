package num

import "github.com/rustyeddy/trader/num/internal/fixed"

// Money is an immutable, self-describing monetary amount: a signed scaled
// int64 at the common 1e8 scale (ADR-004), paired with a mandatory Currency.
//
// The Go zero value of Money is invalid because it has no currency. Valid
// zero money must be constructed explicitly with a currency, for example
// MustParseMoney("0", MustParseCurrency("USD")).
//
// Same-currency amounts may be compared, added, and subtracted with checked
// arithmetic. Cross-currency arithmetic and comparison are rejected outright:
// currency is never silently assumed, dropped, or coerced. Currency
// conversion is not implicit. Money.MulRate deliberately keeps the existing
// currency rather than serving as a disguised conversion; Convert is the one
// method that changes currency, and it requires an explicit target currency
// and an explicit rate. Convert performs only the arithmetic — rate
// provenance (source, observation time) is a caller concern, most notably
// the portfolio package's cross-account aggregation.
type Money struct {
	amount   int64
	currency Currency
	valid    bool
}

// ParseMoney parses exact decimal text into a Money value denominated in
// currency.
//
// currency must itself be valid; an invalid or zero-value Currency is
// rejected with ErrMissingCurrency so a caller cannot construct money that
// looks self-describing but silently isn't.
func ParseMoney(s string, currency Currency) (Money, error) {
	if !currency.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.Parse(s)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: currency, valid: true}, nil
}

// MustParseMoney is like ParseMoney but panics on error.
//
// It is intended for programmer-controlled constants, fixtures, and tests,
// not for parsing external or market input.
func MustParseMoney(s string, currency Currency) Money {
	m, err := ParseMoney(s, currency)
	if err != nil {
		panic(err)
	}
	return m
}

// IsValid reports whether m has a valid currency. The Go zero value of Money,
// and any Money left unset, reports false.
func (m Money) IsValid() bool {
	return m.valid && m.currency.IsValid()
}

// Currency returns m's denomination.
func (m Money) Currency() Currency {
	return m.currency
}

// String returns m formatted as "<canonical amount> <currency>", for example
// "123.45 USD". This is Money's canonical text form; see MarshalText.
//
// String renders invalid Money, including the Go zero value, as
// "<invalid money>" rather than a currency-shaped string such as "0 ": an
// amount with no currency is not canonical output and must not be mistaken
// for it. MarshalText and MarshalJSON reject invalid Money outright; String
// exists for logging and debugging, where a caller cannot check an error, so
// it makes the problem visible instead.
func (m Money) String() string {
	if !m.IsValid() {
		return "<invalid money>"
	}
	return fixed.Format(m.amount) + " " + m.currency.String()
}

// IsZero reports whether m's amount is exactly zero. IsZero does not imply
// IsValid: a currency-less zero amount is still invalid Money.
func (m Money) IsZero() bool {
	return m.amount == 0
}

// sameCurrency reports whether m and o are both valid and share a currency,
// returning the mismatch error to return otherwise.
func (m Money) sameCurrency(o Money) error {
	if !m.IsValid() || !o.IsValid() {
		return ErrMissingCurrency
	}
	if !m.currency.Equal(o.currency) {
		return ErrCurrencyMismatch
	}
	return nil
}

// Cmp compares m and o, returning -1, 0, or +1.
//
// Cmp reports an error, rather than a meaningless ordering, when the two
// values are not both valid same-currency Money.
func (m Money) Cmp(o Money) (int, error) {
	if err := m.sameCurrency(o); err != nil {
		return 0, err
	}
	return fixed.Cmp(m.amount, o.amount), nil
}

// Equal reports whether m and o are the exact same amount in the same
// currency. Unlike Cmp, Equal never errors: differing or invalid currencies
// simply make two Money values unequal.
func (m Money) Equal(o Money) bool {
	return m.valid && o.valid && m.currency.Equal(o.currency) && m.amount == o.amount
}

// Add returns m+o using checked arithmetic.
//
// Add reports ErrCurrencyMismatch if m and o are denominated differently, and
// ErrMissingCurrency if either is invalid.
func (m Money) Add(o Money) (Money, error) {
	if err := m.sameCurrency(o); err != nil {
		return Money{}, err
	}
	raw, err := fixed.Add(m.amount, o.amount)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: m.currency, valid: true}, nil
}

// Sub returns m-o using checked arithmetic.
//
// Sub reports ErrCurrencyMismatch if m and o are denominated differently, and
// ErrMissingCurrency if either is invalid. Unlike Price and Quantity, a
// negative result is valid: Money is signed, since balances, fees, and PnL
// are routinely negative.
func (m Money) Sub(o Money) (Money, error) {
	if err := m.sameCurrency(o); err != nil {
		return Money{}, err
	}
	raw, err := fixed.Sub(m.amount, o.amount)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: m.currency, valid: true}, nil
}

// Neg returns -m, preserving currency.
//
// Neg rejects the one amount whose negation is not representable: the value
// backed by math.MinInt64.
func (m Money) Neg() (Money, error) {
	if !m.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.Neg(m.amount)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: m.currency, valid: true}, nil
}

// Abs returns the absolute value of m, preserving currency.
//
// Abs rejects the one amount whose absolute value is not representable: the
// value backed by math.MinInt64.
func (m Money) Abs() (Money, error) {
	if !m.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.Abs(m.amount)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: m.currency, valid: true}, nil
}

// MulRate returns m*r, preserving currency and rounding to nearest with ties
// to even.
//
// MulRate is not currency conversion: the result stays denominated in m's
// existing currency. It is meaningful for fee schedules, financing rates, and
// proportional adjustments, not for converting one currency into another.
func (m Money) MulRate(r Rate) (Money, error) {
	if !m.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.MulScaled(m.amount, r.raw, fixed.RoundHalfEven)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: m.currency, valid: true}, nil
}

// Convert returns m's amount multiplied by rate and re-denominated in to,
// rounding to nearest with ties to even.
//
// Convert is the one sanctioned currency-conversion primitive in num; see
// the package-level Money doc comment for why MulRate deliberately does
// not serve this purpose. Convert performs only the exact arithmetic:
// it has no concept of market data, so callers are responsible for the
// rate's provenance (source currency pair, observation time, source) and
// for deciding whether a stale or missing rate is acceptable.
//
// Convert reports ErrMissingCurrency if m is invalid or to is not a
// structurally valid Currency. Converting to m's own currency is
// permitted and is equivalent to MulRate(rate).
func (m Money) Convert(to Currency, rate Rate) (Money, error) {
	if !m.IsValid() || !to.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.MulScaled(m.amount, rate.raw, fixed.RoundHalfEven)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: to, valid: true}, nil
}

// DivRate returns m/r, preserving currency and rounding to nearest with ties
// to even.
//
// DivRate reports ErrDivideByZero when r is zero.
func (m Money) DivRate(r Rate) (Money, error) {
	if !m.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.DivScaled(m.amount, r.raw, fixed.RoundHalfEven)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: m.currency, valid: true}, nil
}

// Div returns m/o as a dimensionless Rate, rounded to nearest with ties to
// even.
//
// Div reports ErrCurrencyMismatch if m and o are denominated differently, and
// ErrDivideByZero when o is zero.
func (m Money) Div(o Money) (Rate, error) {
	if err := m.sameCurrency(o); err != nil {
		return Rate{}, err
	}
	raw, err := fixed.DivScaled(m.amount, o.amount, fixed.RoundHalfEven)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// DivQuantity returns m/q as a Price, rounded to nearest with ties to
// even (ADR-027) — the inverse of Price.MulQuantity. It is meaningful
// as a weighted-average price: given a position's total cost basis
// (m, in the listing's quote/settlement currency) and its total
// quantity (q), DivQuantity recovers the average price per unit.
//
// DivQuantity reports ErrMissingCurrency if m is invalid,
// ErrDivideByZero when q is zero, and ErrNegative if the true
// quotient would be negative — Price has no signed counterpart, the
// same rule Price.Sub already applies. The underlying division uses
// the same widened 128-bit intermediate (num/internal/fixed
// .DivScaled) that already backs DivRate and Div, so it never
// silently overflows or loses precision the way naive scaled-int64
// division would.
func (m Money) DivQuantity(q Quantity) (Price, error) {
	if !m.IsValid() {
		return Price{}, ErrMissingCurrency
	}
	raw, err := fixed.DivScaled(m.amount, q.raw, fixed.RoundHalfEven)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Price{}, ErrNegative
	}
	return Price{raw: raw}, nil
}

// DivPrice returns m/p as a Quantity, rounded to nearest with ties to
// even (ADR-030) — the inverse of Price.MulQuantity's own dimension,
// arranged the other way around from DivQuantity: given a budget (m,
// for example a risk budget) and a price-per-unit distance (p), how
// many units does that budget correspond to.
//
// DivPrice is pure numeric dimensional arithmetic only: neither Money
// nor Price carries enough information for DivPrice to confirm p is
// actually denominated in m's own currency — Price carries no currency
// at all. Confirming that is the caller's responsibility (see
// risk.FixedFractionSizer, which owns exactly this check before
// calling DivPrice).
//
// DivPrice reports ErrMissingCurrency if m is invalid, ErrDivideByZero
// when p is zero, and ErrNegative if the true quotient would be
// negative — Quantity has no signed counterpart, the same rule
// DivQuantity already applies. The underlying division uses the same
// widened 128-bit intermediate (num/internal/fixed.DivScaled) that
// already backs DivQuantity, DivRate, and Div.
func (m Money) DivPrice(p Price) (Quantity, error) {
	if !m.IsValid() {
		return Quantity{}, ErrMissingCurrency
	}
	raw, err := fixed.DivScaled(m.amount, p.raw, fixed.RoundHalfEven)
	if err != nil {
		return Quantity{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Quantity{}, ErrNegative
	}
	return Quantity{raw: raw}, nil
}
