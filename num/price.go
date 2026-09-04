package num

import "github.com/rustyeddy/trader/num/internal/fixed"

// Price represents an authoritative instrument price.
//
// Price is backed by a scaled int64 at the common 1e8 scale (ADR-004). Normal
// domain values are non-negative; constructors and parsers reject negative
// input. Zero is representable — whether zero is a valid price is decided by
// the surrounding context, not by this type.
//
// Tick size, permitted increments, and display precision are instrument
// rules, not properties of Price. Price increment validation is exact when
// price and tick size share this scale: priceRaw % tickRaw == 0.
//
// The zero value of Price is the exact value 0 and is valid.
type Price struct {
	raw int64
}

// ParsePrice parses exact decimal text into a Price.
//
// Negative input is rejected, as are malformed text, excess precision, and
// out-of-range values. Nothing is silently rounded.
func ParsePrice(s string) (Price, error) {
	raw, err := fixed.Parse(s)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Price{}, ErrNegative
	}
	return Price{raw: raw}, nil
}

// MustParsePrice is like ParsePrice but panics on error.
//
// It is intended for programmer-controlled constants, fixtures, and tests,
// not for parsing external or market input.
func MustParsePrice(s string) Price {
	p, err := ParsePrice(s)
	if err != nil {
		panic(err)
	}
	return p
}

// String returns p formatted as canonical decimal text.
func (p Price) String() string {
	return fixed.Format(p.raw)
}

// Float64 returns p as a float64, for crossing into Trader's analytical
// domain — indicators and other float64-based calculations (ADR-045,
// docs/arch/adr-045-analytical-float64-conversion-boundary.org).
//
// This is a direct numeric conversion (raw scaled int64 divided by the
// common scale), never a serialize/reparse round-trip through String
// and strconv.ParseFloat: the two are not equivalent in general, and
// ADR-045 explicitly rejects the round-trip form as an implementation
// smell. float64 cannot represent every value Price can exactly (IEEE
// 754 double precision gives roughly 15-17 significant decimal digits,
// against Price's exact 1e8 scale), so this conversion is lossy in the
// same way any exact-to-float64 conversion is; that loss is acceptable
// for analytical use and is exactly why a value must never re-enter
// Trader's exact domain by parsing a float64 back into a Price —
// construct a new Price from a validated decimal/order-derived source
// instead.
func (p Price) Float64() float64 {
	return float64(p.raw) / float64(fixed.Scale())
}

// IsZero reports whether p is exactly zero.
func (p Price) IsZero() bool {
	return p.raw == 0
}

// Cmp compares p and o, returning -1, 0, or +1.
func (p Price) Cmp(o Price) int {
	return fixed.Cmp(p.raw, o.raw)
}

// Equal reports whether p and o are the exact same value.
func (p Price) Equal(o Price) bool {
	return p.raw == o.raw
}

// Add returns p+o using checked arithmetic.
func (p Price) Add(o Price) (Price, error) {
	raw, err := fixed.Add(p.raw, o.raw)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	return Price{raw: raw}, nil
}

// Sub returns p-o using checked arithmetic.
//
// Sub reports ErrNegative if the true difference would be negative: Price
// has no signed counterpart. Callers that need a signed difference between
// two prices should use a dedicated signed type rather than relax this one.
func (p Price) Sub(o Price) (Price, error) {
	raw, err := fixed.Sub(p.raw, o.raw)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Price{}, ErrNegative
	}
	return Price{raw: raw}, nil
}

// MulRate returns p*r, rounded to nearest with ties to even.
//
// MulRate reports ErrNegative if r is negative enough to make the result
// negative.
func (p Price) MulRate(r Rate) (Price, error) {
	raw, err := fixed.MulScaled(p.raw, r.raw, fixed.RoundHalfEven)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Price{}, ErrNegative
	}
	return Price{raw: raw}, nil
}

// Div returns p/o as a dimensionless Rate, rounded to nearest with ties to
// even.
//
// Div reports ErrDivideByZero when o is zero.
func (p Price) Div(o Price) (Rate, error) {
	raw, err := fixed.DivScaled(p.raw, o.raw, fixed.RoundHalfEven)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// MulQuantity returns the notional value p*q, denominated in currency,
// rounded to nearest with ties to even (ADR-025). Currency is required
// explicitly: neither Price nor Quantity carries a currency of its own,
// so the caller states which currency the resulting Money is
// denominated in — typically the instrument's quote/settlement
// currency, which is not necessarily an account's home currency;
// converting to a different currency is a separate, later step (see
// Money.Convert).
//
// The underlying multiplication uses a widened 128-bit intermediate
// (num/internal/fixed.MulScaled), so a realistic notional value — for
// example a large FX or equity block — never silently overflows int64
// the way a naive scaled-int64 product would; MulQuantity reports
// ErrOverflow instead. Because Price and Quantity are both
// non-negative by construction, the result is always non-negative.
func (p Price) MulQuantity(q Quantity, currency Currency) (Money, error) {
	if !currency.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.MulScaled(p.raw, q.raw, fixed.RoundHalfEven)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: currency, valid: true}, nil
}

// DivisibleBy reports whether p is an exact integer multiple of step, per
// ADR-004's price-increment rule: priceRaw % tickRaw == 0. The comparison is
// exact scaled-integer arithmetic; no rounding is involved.
//
// DivisibleBy reports ErrDivideByZero when step is zero.
func (p Price) DivisibleBy(step Price) (bool, error) {
	if step.raw == 0 {
		return false, ErrDivideByZero
	}
	return p.raw%step.raw == 0, nil
}
