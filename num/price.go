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
