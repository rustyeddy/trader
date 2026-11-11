package num

import (
	"cmp"

	"github.com/rustyeddy/trader/num/internal/fixed"
)

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
		return Price{}, err
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

// Float64 returns p as a float64, for crossing into Trader's analytical domain
// (indicators and other float64 calculations). It is the one sanctioned
// exact-to-float64 conversion (ADR-045): a direct numeric conversion, never a
// String/ParseFloat round-trip. It is lossy, because float64 cannot represent
// every Price exactly. There is no Price-from-float64 constructor: an
// analytical result becomes authoritative again only by being quantized to the
// tick size and parsed through ParsePrice (ADR-004).
func (p Price) Float64() float64 {
	scale := fixed.Scale()
	// Convert the whole and fractional parts separately: raw can exceed
	// float64's 53-bit exact-integer range, so converting it whole and then
	// dividing would round twice. Each part is exact, so this rounds once, at
	// the final addition.
	whole := p.raw / scale
	frac := p.raw % scale
	return float64(whole) + float64(frac)/float64(scale)
}

// IsZero reports whether p is exactly zero.
func (p Price) IsZero() bool {
	return p.raw == 0
}

// Cmp compares p and o, returning -1, 0, or +1.
func (p Price) Cmp(o Price) int {
	return cmp.Compare(p.raw, o.raw)
}

// Equal reports whether p and o are the exact same value.
func (p Price) Equal(o Price) bool {
	return p.raw == o.raw
}

// Add returns p+o using checked arithmetic.
func (p Price) Add(o Price) (Price, error) {
	raw, err := fixed.Add(p.raw, o.raw)
	if err != nil {
		return Price{}, err
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
		return Price{}, err
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
		return Price{}, err
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
		return Rate{}, err
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
		return Money{}, err
	}
	return Money{amount: raw, currency: currency}, nil
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

// RoundDown returns the largest multiple of tick that is <= p.
//
// Prices derived by arithmetic (an ATR multiple, a fraction of a high-water
// mark) must be rounded to the instrument's tick size explicitly before an
// order is built; num never rounds silently, and RoundDown is the named exact
// operation for it. It reports ErrDivideByZero when tick is zero.
func (p Price) RoundDown(tick Price) (Price, error) {
	if tick.raw == 0 {
		return Price{}, ErrDivideByZero
	}
	count := p.raw / tick.raw
	return Price{raw: count * tick.raw}, nil
}

// RoundUp returns the smallest multiple of tick that is >= p; a p already on a
// tick rounds to itself.
//
// It rounds up so a protective Buy stop, which guards a short from above, is
// never tightened by rounding. RoundUp reports ErrDivideByZero when tick is
// zero and ErrOverflow when the next multiple does not fit.
//
// It adds one tick to RoundDown's result with checked arithmetic rather than
// computing (count+1)*tick, which could wrap negative near the top of the
// range and break Price's non-negative invariant.
func (p Price) RoundUp(tick Price) (Price, error) {
	if tick.raw == 0 {
		return Price{}, ErrDivideByZero
	}
	down := (p.raw / tick.raw) * tick.raw
	if down == p.raw {
		return Price{raw: down}, nil
	}
	raw, err := fixed.Add(down, tick.raw)
	if err != nil {
		return Price{}, err
	}
	return Price{raw: raw}, nil
}
