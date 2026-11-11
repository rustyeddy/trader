package num

import "github.com/rustyeddy/trader/num/internal/fixed"

// Quantity represents an authoritative order or position size.
//
// Quantity is backed by a scaled int64 at the common 1e8 scale (ADR-004).
// Public values are non-negative; direction is represented by order side,
// never by the sign of a quantity. Zero is representable and valid for states
// such as a flat position or a fully closed lot; order construction will
// eventually require a quantity greater than zero, but that is an order-level
// rule, not a representation rule.
//
// Quantity increment, minimum order size, maximum order size, and
// integral-only requirements are instrument or broker rules and are not
// enforced here. The supported whole-unit range tops out at approximately
// 92,233,720,368 — see ADR-004 for why that boundary was accepted rather than
// widened for extreme high-supply-token cases.
//
// The zero value of Quantity is the exact value 0 and is valid.
type Quantity struct {
	raw int64
}

// ParseQuantity parses exact decimal text into a Quantity.
//
// Negative input is rejected, as are malformed text, excess precision, and
// out-of-range values. Nothing is silently rounded or truncated.
func ParseQuantity(s string) (Quantity, error) {
	raw, err := fixed.Parse(s)
	if err != nil {
		return Quantity{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Quantity{}, ErrNegative
	}
	return Quantity{raw: raw}, nil
}

// MustParseQuantity is like ParseQuantity but panics on error.
//
// It is intended for programmer-controlled constants, fixtures, and tests,
// not for parsing external or market input.
func MustParseQuantity(s string) Quantity {
	q, err := ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return q
}

// String returns q formatted as canonical decimal text.
func (q Quantity) String() string {
	return fixed.Format(q.raw)
}

// IsZero reports whether q is exactly zero.
func (q Quantity) IsZero() bool {
	return q.raw == 0
}

// Cmp compares q and o, returning -1, 0, or +1.
func (q Quantity) Cmp(o Quantity) int {
	return fixed.Cmp(q.raw, o.raw)
}

// Equal reports whether q and o are the exact same value.
func (q Quantity) Equal(o Quantity) bool {
	return q.raw == o.raw
}

// Add returns q+o using checked arithmetic.
func (q Quantity) Add(o Quantity) (Quantity, error) {
	raw, err := fixed.Add(q.raw, o.raw)
	if err != nil {
		return Quantity{}, wrapFixedErr(err)
	}
	return Quantity{raw: raw}, nil
}

// Sub returns q-o using checked arithmetic.
//
// Sub reports ErrNegative if the true difference would be negative: public
// Quantity values are never negative. Reducing a position below zero is a
// domain error to be reported at the position/order boundary, not a case for
// this type to represent.
func (q Quantity) Sub(o Quantity) (Quantity, error) {
	raw, err := fixed.Sub(q.raw, o.raw)
	if err != nil {
		return Quantity{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Quantity{}, ErrNegative
	}
	return Quantity{raw: raw}, nil
}

// MulRate returns q*r, rounded to nearest with ties to even.
//
// MulRate reports ErrNegative if r is negative enough to make the result
// negative.
func (q Quantity) MulRate(r Rate) (Quantity, error) {
	raw, err := fixed.MulScaled(q.raw, r.raw, fixed.RoundHalfEven)
	if err != nil {
		return Quantity{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Quantity{}, ErrNegative
	}
	return Quantity{raw: raw}, nil
}

// Div returns q/o as a dimensionless Rate, rounded to nearest with ties to
// even.
//
// Div reports ErrDivideByZero when o is zero.
func (q Quantity) Div(o Quantity) (Rate, error) {
	raw, err := fixed.DivScaled(q.raw, o.raw, fixed.RoundHalfEven)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// DivisibleBy reports whether q is an exact integer multiple of step, the
// same exact-remainder rule ADR-004 prescribes for price increments applied
// to quantity increments. The comparison is exact scaled-integer arithmetic;
// no rounding is involved.
//
// DivisibleBy reports ErrDivideByZero when step is zero.
func (q Quantity) DivisibleBy(step Quantity) (bool, error) {
	if step.raw == 0 {
		return false, ErrDivideByZero
	}
	return q.raw%step.raw == 0, nil
}

// RoundDown returns the largest multiple of step that is <= q:
// floor(q/step) x step (ADR-030). Because q.raw and step.raw already
// share the same fixed-point scale, their plain integer quotient is
// already the exact, dimensionless multiple count — no rescaling or
// rounding-mode decision is needed the way num/internal/fixed.DivScaled
// requires for a ratio expressed in a different type's scale (compare
// Money.DivQuantity/DivPrice). Go's integer division truncates toward
// zero, which equals floor for the non-negative values Quantity always
// holds.
//
// RoundDown reports ErrDivideByZero when step is zero, matching
// DivisibleBy's own existing convention exactly. The result may
// legitimately be zero, when q is smaller than one whole step;
// RoundDown itself does not decide whether that is acceptable — that
// is entirely a caller concern.
func (q Quantity) RoundDown(step Quantity) (Quantity, error) {
	if step.raw == 0 {
		return Quantity{}, ErrDivideByZero
	}
	count := q.raw / step.raw
	return Quantity{raw: count * step.raw}, nil
}
