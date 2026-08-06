package num

import "github.com/rustyeddy/trader/num/internal/fixed"

// Rate is Trader's foundational exact dimensionless numeric type.
//
// Rate is signed and backed by a scaled int64 at the common 1e8 scale (ADR-004).
// It represents percentages, financing rates, conversion rates, ratios,
// bounded oscillators, and any other dimensionless quantity.  Zero validity
// and semantic bounds depend on use; Rate itself imposes none.  Domain
// packages that need direction, bounds, or a specific interpretation should
// introduce a semantic wrapper around Rate rather than weaken this type — see
// the package doc comment for why Percent and Ratio are deferred rather than
// added here.
//
// The zero value of Rate is the exact value 0 and is valid.
type Rate struct {
	raw int64
}

// ParseRate parses exact decimal text into a Rate.
//
// See the package doc comment and ADR-004 for the accepted grammar: an
// optional sign, decimal digits, and up to eight fraction digits.  Malformed
// input, excess precision, and out-of-range values are rejected; nothing is
// silently rounded.
func ParseRate(s string) (Rate, error) {
	raw, err := fixed.Parse(s)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// MustParseRate is like ParseRate but panics on error.
//
// It is intended for programmer-controlled constants, fixtures, and tests,
// not for parsing external or market input.
func MustParseRate(s string) Rate {
	r, err := ParseRate(s)
	if err != nil {
		panic(err)
	}
	return r
}

// String returns r formatted as canonical decimal text.
func (r Rate) String() string {
	return fixed.Format(r.raw)
}

// IsZero reports whether r is exactly zero.
func (r Rate) IsZero() bool {
	return r.raw == 0
}

// Sign returns -1, 0, or +1 according to the sign of r.
func (r Rate) Sign() int {
	return fixed.Sign(r.raw)
}

// Cmp compares r and o, returning -1, 0, or +1.
func (r Rate) Cmp(o Rate) int {
	return fixed.Cmp(r.raw, o.raw)
}

// Equal reports whether r and o are the exact same value.
func (r Rate) Equal(o Rate) bool {
	return r.raw == o.raw
}

// Add returns r+o using checked arithmetic.
func (r Rate) Add(o Rate) (Rate, error) {
	raw, err := fixed.Add(r.raw, o.raw)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// Sub returns r-o using checked arithmetic.
func (r Rate) Sub(o Rate) (Rate, error) {
	raw, err := fixed.Sub(r.raw, o.raw)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// Neg returns -r.
//
// Neg rejects the one Rate whose negation is not representable: the value
// backed by math.MinInt64.
func (r Rate) Neg() (Rate, error) {
	raw, err := fixed.Neg(r.raw)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// Abs returns the absolute value of r.
//
// Abs rejects the one Rate whose absolute value is not representable: the
// value backed by math.MinInt64.
func (r Rate) Abs() (Rate, error) {
	raw, err := fixed.Abs(r.raw)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// MulRate returns r*o, rounded to nearest with ties to even.
//
// The product is computed with a widened intermediate before the common scale
// is restored, per ADR-004; it is never computed as raw*raw.
func (r Rate) MulRate(o Rate) (Rate, error) {
	raw, err := fixed.MulScaled(r.raw, o.raw, fixed.RoundHalfEven)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// DivRate returns r/o, rounded to nearest with ties to even.
//
// DivRate reports ErrDivideByZero when o is zero.
func (r Rate) DivRate(o Rate) (Rate, error) {
	raw, err := fixed.DivScaled(r.raw, o.raw, fixed.RoundHalfEven)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}
